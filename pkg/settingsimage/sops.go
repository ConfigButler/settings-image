// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Checks for SOPS-encrypted files (format-v1.md §6), aligned with getsops/sops: the same
// metadata keys (stores/stores.go), the same rule for which values a file encrypts
// (Tree.shouldBeEncrypted in sops.go), and the same ciphertext format (aes/cipher.go). The
// writer never decrypts anything.

// sopsKey is the top-level key that holds SOPS metadata, as in stores.SopsMetadataKey.
const sopsKey = "sops"

// sopsCiphertext is SOPS's AES256_GCM value format (aes/cipher.go's encre).
var sopsCiphertext = regexp.MustCompile(`^ENC\[AES256_GCM,data:(.+),iv:(.+),tag:(.+),type:(.+)\]`)

// sopsDefaultUnencryptedSuffix is what SOPS writes when no rule is configured
// (sops.DefaultUnencryptedSuffix): every value is encrypted except under a key with this suffix.
const sopsDefaultUnencryptedSuffix = "_unencrypted"

// sopsRule is the one rule a SOPS file uses to choose which values it encrypts. SOPS refuses a
// file with more than one.
type sopsRule struct {
	name   string // the metadata key, for messages
	suffix string // for encrypted_suffix and unencrypted_suffix
	re     *regexp.Regexp
	// encrypts is true for encrypted_suffix and encrypted_regex: a match encrypts. For the
	// unencrypted_ rules, a match leaves the value in plaintext.
	encrypts bool
}

// encrypted reports whether SOPS encrypts the value at path (its keys, without list indices).
// It is SOPS's own shouldBeEncrypted for a single, key-based rule.
func (r *sopsRule) encrypted(path []string) bool {
	for _, key := range path {
		var match bool
		if r.re != nil {
			match = r.re.MatchString(key)
		} else {
			match = strings.HasSuffix(key, r.suffix)
		}
		if match {
			return r.encrypts
		}
	}
	return !r.encrypts
}

// sopsMetadata returns the rule of a SOPS-encrypted file, or nil for a file that is not one. As
// in SOPS, the metadata is the first document's sops block, and it governs every document.
func sopsMetadata(p string, docs []document) (*sopsRule, error) {
	found := false
	for _, doc := range docs {
		if _, ok := doc[sopsKey]; ok {
			found = true
		}
	}
	if !found {
		return nil, nil //nolint:nilnil // not a SOPS file: no rule, and nothing wrong
	}
	meta, ok := docs[0][sopsKey].(map[string]any)
	if !ok {
		return nil, refuse(p, "SOPS metadata must be a mapping in the first document, as SOPS writes it")
	}
	for _, key := range []string{"encrypted_comment_regex", "unencrypted_comment_regex"} {
		if set(meta[key]) {
			return nil, refuse(p, "encrypted with the SOPS rule %s, which depends on comments and cannot be "+
				"checked; use encrypted_regex, unencrypted_regex or a suffix rule", key)
		}
	}
	if set(meta["mac_only_encrypted"]) {
		return nil, refuse(p, "encrypted with SOPS mac_only_encrypted: the MAC leaves the plaintext values unprotected")
	}
	if mac, _ := meta["mac"].(string); !sopsCiphertext.MatchString(mac) {
		return nil, refuse(p, "SOPS-encrypted, but without a MAC")
	}
	return sopsRuleOf(p, meta)
}

func sopsRuleOf(p string, meta map[string]any) (*sopsRule, error) {
	var rules []*sopsRule
	for _, r := range []struct {
		key      string
		isRegex  bool
		encrypts bool
	}{
		{"unencrypted_suffix", false, false},
		{"encrypted_suffix", false, true},
		{"unencrypted_regex", true, false},
		{"encrypted_regex", true, true},
	} {
		value, _ := meta[r.key].(string)
		if value == "" {
			continue
		}
		rule := &sopsRule{name: r.key, suffix: value, encrypts: r.encrypts}
		if r.isRegex {
			re, err := regexp.Compile(value)
			if err != nil {
				return nil, refuse(p, "SOPS %s %q does not compile: %v", r.key, value, err)
			}
			rule.re, rule.suffix = re, ""
		}
		rules = append(rules, rule)
	}
	switch len(rules) {
	case 0:
		return &sopsRule{name: "unencrypted_suffix", suffix: sopsDefaultUnencryptedSuffix}, nil
	case 1:
		return rules[0], nil
	default:
		return nil, refuse(
			p,
			"more than one SOPS rule (%s and %s); SOPS itself refuses that",
			rules[0].name,
			rules[1].name,
		)
	}
}

// checkSOPSEncrypted refuses a SOPS file in which a value its rule encrypts is in plaintext, or
// in which nothing is encrypted at all. An empty string and null are left alone, as SOPS leaves
// them.
func checkSOPSEncrypted(p string, docs []document, rule *sopsRule) error {
	w := sopsWalker{rule: rule}
	for _, doc := range docs {
		for _, k := range sortedKeys(doc) {
			if k != sopsKey {
				w.walk([]string{k}, doc[k])
			}
		}
	}
	switch {
	case w.plaintext != "":
		return refuse(p, "SOPS-encrypted, but %s is in plaintext (its %s rule encrypts it)", w.plaintext, rule.name)
	case w.encrypted == 0:
		return refuse(p, "SOPS metadata, but no value is encrypted")
	}
	return nil
}

// sopsWalker finds the first plaintext value that the rule says is encrypted.
type sopsWalker struct {
	rule      *sopsRule
	encrypted int
	plaintext string // the key path of the first plaintext value
}

func (w *sopsWalker) walk(path []string, v any) {
	switch v := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(v) {
			w.walk(append(path[:len(path):len(path)], k), v[k])
		}
	case []any:
		for _, item := range v {
			w.walk(path, item) // SOPS walks a list's items at the list's own path
		}
	case nil:
		// SOPS does not encrypt null.
	default:
		if s, ok := v.(string); ok && (s == "" || sopsCiphertext.MatchString(s)) {
			if s != "" {
				w.encrypted++
			}
			return
		}
		if w.rule.encrypted(path) && w.plaintext == "" {
			w.plaintext = strings.Join(path, ".")
		}
	}
}

// set is a metadata value that turns an option on: a non-empty string, or true.
func set(v any) bool {
	switch v := v.(type) {
	case string:
		return v != ""
	case bool:
		return v
	case nil:
		return false
	default:
		return fmt.Sprint(v) != ""
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// kindKey is how a sensitive kind is named: "group/Kind", or "Kind" for the core group.
func kindKey(doc document) string {
	group, _, hasGroup := strings.Cut(doc.str("apiVersion"), "/")
	if !hasGroup {
		return doc.str("kind")
	}
	return group + "/" + doc.str("kind")
}
