package modelgen

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func schemaOf(t *testing.T, raw string) *jsonschema.Schema {
	t.Helper()
	var s jsonschema.Schema
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	return &s
}

// TestGoTypeFor pins the outside-the-package spelling testgen relies on: $defs
// and bare exported x-go-types are alias-qualified, imported x-go-types keep
// their selector and report their import, primitives are bare — and the
// package's one-pass emission state is untouched afterwards.
func TestGoTypeFor(t *testing.T) {
	pendingImports["sentinel"] = "keep"
	defer delete(pendingImports, "sentinel")
	cases := []struct {
		raw, typ string
		imports  []string
	}{
		{`{"$ref":"#/$defs/Order"}`, "order.Order", nil},
		{`{"type":"array","items":{"$ref":"#/$defs/Order"}}`, "[]order.Order", nil},
		{`{"type":"string","x-go-type":"uuid.UUID","x-go-import":"github.com/google/uuid"}`, "uuid.UUID", []string{"github.com/google/uuid"}},
		{`{"type":"string","x-go-type":"Receipt"}`, "order.Receipt", nil},
		{`{"type":"integer"}`, "int64", nil},
	}
	for _, c := range cases {
		typ, imports := GoTypeFor(schemaOf(t, c.raw), "order")
		if typ != c.typ || len(imports) != len(c.imports) || (len(imports) > 0 && imports[0] != c.imports[0]) {
			t.Errorf("GoTypeFor(%s) = %q %v, want %q %v", c.raw, typ, imports, c.typ, c.imports)
		}
	}
	if pendingImports["sentinel"] != "keep" || len(pendingImports) != 1 || fakeQualifyAlias != "" {
		t.Errorf("GoTypeFor leaked emission state: imports=%v alias=%q", pendingImports, fakeQualifyAlias)
	}
}
