// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"context"
	"errors"
	"fmt"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/errdef"
)

// PushResult is what [Image.Push] did.
type PushResult struct {
	// Changed is false when the image already existed in the target and every tag already
	// pointed at it: nothing needed doing.
	Changed bool
}

// Push copies the image, then its provenance referrer, then sets tags in the order given, to
// target (format-v1.md §8). The caller puts moving tags last, so whoever follows one never sees a
// digest without its provenance. Every step is idempotent. Every tag is validated before
// anything is written.
func (img *Image) Push(ctx context.Context, target oras.Target, tags []string) (PushResult, error) {
	for _, tag := range tags {
		if err := ValidateTag(tag); err != nil {
			return PushResult{}, err
		}
	}
	unchanged, err := img.upToDate(ctx, target, tags)
	if err != nil {
		return PushResult{}, classify(err)
	}

	if err := oras.CopyGraph(ctx, img.store, target, img.Manifest, oras.DefaultCopyGraphOptions); err != nil {
		return PushResult{}, classify(fmt.Errorf("pushing image: %w", err))
	}
	if img.Provenance != nil {
		err := oras.CopyGraph(ctx, img.store, target, *img.Provenance, oras.DefaultCopyGraphOptions)
		if err != nil {
			return PushResult{}, classify(fmt.Errorf("pushing provenance: %w", err))
		}
	}
	for _, tag := range tags {
		if err := target.Tag(ctx, img.Manifest, tag); err != nil {
			return PushResult{}, classify(fmt.Errorf("tagging %s: %w", tag, err))
		}
	}
	return PushResult{Changed: !unchanged}, nil
}

// upToDate reports whether the manifest already exists in target and every tag resolves to it.
func (img *Image) upToDate(ctx context.Context, target oras.Target, tags []string) (bool, error) {
	exists, err := target.Exists(ctx, img.Manifest)
	if err != nil || !exists {
		return false, err
	}
	for _, tag := range tags {
		desc, err := target.Resolve(ctx, tag)
		if errors.Is(err, errdef.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("resolving %s: %w", tag, err)
		}
		if desc.Digest != img.Manifest.Digest {
			return false, nil
		}
	}
	return true, nil
}
