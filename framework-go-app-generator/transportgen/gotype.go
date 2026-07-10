package transportgen

import (
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// goType resolves a schema node to the Go type the emitted SDK references in its
// single flat package. Unlike projectmodel.GoType (which qualifies $refs with a
// manager package alias), the SDK carries every DTO in-package, so a $ref
// resolves to its bare $def name — remapped through rename when that name is a
// per-manager conflict type prefixed away from a colliding sibling. With
// uuidAsString a uuid-bound scalar collapses to plain string, keeping the SDK
// free of the non-stdlib github.com/google/uuid dependency (the wire form of a
// uuid is its canonical string, so this is wire-identical).
func goType(n *projectmodel.SchemaNode, ptr bool, rename map[string]string, uuidAsString bool) string {
	t := goTypeInner(n, rename, uuidAsString)
	if ptr {
		return "*" + t
	}
	return t
}

func goTypeInner(n *projectmodel.SchemaNode, rename map[string]string, uuidAsString bool) string {
	if n == nil {
		return "any"
	}
	if uuidAsString && (n.XGoType == "uuid.UUID" || n.Format == "uuid") {
		return "string"
	}
	if n.XGoType != "" {
		return n.XGoType
	}
	if name := n.RefName(); name != "" {
		if r, ok := rename[name]; ok {
			return r
		}
		return name
	}
	return primitiveType(n, rename, uuidAsString)
}

// primitiveType maps an inline (non-$ref, non-x-go-type) schema node to its Go
// type, recursing on array items.
func primitiveType(n *projectmodel.SchemaNode, rename map[string]string, uuidAsString bool) string {
	switch n.Type.Primary() {
	case "string":
		return "string"
	case "integer":
		return "int"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		return "[]" + goTypeInner(n.Items, rename, uuidAsString)
	case "object":
		return "map[string]any"
	default:
		return "any"
	}
}

// jsonTag builds the json struct tag for a request/input field: an optional
// (pointer) param gets ,omitempty so a nil value is dropped from the wire — the
// hand transports' conditional-send behavior for optional feedback/scope args.
func jsonTag(name string, pointer bool) string {
	if pointer {
		return name + ",omitempty"
	}
	return name
}

// upperFirst upper-cases the first rune (the Go export convention modelgen and
// mcpgen both use to turn a wire param name into a struct field name).
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}
