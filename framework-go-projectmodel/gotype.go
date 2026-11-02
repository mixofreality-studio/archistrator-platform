package projectmodel

// GoType resolves a schema node to the Go type the generated code must use,
// qualifying $ref types with the manager package alias (e.g. "mgr"). Inline
// primitives map to their Go counterpart; arrays recurse on items. A leading
// pointer is applied when ptr is set.
//
// Every generator that calls a component (temporalgen, httpgen, mcpgen, clientgen) spells
// its types here, while the component's own interface is spelled by modelgen, so the two
// must agree on every inline schema or the caller does not compile. A differential test in
// modelgen (TestInlineSchemaMappersAgree) holds them together.
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
	return primitiveGoType(n, managerAlias)
}

// primitiveGoType maps an inline (non-$ref, non-x-go-type) schema node to its Go
// type, recursing on array items.
func primitiveGoType(n *SchemaNode, managerAlias string) string {
	switch n.Type.Primary() {
	case "string":
		return "string"
	case "integer":
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		return "[]" + goTypeInner(n.Items, managerAlias)
	case "object":
		return "map[string]" + objectValueType(n, managerAlias)
	default:
		return "any"
	}
}

// objectValueType is the value type of an inline object's map: the schema of its other keys
// when it has no properties of its own, and any otherwise.
func objectValueType(n *SchemaNode, managerAlias string) string {
	if len(n.Properties) > 0 || n.AdditionalProperties == nil || n.AdditionalProperties.Schema == nil {
		return "any"
	}
	return goTypeInner(n.AdditionalProperties.Schema, managerAlias)
}

// IsArray reports whether the node is an inline array schema.
func (n *SchemaNode) IsArray() bool { return n != nil && n.Ref == "" && n.Type.Primary() == "array" }
