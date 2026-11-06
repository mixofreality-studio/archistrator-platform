package projectmodel

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Model is the validated codegen view of one project.json.
type Model struct {
	Contracts  map[string]*Contract
	System     *System
	Deployment *Deployment
	Warnings   []string
}

// LoadFile reads path and delegates to Load. The path is caller-supplied
// (a CLI flag / build argument), not attacker-controlled request input.
func LoadFile(path string) (*Model, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: caller-supplied path, not request input
	if err != nil {
		return nil, err
	}
	return Load(raw)
}

// projectDoc is the tolerant top-level subset Load needs: the
// serviceContracts map and the slots map (the systemDesign slot is located
// among them by kind==5).
type projectDoc struct {
	ServiceContracts map[string]json.RawMessage `json:"serviceContracts"`
	Slots            map[string]struct {
		Kind  int             `json:"kind"`
		Model json.RawMessage `json:"model"`
	} `json:"slots"`
}

// Load parses the codegen-relevant subset and cross-validates it, and reports EVERY problem it
// finds at once: when any exists the error is a [Problems]. Unknown top-level fields are
// ignored; unknown fields inside a contract are a problem (see [ValidateContract]). The
// systemDesign slot is located by kind==5.
func Load(raw []byte) (*Model, error) {
	var top projectDoc
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, Problems{problem("$", CodeDecode, "%v", err)}
	}
	contracts, probs := parseContracts(top.ServiceContracts)
	sys, err := findSystemDesign(top.Slots)
	if err != nil {
		probs = append(probs, problem("systemDesign", CodeDecode, "%v", err))
	}
	dep, err := findDeployment(top.Slots)
	if err != nil {
		probs = append(probs, problem("deployment", CodeDecode, "%v", err))
	}
	m := &Model{Contracts: contracts, System: sys, Deployment: dep}
	probs = append(probs, m.validate()...)
	if err := probs.err(); err != nil {
		return nil, err
	}
	return m, nil
}

// parseContracts validates and parses every serviceContracts entry, in key order (for
// deterministic problem order). An entry that cannot be parsed is reported and left out of the
// returned map.
func parseContracts(raw map[string]json.RawMessage) (map[string]*Contract, Problems) {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]*Contract, len(raw))
	var probs Problems
	for _, k := range keys {
		found := ValidateContract(k, raw[k])
		probs = append(probs, found...)
		c, err := ParseContract(k, raw[k])
		if err != nil {
			if len(found) == 0 {
				probs = append(probs, problem("serviceContracts."+k, CodeDecode, "%v", err))
			}
			continue
		}
		out[k] = c
	}
	return out, probs
}

// findSystemDesign locates the systemDesign slot (kind==5) among the parsed
// slots and parses its model. Returns nil, nil if no such slot is present.
func findSystemDesign(slots map[string]struct {
	Kind  int             `json:"kind"`
	Model json.RawMessage `json:"model"`
}) (*System, error) {
	for _, slot := range slots {
		if slot.Kind != 5 || len(slot.Model) == 0 {
			continue
		}
		s, err := ParseSystem(slot.Model)
		if err != nil {
			return nil, fmt.Errorf("projectmodel: systemDesign slot: %w", err)
		}
		return s, nil
	}
	return nil, nil
}
