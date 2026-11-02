// SPDX-License-Identifier: Apache-2.0

package settingsimage_test

import (
	"testing"
	"testing/fstest"

	"github.com/ConfigButler/settings-image/pkg/settingsimage"
)

// The known answers of docs/format-v1.md §9. A mismatch is a bug, or a new layout version.

const knownSource = "https://github.com/ConfigButler/k8s-trail"

const configMapYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: shop-settings
data:
  settings.yaml: |
    maxScoops: 3
`

const orderYAML = `apiVersion: shop.configbutler.ai/v1
kind: IceCreamOrder
metadata:
  name: vanilla
spec:
  flavour: vanilla
`

type knownAnswer struct {
	name                     string
	folder                   fstest.MapFS
	wantTree                 []string
	diffID, layer, config    string
	layerSize, manifestSize  int64
	manifest                 string
	wantManifestBytes        string
	provenance               *settingsimage.Provenance
	wantProvenanceBlob       string
	provenanceBlob, referrer string
	referrerSize             int64
}

func knownAnswers() []knownAnswer {
	return []knownAnswer{
		{
			name:      "configmap",
			folder:    fstest.MapFS{"configmap.yaml": {Data: []byte(configMapYAML)}},
			wantTree:  []string{"files/shop-settings/settings.yaml", "objects/configmap.yaml"},
			diffID:    "sha256:a754f5cb0b39e974c519c04b2bbb3663a2bad2c003f6cdd43aca29c199db9575",
			layer:     "sha256:d85cf97c694d719efd32a6d8b4b73d67ad464cb222cb747ee69e18bdbbbb7d4a",
			layerSize: 254,
			config:    "sha256:f75f1a93569b199706fc140e4b1f735279e124d1bc56c013ef9ae10303fa0766",
			manifest:  "sha256:c5edc2c3ef1c35d2a810d55d6ce161a30bf5a9e3337ff560a7e3ae6eabf6516d",
			wantManifestBytes: `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json",` +
				`"config":{"mediaType":"application/vnd.oci.image.config.v1+json",` +
				`"digest":"sha256:f75f1a93569b199706fc140e4b1f735279e124d1bc56c013ef9ae10303fa0766","size":151},` +
				`"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip",` +
				`"digest":"sha256:d85cf97c694d719efd32a6d8b4b73d67ad464cb222cb747ee69e18bdbbbb7d4a","size":254}],` +
				`"annotations":{"ai.configbutler.settings-image.layout":"v1",` +
				`"org.opencontainers.image.created":"1970-01-01T00:00:00Z",` +
				`"org.opencontainers.image.source":"https://github.com/ConfigButler/k8s-trail"}}`,
			manifestSize: 598,
			provenance: &settingsimage.Provenance{
				Author:      &settingsimage.Author{Email: "ada@example.com", Name: "Ada"},
				Commit:      "0123456789abcdef0123456789abcdef01234567",
				CommittedAt: "2026-11-02T20:41:07Z",
				Path:        "configs/shop",
				Ref:         "refs/heads/main",
				Repository:  knownSource,
				Subject:     "feat: three scoops",
			},
			wantProvenanceBlob: `{"author":{"email":"ada@example.com","name":"Ada"},` +
				`"commit":"0123456789abcdef0123456789abcdef01234567","committedAt":"2026-11-02T20:41:07Z",` +
				`"path":"configs/shop","ref":"refs/heads/main","repository":"https://github.com/ConfigButler/k8s-trail",` +
				`"subject":"feat: three scoops"}`,
			provenanceBlob: "sha256:ce56f1d41b61539fe3ee169c1c23ff7edf39a70cc81d948789cf55d7a351bdae",
			referrer:       "sha256:8962065a29e9443d5e12b28da691cd13c85799980b12fed477507b1bce20d682",
			referrerSize:   748,
		},
		{
			name: "nested",
			folder: fstest.MapFS{
				"order.yaml":        {Data: []byte(orderYAML)},
				"sub/dir/notes.txt": {Data: []byte("plain text\n")},
			},
			wantTree:     []string{"objects/order.yaml", "objects/sub/dir/notes.txt"},
			diffID:       "sha256:3be4c2cddd800adbbde12d51f6c8edb8736e3560209faf8f605d5ae11be1a0c5",
			layer:        "sha256:b05f2fb88911ea5f7e96824ad041f59f28c9eb7183c7efda95303fa2832436c3",
			layerSize:    266,
			config:       "sha256:c6ddd5af0df19340fac591cf1abff9b4073c1220ac5afeda16b12959d3449bc2",
			manifest:     "sha256:68b9a5c104a4bbdf9ffcc3762f0aa3a63c9519766d48430ce3e53d157fa694db",
			manifestSize: 598,
		},
	}
}

func TestKnownAnswers(t *testing.T) {
	for _, ka := range knownAnswers() {
		t.Run(ka.name, func(t *testing.T) {
			tree, err := settingsimage.Render(ka.folder, settingsimage.RenderOptions{})
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			assertTreePaths(t, tree, ka.wantTree)

			img, err := settingsimage.Build(t.Context(), tree, settingsimage.BuildOptions{Source: knownSource})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			assertEqual(t, "diffID", img.DiffID.String(), ka.diffID)
			assertEqual(t, "layer", img.Layer.Digest.String(), ka.layer)
			assertEqual(t, "layer size", img.Layer.Size, ka.layerSize)
			assertEqual(t, "config", img.Config.Digest.String(), ka.config)
			assertEqual(t, "config size", img.Config.Size, int64(151))
			assertEqual(t, "manifest", img.Manifest.Digest.String(), ka.manifest)
			assertEqual(t, "manifest size", img.Manifest.Size, ka.manifestSize)
			if ka.wantManifestBytes != "" {
				b, err := img.Fetch(t.Context(), img.Manifest)
				assertEqual(t, "fetch error", err, nil)
				assertEqual(t, "manifest bytes", string(b), ka.wantManifestBytes)
			}

			if ka.provenance != nil {
				checkProvenance(t, img, ka)
			}
		})
	}
}

func checkProvenance(t *testing.T, img *settingsimage.Image, ka knownAnswer) {
	t.Helper()
	blob, err := ka.provenance.MarshalBlob()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "provenance blob", string(blob), ka.wantProvenanceBlob)
	if err := img.AttachProvenance(t.Context(), *ka.provenance); err != nil {
		t.Fatalf("AttachProvenance: %v", err)
	}
	assertEqual(t, "referrer", img.Provenance.Digest.String(), ka.referrer)
	assertEqual(t, "referrer size", img.Provenance.Size, ka.referrerSize)
	assertReferrerBlob(t, img, ka.provenanceBlob)
}
