// SPDX-License-Identifier: Apache-2.0

package settingsimage_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ConfigButler/settings-image/pkg/settingsimage"
)

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

type renderRefusal struct {
	name     string
	folder   fstest.MapFS
	wantPath string
	wantIn   string
}

func TestRenderRefusals(t *testing.T) {
	for _, tt := range slices.Concat(renderRefusals(), resourceRefusals(), configMapRefusals()) {
		t.Run(tt.name, func(t *testing.T) {
			_, err := settingsimage.Render(tt.folder, settingsimage.RenderOptions{})
			assertRefused(t, err, tt.wantPath, tt.wantIn)
		})
	}
}

func renderRefusals() []renderRefusal {
	return []renderRefusal{
		{name: "empty folder", folder: fstest.MapFS{}, wantPath: ".", wantIn: "empty"},
		{
			name:     "only .git",
			folder:   fstest.MapFS{".git/config": file("x")},
			wantPath: ".",
			wantIn:   "empty",
		},
		{
			name:     "only skipped files",
			folder:   fstest.MapFS{"README.md": file("x"), ".gittargetignore": file("")},
			wantPath: ".",
			wantIn:   "empty",
		},
		{
			name:     "a file at the top",
			folder:   fstest.MapFS{"raw/a.txt": file("x"), "app.yaml": file(configMap("cm", ""))},
			wantPath: "app.yaml",
			wantIn:   "not under resources/ or raw/",
		},
		{
			name:     "another folder at the top",
			folder:   fstest.MapFS{"objects/a.txt": file("x")},
			wantPath: "objects/a.txt",
			wantIn:   "not under resources/ or raw/",
		},
		{
			name:     "a file named resources",
			folder:   fstest.MapFS{"resources": file("x")},
			wantPath: "resources",
			wantIn:   "not under resources/ or raw/",
		},
		{
			name: "symlink",
			folder: fstest.MapFS{
				"raw/a.txt": file("x"),
				"raw/link":  {Data: []byte("a.txt"), Mode: os.ModeSymlink},
			},
			wantPath: "raw/link",
			wantIn:   "symlink",
		},
		{
			name:     "device",
			folder:   fstest.MapFS{"raw/dev": {Mode: os.ModeDevice}},
			wantPath: "raw/dev",
			wantIn:   "not a regular file",
		},
		{
			name: "too large",
			folder: fstest.MapFS{
				"raw/a": {Data: make([]byte, 6<<20)},
				"raw/b": {Data: make([]byte, 6<<20)},
			},
			wantPath: ".",
			wantIn:   "bytes in total",
		},
		{
			name:     "plaintext Secret",
			folder:   fstest.MapFS{"resources/s.yaml": file(secret("s", "stringData:\n  password: plain\n"))},
			wantPath: "resources/s.yaml",
			wantIn:   "Secret s is not SOPS-encrypted",
		},
		{
			name: "Secret as second document",
			folder: fstest.MapFS{
				"resources/s.yaml": file(configMap("cm", "") + "---\n" + secret("s", "")),
			},
			wantPath: "resources/s.yaml",
			wantIn:   "Secret s is not SOPS-encrypted",
		},
	}
}

// resourceRefusals are the rules of format-v2.md §4: what a file under resources/ must be.
func resourceRefusals() []renderRefusal {
	one := func(name, content, wantIn string) renderRefusal {
		return renderRefusal{
			name:     name,
			folder:   fstest.MapFS{"resources/r.yaml": file(content)},
			wantPath: "resources/r.yaml",
			wantIn:   wantIn,
		}
	}
	order := "apiVersion: shop.configbutler.ai/v1\nkind: Order\nmetadata:\n  name: o\n"
	return []renderRefusal{
		{
			name:     "not YAML or JSON",
			folder:   fstest.MapFS{"resources/pricing.toml": file("a = 1\n")},
			wantPath: "resources/pricing.toml",
			wantIn:   "put any other file in raw/",
		},
		one("unparseable", "a: [unclosed\n", "does not parse"),
		one("top-level list", "- a\n- b\n", "must be a mapping"),
		one("scalar", "42\n", "must be a mapping"),
		one("no apiVersion", "kind: Order\nmetadata:\n  name: o\n", "without apiVersion"),
		one("no kind", "apiVersion: v1\nmetadata:\n  name: o\n", "without kind"),
		one("no metadata", "apiVersion: v1\nkind: Order\n", "without metadata"),
		one("no name", "apiVersion: v1\nkind: Order\nmetadata:\n  namespace: x\n", "without metadata.name"),
		one("name not a string", "apiVersion: v1\nkind: Order\nmetadata:\n  name: 3\n", "without metadata.name"),
		one("namespace not a string", order+"  namespace: [a]\n", "namespace is not a string"),
		one("duplicate kind key", "apiVersion: v1\nkind: Secret\nkind: ConfigMap\nmetadata:\n  name: x\n",
			`the key "kind" appears twice`),
		one("duplicate nested key", configMap("cm", "data:\n  a: x\n  a: y\n"), `the key "a" appears twice`),
		one("duplicate key in JSON", `{"apiVersion":"v1","kind":"ConfigMap","kind":"Secret"}`, "appears twice"),
		one("an anchor", order+"spec: &s\n  a: b\n", "anchors and aliases"),
		one("a merge key", order+"spec:\n  <<: {a: b}\n", "merge keys"),
		one("a v1 List", "apiVersion: v1\nkind: List\nmetadata:\n  name: l\nitems:\n- "+
			strings.ReplaceAll(secret("s", ""), "\n", "\n  "), "is a list"),
		one("a SecretList", "apiVersion: v1\nkind: SecretList\nmetadata:\n  name: l\n", "is a list"),
		one("the same resource twice", order+"---\n"+order, "declared twice"),
		one("the same resource at two versions", order+"---\n"+
			strings.Replace(order, "/v1", "/v2", 1), "declared twice"),
		{
			name: "the same resource in two files",
			folder: fstest.MapFS{
				"resources/a.yaml":     file(order),
				"resources/sub/b.yaml": file(order),
			},
			wantPath: "resources/sub/b.yaml",
			wantIn:   "Order o is declared twice, here and in resources/a.yaml",
		},
	}
}

// configMapRefusals are the refusals of the configmaps/ files (format-v2.md §5).
func configMapRefusals() []renderRefusal {
	one := func(name, content, wantIn string) renderRefusal {
		return renderRefusal{
			name:     name,
			folder:   fstest.MapFS{"resources/c.yaml": file(content)},
			wantPath: "resources/c.yaml",
			wantIn:   wantIn,
		}
	}
	return []renderRefusal{
		one("unsafe configmap name", configMap("a:b", "data:\n  k: v\n"), "not safe"),
		one("unsafe key", configMap("cm", "data:\n  a/b: v\n"), "not safe"),
		one("non-string data value", configMap("cm", "data:\n  k: 3\n"), "not a string"),
		one("bad base64", configMap("cm", "binaryData:\n  k: '!!'\n"), "not base64"),
		one("data not a mapping", configMap("cm", "data: [a]\n"), "data is not a mapping"),
		one("key in data and binaryData", configMap("cm", "data:\n  k: v\nbinaryData:\n  k: dg==\n"),
			"written twice"),
		one("bad annotation value", configMap("cm", "")+"  annotations:\n    "+
			settingsimage.FilesAnnotation+": \"no\"\n", `must be "true" or "false"`),
		{
			name: "two configmaps of one name in two namespaces",
			folder: fstest.MapFS{
				"resources/a.yaml": file(configMap("cm", "data:\n  k: v\n")),
				"resources/b.yaml": file(strings.Replace(configMap("cm", "data:\n  other: w\n"),
					"name: cm", "name: cm\n  namespace: elsewhere", 1)),
			},
			wantPath: "resources/b.yaml",
			wantIn:   "already becomes configmaps/cm/",
		},
	}
}

func configMap(name, body string) string {
	return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + name + "\n" + body
}

func secret(name, body string) string {
	return "apiVersion: v1\nkind: Secret\nmetadata:\n  name: " + name + "\n" + body
}

func assertRefused(t *testing.T, err error, wantPath, wantIn string) {
	t.Helper()
	var refusal *settingsimage.RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v, want a *RefusalError", err)
	}
	if refusal.Path != wantPath || !strings.Contains(refusal.Reason, wantIn) {
		t.Errorf("got refusal %q, want path %q and a reason containing %q", refusal, wantPath, wantIn)
	}
}

func TestRenderTree(t *testing.T) {
	keepAsResource := strings.Replace(configMap("kept", "data:\n  k: v\n"), "name: kept",
		"name: kept\n  annotations:\n    "+settingsimage.FilesAnnotation+": \"false\"", 1)
	folder := fstest.MapFS{
		"resources/cm.yaml": file(configMap("app", "data:\n  a.conf: x=1\nbinaryData:\n  logo.bin: AAEC\n") +
			"---\n" + configMap("other", "data:\n  b: two\n") + "---\n" + keepAsResource),
		"resources/other-cm.yaml": file(
			"apiVersion: example.com/v1\nkind: ConfigMap\nmetadata:\n  name: not-core\ndata:\n  k: v\n",
		),
		"resources/order.JSON": file("{\n\t\"apiVersion\": \"shop.configbutler.ai/v1\",\n\t\"kind\": \"Order\"," +
			"\n\t\"metadata\": {\"name\": \"o\", \"namespace\": \"shop\"}\n}\n"),
		"resources/same-name-elsewhere.yaml": file(
			"apiVersion: shop.configbutler.ai/v1\nkind: Order\nmetadata:\n  name: o\n",
		),
		"resources/empty.yaml":  file("# only a comment\n---\n"),
		"raw/list.yaml":         file("- a\n- b\n"),
		"raw/secret.yaml":       file(secret("s", "stringData:\n  password: plain\n")),
		"raw/notes.txt":         file("not: [parsed\n"),
		"raw/dup.yaml":          file("a: 1\na: 2\n"),
		".git/HEAD":             file("ref: refs/heads/main\n"),
		"raw/sub/.git":          file("gitdir: ../.git/modules/sub\n"),
		"resources/.git/x.toml": file("not checked"),
	}
	rendered, err := settingsimage.Render(folder, settingsimage.RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tree := rendered.Tree
	assertTreePaths(t, tree, []string{
		"configmaps/app/a.conf", "configmaps/app/logo.bin", "configmaps/other/b",
		"raw/dup.yaml", "raw/list.yaml", "raw/notes.txt", "raw/secret.yaml",
		"resources/cm.yaml", "resources/empty.yaml", "resources/order.JSON", "resources/other-cm.yaml",
		"resources/same-name-elsewhere.yaml",
	})
	assertEqual(t, "data value", string(tree["configmaps/app/a.conf"]), "x=1")
	assertEqual(t, "binaryData value", string(tree["configmaps/app/logo.bin"]), "\x00\x01\x02")
	assertEqual(t, "raw bytes", string(tree["raw/notes.txt"]), "not: [parsed\n")
}

// knownTopLevelFiles is format-v2.md §2's closed list, with a name for each pattern.
func knownTopLevelFiles() []string {
	return []string{
		".gittargetignore", ".sops.yaml", ".gitignore", ".gitattributes", ".gitkeep", ".keep",
		"README.md", "CHANGES.markdown", ".md",
		"LICENSE", "LICENSE.txt", "LICENCE", "LICENCE.txt", "COPYING", "COPYING.txt", "NOTICE", "NOTICE.txt",
	}
}

// A known file at the top is skipped and named, and changes nothing about the image.
func TestRenderSkipsKnownTopLevelFiles(t *testing.T) {
	base := fstest.MapFS{"resources/configmap.yaml": file(configMapYAML)}
	want := digestOf(t, base)
	for _, name := range knownTopLevelFiles() {
		t.Run(name, func(t *testing.T) {
			folder := maps.Clone(base)
			folder[name] = file("kind: Secret\nnot: [yaml\n") // never read
			rendered, err := settingsimage.Render(folder, settingsimage.RenderOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(rendered.Skipped, []string{name}) {
				t.Errorf("skipped %q, want [%q]", rendered.Skipped, name)
			}
			assertEqual(t, "digest", digestOf(t, folder), want)
		})
	}

	rendered, err := settingsimage.Render(fstest.MapFS{
		"raw/a.txt": file("a"), "README.md": file("x"), ".gittargetignore": file(""), "LICENSE": file("x"),
	}, settingsimage.RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".gittargetignore", "LICENSE", "README.md"}; !slices.Equal(rendered.Skipped, want) {
		t.Errorf("skipped %q, want %q (sorted)", rendered.Skipped, want)
	}
}

// Anything at the top that is not on the closed list is still refused: a resource one level too
// high must stop the build, not publish an image without it.
func TestRenderRefusesUnknownTopLevelFiles(t *testing.T) {
	for _, p := range []string{
		"menu.yaml", "README.txt", "README.MD", "readme.rst", "LICENSE.md.bak", "license", ".env",
		"docs/README.md", "resource/menu.yaml", ".github/CODEOWNERS", "raw.txt",
	} {
		t.Run(p, func(t *testing.T) {
			folder := fstest.MapFS{"resources/configmap.yaml": file(configMapYAML), p: file("x")}
			_, err := settingsimage.Render(folder, settingsimage.RenderOptions{})
			assertRefused(t, err, p, "not under resources/ or raw/")
		})
	}
}

func digestOf(t *testing.T, folder fstest.MapFS) string {
	t.Helper()
	rendered, err := settingsimage.Render(folder, settingsimage.RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	img, err := settingsimage.Build(t.Context(), rendered.Tree, settingsimage.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return img.Manifest.Digest.String()
}

// Only raw/: no resource, no configmaps/.
func TestRenderRawOnly(t *testing.T) {
	rendered, err := settingsimage.Render(
		fstest.MapFS{"raw/settings.toml": file("a = 1\n")},
		settingsimage.RenderOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	tree := rendered.Tree
	assertTreePaths(t, tree, []string{"raw/settings.toml"})
}

// One folder, as a directory and as a messy archive, renders to one tree and one digest.
func TestRenderArchiveMatchesDirectory(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"resources/configmap.yaml": configMapYAML,
		"raw/sub/dir/notes.txt":    "plain text\n",
	}
	for p, s := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fromDirRendered, err := settingsimage.Render(os.DirFS(dir), settingsimage.RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fromDir := fromDirRendered.Tree

	archive := tarball(t, true, []tarEntry{
		{name: "./outside.yaml", body: "kind: Secret\n"}, // outside the root: ignored
		{name: "./configs/shop/raw/sub/dir/notes.txt", body: "plain text\n", mode: 0o755},
		{name: "./configs/shop/", dir: true},
		{name: "./configs/shop/.git/config", body: "x"},
		{name: "./configs/shop/README.md", body: "skipped"},
		{name: "./README.md", body: "outside the root"},
		{name: "configs/shop/resources/configmap.yaml", body: configMapYAML, mode: 0o600},
		{name: "./configs/shopping/x.txt", body: "a sibling with a common prefix"},
	})
	fromArchiveRendered, err := settingsimage.RenderArchive(
		bytes.NewReader(archive),
		"configs/shop",
		settingsimage.RenderOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	fromArchive := fromArchiveRendered.Tree
	if !slices.Equal(fromArchiveRendered.Skipped, []string{"README.md"}) {
		t.Errorf("archive skipped %q, want [README.md]", fromArchiveRendered.Skipped)
	}
	if !maps.EqualFunc(fromDir, fromArchive, bytes.Equal) {
		t.Fatalf("trees differ:\n dir     %q\n archive %q", keys(fromDir), keys(fromArchive))
	}
	a, err := settingsimage.Build(t.Context(), fromDir, settingsimage.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := settingsimage.Build(t.Context(), fromArchive, settingsimage.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "digest", a.Manifest.Digest, b.Manifest.Digest)
}

func TestRenderArchiveRefusals(t *testing.T) {
	tests := []struct {
		name     string
		entries  []tarEntry
		wantPath string
		wantIn   string
	}{
		{name: "dotdot", entries: []tarEntry{{name: "a/../../etc/passwd", body: "x"}}, wantPath: "a/../../etc/passwd",
			wantIn: "escapes"},
		{name: "absolute", entries: []tarEntry{{name: "/etc/passwd", body: "x"}}, wantPath: "/etc/passwd",
			wantIn: "absolute"},
		{name: "symlink", entries: []tarEntry{{name: "l", link: tar.TypeSymlink}}, wantPath: "l", wantIn: "link"},
		{name: "hard link", entries: []tarEntry{{name: "l", link: tar.TypeLink}}, wantPath: "l", wantIn: "link"},
		{name: "fifo", entries: []tarEntry{{name: "f", link: tar.TypeFifo}}, wantPath: "f", wantIn: "regular"},
		{
			name:     "duplicate",
			entries:  []tarEntry{{name: "raw/a", body: "1"}, {name: "./raw/a", body: "2"}},
			wantPath: "raw/a",
			wantIn:   "twice",
		},
		{
			name:     "file and directory",
			entries:  []tarEntry{{name: "raw/a", body: "1"}, {name: "raw/a/b", body: "2"}},
			wantPath: "raw/a",
			wantIn:   "both",
		},
		{name: "empty", entries: []tarEntry{{name: "d/", dir: true}}, wantPath: ".", wantIn: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := settingsimage.RenderArchive(bytes.NewReader(tarball(t, false, tt.entries)), "",
				settingsimage.RenderOptions{})
			assertRefused(t, err, tt.wantPath, tt.wantIn)
		})
	}
}

type tarEntry struct {
	name string
	body string
	mode int64
	dir  bool
	link byte
}

func tarball(t *testing.T, gz bool, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for i, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, ModTime: time.Unix(int64(1_700_000_000+i*1000), 0)}
		switch {
		case e.dir:
			hdr.Typeflag, hdr.Mode = tar.TypeDir, 0o700
		case e.link != 0:
			hdr.Typeflag, hdr.Linkname = e.link, "target"
		default:
			hdr.Typeflag, hdr.Size = tar.TypeReg, int64(len(e.body))
		}
		if e.mode != 0 {
			hdr.Mode = e.mode
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if !gz {
		return buf.Bytes()
	}
	var zbuf bytes.Buffer
	zw := gzip.NewWriter(&zbuf)
	if _, err := zw.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zbuf.Bytes()
}

func keys(tree settingsimage.Tree) []string {
	out := make([]string, 0, len(tree))
	for p := range tree {
		out = append(out, p)
	}
	return out
}
