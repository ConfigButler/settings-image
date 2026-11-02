// SPDX-License-Identifier: Apache-2.0

package settingsimage_test

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ConfigButler/settings-image/pkg/settingsimage"
)

// The fixtures in testdata/sops were written by the sops CLI, one per SOPS rule (see the README
// there); each decrypts with the committed test key.
func sopsFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/sops/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func renderOne(doc string, opts settingsimage.RenderOptions) (settingsimage.Tree, error) {
	return settingsimage.Render(fstest.MapFS{"s.yaml": file(doc)}, opts)
}

func TestSOPSRulesAdmitted(t *testing.T) {
	for _, name := range []string{
		"default.yaml", // no rule: SOPS writes unencrypted_suffix: _unencrypted
		"encrypted-regex.yaml",
		"unencrypted-regex.yaml",
		"encrypted-suffix.yaml",
		"multi-document.yaml", // the sops block in every document, read from the first
	} {
		t.Run(name, func(t *testing.T) {
			doc := sopsFixture(t, name)
			tree, err := renderOne(doc, settingsimage.RenderOptions{})
			if err != nil {
				t.Fatal(err)
			}
			// Carried byte for byte, so the MAC stays valid; no files/ view of ciphertext.
			assertTreePaths(t, tree, []string{"objects/s.yaml"})
			assertEqual(t, "bytes", string(tree["objects/s.yaml"]), doc)
		})
	}
}

func TestSOPSExampleAdmitted(t *testing.T) {
	b, err := os.ReadFile("../../examples/sops-secret/database.secret.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := renderOne(string(b), settingsimage.RenderOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestSOPSRefusals(t *testing.T) {
	regex := sopsFixture(t, "encrypted-regex.yaml")
	def := sopsFixture(t, "default.yaml")
	replace := func(doc, old, replacement string) string {
		t.Helper()
		if !strings.Contains(doc, old) {
			t.Fatalf("fixture has no %q", old)
		}
		return strings.Replace(doc, old, replacement, 1)
	}
	line := func(doc, key string) string { // the whole line holding key
		t.Helper()
		for l := range strings.Lines(doc) {
			if strings.HasPrefix(strings.TrimSpace(l), key+":") {
				return l
			}
		}
		t.Fatalf("fixture has no %s line", key)
		return ""
	}
	tests := []struct {
		name   string
		doc    string
		opts   settingsimage.RenderOptions
		wantIn string
	}{
		{name: "plaintext under encrypted_regex", doc: replace(regex, line(regex, "token"), "    token: hunter2\n"),
			wantIn: "stringData.token is in plaintext"},
		{name: "plaintext list item", doc: replace(regex, "- ENC[", "- plain\n        - ENC["),
			wantIn: "stringData.list is in plaintext"},
		{name: "plaintext under the default rule", doc: replace(def, line(def, "type"), "type: Opaque\n"),
			wantIn: "type is in plaintext (its unencrypted_suffix rule"},
		{name: "a number in plaintext", doc: replace(regex, "stringData:\n", "stringData:\n    port: 5432\n"),
			wantIn: "stringData.port is in plaintext"},
		{name: "no MAC", doc: replace(regex, "    mac: ENC[", "    notmac: ENC["), wantIn: "without a MAC"},
		{
			name:   "two rules",
			doc:    replace(regex, "    encrypted_regex:", "    unencrypted_suffix: _x\n    encrypted_regex:"),
			wantIn: "more than one SOPS rule",
		},
		{
			name: "nothing encrypted",
			doc: "kind: Thing\nspec:\n  a: b\nsops:\n  encrypted_regex: ^nothing$\n" +
				"  mac: ENC[AES256_GCM,data:eA==,iv:eA==,tag:eA==,type:str]\n",
			wantIn: "no value is encrypted",
		},
		{name: "mac_only_encrypted", doc: sopsFixture(t, "mac-only-encrypted.yaml"), wantIn: "mac_only_encrypted"},
		{name: "comment rule", doc: sopsFixture(t, "comment-regex.yaml"), wantIn: "encrypted_comment_regex"},
		{name: "sops is not a mapping", doc: "kind: Thing\nsops: yes\n", wantIn: "must be a mapping"},
		{name: "metadata only in a later document", doc: "kind: Thing\n---\n" + regex, wantIn: "first document"},
		{name: "SOPS-encrypted ConfigMap with files/", doc: replace(regex, "kind: Secret", "kind: ConfigMap"),
			wantIn: "files/ view"},
		{
			name:   "plain Secret",
			doc:    "apiVersion: v1\nkind: Secret\nmetadata:\n  name: s\n",
			wantIn: "not SOPS-encrypted",
		},
		{
			name: "plain sensitive kind",
			doc:  "apiVersion: shop.configbutler.ai/v1\nkind: Recipe\n",
			opts: settingsimage.RenderOptions{
				SensitiveKinds: []string{"shop.configbutler.ai/Recipe"},
			},
			wantIn: "sensitive",
		},
		{name: "plain sensitive core kind", doc: configMap("cm", ""),
			opts: settingsimage.RenderOptions{SensitiveKinds: []string{"ConfigMap"}}, wantIn: "sensitive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := renderOne(tt.doc, tt.opts)
			assertRefused(t, err, "s.yaml", tt.wantIn)
		})
	}
}

func TestSOPSEncryptedConfigMapWithoutFiles(t *testing.T) {
	doc := strings.Replace(sopsFixture(t, "encrypted-regex.yaml"), "kind: Secret", "kind: ConfigMap", 1)
	if _, err := renderOne(doc, settingsimage.RenderOptions{NoFiles: true}); err != nil {
		t.Errorf("a SOPS-encrypted ConfigMap with NoFiles: %v", err)
	}
	sensitive := settingsimage.RenderOptions{SensitiveKinds: []string{"Secret", "ConfigMap"}, NoFiles: true}
	if _, err := renderOne(doc, sensitive); err != nil {
		t.Errorf("a SOPS-encrypted sensitive kind: %v", err)
	}
}

// A Sealed Secrets SealedSecret is not SOPS, and not a Secret: an ordinary custom resource.
func TestSealedSecretIsACustomResource(t *testing.T) {
	doc := "apiVersion: bitnami.com/v1alpha1\nkind: SealedSecret\nmetadata:\n  name: s\n" +
		"spec:\n  encryptedData:\n    token: AgBy3i4OJSWK+PiTySYZZA==\n"
	tree, err := renderOne(doc, settingsimage.RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertTreePaths(t, tree, []string{"objects/s.yaml"})
}
