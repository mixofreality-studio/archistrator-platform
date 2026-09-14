package webgen

import (
	"fmt"
	"strings"
)

// Skeleton returns a fixture file for one screen state: route, and for each op a
// `result` that is the SMALLEST value its schema admits (required properties
// only, empty arrays, zero numbers, the first enum value, null wherever null is
// allowed). It is schema-valid and deliberately empty of meaning: the
// ui-designer fills in plausible domain values and adds the pending and error
// states (design §2′.1, "skeletons, not content").
//
// schema is the generated fixture schema (FixtureSchema), so a skeleton can
// name only ops the OpsClient has.
func Skeleton(schema Value, route string, opIDs []string) (Value, error) {
	if !strings.HasPrefix(route, "/") {
		return nil, fmt.Errorf("webgen: skeleton: route %q must start with /", route)
	}
	s := skel{defs: obj(path(schema, "definitions"))}
	ops := NewObject()
	for _, op := range opIDs {
		result, err := s.opResult(schema, op)
		if err != nil {
			return nil, err
		}
		ops.Set(op, NewObject().Set("result", result))
	}
	return NewObject().Set("route", route).Set("ops", ops), nil
}

type skel struct {
	defs  *Object
	stack []string // the $refs being expanded, for cycle detection
}

func (s *skel) opResult(schema Value, op string) (Value, error) {
	answer, ok := obj(path(schema, "properties", "ops", "properties")).getOr(op, nil)
	if !ok {
		return nil, fmt.Errorf("webgen: skeleton: %q is not an OpsClient op", op)
	}
	branches, _ := path(answer, "oneOf").([]Value)
	if len(branches) == 0 {
		return nil, fmt.Errorf("webgen: skeleton: %q has no answer schema", op)
	}
	return s.minimal(path(branches[0], "properties", "result"), op)
}

// minimal is the smallest instance of schema; at names the position for errors.
func (s *skel) minimal(schema Value, at string) (Value, error) {
	o := obj(schema)
	if o == nil {
		return nil, nil
	}
	if v, ok, err := s.fixed(o, at); ok || err != nil {
		return v, err
	}
	if branches, ok := combinator(o); ok {
		return s.firstBranch(branches, at)
	}
	return s.byType(o, at)
}

// fixed handles a $ref, a const and an enum: the value is decided by the schema.
func (s *skel) fixed(o *Object, at string) (Value, bool, error) {
	if ref, ok := o.Get("$ref"); ok {
		v, err := s.ref(ref, at)
		return v, true, err
	}
	if c, ok := o.Get("const"); ok {
		return c, true, nil
	}
	if e, ok := o.Get("enum"); ok {
		if vals, _ := e.([]Value); len(vals) > 0 {
			return vals[0], true, nil
		}
	}
	return nil, false, nil
}

func combinator(o *Object) ([]Value, bool) {
	for _, k := range []string{"anyOf", "oneOf", "allOf"} {
		if v, ok := o.Get(k); ok {
			bs, _ := v.([]Value)
			return bs, true
		}
	}
	return nil, false
}

// firstBranch prefers null when any branch admits it (the smallest instance, and
// it ends recursion through nullable refs); otherwise the first branch.
func (s *skel) firstBranch(branches []Value, at string) (Value, error) {
	for _, b := range branches {
		if admitsNull(obj(b)) {
			return nil, nil
		}
	}
	if len(branches) == 0 {
		return nil, fmt.Errorf("webgen: skeleton: %s: an empty combinator", at)
	}
	return s.minimal(branches[0], at)
}

func admitsNull(o *Object) bool {
	if o == nil {
		return false
	}
	t, _ := o.Get("type")
	switch x := t.(type) {
	case string:
		return x == "null"
	case []Value:
		for _, e := range x {
			if e == "null" {
				return true
			}
		}
	}
	return false
}

func (s *skel) ref(ref Value, at string) (Value, error) {
	r, _ := ref.(string)
	name, ok := strings.CutPrefix(r, "#/definitions/")
	def, found := s.defs.getOr(name, nil)
	if !ok || !found {
		return nil, fmt.Errorf("webgen: skeleton: %s: unresolvable $ref %q", at, r)
	}
	for _, seen := range s.stack {
		if seen == name {
			return nil, fmt.Errorf("webgen: skeleton: %s: a required cycle through %s", at, name)
		}
	}
	s.stack = append(s.stack, name)
	defer func() { s.stack = s.stack[:len(s.stack)-1] }()
	return s.minimal(def, name)
}

func (s *skel) byType(o *Object, at string) (Value, error) {
	if admitsNull(o) {
		return nil, nil
	}
	switch t := schemaType(o); t {
	case "object":
		return s.object(o, at)
	case "array":
		return s.array(o, at)
	case "string":
		return minimalString(o), nil
	case "integer", "number":
		return minimalNumber(o, t == "integer"), nil
	case "boolean":
		return false, nil
	}
	return nil, nil // no type: anything goes, and null is the smallest thing
}

// schemaType is the schema's (first) type, "object" for an untyped schema with
// properties, and "" when it has neither.
func schemaType(o *Object) string {
	t, _ := o.Get("type")
	if ts, ok := t.([]Value); ok && len(ts) > 0 {
		t = ts[0]
	}
	if s, ok := t.(string); ok {
		return s
	}
	if _, hasProps := o.Get("properties"); hasProps {
		return "object"
	}
	return ""
}

func (s *skel) object(o *Object, at string) (Value, error) {
	out := NewObject()
	required, _ := path(o, "required").([]Value)
	for _, r := range required {
		name, _ := r.(string)
		v, err := s.minimal(path(o, "properties", name), at+"."+name)
		if err != nil {
			return nil, err
		}
		out.Set(name, v)
	}
	return out, nil
}

func (s *skel) array(o *Object, at string) (Value, error) {
	n, _ := path(o, "minItems").(float64)
	out := []Value{}
	for i := 0; i < int(n); i++ {
		v, err := s.minimal(path(o, "items"), at+"[]")
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

var formatSamples = map[string]string{
	"date-time": "1970-01-01T00:00:00Z",
	"date":      "1970-01-01",
	"uuid":      "00000000-0000-0000-0000-000000000000",
	"uri":       "https://example.invalid/",
	"email":     "user@example.invalid",
}

func minimalString(o *Object) Value {
	if f, _ := path(o, "format").(string); formatSamples[f] != "" {
		return formatSamples[f]
	}
	n, _ := path(o, "minLength").(float64)
	return strings.Repeat("x", int(n))
}

func minimalNumber(o *Object, integer bool) Value {
	if m, ok := path(o, "minimum").(float64); ok && m > 0 {
		return m
	}
	if m, ok := path(o, "exclusiveMinimum").(float64); ok && m >= 0 {
		if integer {
			return m + 1
		}
		return m + 0.5
	}
	return 0.0
}
