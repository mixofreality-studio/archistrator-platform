package modelgen

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// sumTypeOf decodes the `x-go-sumtype` descriptor from a `$def`, if present. A
// sum `$def` carries the descriptor under Extra alongside its `oneOf` variant list.
func sumTypeOf(s *jsonschema.Schema) (SumType, bool) {
	if s == nil || s.Extra == nil {
		return SumType{}, false
	}
	raw, ok := s.Extra["x-go-sumtype"]
	if !ok {
		return SumType{}, false
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return SumType{}, false
	}
	var st SumType
	if err := json.Unmarshal(b, &st); err != nil {
		return SumType{}, false
	}
	return st, true
}

// emitSumType writes a sealed-interface (discriminated-union) type: the sealed
// interface itself (with a //sumtype:decl directive for go-check-sumtype), the
// per-variant marker + discriminator methods that seal the sum, and an envelope
// codec (Marshal<X>/Unmarshal<X>) that round-trips a value through the
// {discriminatorKey: <kind>, "model": <variant>} wire form — byte-identical to the
// hand-written codec.
func emitSumType(buf *bytes.Buffer, st SumType) {
	pendingImports["encoding/json"] = ""
	pendingImports["fmt"] = ""

	env := st.Iface + "Envelope"
	kindStrFn := "kindString" + st.Iface
	factory := "new" + st.Iface + "ForKind"

	emitSumInterface(buf, st)
	emitSumVariantMethods(buf, st)
	emitSumEnvelope(buf, st, env)
	emitSumKindString(buf, st, kindStrFn)
	emitSumMarshal(buf, st, env, kindStrFn)
	emitSumFactory(buf, st, factory)
	emitSumUnmarshal(buf, st, env, factory)
}

// emitSumInterface writes the sealed interface. //sumtype:decl makes
// go-check-sumtype enforce exhaustiveness of switches over it.
func emitSumInterface(buf *bytes.Buffer, st SumType) {
	fmt.Fprintf(buf, "// %s is the generated sealed sum interface for this contract.\n", st.Iface)
	fmt.Fprintf(buf, "//\n//sumtype:decl\n")
	fmt.Fprintf(buf, "type %s interface {\n", st.Iface)
	fmt.Fprintf(buf, "\tKind() %s\n", st.KindEnum)
	fmt.Fprintf(buf, "\t%s() // unexported marker: seals the sum to this package's variants\n", st.Marker)
	buf.WriteString("}\n\n")
}

// emitSumVariantMethods writes the per-variant marker + discriminator methods
// (pointer receivers — variants are referenced as *T, matching the hand-written
// convention).
func emitSumVariantMethods(buf *bytes.Buffer, st SumType) {
	for _, v := range st.Variants {
		fmt.Fprintf(buf, "func (*%s) %s() {}\n", v.Type, st.Marker)
		fmt.Fprintf(buf, "func (*%s) Kind() %s { return %s }\n\n", v.Type, st.KindEnum, v.KindConst)
	}
}

// emitSumEnvelope writes the wire form
// {discriminatorKey: <kindStr>, "model": <variant json>}.
func emitSumEnvelope(buf *bytes.Buffer, st SumType, env string) {
	fmt.Fprintf(buf, "// %s is the wire form of a %s: the string kind discriminator + the\n", env, st.Iface)
	fmt.Fprintf(buf, "// concrete variant's own JSON under \"model\".\n")
	fmt.Fprintf(buf, "type %s struct {\n", env)
	fmt.Fprintf(buf, "\tKind  string `json:%q`\n", st.DiscriminatorKey)
	fmt.Fprintf(buf, "\tModel json.RawMessage `json:%q`\n", "model,omitempty")
	buf.WriteString("}\n\n")
}

// emitSumKindString writes kindString<X>: maps a variant's typed discriminator
// value to its wire string.
func emitSumKindString(buf *bytes.Buffer, st SumType, kindStrFn string) {
	fmt.Fprintf(buf, "// %s maps a variant's discriminator value to its wire kind string.\n", kindStrFn)
	fmt.Fprintf(buf, "func %s(k %s) (string, bool) {\n", kindStrFn, st.KindEnum)
	fmt.Fprintf(buf, "\tswitch k {\n")
	for _, v := range st.Variants {
		fmt.Fprintf(buf, "\tcase %s:\n\t\treturn %q, true\n", v.KindConst, v.Kind)
	}
	fmt.Fprintf(buf, "\tdefault:\n\t\treturn \"\", false\n")
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "}\n\n")
}

// emitSumMarshal writes Marshal<X>: encode any variant value to the envelope
// bytes. The kind is a plain STRING on the wire, so the codec is self-contained
// and byte-identical to the hand-written form WITHOUT depending on a MarshalJSON
// on the enum (generated enums carry no behavior — founder rule).
func emitSumMarshal(buf *bytes.Buffer, st SumType, env, kindStrFn string) {
	fmt.Fprintf(buf, "// Marshal%s encodes a %s to its {%q,\"model\"} envelope, byte-identical to\n", st.Iface, st.Iface, st.DiscriminatorKey)
	fmt.Fprintf(buf, "// the hand-written wire form. A nil value encodes as the zero envelope.\n")
	fmt.Fprintf(buf, "func Marshal%s(v %s) ([]byte, error) {\n", st.Iface, st.Iface)
	fmt.Fprintf(buf, "\tif v == nil {\n")
	fmt.Fprintf(buf, "\t\treturn json.Marshal(%s{})\n", env)
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "\tkind, ok := %s(v.Kind())\n", kindStrFn)
	fmt.Fprintf(buf, "\tif !ok {\n")
	fmt.Fprintf(buf, "\t\treturn nil, fmt.Errorf(\"marshal %s: no wire kind for %%v\", v.Kind())\n", st.Iface)
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "\traw, err := json.Marshal(v)\n")
	fmt.Fprintf(buf, "\tif err != nil {\n")
	fmt.Fprintf(buf, "\t\treturn nil, fmt.Errorf(\"marshal %s %%v: %%w\", v.Kind(), err)\n", st.Iface)
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "\treturn json.Marshal(%s{Kind: kind, Model: raw})\n", env)
	fmt.Fprintf(buf, "}\n\n")
}

// emitSumFactory writes new<X>ForKind: factory selecting the concrete variant by
// wire kind string.
func emitSumFactory(buf *bytes.Buffer, st SumType, factory string) {
	fmt.Fprintf(buf, "// %s returns a freshly-allocated zero-value concrete variant for\n", factory)
	fmt.Fprintf(buf, "// the wire kind string, suitable for json.Unmarshal, or (nil,false) if unknown.\n")
	fmt.Fprintf(buf, "func %s(kind string) (%s, bool) {\n", factory, st.Iface)
	fmt.Fprintf(buf, "\tswitch kind {\n")
	for _, v := range st.Variants {
		fmt.Fprintf(buf, "\tcase %q:\n\t\treturn &%s{}, true\n", v.Kind, v.Type)
	}
	fmt.Fprintf(buf, "\tdefault:\n\t\treturn nil, false\n")
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "}\n\n")
}

// emitSumUnmarshal writes Unmarshal<X>: decode envelope bytes back to the
// concrete variant.
func emitSumUnmarshal(buf *bytes.Buffer, st SumType, env, factory string) {
	fmt.Fprintf(buf, "// Unmarshal%s decodes a {%q,\"model\"} envelope back into the concrete\n", st.Iface, st.DiscriminatorKey)
	fmt.Fprintf(buf, "// variant selected by its kind. An empty model payload decodes to a nil value.\n")
	fmt.Fprintf(buf, "func Unmarshal%s(data []byte) (%s, error) {\n", st.Iface, st.Iface)
	fmt.Fprintf(buf, "\tvar env %s\n", env)
	fmt.Fprintf(buf, "\tif err := json.Unmarshal(data, &env); err != nil {\n")
	fmt.Fprintf(buf, "\t\treturn nil, err\n")
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "\tif len(env.Model) == 0 {\n")
	fmt.Fprintf(buf, "\t\treturn nil, nil\n")
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "\tv, ok := %s(env.Kind)\n", factory)
	fmt.Fprintf(buf, "\tif !ok {\n")
	fmt.Fprintf(buf, "\t\treturn nil, fmt.Errorf(\"unmarshal %s: no variant for kind %%q\", env.Kind)\n", st.Iface)
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "\tif err := json.Unmarshal(env.Model, v); err != nil {\n")
	fmt.Fprintf(buf, "\t\treturn nil, fmt.Errorf(\"unmarshal %s %%q: %%w\", env.Kind, err)\n", st.Iface)
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "\treturn v, nil\n")
	fmt.Fprintf(buf, "}\n")
}
