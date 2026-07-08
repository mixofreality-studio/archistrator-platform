package modelgen

import (
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// goType maps a resolved schema node to its Go type.
func goType(s *jsonschema.Schema) string {
	if s == nil {
		return "interface{}"
	}
	// A well-known foundational type (time.Time, uuid.UUID, …) binds directly to
	// its canonical Go type via x-go-type, pulling in x-go-import.
	if s.Extra != nil {
		if gt, ok := s.Extra["x-go-type"].(string); ok && gt != "" {
			if imp, ok := s.Extra["x-go-import"].(string); ok && imp != "" {
				pendingImports[imp] = ""
			}
			return gt
		}
	}
	if s.Ref != "" {
		return refName(s.Ref)
	}
	return goTypeForType(s)
}

// goTypeForType maps a schema's JSON type (its effectiveType) to the Go type,
// recursing for arrays and open objects.
func goTypeForType(s *jsonschema.Schema) string {
	t := effectiveType(s)
	nullable := hasNull(s)
	switch t {
	case "string":
		return "string"
	case "integer":
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		return "[]" + goType(s.Items)
	case "object":
		if len(s.Properties) == 0 && s.AdditionalProperties != nil {
			return "map[string]" + goType(s.AdditionalProperties)
		}
		// An open/arbitrary object (e.g. "a JSON Schema"): a generic JSON value.
		return "map[string]interface{}"
	default:
		_ = nullable
		return "interface{}"
	}
}

// effectiveType returns the schema's JSON type, ignoring a "null" member of a
// type union (nullability is handled separately via pointers).
func effectiveType(s *jsonschema.Schema) string {
	if s.Type != "" {
		return s.Type
	}
	for _, t := range s.Types {
		if t != "null" {
			return t
		}
	}
	return ""
}

func hasNull(s *jsonschema.Schema) bool {
	for _, t := range s.Types {
		if t == "null" {
			return true
		}
	}
	return false
}

// isValueType reports whether t is a Go value type that needs a pointer to be
// nilable (slices/maps/interfaces are already nilable).
func isValueType(t string) bool {
	return !strings.HasPrefix(t, "[]") &&
		!strings.HasPrefix(t, "map[") &&
		!strings.HasPrefix(t, "*") &&
		t != "interface{}"
}

// refName extracts the type name from a "#/$defs/Name" reference.
func refName(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

// exportName upper-cases the first rune so the field is exported. Property names
// in our contracts are already Go identifiers; this keeps them valid + exported.
func exportName(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
