// projectmodel.go
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

// Load parses the codegen-relevant subset and cross-validates it. Unknown
// fields anywhere are ignored; the systemDesign slot is located by kind==5.
func Load(raw []byte) (*Model, error) {
	var top projectDoc
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("projectmodel: %w", err)
	}
	contracts, err := parseContracts(top.ServiceContracts)
	if err != nil {
		return nil, err
	}
	sys, err := findSystemDesign(top.Slots)
	if err != nil {
		return nil, err
	}
	dep, err := findDeployment(top.Slots)
	if err != nil {
		return nil, err
	}
	m := &Model{Contracts: contracts, System: sys, Deployment: dep}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// parseContracts parses every serviceContracts entry, in key order (for
// deterministic first-error reporting).
func parseContracts(raw map[string]json.RawMessage) (map[string]*Contract, error) {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]*Contract, len(raw))
	for _, k := range keys {
		c, err := ParseContract(k, raw[k])
		if err != nil {
			return nil, err
		}
		out[k] = c
	}
	return out, nil
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
