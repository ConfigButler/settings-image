// SPDX-License-Identifier: Apache-2.0

package settingsimage_test

import (
	"encoding/json"
	"slices"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/ConfigButler/settings-image/pkg/settingsimage"
)

func assertEqual[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s:\n got %v\nwant %v", what, got, want)
	}
}

func assertTreePaths(t *testing.T, tree settingsimage.Tree, want []string) {
	t.Helper()
	got := make([]string, 0, len(tree))
	for p := range tree {
		got = append(got, p)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("tree paths:\n got %q\nwant %q", got, want)
	}
}

func assertReferrerBlob(t *testing.T, img *settingsimage.Image, want string) {
	t.Helper()
	b, err := img.Fetch(t.Context(), *img.Provenance)
	if err != nil {
		t.Fatal(err)
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Layers) != 1 {
		t.Fatalf("referrer has %d layers, want 1", len(m.Layers))
	}
	assertEqual(t, "provenance blob digest", m.Layers[0].Digest.String(), want)
	assertEqual(t, "referrer subject", m.Subject.Digest, img.Manifest.Digest)
}
