package webgen

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ParseYAML reads a YAML (or JSON) document into a Value the way js-yaml's
// default schema loads it: mappings become objects in document order, scalars
// resolve by the YAML 1.2 core tags, and a timestamp becomes the ISO string that
// JSON.stringify writes for the Date js-yaml would build. Merge keys are refused
// rather than half-supported.
func ParseYAML(src []byte) (Value, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return nil, fmt.Errorf("webgen: parse YAML: %w", err)
	}
	if root.Kind == 0 {
		return nil, nil
	}
	n := &root
	if n.Kind == yaml.DocumentNode {
		n = n.Content[0]
	}
	return fromNode(n)
}

func fromNode(n *yaml.Node) (Value, error) {
	switch n.Kind {
	case yaml.MappingNode:
		return fromMapping(n)
	case yaml.SequenceNode:
		return fromSequence(n)
	case yaml.AliasNode:
		return fromNode(n.Alias)
	case yaml.ScalarNode:
		return fromScalar(n)
	}
	return nil, fmt.Errorf("webgen: line %d: unsupported YAML node kind %d", n.Line, n.Kind)
}

func fromMapping(n *yaml.Node) (Value, error) {
	o := NewObject()
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.ShortTag() == "!!merge" {
			return nil, fmt.Errorf("webgen: line %d: YAML merge keys are not supported", k.Line)
		}
		key, err := mappingKey(k)
		if err != nil {
			return nil, err
		}
		val, err := fromNode(v)
		if err != nil {
			return nil, err
		}
		o.Set(key, val)
	}
	return o, nil
}

// mappingKey is String(key) for the scalar js-yaml would resolve.
func mappingKey(k *yaml.Node) (string, error) {
	if k.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("webgen: line %d: a non-scalar mapping key", k.Line)
	}
	v, err := fromScalar(k)
	if err != nil {
		return "", err
	}
	switch x := v.(type) {
	case nil:
		return "null", nil
	case string:
		return x, nil
	case float64:
		return jsNumber(x), nil
	default:
		return fmt.Sprint(x), nil
	}
}

func fromSequence(n *yaml.Node) (Value, error) {
	out := make([]Value, 0, len(n.Content))
	for _, c := range n.Content {
		v, err := fromNode(c)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func fromScalar(n *yaml.Node) (Value, error) {
	switch n.ShortTag() {
	case "!!null":
		return nil, nil
	case "!!bool":
		return strings.EqualFold(n.Value, "true"), nil
	case "!!int":
		return parseYAMLInt(n)
	case "!!float":
		return parseYAMLFloat(n)
	case "!!timestamp":
		return parseYAMLTimestamp(n)
	case "!!str":
		return n.Value, nil
	}
	return nil, fmt.Errorf("webgen: line %d: unsupported YAML tag %s", n.Line, n.ShortTag())
}

func parseYAMLInt(n *yaml.Node) (Value, error) {
	s := strings.ReplaceAll(n.Value, "_", "")
	if strings.HasPrefix(s, "0o") {
		s = "0" + s[2:]
	}
	i, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		return nil, fmt.Errorf("webgen: line %d: int %q: %w", n.Line, n.Value, err)
	}
	return float64(i), nil
}

func parseYAMLFloat(n *yaml.Node) (Value, error) {
	switch strings.ToLower(n.Value) {
	case ".inf", "+.inf":
		return math.Inf(1), nil
	case "-.inf":
		return math.Inf(-1), nil
	case ".nan":
		return math.NaN(), nil
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(n.Value, "_", ""), 64)
	if err != nil {
		return nil, fmt.Errorf("webgen: line %d: float %q: %w", n.Line, n.Value, err)
	}
	return f, nil
}

var timestampLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"}

func parseYAMLTimestamp(n *yaml.Node) (Value, error) {
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, n.Value); err == nil {
			return t.UTC().Format("2006-01-02T15:04:05.000Z"), nil
		}
	}
	return nil, fmt.Errorf("webgen: line %d: unsupported timestamp %q", n.Line, n.Value)
}
