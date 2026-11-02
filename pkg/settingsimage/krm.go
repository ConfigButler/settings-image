// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
)

// filesNameRE is the apiserver's rule for a ConfigMap key, used for both the name and the key of
// a files/ path.
var filesNameRE = regexp.MustCompile(`^[-._a-zA-Z0-9]+$`)

// decodeBufferSize is how far NewYAMLOrJSONDecoder looks ahead to tell JSON from YAML.
const decodeBufferSize = 4096

// document is one decoded YAML or JSON document. Only a mapping decodes into it.
type document map[string]any

// str is the string at a chain of keys, or "" when any step is missing or not a mapping.
func (d document) str(keys ...string) string {
	var v any = map[string]any(d)
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		v = m[k]
	}
	s, _ := v.(string)
	return s
}

// configMapFiles parses every .yaml, .yml and .json input file, refuses what format-v1.md §6
// refuses, and returns the files/ view of every ConfigMap.
func configMapFiles(files map[string][]byte) (map[string][]byte, error) {
	paths := make([]string, 0, len(files))
	for p := range files {
		if isParsed(p) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths) // the first refusal reported does not depend on map order

	views := map[string][]byte{}
	for _, p := range paths {
		docs, err := decodeAll(p, files[p])
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			if err := addConfigMap(views, p, doc); err != nil {
				return nil, err
			}
		}
	}
	return views, nil
}

func isParsed(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}

// decodeAll reads a stream of YAML or JSON documents. A document that is null or empty is
// skipped; anything that does not decode to a mapping is refused.
func decodeAll(p string, b []byte) ([]document, error) {
	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(b), decodeBufferSize)
	var docs []document
	for {
		var doc document
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, refuse(p, "does not parse as YAML or JSON objects: %v", err)
		}
		if doc != nil {
			docs = append(docs, doc)
		}
	}
}

func addConfigMap(views map[string][]byte, p string, doc document) error {
	if doc.str("kind") == "Secret" {
		return refuse(p, "a Secret; a registry is not a secret store")
	}
	if doc.str("apiVersion") != "v1" || doc.str("kind") != "ConfigMap" {
		return nil
	}
	name := doc.str("metadata", "name")
	if !safeName(name) {
		return refuse(p, "ConfigMap name %q is not safe as a files/ directory", name)
	}
	data, binaryData, err := configMapMaps(p, name, doc)
	if err != nil {
		return err
	}
	if err := addKeys(
		views,
		p,
		name,
		"data",
		data,
		func(s string) ([]byte, error) { return []byte(s), nil },
	); err != nil {
		return err
	}
	return addKeys(views, p, name, "binaryData", binaryData, base64.StdEncoding.DecodeString)
}

// addKeys writes one files/<name>/<key> per key of a ConfigMap's data or binaryData, with the
// value decoded by decode.
func addKeys(views map[string][]byte, p, name, field string, keys map[string]any,
	decode func(string) ([]byte, error),
) error {
	for key, v := range keys {
		s, ok := v.(string)
		if !ok {
			return refuse(p, "ConfigMap %s: %s key %q is not a string", name, field, key)
		}
		b, err := decode(s)
		if err != nil {
			return refuse(p, "ConfigMap %s: %s key %q is not base64", name, field, key)
		}
		if !safeName(key) {
			return refuse(p, "ConfigMap %s: key %q is not safe as a file name", name, key)
		}
		target := "files/" + name + "/" + key
		if _, dup := views[target]; dup {
			return refuse(p, "ConfigMap %s: %s is written twice", name, target)
		}
		views[target] = b
	}
	return nil
}

// configMapMaps returns a ConfigMap's data and binaryData, refusing either when it is present but
// not a mapping.
func configMapMaps(p, name string, doc document) (map[string]any, map[string]any, error) {
	var maps [2]map[string]any
	for i, field := range []string{"data", "binaryData"} {
		v, present := doc[field]
		if !present || v == nil {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, nil, refuse(p, "ConfigMap %s: %s is not a mapping", name, field)
		}
		maps[i] = m
	}
	return maps[0], maps[1], nil
}

func safeName(s string) bool {
	return s != "." && s != ".." && filesNameRE.MatchString(s)
}
