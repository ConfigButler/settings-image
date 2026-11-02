// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
)

const (
	// ProvenanceArtifactType is the artifactType of the provenance referrer.
	ProvenanceArtifactType = "application/vnd.configbutler.settings-image.provenance.v1+json"
	// ProvenanceLayerMediaType is the media type of the provenance referrer's one blob.
	ProvenanceLayerMediaType = "application/vnd.configbutler.settings-image.provenance.layer.v1+json"
)

// Provenance is the per-commit record attached to an image as an OCI referrer (format-v1.md §7).
// Fields are declared in key order, so the JSON comes out with its keys sorted. An empty field is
// omitted.
type Provenance struct {
	Author      *Author `json:"author,omitempty"`
	Commit      string  `json:"commit,omitempty"`
	CommittedAt string  `json:"committedAt,omitempty"` // RFC 3339, UTC
	Path        string  `json:"path,omitempty"`
	Ref         string  `json:"ref,omitempty"`
	Repository  string  `json:"repository,omitempty"`
	Subject     string  `json:"subject,omitempty"`
}

// Author is the author of the commit that last changed the folder.
type Author struct {
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

// MarshalBlob is the referrer's blob: compact JSON with sorted keys.
func (p Provenance) MarshalBlob() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return nil, fmt.Errorf("encoding provenance: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// AttachProvenance builds the provenance referrer, whose subject is the image manifest, and
// records it in img.Provenance. It is pushed by [Image.Push].
func (img *Image) AttachProvenance(ctx context.Context, p Provenance) error {
	blob, err := p.MarshalBlob()
	if err != nil {
		return err
	}
	blobDesc := content.NewDescriptorFromBytes(ProvenanceLayerMediaType, blob)
	if err := img.store.Push(ctx, blobDesc, bytes.NewReader(blob)); err != nil {
		return fmt.Errorf("storing provenance: %w", err)
	}
	// The subject is the bare descriptor: PackManifest's result also carries the manifest's
	// annotations, which would change the referrer's bytes.
	subject := ocispec.Descriptor{
		MediaType: img.Manifest.MediaType,
		Digest:    img.Manifest.Digest,
		Size:      img.Manifest.Size,
	}
	desc, err := oras.PackManifest(ctx, img.store, oras.PackManifestVersion1_1, ProvenanceArtifactType,
		oras.PackManifestOptions{
			Subject:             &subject,
			Layers:              []ocispec.Descriptor{blobDesc},
			ManifestAnnotations: map[string]string{ocispec.AnnotationCreated: epoch},
		})
	if err != nil {
		return fmt.Errorf("packing provenance manifest: %w", err)
	}
	img.Provenance = &desc
	return nil
}
