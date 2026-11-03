// SPDX-License-Identifier: Apache-2.0

package settingsimage_test

import (
	"testing"
	"testing/fstest"

	"github.com/ConfigButler/settings-image/pkg/settingsimage"
)

// The known answers of docs/format-v2.md §11. A mismatch is a bug, or a new layout version.

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
			folder:    fstest.MapFS{"resources/configmap.yaml": {Data: []byte(configMapYAML)}},
			wantTree:  []string{"configmaps/shop-settings/settings.yaml", "resources/configmap.yaml"},
			diffID:    "sha256:05e74870873a10c8a89950c4a7fd2bd55ac48aaaa311060c4da95f0eb56ba5ce",
			layer:     "sha256:8a76316fa9647d30e23e8b1d2cd2004d426a13136a2df028766efd609fa61705",
			layerSize: 257,
			config:    "sha256:2880ab7eaf378f9129049468d3e8c515eb108720d64169c35f297c35ef675169",
			manifest:  "sha256:80b524374fdf0db1088308fe2941e6399c50b8d268891a4cd9e2b4e1775c88cd",
			wantManifestBytes: `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json",` +
				`"config":{"mediaType":"application/vnd.oci.image.config.v1+json",` +
				`"digest":"sha256:2880ab7eaf378f9129049468d3e8c515eb108720d64169c35f297c35ef675169","size":151},` +
				`"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip",` +
				`"digest":"sha256:8a76316fa9647d30e23e8b1d2cd2004d426a13136a2df028766efd609fa61705","size":257}],` +
				`"annotations":{"ai.configbutler.settings-image.layout":"v2",` +
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
			referrer:       "sha256:1ba0abbf99d335ce87cdc1c188679d4aa51b6f44c0956287c83fc1baa5a934cb",
			referrerSize:   748,
		},
		{
			name: "nested",
			folder: fstest.MapFS{
				"resources/order.yaml":  {Data: []byte(orderYAML)},
				"raw/sub/dir/notes.txt": {Data: []byte("plain text\n")},
			},
			wantTree:     []string{"raw/sub/dir/notes.txt", "resources/order.yaml"},
			diffID:       "sha256:5898df62870752913ecb4012abf516466a47cfb84f197b53fd09c1a26283f557",
			layer:        "sha256:efca61f147c9e742bd7618c9f284a2c5669d71442e5a02e9ddcfa899b8e9832c",
			layerSize:    274,
			config:       "sha256:4cf20694c2ae0fd5cb398148251aded045c87ef51233732ae077661a27d4b982",
			manifest:     "sha256:6101f3d18cd146af08b06eab924a2fa4ecc411e09c3e7a0883118e9b9b9e101b",
			manifestSize: 598,
		},
	}
}

func TestKnownAnswers(t *testing.T) {
	for _, ka := range knownAnswers() {
		t.Run(ka.name, func(t *testing.T) {
			rendered, err := settingsimage.Render(ka.folder, settingsimage.RenderOptions{})
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			tree := rendered.Tree
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
