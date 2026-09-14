package webgen

import "strings"

// The preview fixtures' JSON Schema, generated from the server OAS
// (design-renderer-data.md §2′.1, "Sync is enforced mechanically"). One fixture
// file is one screen state:
//
//	{ "route": "/project/demo/construction",
//	  "ops": { "<OpId>": { "result": <the op's 200 body> }
//	                   | { "error": { "status": 500, "code": "…", "message": "…" } }
//	                   | { "pending": true } } }
//
// The ops keys are exactly the OpsClient's OpIds, so a fixture cannot answer an
// op the transport does not have, and each result is checked against that op's
// OAS 200 schema (a composition route's against its config schema). The OAS
// component schemas are carried over as draft-07 definitions.
//
// The OAS is read the way openapi-typescript reads it, which is the TS the app
// compiles against: a $ref stands ALONE (its siblings are dropped), and a oneOf
// is read as anyOf. The strict OAS 3.1 reading would reject real data: the
// emitter writes a nullable ref as anyOf:[{$ref, type:["null"]},{type:"null"}],
// whose first branch admits only null under 3.1, and the 14-kind slot-model
// union has no discriminator, so an empty {items: []} matches four kinds.

const oasRefPrefix = "#/components/schemas/"

// FixtureSchema builds the fixture JSON Schema; generator names the tool in the
// schema's description.
func FixtureSchema(in Inputs, bindings []Binding) Value {
	ops := NewObject()
	routes := map[string]Value{}
	for _, r := range in.Config.Composition {
		routes[r.OpID] = r.Result
	}
	for _, b := range bindings {
		ops.Set(b.OpID, opAnswerSchema(in.OAS, b, routes))
	}
	schemas, _ := obj(path(in.OAS, "components")).getOr("schemas", NewObject())
	return NewObject().
		Set("$schema", "http://json-schema.org/draft-07/schema#").
		Set("title", in.Config.App+" preview fixture (one screen state)").
		Set("description", "Generated from "+in.Config.OAS+" by framework-go-app-generator/webgen. "+
			"Each ops key is an OpsClient OpId; each answer is a result, an error, or pending.").
		Set("type", "object").
		Set("required", []Value{"route", "ops"}).
		Set("additionalProperties", false).
		Set("properties", NewObject().
			Set("$schema", typeOnly("string")).
			Set("route", typeOnly("string").Set("pattern", "^/")).
			Set("title", typeOnly("string")).
			Set("note", typeOnly("string")).
			Set("ops", typeOnly("object").Set("additionalProperties", false).Set("properties", ops))).
		Set("definitions", rewriteRefs(schemas))
}

func (o *Object) getOr(key string, def Value) (Value, bool) {
	if o == nil {
		return def, false
	}
	if v, ok := o.Get(key); ok && v != nil {
		return v, true
	}
	return def, false
}

func typeOnly(t string) *Object { return NewObject().Set("type", t) }

func opAnswerSchema(doc Value, b Binding, routes map[string]Value) Value {
	return NewObject().
		Set("description", b.Method+" "+b.Path).
		Set("oneOf", []Value{
			answerBranch("result", resultSchema(doc, b, routes)),
			answerBranch("error", errorSchema()),
			answerBranch("pending", NewObject().Set("const", true)),
		})
}

func answerBranch(key string, schema Value) Value {
	return typeOnly("object").
		Set("required", []Value{key}).
		Set("additionalProperties", false).
		Set("properties", NewObject().Set(key, schema))
}

func errorSchema() Value {
	return typeOnly("object").
		Set("required", []Value{"status"}).
		Set("additionalProperties", false).
		Set("properties", NewObject().
			Set("status", typeOnly("integer").Set("minimum", 400.0).Set("maximum", 599.0)).
			Set("code", typeOnly("string")).
			Set("message", typeOnly("string")))
}

// resultSchema is the op's 200 application/json schema, or {} for a void op
// (a 204 has no body; its fixture's result is never read, so anything passes).
func resultSchema(doc Value, b Binding, routes map[string]Value) Value {
	if r, ok := routes[b.OpID]; ok && b.Composition {
		return r
	}
	s := path(doc, "paths", b.Path, strings.ToLower(b.Method), "responses", "200", "content", "application/json", "schema")
	if s == nil {
		return NewObject()
	}
	return rewriteRefs(s)
}

// rewriteRefs moves component refs to #/definitions/, lets a $ref stand alone,
// and reads oneOf as anyOf, recursively.
func rewriteRefs(v Value) Value {
	switch x := v.(type) {
	case []Value:
		out := make([]Value, len(x))
		for i, e := range x {
			out[i] = rewriteRefs(e)
		}
		return out
	case *Object:
		return rewriteObjectRefs(x)
	}
	return v
}

func rewriteObjectRefs(o *Object) Value {
	if ref, ok := o.Get("$ref"); ok {
		return NewObject().Set("$ref", draft07Ref(ref))
	}
	out := NewObject()
	for _, k := range o.Keys() {
		v, _ := o.Get(k)
		if k == "oneOf" {
			k = "anyOf"
		}
		out.Set(k, rewriteRefs(v))
	}
	return out
}

func draft07Ref(ref Value) Value {
	if s, ok := ref.(string); ok && strings.HasPrefix(s, oasRefPrefix) {
		return "#/definitions/" + strings.TrimPrefix(s, oasRefPrefix)
	}
	return ref
}
