package methodcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// Kind implements TestArgValue.
func (LiteralValue) Kind() TestArgValueKind { return TestArgLiteral }

// Kind implements TestArgValue.
func (RefValue) Kind() TestArgValueKind { return TestArgRef }

func (LiteralValue) isTestArgValue() {}
func (RefValue) isTestArgValue()     {}

// MarshalJSON writes the envelope.
func (v LiteralValue) MarshalJSON() ([]byte, error) { return marshalEnvelope(v.Kind(), literalWire(v)) }

// MarshalJSON writes the envelope.
func (v RefValue) MarshalJSON() ([]byte, error) { return marshalEnvelope(v.Kind(), refWire(v)) }

// The wire aliases drop the MarshalJSON method so the envelope marshals the bare
// model without recursing.
type (
	literalWire LiteralValue
	refWire     RefValue
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
		return fmt.Errorf("test plan: %w", err)
	}
	*r = TestPlanRecord(v)
	return nil
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
	if len(e.Model) == 0 {
		return e.Kind, json.RawMessage(`{}`), nil
	}
	return e.Kind, e.Model, nil
}

// strictUnmarshal decodes raw into v refusing fields v does not declare.
func strictUnmarshal(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
