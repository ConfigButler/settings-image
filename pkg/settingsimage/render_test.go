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
	for _, tt := range append(renderRefusals(), configMapRefusals()...) {
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
			name:     "symlink",
			folder:   fstest.MapFS{"a.txt": file("x"), "link": {Data: []byte("a.txt"), Mode: os.ModeSymlink}},
			wantPath: "link",
			wantIn:   "symlink",
		},
		{
			name:     "device",
			folder:   fstest.MapFS{"dev": {Mode: os.ModeDevice}},
			wantPath: "dev",
			wantIn:   "not a regular file",
		},
		{
			name:     "too large",
			folder:   fstest.MapFS{"a": {Data: make([]byte, 6<<20)}, "b": {Data: make([]byte, 6<<20)}},
			wantPath: ".",
			wantIn:   "bytes in total",
		},
		{
			name:     "unparseable yaml",
			folder:   fstest.MapFS{"bad.yaml": file("a: [unclosed\n")},
			wantPath: "bad.yaml",
			wantIn:   "does not parse",
		},
		{
			name:     "top-level list",
			folder:   fstest.MapFS{"list.yml": file("- a\n- b\n")},
			wantPath: "list.yml",
			wantIn:   "does not parse",
		},
		{
			name:     "json scalar, uppercase extension",
			folder:   fstest.MapFS{"x.JSON": file("42")},
			wantPath: "x.JSON",
			wantIn:   "does not parse",
		},
		{
			name:     "secret",
			folder:   fstest.MapFS{"s.yaml": file("apiVersion: v1\nkind: Secret\nmetadata:\n  name: s\n")},
			wantPath: "s.yaml",
			wantIn:   "Secret",
		},
		{
			name: "secret as second document",
			folder: fstest.MapFS{
				"s.yaml": file("kind: Thing\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: s\n"),
			},
			wantPath: "s.yaml",
			wantIn:   "Secret",
		},
	}
}

// configMapRefusals are the refusals of the files/ view (format-v1.md §3).
func configMapRefusals() []renderRefusal {
	return []renderRefusal{
		{
			name:     "unsafe configmap name",
			folder:   fstest.MapFS{"c.yaml": file(configMap("..", "data:\n  k: v\n"))},
			wantPath: "c.yaml",
			wantIn:   "not safe",
		},
		{
			name:     "configmap without a name",
			folder:   fstest.MapFS{"c.yaml": file("apiVersion: v1\nkind: ConfigMap\ndata:\n  k: v\n")},
			wantPath: "c.yaml",
			wantIn:   "not safe",
		},
		{
			name:     "unsafe key",
			folder:   fstest.MapFS{"c.yaml": file(configMap("cm", "data:\n  a/b: v\n"))},
			wantPath: "c.yaml",
			wantIn:   "not safe",
		},
		{
			name:     "non-string data value",
			folder:   fstest.MapFS{"c.yaml": file(configMap("cm", "data:\n  k: 3\n"))},
			wantPath: "c.yaml",
			wantIn:   "not a string",
		},
		{
			name:     "bad base64",
			folder:   fstest.MapFS{"c.yaml": file(configMap("cm", "binaryData:\n  k: '!!'\n"))},
			wantPath: "c.yaml",
			wantIn:   "not base64",
		},
		{
			name:     "key in data and binaryData",
			folder:   fstest.MapFS{"c.yaml": file(configMap("cm", "data:\n  k: v\nbinaryData:\n  k: dg==\n"))},
			wantPath: "c.yaml",
			wantIn:   "written twice",
		},
		{
			name: "two configmaps of one name",
			folder: fstest.MapFS{
				"a.yaml": file(configMap("cm", "data:\n  k: v\n")),
				"b.yaml": file(configMap("cm", "data:\n  k: w\n")),
			},
			wantPath: "b.yaml",
			wantIn:   "written twice",
		},
	}
}

func configMap(name, body string) string {
	return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + name + "\n" + body
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

func TestRenderViews(t *testing.T) {
	folder := fstest.MapFS{
		"cm.yaml": file(configMap("app", "data:\n  a.conf: x=1\nbinaryData:\n  logo.bin: AAEC\n") +
			"---\n" + configMap("other", "data:\n  b: two\n")),
		"list.yaml": file(
			"apiVersion: v1\nkind: List\nitems:\n- " + strings.ReplaceAll(configMap("in-list", ""), "\n", "\n  "),
		),
		"other-cm.yaml": file(
			"apiVersion: example.com/v1\nkind: ConfigMap\nmetadata:\n  name: not-core\ndata:\n  k: v\n",
		),
		"plain.json": file(`{"metadata": "not krm", "kind": 3}`),
		"empty.yaml": file("# only a comment\n---\n"),
		"notes.txt":  file("not: [parsed\n"),
		".git/HEAD":  file("ref: refs/heads/main\n"),
		"sub/.git":   file("gitdir: ../.git/modules/sub\n"),
	}
	tree, err := settingsimage.Render(folder, settingsimage.RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertTreePaths(t, tree, []string{
		"files/app/a.conf", "files/app/logo.bin", "files/other/b",
		"objects/cm.yaml", "objects/empty.yaml", "objects/list.yaml", "objects/notes.txt",
		"objects/other-cm.yaml", "objects/plain.json",
	})
	assertEqual(t, "data value", string(tree["files/app/a.conf"]), "x=1")
	assertEqual(t, "binaryData value", string(tree["files/app/logo.bin"]), "\x00\x01\x02")
	assertEqual(t, "object bytes", string(tree["objects/notes.txt"]), "not: [parsed\n")

	noFiles, err := settingsimage.Render(folder, settingsimage.RenderOptions{NoFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	for p := range noFiles {
		if strings.HasPrefix(p, "files/") {
			t.Errorf("NoFiles: tree has %s", p)
		}
	}
}

// One folder, as a directory and as a messy archive, renders to one tree and one digest.
func TestRenderArchiveMatchesDirectory(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"configmap.yaml":    configMapYAML,
		"sub/dir/notes.txt": "plain text\n",
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
	fromDir, err := settingsimage.Render(os.DirFS(dir), settingsimage.RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}

	archive := tarball(t, true, []tarEntry{
		{name: "./outside.yaml", body: "kind: Secret\n"}, // outside the root: ignored
		{name: "./configs/shop/sub/dir/notes.txt", body: "plain text\n", mode: 0o755},
		{name: "./configs/shop/", dir: true},
		{name: "./configs/shop/.git/config", body: "x"},
		{name: "configs/shop/configmap.yaml", body: configMapYAML, mode: 0o600},
		{name: "./configs/shopping/x.txt", body: "a sibling with a common prefix"},
	})
	fromArchive, err := settingsimage.RenderArchive(
		bytes.NewReader(archive),
		"configs/shop",
		settingsimage.RenderOptions{},
	)
	if err != nil {
		t.Fatal(err)
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
			entries:  []tarEntry{{name: "a", body: "1"}, {name: "./a", body: "2"}},
			wantPath: "a",
			wantIn:   "twice",
		},
		{
			name:     "file and directory",
			entries:  []tarEntry{{name: "a", body: "1"}, {name: "a/b", body: "2"}},
			wantPath: "a",
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
