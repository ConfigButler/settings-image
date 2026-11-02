// SPDX-License-Identifier: Apache-2.0

package settingsimage_test

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ConfigButler/settings-image/pkg/settingsimage"
)

// sealedSecret is examples/sops-secret's Secret, as the sops CLI wrote it.
func sealedSecret(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../examples/sops-secret/database.secret.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSealedSecretIsAdmitted(t *testing.T) {
	secret := sealedSecret(t)
	tree, err := settingsimage.Render(fstest.MapFS{"s.yaml": file(secret)}, settingsimage.RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Carried byte for byte, so the MAC stays valid; no files/ view of ciphertext.
	assertTreePaths(t, tree, []string{"objects/s.yaml"})
	assertEqual(t, "bytes", string(tree["objects/s.yaml"]), secret)
}

func TestSealedRefusals(t *testing.T) {
	secret := sealedSecret(t)
	replace := func(old, replacement string) string {
		if !strings.Contains(secret, old) {
			t.Fatalf("fixture has no %q", old)
		}
		return strings.Replace(secret, old, replacement, 1)
	}
	// The encrypted password line, for swapping in plaintext.
	var passwordLine string
	for line := range strings.Lines(secret) {
		if strings.Contains(line, "password: ENC[") {
			passwordLine = line
		}
	}
	tests := []struct {
		name   string
		doc    string
		opts   settingsimage.RenderOptions
		wantIn string
	}{
		{name: "plaintext under a matched key", doc: replace(passwordLine, "    password: hunter2\n"),
			wantIn: "stringData.password is not encrypted"},
		{name: "plaintext key added", doc: replace("stringData:\n", "stringData:\n    extra: plain\n"),
			wantIn: "stringData.extra is not encrypted"},
		{name: "no MAC", doc: replace("    mac: ENC[", "    notmac: ENC["), wantIn: "MAC"},
		{name: "no encrypted_regex", doc: replace("    encrypted_regex:", "    unused:"), wantIn: "encrypted_regex"},
		{
			name:   "another SOPS rule",
			doc:    replace("    encrypted_regex:", "    unencrypted_suffix: _x\n    encrypted_regex:"),
			wantIn: "unencrypted_suffix",
		},
		{name: "nothing encrypted", doc: replace("encrypted_regex: ^(data|stringData)$", "encrypted_regex: ^nothing$"),
			wantIn: "no value is encrypted"},
		{name: "sops is not a mapping", doc: "kind: Thing\nsops: yes\n", wantIn: "not a mapping"},
		{name: "sealed ConfigMap with files/", doc: replace("kind: Secret", "kind: ConfigMap"),
			wantIn: "files/ view"},
		{
			name: "unsealed sensitive kind",
			doc:  "apiVersion: shop.configbutler.ai/v1\nkind: Recipe\n",
			opts: settingsimage.RenderOptions{
				SensitiveKinds: []string{"shop.configbutler.ai/Recipe"},
			},
			wantIn: "sensitive",
		},
		{name: "unsealed sensitive core kind", doc: configMap("cm", ""),
			opts: settingsimage.RenderOptions{SensitiveKinds: []string{"ConfigMap"}}, wantIn: "sensitive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := settingsimage.Render(fstest.MapFS{"s.yaml": file(tt.doc)}, tt.opts)
			assertRefused(t, err, "s.yaml", tt.wantIn)
		})
	}
}

func TestSealedConfigMapWithoutFiles(t *testing.T) {
	doc := strings.Replace(sealedSecret(t), "kind: Secret", "kind: ConfigMap", 1)
	_, err := settingsimage.Render(fstest.MapFS{"c.yaml": file(doc)}, settingsimage.RenderOptions{NoFiles: true})
	if err != nil {
		t.Errorf("a sealed ConfigMap with NoFiles: %v", err)
	}
	sensitive := settingsimage.RenderOptions{SensitiveKinds: []string{"Secret", "ConfigMap"}, NoFiles: true}
	if _, err := settingsimage.Render(fstest.MapFS{"c.yaml": file(doc)}, sensitive); err != nil {
		t.Errorf("a sealed sensitive kind: %v", err)
	}
}
