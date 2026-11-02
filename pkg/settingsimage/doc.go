// SPDX-License-Identifier: Apache-2.0

// Package settingsimage builds a folder of settings into a settings image, as specified in
// docs/format-v1.md, and pushes it.
//
// The steps are separate so that each can be tested and reused:
//
//   - [Render] or [RenderArchive] reads a folder into a [Tree], or refuses it with a
//     [*RefusalError];
//   - [Build] turns the tree into an [*Image]: a layer, a config and a manifest, held in memory;
//   - [Image.AttachProvenance] adds the per-commit referrer;
//   - [Image.Push] copies everything to any oras.Target, such as [NewRepository]'s remote or an
//     OCI layout directory, and tags it.
//
// Nothing per-commit or per-run goes into the image itself: the same folder always gives the
// same digest.
package settingsimage
