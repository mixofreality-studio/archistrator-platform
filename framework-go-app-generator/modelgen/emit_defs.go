package modelgen

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/google/jsonschema-go/jsonschema"
)

// emitDefs writes every model `$def` (sum types + ordinary structs) in sorted
// order and returns the sorted def names (its count is the "models" tally).
func emitDefs(buf *bytes.Buffer, doc *jsonschema.Schema, order map[string][]string) ([]string, error) {
	names := make([]string, 0, len(doc.Defs))
	for n := range doc.Defs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if st, ok := sumTypeOf(doc.Defs[n]); ok {
			// A sum `$def` is not a data struct: emit the sealed interface, the
			// per-variant marker + discriminator methods, and the envelope codec.
			// Its variant structs are emitted as ordinary `$def`s in this same loop.
			emitSumType(buf, st)
			buf.WriteByte('\n')
			continue
		}
		if err := emitType(buf, n, doc.Defs[n], order[n]); err != nil {
			return nil, fmt.Errorf("emit %s: %w", n, err)
		}
		buf.WriteByte('\n')
	}
	return names, nil
}

// emitType writes one named type. Enum schemas become a named scalar + a const
// block; object schemas become structs; everything else becomes a named alias.
func emitType(buf *bytes.Buffer, name string, s *jsonschema.Schema, order []string) error {
	if len(s.Enum) > 0 && effectiveType(s) != "object" {
		emitEnum(buf, name, s)
		return nil
	}
	if effectiveType(s) != "object" || len(s.Properties) == 0 {
		buf.WriteString("type " + name + " " + goType(s) + "\n")
		return nil
	}
	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	keys := structKeys(s, order)
	fmt.Fprintf(buf, "type %s struct {\n", name)
	for _, k := range keys {
		ps, ok := s.Properties[k]
		if !ok {
			continue
		}
		emitStructField(buf, k, ps, required[k])
	}
	buf.WriteString("}\n")
	return nil
}

// structKeys returns the property order to emit: the recorded document order,
// falling back to a stable alphabetical order when none was recovered.
func structKeys(s *jsonschema.Schema, order []string) []string {
	if len(order) > 0 {
		return order
	}
	keys := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// emitStructField writes one struct field: its Go name (x-go-name when present,
// else derived from the wire key), its Go type (pointer-wrapped + ,omitempty when
// optional), and the json tag carrying the wire key.
func emitStructField(buf *bytes.Buffer, k string, ps *jsonschema.Schema, isRequired bool) {
	gt := goType(ps)
	opt := !isRequired
	tag := k
	if opt {
		if isValueType(gt) {
			gt = "*" + gt
		}
		tag += ",omitempty"
	}
	// Go field name: the recorded original (x-go-name) when present, else
	// derived from the wire key. The json tag always carries the wire key.
	goName := exportName(k)
	if ps.Extra != nil {
		if n, ok := ps.Extra["x-go-name"].(string); ok && n != "" {
			goName = n
		}
	}
	fmt.Fprintf(buf, "\t%s %s `json:%q`\n", goName, gt, tag)
}
