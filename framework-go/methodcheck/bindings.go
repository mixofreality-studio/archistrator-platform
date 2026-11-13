package methodcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// Binding model v2 (FPC plan 2). The sum types below are hand-written in the shape
// modelgen emits for `oneOf` + `x-go-sumtype`: a sealed interface with an
// unexported marker, one struct per variant, and the wire envelope
// {"kind": "<variant>", "model": {...}}. An unknown kind is refused at decode.
//
// A test-plan record is complete by schema and strict on decode: the removed
// `hook` / `unobservable` fields (and any other field the model does not carry)
// are decode errors that name the field — that is TP-HOOK's enforcement.

// ---- TestArgValue ----

// TestArgValueKind discriminates TestArgValue variants.
type TestArgValueKind string

// The TestArgValue variants.
const (
	TestArgLiteral TestArgValueKind = "literal"
	TestArgRef     TestArgValueKind = "ref"
	TestArgPlanRef TestArgValueKind = "planRef"
)

// TestArg is one concrete input argument to a step's operation call.
type TestArg struct {
	Name      string       `json:"name"`
	Value     TestArgValue `json:"value"`
	SchemaRef string       `json:"schemaRef,omitempty"`
}

// TestArgValue is an argument's value: a literal JSON value or a reference to an
// earlier result in the same scenario.
//
//sumtype:decl
type TestArgValue interface {
	Kind() TestArgValueKind
	isTestArgValue()
}

// LiteralValue is a literal JSON value (kind "literal").
type LiteralValue struct {
	JSON json.RawMessage `json:"json"`
}

// RefValue refers to step<Step>.result.<Path>, or, when Probe is set,
// probe<Probe> of that step (kind "ref").
type RefValue struct {
	Step  int    `json:"step"`
	Path  string `json:"path"`
	Probe *int   `json:"probe,omitempty"`
}

// PlanRef is the literal bound to input Input of step Seq of the client plan
// Component/Scenario (kind "planRef"). Only the acceptance plan carries it: a shared step
// input is written once, in the client plan, and the acceptance plan names it instead of
// restating it (ATP-REF refuses one anywhere else and one that does not resolve).
type PlanRef struct {
	Component string `json:"component"`
	Scenario  string `json:"scenario"`
	Seq       int    `json:"seq"`
	Input     string `json:"input"`
}

// Kind implements TestArgValue.
func (LiteralValue) Kind() TestArgValueKind { return TestArgLiteral }

// Kind implements TestArgValue.
func (RefValue) Kind() TestArgValueKind { return TestArgRef }

// Kind implements TestArgValue.
func (PlanRef) Kind() TestArgValueKind { return TestArgPlanRef }

func (LiteralValue) isTestArgValue() {}
func (RefValue) isTestArgValue()     {}
func (PlanRef) isTestArgValue()      {}

// MarshalJSON writes the envelope.
func (v LiteralValue) MarshalJSON() ([]byte, error) { return marshalEnvelope(v.Kind(), literalWire(v)) }

// MarshalJSON writes the envelope.
func (v RefValue) MarshalJSON() ([]byte, error) { return marshalEnvelope(v.Kind(), refWire(v)) }

// MarshalJSON writes the envelope.
func (v PlanRef) MarshalJSON() ([]byte, error) { return marshalEnvelope(v.Kind(), planRefWire(v)) }

// The wire aliases drop the MarshalJSON method so the envelope marshals the bare
// model without recursing.
type (
	literalWire LiteralValue
	refWire     RefValue
	planRefWire PlanRef
)

// UnmarshalTestArgValue decodes the {"kind","model"} envelope; an unknown kind is an error.
func UnmarshalTestArgValue(raw []byte) (TestArgValue, error) {
	kind, model, err := openEnvelope(raw)
	if err != nil {
		return nil, fmt.Errorf("methodcheck: test arg value: %w", err)
	}
	switch TestArgValueKind(kind) {
	case TestArgLiteral:
		var v literalWire
		if err := strictUnmarshal(model, &v); err != nil {
			return nil, fmt.Errorf("methodcheck: test arg value literal: %w", err)
		}
		return LiteralValue(v), nil
	case TestArgRef:
		var v refWire
		if err := strictUnmarshal(model, &v); err != nil {
			return nil, fmt.Errorf("methodcheck: test arg value ref: %w", err)
		}
		return RefValue(v), nil
	case TestArgPlanRef:
		var v planRefWire
		if err := strictUnmarshal(model, &v); err != nil {
			return nil, fmt.Errorf("methodcheck: test arg value planRef: %w", err)
		}
		return PlanRef(v), nil
	}
	return nil, fmt.Errorf("methodcheck: test arg value: unknown kind %q", kind)
}

// UnmarshalJSON decodes a TestArg, routing its value through the envelope decoder.
func (a *TestArg) UnmarshalJSON(raw []byte) error {
	var aux struct {
		Name      string          `json:"name"`
		Value     json.RawMessage `json:"value"`
		SchemaRef string          `json:"schemaRef"`
	}
	if err := strictUnmarshal(raw, &aux); err != nil {
		return err
	}
	v, err := UnmarshalTestArgValue(aux.Value)
	if err != nil {
		return fmt.Errorf("input %q: %w", aux.Name, err)
	}
	*a = TestArg{Name: aux.Name, Value: v, SchemaRef: aux.SchemaRef}
	return nil
}

// ---- ArrangeCall ----

// ArrangeKind discriminates ArrangeCall variants.
type ArrangeKind string

// The ArrangeCall variants.
const (
	ArrangeContractCall ArrangeKind = "contractCall"
	ArrangeAdvanceClock ArrangeKind = "advanceClock"
	ArrangeFailNext     ArrangeKind = "failNext"
	ArrangeSeedResource ArrangeKind = "seedResource"
)

// ArrangeCall is one typed setup action run before a scenario's first step.
//
//sumtype:decl
type ArrangeCall interface {
	Kind() ArrangeKind
	isArrangeCall()
}

// ContractCall calls a contract operation to build up state (kind "contractCall").
type ContractCall struct {
	Component string     `json:"component"`
	Operation string     `json:"operation"`
	Inputs    []TestArg  `json:"inputs"`
	Expect    TestExpect `json:"expect"`
}

// AdvanceClock moves the test clock forward by a Go duration string (kind "advanceClock").
type AdvanceClock struct {
	Duration string `json:"duration"`
}

// FailNext makes the next call of an operation fail with ErrorCode (kind "failNext").
type FailNext struct {
	Component string `json:"component"`
	Operation string `json:"operation"`
	ErrorCode string `json:"errorCode"`
}

// SeedResource loads a fixture into a resource (kind "seedResource").
type SeedResource struct {
	Resource string          `json:"resource"`
	Fixture  json.RawMessage `json:"fixture"`
}

// Kind implements ArrangeCall.
func (ContractCall) Kind() ArrangeKind { return ArrangeContractCall }

// Kind implements ArrangeCall.
func (AdvanceClock) Kind() ArrangeKind { return ArrangeAdvanceClock }

// Kind implements ArrangeCall.
func (FailNext) Kind() ArrangeKind { return ArrangeFailNext }

// Kind implements ArrangeCall.
func (SeedResource) Kind() ArrangeKind { return ArrangeSeedResource }

func (ContractCall) isArrangeCall() {}
func (AdvanceClock) isArrangeCall() {}
func (FailNext) isArrangeCall()     {}
func (SeedResource) isArrangeCall() {}

type (
	contractCallWire ContractCall
	advanceClockWire AdvanceClock
	failNextWire     FailNext
	seedResourceWire SeedResource
)

// MarshalJSON writes the envelope.
func (v ContractCall) MarshalJSON() ([]byte, error) {
	return marshalEnvelope(v.Kind(), contractCallWire(v))
}

// MarshalJSON writes the envelope.
func (v AdvanceClock) MarshalJSON() ([]byte, error) {
	return marshalEnvelope(v.Kind(), advanceClockWire(v))
}

// MarshalJSON writes the envelope.
func (v FailNext) MarshalJSON() ([]byte, error) { return marshalEnvelope(v.Kind(), failNextWire(v)) }

// MarshalJSON writes the envelope.
func (v SeedResource) MarshalJSON() ([]byte, error) {
	return marshalEnvelope(v.Kind(), seedResourceWire(v))
}

// UnmarshalArrangeCall decodes the {"kind","model"} envelope; an unknown kind is an error.
func UnmarshalArrangeCall(raw []byte) (ArrangeCall, error) {
	kind, model, err := openEnvelope(raw)
	if err != nil {
		return nil, fmt.Errorf("methodcheck: arrange call: %w", err)
	}
	switch ArrangeKind(kind) {
	case ArrangeContractCall:
		var v contractCallWire
		if err := strictUnmarshal(model, &v); err != nil {
			return nil, fmt.Errorf("methodcheck: arrange contractCall: %w", err)
		}
		return ContractCall(v), nil
	case ArrangeAdvanceClock:
		var v advanceClockWire
		if err := strictUnmarshal(model, &v); err != nil {
			return nil, fmt.Errorf("methodcheck: arrange advanceClock: %w", err)
		}
		return AdvanceClock(v), nil
	case ArrangeFailNext:
		var v failNextWire
		if err := strictUnmarshal(model, &v); err != nil {
			return nil, fmt.Errorf("methodcheck: arrange failNext: %w", err)
		}
		return FailNext(v), nil
	case ArrangeSeedResource:
		var v seedResourceWire
		if err := strictUnmarshal(model, &v); err != nil {
			return nil, fmt.Errorf("methodcheck: arrange seedResource: %w", err)
		}
		return SeedResource(v), nil
	}
	return nil, fmt.Errorf("methodcheck: arrange call: unknown kind %q", kind)
}

// UnmarshalJSON decodes a ScenarioBinding, routing each arrange entry through the
// envelope decoder. Unknown fields are errors.
func (b *ScenarioBinding) UnmarshalJSON(raw []byte) error {
	var aux struct {
		Scenario string            `json:"scenario"`
		Arrange  []json.RawMessage `json:"arrange"`
		Steps    []StepBinding     `json:"steps"`
		Skip     *SkipReason       `json:"skip"`
	}
	if err := strictUnmarshal(raw, &aux); err != nil {
		return fmt.Errorf("binding %q: %w", aux.Scenario, err)
	}
	arrange := make([]ArrangeCall, 0, len(aux.Arrange))
	for i, a := range aux.Arrange {
		c, err := UnmarshalArrangeCall(a)
		if err != nil {
			return fmt.Errorf("binding %q arrange[%d]: %w", aux.Scenario, i, err)
		}
		arrange = append(arrange, c)
	}
	*b = ScenarioBinding{Scenario: aux.Scenario, Arrange: arrange, Steps: aux.Steps, Skip: aux.Skip}
	return nil
}

// UnmarshalJSON decodes a TestPlanRecord strictly: a field the model does not carry
// (notably the removed `hook` and `unobservable`) is an error that names it.
func (r *TestPlanRecord) UnmarshalJSON(raw []byte) error {
	type plain TestPlanRecord // drops this method; nested types keep theirs
	var v plain
	if err := strictUnmarshal(raw, &v); err != nil {
		if removedHookField(err) {
			return fmt.Errorf("%s: test plan: %w", ruleTPHook, err)
		}
		return fmt.Errorf("test plan: %w", err)
	}
	*r = TestPlanRecord(v)
	return nil
}

// UnmarshalJSON decodes an AcceptancePlanRecord as strictly as a TestPlanRecord: the removed
// `hook` and `unobservable` are TP-HOOK's, any other undeclared field is a decode error.
func (r *AcceptancePlanRecord) UnmarshalJSON(raw []byte) error {
	type plain AcceptancePlanRecord // drops this method; nested types keep theirs
	var v plain
	if err := strictUnmarshal(raw, &v); err != nil {
		if removedHookField(err) {
			return fmt.Errorf("%s: acceptance plan: %w", ruleTPHook, err)
		}
		return fmt.Errorf("acceptance plan: %w", err)
	}
	*r = AcceptancePlanRecord(v)
	return nil
}

// ResolvePlanRef is the literal v names: input v.Input of step v.Seq of binding v.Scenario of
// the client plan v.Component. ok is false when the plan, binding, step or input is missing,
// the binding is skipped, or the input is not a literal.
func ResolvePlanRef(p Project, v PlanRef) (LiteralValue, bool) {
	lit, problem := resolvePlanRef(p, v)
	return lit, problem == ""
}

// resolvePlanRef is ResolvePlanRef with the reason it failed ("" on success).
func resolvePlanRef(p Project, v PlanRef) (LiteralValue, string) {
	if p.PhaseArtifacts == nil {
		return LiteralValue{}, fmt.Sprintf("no client plan %q is recorded", v.Component)
	}
	rec, ok := planRecord(p, v.Component)
	if !ok {
		return LiteralValue{}, fmt.Sprintf("no client plan %q is recorded", v.Component)
	}
	for _, b := range rec.Bindings {
		if b.Scenario != v.Scenario {
			continue
		}
		if b.Skip != nil {
			return LiteralValue{}, fmt.Sprintf("%s's binding of %s is skipped, so it binds no step", v.Component, v.Scenario)
		}
		return resolveStepInput(b, v)
	}
	return LiteralValue{}, fmt.Sprintf("client plan %q has no binding for scenario %q", v.Component, v.Scenario)
}

func resolveStepInput(b ScenarioBinding, v PlanRef) (LiteralValue, string) {
	si := slices.IndexFunc(b.Steps, func(st StepBinding) bool { return st.Seq == v.Seq })
	if si < 0 {
		return LiteralValue{}, fmt.Sprintf("%s has no step %d", v.Scenario, v.Seq)
	}
	inputs := b.Steps[si].Inputs
	ii := slices.IndexFunc(inputs, func(in TestArg) bool { return in.Name == v.Input })
	if ii < 0 {
		return LiteralValue{}, fmt.Sprintf("step %d of %s has no input %q", v.Seq, v.Scenario, v.Input)
	}
	lit, ok := inputs[ii].Value.(LiteralValue)
	if !ok {
		return LiteralValue{}, fmt.Sprintf("step %d input %q of %s must name a literal, and it is a %s", v.Seq, v.Input, v.Scenario, inputs[ii].Value.Kind())
	}
	return lit, ""
}

// planRecord finds a client plan by its key, then by scenario.Normalize equality of the key.
func planRecord(p Project, comp string) (TestPlanRecord, bool) {
	if rec, ok := p.PhaseArtifacts.TestPlan[comp]; ok {
		return rec, true
	}
	want := scenario.Normalize(comp)
	best, found := "", false
	for key := range p.PhaseArtifacts.TestPlan {
		if scenario.Normalize(key) == want && (!found || key < best) {
			best, found = key, true
		}
	}
	if !found {
		return TestPlanRecord{}, false
	}
	return p.PhaseArtifacts.TestPlan[best], true
}

// removedHookField reports a decode error caused by one of the fields binding model v2
// removed: `hook` and `unobservable`. Those are TP-HOOK's.
func removedHookField(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, `unknown field "hook"`) || strings.Contains(msg, `unknown field "unobservable"`)
}

// ---- envelope helpers ----

type envelope struct {
	Kind  string          `json:"kind"`
	Model json.RawMessage `json:"model"`
}

func marshalEnvelope(kind any, model any) ([]byte, error) {
	m, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Kind  any             `json:"kind"`
		Model json.RawMessage `json:"model"`
	}{kind, m})
}

func openEnvelope(raw []byte) (kind string, model json.RawMessage, err error) {
	var e envelope
	if err := strictUnmarshal(raw, &e); err != nil {
		return "", nil, fmt.Errorf("expected a {\"kind\",\"model\"} envelope: %w", err)
	}
	if e.Kind == "" {
		return "", nil, fmt.Errorf("envelope has no kind")
	}
	if len(e.Model) == 0 || string(e.Model) == "null" {
		return "", nil, fmt.Errorf("envelope of kind %q has no model", e.Kind)
	}
	return e.Kind, e.Model, nil
}

// strictUnmarshal decodes raw into v refusing fields v does not declare.
func strictUnmarshal(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
