package methodassets

import (
	"bytes"
	_ "embed" // go:embed doctrine/registry.json
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// doctrine.go is the doctrine registry: every rule the platform requires of a built app (and of
// the agents that build it), with the rule that enforces it. It is data, embedded, so a prompt
// sentence that states a requirement can be traced to the code that holds the line, and a
// requirement with no enforcing rule is either waived with a date or a failing test.
//
// method-assets has no Go dependencies, so the rule ids are strings here; the test that resolves
// them against the methodcheck, arch and generator test sources lives in framework-go-conformance
// (doctrine_test.go), which requires all of them. The prompt-side tests (citations, anchors, the
// prompt-lint ratchet) live beside this file.

//go:embed doctrine/registry.json
var registryJSON []byte

// Scope says whose behaviour a doctrine entry constrains.
type Scope string

const (
	// ScopeBuiltApp constrains the app archistrator builds: it must be enforced by a rule or waived.
	ScopeBuiltApp Scope = "builtApp"
	// ScopeAgentConduct constrains what an agent does while building.
	ScopeAgentConduct Scope = "agentConduct"
	// ScopeJudgement is a matter for review that no rule can decide.
	ScopeJudgement Scope = "judgement"
)

func (s Scope) valid() bool {
	switch s {
	case ScopeBuiltApp, ScopeAgentConduct, ScopeJudgement:
		return true
	}
	return false
}

// RuleRef is one enforcing rule of a doctrine entry. The wire form is {"kind": "<variant>", ...}.
//
//sumtype:decl
type RuleRef interface{ isRuleRef() }

// MethodcheckRule is a methodcheck rule id, with the methodcheck test that makes it fire on a
// violating fixture.
type MethodcheckRule struct{ Rule, FiredBy string }

// ArchRule is an arch gate rule id, with the arch test that makes it fire.
type ArchRule struct{ Rule, FiredBy string }

// GeneratorInvariant is a generator or runtime property held by a test: the test in Package of
// Module (both relative to the platform checkout) fails when the invariant breaks.
type GeneratorInvariant struct{ Module, Package, Test string }

func (MethodcheckRule) isRuleRef()    {}
func (ArchRule) isRuleRef()           {}
func (GeneratorInvariant) isRuleRef() {}

// Waiver records a conscious, dated gap: the rule is owed, not yet written.
type Waiver struct{ Reason, Owner, Expires string } // Expires is YYYY-MM-DD

// DoctrineEntry is one stated requirement.
type DoctrineEntry struct {
	ID, Statement string
	Scope         Scope
	EnforcedBy    []RuleRef
	PromptRefs    []string // "<assets-relative file>#<anchor text>"
	Waiver        *Waiver
}

// DoctrineRegistry decodes the embedded doctrine/registry.json strictly: an unknown field, an
// unknown ref kind or a malformed entry is an error.
func DoctrineRegistry() ([]DoctrineEntry, error) { return decodeRegistry(registryJSON) }

type registryWire struct {
	Entries []entryWire `json:"entries"`
}

type entryWire struct {
	ID         string            `json:"id"`
	Statement  string            `json:"statement"`
	Scope      Scope             `json:"scope"`
	EnforcedBy []json.RawMessage `json:"enforcedBy"`
	PromptRefs []string          `json:"promptRefs"`
	Waiver     *waiverWire       `json:"waiver"`
}

type waiverWire struct {
	Reason  string `json:"reason"`
	Owner   string `json:"owner"`
	Expires string `json:"expires"`
}

func strictUnmarshal(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}

func decodeRegistry(raw []byte) ([]DoctrineEntry, error) {
	var w registryWire
	if err := strictUnmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("doctrine registry: %w", err)
	}
	seen := map[string]bool{}
	out := make([]DoctrineEntry, 0, len(w.Entries))
	for i, ew := range w.Entries {
		if seen[ew.ID] {
			return nil, fmt.Errorf("doctrine registry: duplicate entry id %q", ew.ID)
		}
		seen[ew.ID] = true
		e, err := decodeEntry(i, ew)
		if err != nil {
			return nil, fmt.Errorf("doctrine registry: %w", err)
		}
		out = append(out, e)
	}
	return out, nil
}

func decodeEntry(i int, ew entryWire) (DoctrineEntry, error) {
	if ew.ID == "" || ew.Statement == "" {
		return DoctrineEntry{}, fmt.Errorf("entry %d needs an id and a statement", i)
	}
	if !ew.Scope.valid() {
		return DoctrineEntry{}, fmt.Errorf("%s: unknown scope %q", ew.ID, ew.Scope)
	}
	e := DoctrineEntry{ID: ew.ID, Statement: ew.Statement, Scope: ew.Scope, PromptRefs: ew.PromptRefs}
	for _, rr := range ew.EnforcedBy {
		ref, err := decodeRef(rr)
		if err != nil {
			return DoctrineEntry{}, fmt.Errorf("%s: %w", ew.ID, err)
		}
		e.EnforcedBy = append(e.EnforcedBy, ref)
	}
	if ew.Waiver != nil {
		wv, err := decodeWaiver(*ew.Waiver)
		if err != nil {
			return DoctrineEntry{}, fmt.Errorf("%s: %w", ew.ID, err)
		}
		e.Waiver = &wv
	}
	return e, nil
}

func decodeWaiver(w waiverWire) (Waiver, error) {
	if w.Reason == "" || w.Owner == "" {
		return Waiver{}, errors.New("a waiver needs a reason and an owner")
	}
	if _, err := time.Parse("2006-01-02", w.Expires); err != nil {
		return Waiver{}, fmt.Errorf("waiver expires %q is not YYYY-MM-DD", w.Expires)
	}
	return Waiver(w), nil
}

// ruleWire is the closed field set of a methodcheck or arch ref.
type ruleWire struct {
	Kind    string `json:"kind"`
	Rule    string `json:"rule"`
	FiredBy string `json:"firedBy"`
}

// invariantWire is the closed field set of a generatorInvariant ref.
type invariantWire struct {
	Kind    string `json:"kind"`
	Module  string `json:"module"`
	Package string `json:"package"`
	Test    string `json:"test"`
}

// decodeRef reads one {"kind": ...} ref. Each kind has its own closed field set.
func decodeRef(raw json.RawMessage) (RuleRef, error) {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("rule ref: %w", err)
	}
	switch head.Kind {
	case "methodcheck", "arch":
		return decodeRuleRef(head.Kind, raw)
	case "generatorInvariant":
		var v invariantWire
		if err := strictUnmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("generatorInvariant ref: %w", err)
		}
		if v.Module == "" || v.Package == "" || v.Test == "" {
			return nil, errors.New("generatorInvariant ref needs a module, a package and a test")
		}
		return GeneratorInvariant{Module: v.Module, Package: v.Package, Test: v.Test}, nil
	}
	return nil, fmt.Errorf("unknown rule ref kind %q", head.Kind)
}

func decodeRuleRef(kind string, raw json.RawMessage) (RuleRef, error) {
	var v ruleWire
	if err := strictUnmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("%s ref: %w", kind, err)
	}
	if v.Rule == "" || v.FiredBy == "" {
		return nil, fmt.Errorf("%s ref needs a rule and the test that fires it", kind)
	}
	if kind == "arch" {
		return ArchRule{Rule: v.Rule, FiredBy: v.FiredBy}, nil
	}
	return MethodcheckRule{Rule: v.Rule, FiredBy: v.FiredBy}, nil
}
