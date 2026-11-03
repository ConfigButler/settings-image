// SPDX-License-Identifier: Apache-2.0

package settingsimage_test

import (
	"os"
	"path/filepath"
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
	rendered, err := settingsimage.Render(fstest.MapFS{"resources/s.yaml": file(doc)}, opts)
	if err != nil {
		return nil, err
	}
	return rendered.Tree, nil
}

func TestSOPSRulesAdmitted(t *testing.T) {
	for _, name := range []string{
		"encrypted-regex.yaml",
		"encrypted-suffix.yaml",
		"multi-document.yaml", // the sops block in every document, read from the first
	} {
		t.Run(name, func(t *testing.T) {
			doc := sopsFixture(t, name)
			tree, err := renderOne(doc, settingsimage.RenderOptions{})
			if err != nil {
				t.Fatal(err)
			}
			// Carried byte for byte, so the MAC stays valid.
			assertTreePaths(t, tree, []string{"resources/s.yaml"})
			assertEqual(t, "bytes", string(tree["resources/s.yaml"]), doc)
		})
	}
}

// Every example folder builds; task test-mount checks what a pod sees of them.
func TestExamplesRender(t *testing.T) {
	dirs, err := filepath.Glob("../../examples/*/")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		t.Run(filepath.Base(dir), func(t *testing.T) {
			if _, err := settingsimage.Render(os.DirFS(dir), settingsimage.RenderOptions{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSOPSRefusals(t *testing.T) {
	regex := sopsFixture(t, "encrypted-regex.yaml")
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
		// SOPS's default rule encrypts every value, the identity included: the build cannot name it.
		{name: "the default rule", doc: sopsFixture(t, "default.yaml"), wantIn: "apiVersion is SOPS-encrypted"},
		// A valid SOPS file whose rule leaves part of the Secret's payload in plaintext.
		{name: "a rule that leaves payload plaintext", doc: sopsFixture(t, "unencrypted-regex.yaml"),
			wantIn: "Secret api: stringData.note_unencrypted is in plaintext"},
		{
			name: "a rule that encrypts only an unrelated field",
			doc: secret("api", "stringData:\n  password: plain\nunrelated: "+
				"ENC[AES256_GCM,data:eA==,iv:eA==,tag:eA==,type:str]\nsops:\n  encrypted_regex: ^unrelated$\n"+
				"  mac: ENC[AES256_GCM,data:eA==,iv:eA==,tag:eA==,type:str]\n"),
			wantIn: "Secret api: stringData.password is in plaintext",
		},
		{name: "a number in plaintext", doc: replace(regex, "stringData:\n", "stringData:\n    port: 5432\n"),
			wantIn: "stringData.port is in plaintext"},
		{
			name:   "an encrypted name",
			doc:    replace(regex, "    name: api", "    name: ENC[AES256_GCM,data:eA==,iv:eA==,tag:eA==,type:str]"),
			wantIn: "metadata.name is SOPS-encrypted",
		},
		{name: "no MAC", doc: replace(regex, "    mac: ENC[", "    notmac: ENC["), wantIn: "without a MAC"},
		{
			name:   "two rules",
			doc:    replace(regex, "    encrypted_regex:", "    unencrypted_suffix: _x\n    encrypted_regex:"),
			wantIn: "more than one SOPS rule",
		},
		{
			name: "nothing encrypted",
			doc: "apiVersion: v1\nkind: Thing\nmetadata:\n  name: t\nspec:\n  a: b\nsops:\n" +
				"  encrypted_regex: ^nothing$\n" +
				"  mac: ENC[AES256_GCM,data:eA==,iv:eA==,tag:eA==,type:str]\n",
			wantIn: "no value is encrypted",
		},
		{name: "mac_only_encrypted", doc: sopsFixture(t, "mac-only-encrypted.yaml"), wantIn: "mac_only_encrypted"},
		{name: "comment rule", doc: sopsFixture(t, "comment-regex.yaml"), wantIn: "encrypted_comment_regex"},
		{name: "sops is not a mapping", doc: configMap("cm", "") + "sops: yes\n", wantIn: "must be a mapping"},
		{name: "metadata only in a later document", doc: configMap("cm", "") + "---\n" + regex,
			wantIn: "first document"},
		{name: "SOPS-encrypted ConfigMap with files", doc: replace(regex, "kind: Secret", "kind: ConfigMap"),
			wantIn: "cannot become files"},
		{name: "plain Secret", doc: secret("s", ""), wantIn: "not SOPS-encrypted"},
		{
			name: "plain sensitive kind",
			doc:  "apiVersion: shop.configbutler.ai/v1\nkind: Recipe\nmetadata:\n  name: r\n",
			opts: settingsimage.RenderOptions{
				SensitiveKinds: []string{"shop.configbutler.ai/Recipe"},
			},
			wantIn: "Recipe r is not SOPS-encrypted",
		},
		{name: "plain sensitive core kind", doc: configMap("cm", ""),
			opts: settingsimage.RenderOptions{SensitiveKinds: []string{"ConfigMap"}}, wantIn: "not SOPS-encrypted"},
		{
			name: "sensitive kind with a plaintext spec field",
			doc: replace(replace(regex, "kind: Secret", "kind: Recipe"), "apiVersion: v1",
				"apiVersion: shop.configbutler.ai/v1"),
			opts:   settingsimage.RenderOptions{SensitiveKinds: []string{"shop.configbutler.ai/Recipe"}},
			wantIn: "Recipe api: type is in plaintext",
		},
		{
			name:   "the list of a sensitive kind",
			doc:    "apiVersion: shop.configbutler.ai/v1\nkind: RecipeList\nmetadata:\n  name: r\n",
			opts:   settingsimage.RenderOptions{SensitiveKinds: []string{"shop.configbutler.ai/Recipe"}},
			wantIn: "is a list",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := renderOne(tt.doc, tt.opts)
			assertRefused(t, err, "resources/s.yaml", tt.wantIn)
		})
	}
}

// A SOPS-encrypted ConfigMap annotated to stay a resource is admitted, and becomes no files.
func TestSOPSEncryptedConfigMapAsResource(t *testing.T) {
	doc := strings.Replace(sopsFixture(t, "encrypted-regex.yaml"), "kind: Secret", "kind: ConfigMap", 1)
	doc = strings.Replace(doc, "    name: api\n", "    name: api\n    annotations:\n        "+
		settingsimage.FilesAnnotation+": \"false\"\n", 1)
	tree, err := renderOne(doc, settingsimage.RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertTreePaths(t, tree, []string{"resources/s.yaml"})
	sensitive := settingsimage.RenderOptions{SensitiveKinds: []string{"ConfigMap"}}
	if _, err := renderOne(doc, sensitive); err == nil || !strings.Contains(err.Error(), "type is in plaintext") {
		t.Errorf("a ConfigMap as a sensitive kind must have every field encrypted, got %v", err)
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
	assertTreePaths(t, tree, []string{"resources/s.yaml"})
}
