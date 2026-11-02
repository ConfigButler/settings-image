// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// document is one decoded resource: a YAML (or JSON) mapping.
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

// decodeAll reads a file under resources/ as a YAML stream (JSON is YAML). A null or empty
// document is skipped; every other document must be a mapping. A key that appears twice in one
// mapping, an anchor or alias, and a merge key are refused: each lets two readers see different
// resources in the same bytes.
func decodeAll(p string, b []byte) ([]document, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var docs []document
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, refuse(p, "does not parse as YAML or JSON: %v", err)
		}
		v, err := nodeValue(&n)
		if err != nil {
			return nil, refuse(p, "%v", err)
		}
		switch v := v.(type) {
		case nil:
			continue
		case map[string]any:
			docs = append(docs, v)
		default:
			return nil, refuse(p, "line %d: a document must be a mapping (a resource), not %T; "+
				"put other files in raw/", n.Line, v)
		}
	}
}

// nodeValue converts a node to plain Go values: map[string]any, []any, and scalars as yaml.v3
// decodes them into any (string, int, float64, bool, nil).
func nodeValue(n *yaml.Node) (any, error) {
	if n.Anchor != "" {
		return nil, fmt.Errorf("line %d: anchors and aliases are not supported", n.Line)
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil //nolint:nilnil // an empty document is a null value, and nothing wrong
		}
		return nodeValue(n.Content[0])
	case yaml.MappingNode:
		return mappingValue(n)
	case yaml.SequenceNode:
		items := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := nodeValue(c)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		return items, nil
	case yaml.ScalarNode:
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, fmt.Errorf("line %d: %w", n.Line, err)
		}
		return v, nil
	case yaml.AliasNode:
		return nil, fmt.Errorf("line %d: anchors and aliases are not supported", n.Line)
	default:
		return nil, fmt.Errorf("line %d: an unexpected YAML node", n.Line)
	}
}

func mappingValue(n *yaml.Node) (map[string]any, error) {
	// A mapping node's content alternates key, value.
	m := map[string]any{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		if k.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("line %d: a mapping key must be a scalar", k.Line)
		}
		if k.Tag == "!!merge" {
			return nil, fmt.Errorf("line %d: merge keys (<<) are not supported", k.Line)
		}
		if _, dup := m[k.Value]; dup {
			return nil, fmt.Errorf("line %d: the key %q appears twice in one mapping", k.Line, k.Value)
		}
		v, err := nodeValue(n.Content[i+1])
		if err != nil {
			return nil, err
		}
		m[k.Value] = v
	}
	return m, nil
}
