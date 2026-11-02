// SPDX-License-Identifier: Apache-2.0

package settingsimage_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/ConfigButler/settings-image/pkg/settingsimage"
)

// The layer holds every parent directory explicitly, sorted, with forced modes and no times.
func TestLayerShape(t *testing.T) {
	tree := settingsimage.Tree{
		"raw/b.txt":    []byte("b"),
		"raw/a-b/x":    []byte("x"),
		"raw/a/run.sh": []byte("#!/bin/sh\n"),
		"raw/" + strings.Repeat("long/", 30) + "deep.txt": []byte("deep"),
		"raw/ünïcode.txt": []byte("u"),
	}
	img, err := settingsimage.Build(t.Context(), tree, settingsimage.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, hdr := range layerHeaders(t, img) {
		names = append(names, hdr.Name)
		assertHeader(t, hdr)
	}
	if !slices.Contains(names, "raw/") || !slices.Contains(names, "raw/a/") {
		t.Errorf("parent directories missing: %q", names)
	}
	plain := make([]string, len(names))
	for i, n := range names {
		plain[i] = strings.TrimSuffix(n, "/")
	}
	if !slices.IsSorted(plain) {
		t.Errorf("entries not sorted: %q", plain)
	}
}

// layerHeaders reads the layer back, and checks its gzip header is empty.
func layerHeaders(t *testing.T, img *settingsimage.Image) []*tar.Header {
	t.Helper()
	gz, err := img.Fetch(t.Context(), img.Layer)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "gzip name", zr.Name, "")
	assertEqual(t, "gzip mtime", zr.ModTime.IsZero(), true)
	var hdrs []*tar.Header
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return hdrs
		}
		if err != nil {
			t.Fatal(err)
		}
		hdrs = append(hdrs, hdr)
	}
}

// assertHeader checks the forced modes, and that no time or owner name is recorded.
func assertHeader(t *testing.T, hdr *tar.Header) {
	t.Helper()
	wantMode, wantType := int64(0o644), byte(tar.TypeReg)
	if strings.HasSuffix(hdr.Name, "/") {
		wantMode, wantType = 0o755, tar.TypeDir
	}
	if hdr.Mode != wantMode || hdr.Typeflag != wantType {
		t.Errorf("%s: mode %o type %c, want %o %c", hdr.Name, hdr.Mode, hdr.Typeflag, wantMode, wantType)
	}
	if !hdr.ModTime.Equal(time.Unix(0, 0)) || hdr.Uid != 0 || hdr.Gid != 0 || hdr.Uname != "" || hdr.Gname != "" {
		t.Errorf("%s: mtime %v uid %d gid %d uname %q gname %q", hdr.Name, hdr.ModTime, hdr.Uid, hdr.Gid,
			hdr.Uname, hdr.Gname)
	}
}

func TestManifestAndConfig(t *testing.T) {
	tree := settingsimage.Tree{"raw/a": []byte("a")}
	img, err := settingsimage.Build(t.Context(), tree, settingsimage.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := img.Fetch(t.Context(), img.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	// Without a source, exactly two annotations; with one, three (the known answers).
	assertEqual(t, "annotations", len(m.Annotations), 2)
	assertEqual(t, "layout", m.Annotations[settingsimage.LayoutAnnotation], "v2")
	assertEqual(t, "artifactType", m.ArtifactType, "")

	cfg, err := img.Fetch(t.Context(), img.Config)
	if err != nil {
		t.Fatal(err)
	}
	var c ocispec.Image
	if err := json.Unmarshal(cfg, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.RootFS.DiffIDs) != 1 || c.RootFS.DiffIDs[0] != img.DiffID {
		t.Errorf("config diff_ids %v, want [%s]", c.RootFS.DiffIDs, img.DiffID)
	}
}

// Modes, mtimes and the order files were written do not reach the digest; one byte does.
func TestDigestIsStable(t *testing.T) {
	write := func(dir string, order []string, mode os.FileMode, content map[string]string) {
		for _, p := range order {
			full := filepath.Join(dir, filepath.FromSlash(p))
			if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(content[p]), mode); err != nil {
				t.Fatal(err)
			}
		}
	}
	a, b, m := "resources/a.yaml", "raw/z/b.txt", "raw/m.txt"
	content := map[string]string{a: configMapYAML, b: "b\n", m: "m\n"}
	one, two, three := t.TempDir(), t.TempDir(), t.TempDir()
	write(one, []string{a, m, b}, 0o644, content)
	write(two, []string{b, m, a}, 0o755, content)
	if err := os.Chtimes(filepath.Join(two, "raw", "m.txt"), time.Now(), time.Unix(12345, 0)); err != nil {
		t.Fatal(err)
	}
	content[m] = "m!\n"
	write(three, []string{a, m, b}, 0o644, content)

	digest := func(dir string) string {
		tree, err := settingsimage.Render(os.DirFS(dir), settingsimage.RenderOptions{})
		if err != nil {
			t.Fatal(err)
		}
		img, err := settingsimage.Build(t.Context(), tree, settingsimage.BuildOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return img.Manifest.Digest.String()
	}
	assertEqual(t, "same folder, same digest", digest(one), digest(two))
	if digest(one) == digest(three) {
		t.Error("one changed byte gave the same digest")
	}
}

func TestProvenanceOmitsEmptyFields(t *testing.T) {
	b, err := settingsimage.Provenance{Commit: "abc", Author: &settingsimage.Author{Name: "Ada"}}.MarshalBlob()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "blob", string(b), `{"author":{"name":"Ada"},"commit":"abc"}`)
}

func TestProvenanceKeepsHTMLCharacters(t *testing.T) {
	b, err := settingsimage.Provenance{Subject: "fix: a < b & c > d"}.MarshalBlob()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "blob", string(b), `{"subject":"fix: a < b & c > d"}`)
}
