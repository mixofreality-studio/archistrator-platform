package methodcheck

import (
	"encoding/json"
	"strings"
	"testing"
)

// describe is a switch over both TestArgValue variants with no default: the
// gochecksumtype lint (and the compiler for the variant set) keeps it exhaustive.
func describe(v TestArgValue) string {
	switch x := v.(type) {
	case LiteralValue:
		return "literal:" + string(x.JSON)
	case RefValue:
		return "ref:" + x.Path
	}
	return ""
}

// describeArrange is the same exhaustiveness pin over every ArrangeCall variant.
func describeArrange(a ArrangeCall) string {
	switch x := a.(type) {
	case ContractCall:
		return "contractCall:" + x.Component + "." + x.Operation
	case AdvanceClock:
		return "advanceClock:" + x.Duration
	case FailNext:
		return "failNext:" + x.ErrorCode
	case SeedResource:
		return "seedResource:" + x.Resource
	}
	return ""
}

// lit builds a literal arg value from the plan's historic string spelling: JSON text
// stays JSON (object, number, quoted string); anything else is a JSON string.
func lit(v string) TestArgValue {
	if json.Valid([]byte(v)) {
		return LiteralValue{JSON: json.RawMessage(v)}
	}
	b, _ := json.Marshal(v)
	return LiteralValue{JSON: b}
}

func TestBindings_DecodeLiteralAndRef(t *testing.T) {
	lit, err := UnmarshalTestArgValue([]byte(`{"kind":"literal","model":{"json":{"id":"o-1"}}}`))
	if err != nil {
		t.Fatalf("literal: %v", err)
	}
	if l, ok := lit.(LiteralValue); !ok || string(l.JSON) != `{"id":"o-1"}` {
		t.Fatalf("literal decoded as %#v", lit)
	}
	if lit.Kind() != TestArgLiteral {
		t.Fatalf("literal kind = %q", lit.Kind())
	}
	ref, err := UnmarshalTestArgValue([]byte(`{"kind":"ref","model":{"step":1,"path":"id","probe":2}}`))
	if err != nil {
		t.Fatalf("ref: %v", err)
	}
	r, ok := ref.(RefValue)
	if !ok || r.Step != 1 || r.Path != "id" || r.Probe == nil || *r.Probe != 2 {
		t.Fatalf("ref decoded as %#v", ref)
	}
	if got := describe(lit) + " " + describe(ref); got != `literal:{"id":"o-1"} ref:id` {
		t.Fatalf("describe = %s", got)
	}

	arr, err := UnmarshalArrangeCall([]byte(`{"kind":"advanceClock","model":{"duration":"90s"}}`))
	if err != nil {
		t.Fatalf("arrange: %v", err)
	}
	if got := describeArrange(arr); got != "advanceClock:90s" {
		t.Fatalf("describeArrange = %s", got)
	}
	cc, err := UnmarshalArrangeCall([]byte(`{"kind":"contractCall","model":{"component":"billingManager","operation":"Open","inputs":[{"name":"id","value":{"kind":"literal","model":{"json":"i-1"}}}],"expect":{"errorExpected":false}}}`))
	if err != nil {
		t.Fatalf("contractCall: %v", err)
	}
	if c := cc.(ContractCall); len(c.Inputs) != 1 || describe(c.Inputs[0].Value) != `literal:"i-1"` {
		t.Fatalf("contractCall inputs = %#v", c.Inputs)
	}

	// A full record round-trips through MarshalJSON in the same envelope.
	var tp TestPlanRecord
	raw := `{"component":"c","principals":{"alice":{"subject":"u-1","roles":["admin"]}},"bindings":[{"scenario":"s-P1","arrange":[{"kind":"failNext","model":{"component":"r","operation":"Get","errorCode":"Boom"}}],"steps":[{"seq":1,"operation":"Do","inputs":[{"name":"x","value":{"kind":"ref","model":{"step":1,"path":"id"}}}],"expect":{"errorExpected":false},"probes":[],"before":{"errorExpected":false}}],"skip":null}]}`
	if err := json.Unmarshal([]byte(raw), &tp); err != nil {
		t.Fatalf("plan decode: %v", err)
	}
	if tp.Principals["alice"].Subject != "u-1" || describeArrange(tp.Bindings[0].Arrange[0]) != "failNext:Boom" {
		t.Fatalf("plan decoded as %#v", tp)
	}
	if tp.Bindings[0].Steps[0].Before == nil {
		t.Fatal("before lost")
	}
	out, err := json.Marshal(tp)
	if err != nil {
		t.Fatalf("plan marshal: %v", err)
	}
	var back TestPlanRecord
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("round trip: %v (%s)", err, out)
	}
	if describe(back.Bindings[0].Steps[0].Inputs[0].Value) != "ref:id" {
		t.Fatalf("round trip lost the ref: %s", out)
	}
}

func TestBindings_UnknownKindRefused(t *testing.T) {
	_, err := UnmarshalTestArgValue([]byte(`{"kind":"hook","model":{}}`))
	if err == nil || !strings.Contains(err.Error(), `unknown kind "hook"`) {
		t.Fatalf("want unknown kind \"hook\", got %v", err)
	}
	_, err = UnmarshalArrangeCall([]byte(`{"kind":"teleport","model":{}}`))
	if err == nil || !strings.Contains(err.Error(), `unknown kind "teleport"`) {
		t.Fatalf("want unknown kind \"teleport\", got %v", err)
	}
	// A bare (envelope-less) value is no longer a legal arg value.
	if _, err := UnmarshalTestArgValue([]byte(`"inv-1"`)); err == nil {
		t.Fatal("a bare string must be refused")
	}
}

func TestBindings_LegacyHookFieldRefused(t *testing.T) {
	for _, field := range []string{"hook", "unobservable"} {
		raw := `{"phaseArtifacts":{"testPlan":{"c":{"component":"c","principals":{},"bindings":[{"scenario":"s-P1","arrange":[],"steps":[{"seq":1,"operation":"Do","inputs":[],"expect":{"errorExpected":false},"probes":[],"` + field + `":true}],"skip":null}]}}}}`
		_, _, err := DecodeProject([]byte(raw))
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Fatalf("%s: want a decode error naming the field, got %v", field, err)
		}
	}
}

func TestBindings_SwitchIsExhaustive(t *testing.T) {
	for _, v := range []TestArgValue{LiteralValue{JSON: json.RawMessage(`1`)}, RefValue{Path: "p"}} {
		if describe(v) == "" {
			t.Fatalf("describe(%#v) empty", v)
		}
	}
	for _, a := range []ArrangeCall{ContractCall{}, AdvanceClock{}, FailNext{}, SeedResource{}} {
		if describeArrange(a) == "" {
			t.Fatalf("describeArrange(%#v) empty", a)
		}
	}
}
