package modelgen

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
)

// emitEnum writes a named scalar type plus its const block, modeling a JSON
// Schema `enum`. Behavior (String/valid) is NOT emitted here — by founder rule the
// generated type is used AS-IS and any behavior is a free function over the value.
func emitEnum(buf *bytes.Buffer, name string, s *jsonschema.Schema) {
	base := enumBase(s)
	fmt.Fprintf(buf, "type %s %s\n", name, base)

	var names []string
	if raw, ok := s.Extra["x-enum-varnames"].([]any); ok {
		for _, n := range raw {
			if str, ok := n.(string); ok {
				names = append(names, str)
			}
		}
	}
	if len(names) != len(s.Enum) || len(names) == 0 {
		return // no const names to bind — leave the bare type
	}
	buf.WriteString("\nconst (\n")
	for i, v := range s.Enum {
		fmt.Fprintf(buf, "\t%s %s = %s\n", names[i], name, enumLiteral(v, base))
	}
	buf.WriteString(")\n")
}

// enumBase returns the Go underlying type for an enum (x-go-base wins; else mapped
// from the JSON type).
func enumBase(s *jsonschema.Schema) string {
	if b, ok := s.Extra["x-go-base"].(string); ok && b != "" {
		return b
	}
	switch effectiveType(s) {
	case "string":
		return "string"
	case "number":
		return "float64"
	default:
		return "int64"
	}
}

// enumLiteral renders one enum value as a Go literal for the base type.
func enumLiteral(v any, base string) string {
	if base == "string" {
		return fmt.Sprintf("%q", v)
	}
	if f, ok := v.(float64); ok {
		return strconv.FormatInt(int64(f), 10)
	}
	return fmt.Sprintf("%v", v)
}
