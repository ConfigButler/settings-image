// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"encoding/base64"
	"path"
	"regexp"
	"sort"
	"strings"
)

const (
	// ResourcesDir holds Kubernetes resources: read, checked, and ConfigMaps turned into files.
	ResourcesDir = "resources"
	// RawDir holds any other file: copied byte for byte, never read.
	RawDir = "raw"
	// ConfigMapsDir is where the image holds every ConfigMap's keys as plain files. It is
	// derived, never input.
	ConfigMapsDir = "configmaps"

	// FilesAnnotation on a ConfigMap is "false" to keep it a resource only, without files under
	// configmaps/. "true" is the default.
	FilesAnnotation = "settings-image.configbutler.ai/files"
)

// safeNameRE is the apiserver's rule for a ConfigMap key, used for both the name and the key of
// a configmaps/ path.
var safeNameRE = regexp.MustCompile(`^[-._a-zA-Z0-9]+$`)

// resources checks every file under resources/ (format-v2.md §4) and returns the configmaps/
// files of its ConfigMaps (§5).
type resources struct {
	opts       RenderOptions
	files      map[string][]byte // configmaps/<name>/<key>
	identities map[string]string // identity to the path that first declared it
	configMaps map[string]string // ConfigMap name with files to the path that declared it
}

func renderResources(input map[string][]byte, opts RenderOptions) (map[string][]byte, error) {
	paths := make([]string, 0, len(input))
	for p := range input {
		if strings.HasPrefix(p, ResourcesDir+"/") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths) // which of two clashing files is refused does not depend on map order

	r := &resources{
		opts:       opts,
		files:      map[string][]byte{},
		identities: map[string]string{},
		configMaps: map[string]string{},
	}
	for _, p := range paths {
		if err := r.file(p, input[p]); err != nil {
			return nil, err
		}
	}
	return r.files, nil
}

func (r *resources) file(p string, b []byte) error {
	switch strings.ToLower(path.Ext(p)) {
	case ".yaml", ".yml", ".json":
	default:
		return refuse(p, "resources/ holds only .yaml, .yml and .json files; put any other file in raw/")
	}
	docs, err := decodeAll(p, b)
	if err != nil {
		return err
	}
	rule, err := sopsMetadata(p, docs)
	if err != nil {
		return err
	}
	if rule != nil {
		if err := checkSOPSEncrypted(p, docs, rule); err != nil {
			return err
		}
	}
	for _, doc := range docs {
		if err := r.document(p, doc, rule != nil); err != nil {
			return err
		}
	}
	return nil
}

func (r *resources) document(p string, doc document, sopsEncrypted bool) error {
	id, err := identityOf(p, doc)
	if err != nil {
		return err
	}
	if first, dup := r.identities[id.String()]; dup {
		return refuse(p, "%s is declared twice, here and in %s", id, first)
	}
	r.identities[id.String()] = p

	if err := checkNotAList(p, id, r.opts.SensitiveKinds); err != nil {
		return err
	}
	if err := checkSecretEncrypted(p, doc, id, sopsEncrypted, r.opts.SensitiveKinds); err != nil {
		return err
	}
	if id.group == "" && id.kind == "ConfigMap" {
		return r.configMap(p, doc, id, sopsEncrypted)
	}
	return nil
}

// identity is what makes a resource unique: its group, kind, namespace and name. The version is
// not part of it: one object written at two versions is still one object.
type identity struct {
	group, kind, namespace, name string
}

func (id identity) String() string {
	gk := id.kind
	if id.group != "" {
		gk = id.group + "/" + id.kind
	}
	if id.namespace == "" {
		return gk + " " + id.name
	}
	return gk + " " + id.namespace + "/" + id.name
}

// kindKey is how a sensitive kind is named: "group/Kind", or "Kind" for the core group.
func (id identity) kindKey() string {
	if id.group == "" {
		return id.kind
	}
	return id.group + "/" + id.kind
}

// identityOf reads a resource's identity, and refuses a document that has none, or whose
// identity is encrypted: the build must be able to name every resource, a Secret included.
func identityOf(p string, doc document) (identity, error) {
	metadata, ok := doc["metadata"].(map[string]any)
	if !ok {
		return identity{}, refuse(p, "a resource without metadata; every document under resources/ "+
			"needs apiVersion, kind and metadata.name")
	}
	if ns, present := metadata["namespace"]; present && ns != nil {
		if _, ok := ns.(string); !ok {
			return identity{}, refuse(p, "metadata.namespace is not a string")
		}
	}
	for _, field := range []struct {
		name  string
		value any
	}{
		{"apiVersion", doc["apiVersion"]},
		{"kind", doc["kind"]},
		{"metadata.name", metadata["name"]},
		{"metadata.namespace", metadata["namespace"]},
	} {
		s, _ := field.value.(string)
		if field.name != "metadata.namespace" && s == "" {
			return identity{}, refuse(p, "a resource without %s; every document under resources/ "+
				"needs apiVersion, kind and metadata.name", field.name)
		}
		if sopsCiphertext.MatchString(s) {
			return identity{}, refuse(p, "%s is SOPS-encrypted; leave it readable, for example with "+
				"encrypted_regex: ^(data|stringData)$", field.name)
		}
	}
	group, _, hasGroup := strings.Cut(doc.str("apiVersion"), "/")
	if !hasGroup {
		group = ""
	}
	return identity{
		group:     group,
		kind:      doc.str("kind"),
		namespace: doc.str("metadata", "namespace"),
		name:      doc.str("metadata", "name"),
	}, nil
}

// checkNotAList refuses a v1 List, a core-group typed list (SecretList, ConfigMapList), and the
// list of a sensitive kind: the checks look at each resource, so each must be its own document.
func checkNotAList(p string, id identity, sensitiveKinds []string) error {
	isList := id.group == "" && strings.HasSuffix(id.kind, "List")
	for _, sensitive := range sensitiveKinds {
		if id.kindKey() == sensitive+"List" {
			isList = true
		}
	}
	if isList {
		return refuse(p, "%s is a list; put each item in its own document", id)
	}
	return nil
}

// checkSecretEncrypted refuses a Secret, or a sensitive kind, that is not in a SOPS-encrypted file,
// or whose payload is not all ciphertext, whatever the file's SOPS rule says. The payload of a
// Secret is data and stringData; of a sensitive kind, every field but apiVersion, kind and
// metadata. An empty string and null are exempt, as SOPS leaves them.
func checkSecretEncrypted(p string, doc document, id identity, sopsEncrypted bool, sensitiveKinds []string) error {
	var payload []string
	switch {
	case id.group == "" && id.kind == "Secret":
		payload = []string{"data", "stringData"}
	case isSensitive(id, sensitiveKinds):
		for k := range doc {
			if k != "apiVersion" && k != "kind" && k != "metadata" && k != sopsKey {
				payload = append(payload, k)
			}
		}
		sort.Strings(payload)
	default:
		return nil
	}
	if !sopsEncrypted {
		return refuse(p, "%s is not SOPS-encrypted; a registry is not a secret store", id)
	}
	for _, field := range payload {
		if plain := firstPlaintext([]string{field}, doc[field]); plain != "" {
			return refuse(p, "%s: %s is in plaintext; its payload must be SOPS-encrypted", id, plain)
		}
	}
	return nil
}

func isSensitive(id identity, sensitiveKinds []string) bool {
	for _, sensitive := range sensitiveKinds {
		if id.kindKey() == sensitive {
			return true
		}
	}
	return false
}

// firstPlaintext is the key path of the first value under v that is not SOPS ciphertext, or "".
func firstPlaintext(keyPath []string, v any) string {
	switch v := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(v) {
			if plain := firstPlaintext(append(keyPath[:len(keyPath):len(keyPath)], k), v[k]); plain != "" {
				return plain
			}
		}
	case []any:
		for _, item := range v {
			if plain := firstPlaintext(keyPath, item); plain != "" {
				return plain
			}
		}
	case nil:
	default:
		if s, ok := v.(string); !ok || (s != "" && !sopsCiphertext.MatchString(s)) {
			return strings.Join(keyPath, ".")
		}
	}
	return ""
}

// configMap adds a ConfigMap's keys under configmaps/<name>/, unless its annotation says no.
func (r *resources) configMap(p string, doc document, id identity, sopsEncrypted bool) error {
	files, err := filesWanted(p, doc, id)
	if err != nil || !files {
		return err
	}
	if sopsEncrypted {
		return refuse(p, "%s is SOPS-encrypted, so it cannot become files (one value per file cannot "+
			"carry the MAC); annotate it %s: \"false\"", id, FilesAnnotation)
	}
	if !safeName(id.name) {
		return refuse(p, "%s: the name is not safe as a configmaps/ directory", id)
	}
	if first, dup := r.configMaps[id.name]; dup {
		return refuse(p, "%s: a ConfigMap of that name in %s already becomes %s/%s/; rename one, "+
			"or annotate one %s: \"false\"", id, first, ConfigMapsDir, id.name, FilesAnnotation)
	}
	r.configMaps[id.name] = p

	data, binaryData, err := configMapMaps(p, id, doc)
	if err != nil {
		return err
	}
	if err := r.addKeys(p, id, "data", data, func(s string) ([]byte, error) { return []byte(s), nil }); err != nil {
		return err
	}
	return r.addKeys(p, id, "binaryData", binaryData, base64.StdEncoding.DecodeString)
}

// filesWanted reads the files annotation: absent or "true" is true, "false" is false.
func filesWanted(p string, doc document, id identity) (bool, error) {
	metadata, _ := doc["metadata"].(map[string]any)
	annotations, present := metadata["annotations"]
	if !present || annotations == nil {
		return true, nil
	}
	m, ok := annotations.(map[string]any)
	if !ok {
		return false, refuse(p, "%s: metadata.annotations is not a mapping", id)
	}
	v, present := m[FilesAnnotation]
	if !present {
		return true, nil
	}
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, refuse(p, "%s: the annotation %s must be \"true\" or \"false\"", id, FilesAnnotation)
	}
}

// addKeys writes one configmaps/<name>/<key> per key of a ConfigMap's data or binaryData, with
// the value decoded by decode.
func (r *resources) addKeys(p string, id identity, field string, keys map[string]any,
	decode func(string) ([]byte, error),
) error {
	for key, v := range keys {
		s, ok := v.(string)
		if !ok {
			return refuse(p, "%s: %s key %q is not a string", id, field, key)
		}
		b, err := decode(s)
		if err != nil {
			return refuse(p, "%s: %s key %q is not base64", id, field, key)
		}
		if !safeName(key) {
			return refuse(p, "%s: key %q is not safe as a file name", id, key)
		}
		target := ConfigMapsDir + "/" + id.name + "/" + key
		if _, dup := r.files[target]; dup {
			return refuse(p, "%s: %s is written twice", id, target)
		}
		r.files[target] = b
	}
	return nil
}

// configMapMaps returns a ConfigMap's data and binaryData, refusing either when it is present but
// not a mapping.
func configMapMaps(p string, id identity, doc document) (map[string]any, map[string]any, error) {
	var maps [2]map[string]any
	for i, field := range []string{"data", "binaryData"} {
		v, present := doc[field]
		if !present || v == nil {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, nil, refuse(p, "%s: %s is not a mapping", id, field)
		}
		maps[i] = m
	}
	return maps[0], maps[1], nil
}

func safeName(s string) bool {
	return s != "." && s != ".." && safeNameRE.MatchString(s)
}
