// SPDX-License-Identifier: Apache-2.0

package settingsimage_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/memory"

	"github.com/ConfigButler/settings-image/pkg/settingsimage"
)

// recorder is an in-memory target that records every push and tag, in order.
type recorder struct {
	*memory.Store

	calls []string
}

func (r *recorder) Push(ctx context.Context, desc ocispec.Descriptor, rd io.Reader) error {
	r.calls = append(r.calls, "push "+desc.MediaType)
	return r.Store.Push(ctx, desc, rd)
}

func (r *recorder) Tag(ctx context.Context, desc ocispec.Descriptor, ref string) error {
	r.calls = append(r.calls, "tag "+ref)
	return r.Store.Tag(ctx, desc, ref)
}

func builtImage(t *testing.T, withProvenance bool) *settingsimage.Image {
	t.Helper()
	tree, err := settingsimage.Render(
		fstest.MapFS{"resources/configmap.yaml": file(configMapYAML)},
		settingsimage.RenderOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	img, err := settingsimage.Build(t.Context(), tree, settingsimage.BuildOptions{Source: knownSource})
	if err != nil {
		t.Fatal(err)
	}
	if withProvenance {
		if err := img.AttachProvenance(t.Context(), settingsimage.Provenance{Commit: "0123456"}); err != nil {
			t.Fatal(err)
		}
	}
	return img
}

func TestPushOrder(t *testing.T) {
	img := builtImage(t, true)
	target := &recorder{Store: memory.New()}
	res, err := img.Push(t.Context(), target, []string{"sha-0123456", "v1", "main"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "changed", res.Changed, true)

	// The image's blobs come before its manifest, the manifest before the referrer, and the tags
	// last, in the order given (the moving one, main, at the very end).
	want := []string{
		"push " + ocispec.MediaTypeImageManifest, // the image manifest, after its two blobs
		"push " + ocispec.MediaTypeImageManifest, // the referrer
		"tag sha-0123456", "tag v1", "tag main",
	}
	var got []string
	for i, c := range target.calls {
		if c == "push "+ocispec.MediaTypeImageManifest || i >= len(target.calls)-3 {
			got = append(got, c)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("calls %q", target.calls)
	}
	firstManifest := slices.Index(target.calls, "push "+ocispec.MediaTypeImageManifest)
	if !slices.Contains(target.calls[:firstManifest], "push "+ocispec.MediaTypeImageLayerGzip) {
		t.Errorf("the layer was not pushed before the manifest: %q", target.calls)
	}

	referrers, err := target.Predecessors(t.Context(), img.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(referrers) != 1 || referrers[0].ArtifactType != settingsimage.ProvenanceArtifactType {
		t.Errorf("referrers %v", referrers)
	}
}

func TestPushChanged(t *testing.T) {
	img := builtImage(t, false)
	target := memory.New()

	res, err := img.Push(t.Context(), target, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "first push, no tags: changed", res.Changed, true)

	res, err = img.Push(t.Context(), target, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "second push, no tags: changed", res.Changed, false)

	res, err = img.Push(t.Context(), target, []string{"main"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "a new tag: changed", res.Changed, true)

	res, err = img.Push(t.Context(), target, []string{"main"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "the same tag again: changed", res.Changed, false)

	if err := target.Tag(t.Context(), img.Config, "main"); err != nil {
		t.Fatal(err)
	}
	res, err = img.Push(t.Context(), target, []string{"main"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "a tag moved elsewhere: changed", res.Changed, true)
}

func TestPushRefusesInvalidTagBeforeWriting(t *testing.T) {
	img := builtImage(t, false)
	target := &recorder{Store: memory.New()}
	_, err := img.Push(t.Context(), target, []string{"ok", "not/ok"})
	if !errors.Is(err, settingsimage.ErrInvalidTag) {
		t.Fatalf("got %v, want ErrInvalidTag", err)
	}
	if len(target.calls) != 0 {
		t.Errorf("wrote before refusing: %q", target.calls)
	}
}

func TestTags(t *testing.T) {
	for in, want := range map[string]string{
		"main":           "main",
		"feature/Card":   "feature-card",
		"-.weird":        "weird",
		"..":             "branch",
		"":               "branch",
		"release/v1.2_x": "release-v1.2_x",
		"ünï":            "n-",
	} {
		assertEqual(t, "BranchTag("+in+")", settingsimage.BranchTag(in), want)
	}
	long := settingsimage.BranchTag(strings.Repeat("b", 300))
	assertEqual(t, "long branch tag length", len(long), 128)

	for _, ok := range []string{"main", "v1.2.3", "_x", "sha-0123456"} {
		if err := settingsimage.ValidateTag(ok); err != nil {
			t.Errorf("ValidateTag(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-x", ".x", "a/b", "a:b", strings.Repeat("a", 129)} {
		if err := settingsimage.ValidateTag(bad); err == nil {
			t.Errorf("ValidateTag(%q) accepted", bad)
		}
	}
	assertEqual(t, "ShaTag", settingsimage.ShaTag("0123456789abcdef"), "sha-0123456")
}
