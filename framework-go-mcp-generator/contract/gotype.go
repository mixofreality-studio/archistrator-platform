package contract

// GoType resolves a schema node to the Go type the generated code must use,
// qualifying $ref types with the manager package alias (e.g. "mgr"). Inline
// primitives map to their Go counterpart; arrays recurse on items. A leading
// pointer is applied when ptr is set.
func GoType(n *SchemaNode, ptr bool, managerAlias string) string {
	t := goTypeInner(n, managerAlias)
	if ptr {
		return "*" + t
	}
	return t
}

func goTypeInner(n *SchemaNode, managerAlias string) string {
	if n == nil {
		return "any"
	}
	// A foundational type binds directly to its canonical Go type via x-go-type
	// (uuid.UUID, time.Time, []byte, json.RawMessage, ...), exactly as the server's
	// modelgen does. The matching x-go-import is wired into the file separately.
	if n.XGoType != "" {
		return n.XGoType
	}
	if name := n.RefName(); name != "" {
		if managerAlias == "" {
			return name
		}
		return managerAlias + "." + name
	}
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
		return "[]" + goTypeInner(n.Items, managerAlias)
	case "object":
		return "map[string]any"
	default:
		return "any"
	}
}

// IsArray reports whether the node is an inline array schema.
func (n *SchemaNode) IsArray() bool { return n != nil && n.Ref == "" && n.Type.Primary() == "array" }
