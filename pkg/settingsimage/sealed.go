// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"regexp"
	"sort"
	"strings"
)

// encPrefix starts every value SOPS encrypted, the MAC included.
const encPrefix = "ENC[AES256_GCM,"

// unsupportedSOPSRules are the SOPS metadata keys that select what to encrypt other than by
// encrypted_regex. Each is refused by name: only encrypted_regex can be checked to have left no
// value in plaintext.
func unsupportedSOPSRules() []string {
	return []string{
		"encrypted_suffix", "unencrypted_suffix", "unencrypted_regex",
		"encrypted_comment_regex", "unencrypted_comment_regex", "mac_only_encrypted",
	}
}

// isSealed reports whether a document carries SOPS metadata.
func isSealed(doc document) bool {
	_, ok := doc["sops"]
	return ok
}

// checkSealed refuses a SOPS document unless it is sealed the one way v1 admits
// (format-v1.md §6): an encrypted_regex, a MAC, at least one encrypted value, and every value
// under a key the regex matches encrypted. The writer never decrypts anything.
func checkSealed(p string, doc document) error {
	re, err := checkSOPSMetadata(p, doc["sops"])
	if err != nil {
		return err
	}
	w := sealWalker{re: re}
	keys := sortedKeys(doc)
	for _, k := range keys {
		if k != "sops" {
			w.walk(k, doc[k], re.MatchString(k))
		}
	}
	switch {
	case w.plaintext != "":
		return refuse(p, "sealed, but %s is not encrypted", w.plaintext)
	case w.encrypted == 0:
		return refuse(p, "sealed, but no value is encrypted")
	}
	return nil
}

// checkSOPSMetadata checks the sops: block, and returns its compiled encrypted_regex.
func checkSOPSMetadata(p string, v any) (*regexp.Regexp, error) {
	meta, ok := v.(map[string]any)
	if !ok {
		return nil, refuse(p, "sops: is not a mapping")
	}
	for _, rule := range unsupportedSOPSRules() {
		if v, set := meta[rule]; set && v != nil && v != "" && v != false {
			return nil, refuse(p, "sealed with the SOPS rule %s; only encrypted_regex is admitted", rule)
		}
	}
	expr, _ := meta["encrypted_regex"].(string)
	if expr == "" {
		return nil, refuse(p, "sealed without an encrypted_regex")
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, refuse(p, "sops encrypted_regex %q does not compile: %v", expr, err)
	}
	if mac, _ := meta["mac"].(string); !strings.HasPrefix(mac, encPrefix) {
		return nil, refuse(p, "sealed without a SOPS MAC")
	}
	return re, nil
}

// sealWalker finds the first plaintext value under a key that encrypted_regex matches.
type sealWalker struct {
	re        *regexp.Regexp
	encrypted int
	plaintext string // the key path of the first plaintext value
}

func (w *sealWalker) walk(at string, v any, under bool) {
	switch v := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(v) {
			w.walk(at+"."+k, v[k], under || w.re.MatchString(k))
		}
	case []any:
		for _, item := range v {
			w.walk(at+"[]", item, under)
		}
	default:
		if !under {
			return
		}
		if s, ok := v.(string); ok && strings.HasPrefix(s, encPrefix) {
			w.encrypted++
		} else if w.plaintext == "" {
			w.plaintext = at
		}
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
