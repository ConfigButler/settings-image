// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"bytes"
	"context"
	"fmt"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
)

const (
	// LayoutAnnotation names the layout version of a settings image. A reader refuses a layout it
	// does not know.
	LayoutAnnotation = "ai.configbutler.settings-image.layout"
	// LayoutV2 is the layout docs/format-v2.md describes.
	LayoutV2 = "v2"

	// epoch is the pinned org.opencontainers.image.created: oras-go stamps time.Now() otherwise.
	epoch = "1970-01-01T00:00:00Z"
)

// BuildOptions are the writer's choices for the manifest.
type BuildOptions struct {
	// Source is the source repository's URL, for org.opencontainers.image.source. It must
	// carry no credentials. Empty leaves the annotation out.
	Source string
}

// Image is a built settings image, with its blobs held in memory until [Image.Push].
type Image struct {
	// Manifest is the image manifest's descriptor; its digest is the image's digest.
	Manifest ocispec.Descriptor
	// Config and Layer are the descriptors the manifest points at.
	Config ocispec.Descriptor
	Layer  ocispec.Descriptor
	// DiffID is the sha256 of the uncompressed layer, as listed in the config.
	DiffID digest.Digest
	// Provenance is the referrer's manifest descriptor, once [Image.AttachProvenance] ran.
	Provenance *ocispec.Descriptor

	store *memory.Store
}

// Build turns a tree into a settings image: one layer, the minimal config and the manifest of
// format-v2.md §6 and §7.
func Build(ctx context.Context, tree Tree, opts BuildOptions) (*Image, error) {
	if len(tree) == 0 {
		return nil, refuse(".", "the tree is empty")
	}
	l, err := buildLayer(tree)
	if err != nil {
		return nil, err
	}
	img := &Image{DiffID: digest.FromBytes(l.tar), store: memory.New()}

	img.Layer = content.NewDescriptorFromBytes(ocispec.MediaTypeImageLayerGzip, l.gzip)
	if err := img.store.Push(ctx, img.Layer, bytes.NewReader(l.gzip)); err != nil {
		return nil, fmt.Errorf("storing layer: %w", err)
	}

	config := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":["` +
		img.DiffID.String() + `"]}}`)
	img.Config = content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, config)
	if err := img.store.Push(ctx, img.Config, bytes.NewReader(config)); err != nil {
		return nil, fmt.Errorf("storing config: %w", err)
	}

	annotations := map[string]string{
		ocispec.AnnotationCreated: epoch,
		LayoutAnnotation:          LayoutV2,
	}
	if opts.Source != "" {
		annotations[ocispec.AnnotationSource] = opts.Source
	}
	img.Manifest, err = oras.PackManifest(ctx, img.store, oras.PackManifestVersion1_1, "", oras.PackManifestOptions{
		Layers:              []ocispec.Descriptor{img.Layer},
		ConfigDescriptor:    &img.Config,
		ManifestAnnotations: annotations,
	})
	if err != nil {
		return nil, fmt.Errorf("packing manifest: %w", err)
	}
	return img, nil
}

// Fetch returns the bytes of a blob or manifest of the image, by descriptor.
func (img *Image) Fetch(ctx context.Context, desc ocispec.Descriptor) ([]byte, error) {
	b, err := content.FetchAll(ctx, img.store, desc)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", desc.Digest, err)
	}
	return b, nil
}
