// validate.go
package projectmodel

import (
	"fmt"
	"sort"
)

// legalDepTargets is the Method's downward-only call legality for COMPONENT
// deps ([[the-method-layers]]): who may depend on whom. Utility targets are
// exempt (anyone calls Utilities); plain goType deps are exempt (no contract).
var legalDepTargets = map[string]map[string]bool{
	"Client":         {"Manager": true},
	"Manager":        {"Engine": true, "ResourceAccess": true},
	"Engine":         {"ResourceAccess": true},
	"ResourceAccess": {},
}

// validate cross-checks the parsed contracts and systemDesign slot against
// one another. The dep-legality checks run first (fatal); the systemDesign
// checks run only when a systemDesign slot was present.
func (m *Model) validate() error {
	if err := m.validateDeps(); err != nil {
		return err
	}
	if m.System == nil {
		return nil
	}
	if err := m.validateRelationships(); err != nil {
		return err
	}
	m.validateComponentJoins()
	return nil
}

// validateDeps checks rule 1 (every Dep.Component resolves to a Contracts
// key) and rule 2 (dep-layer legality) for every contract's deps.
func (m *Model) validateDeps() error {
	for _, c := range sortedContracts(m.Contracts) {
		if err := m.validateContractDeps(c); err != nil {
			return err
		}
	}
	return nil
}

// validateContractDeps validates one contract's deps against rules 1 and 2.
func (m *Model) validateContractDeps(c *Contract) error {
	for _, d := range c.Deps {
		if d.Component == "" {
			continue // plain dep: exempt
		}
		target, ok := m.Contracts[d.Component]
		if !ok {
			return fmt.Errorf("projectmodel: contract %s dep %q: unknown component %q", c.Key, d.Name, d.Component)
		}
		if target.Layer == "Utility" {
			continue // Utility targets: exempt
		}
		allowed, known := legalDepTargets[c.Layer]
		if known && !allowed[target.Layer] {
			return fmt.Errorf("projectmodel: contract %s (%s) dep %q → %s (%s): layer rule violation", c.Key, c.Layer, d.Name, d.Component, target.Layer)
		}
	}
	return nil
}

// validateRelationships checks rule 3: every systemDesign relationship
// endpoint resolves to a SystemComponent ID.
func (m *Model) validateRelationships() error {
	ids := make(map[string]bool, len(m.System.Components))
	for _, sc := range m.System.Components {
		ids[sc.ID] = true
	}
	for _, r := range m.System.Relationships {
		if !ids[r.From] || !ids[r.To] {
			return fmt.Errorf("projectmodel: relationship %s→%s: unresolved endpoint", r.From, r.To)
		}
	}
	return nil
}

// validateComponentJoins checks rule 4 (warn-level): every non-stub contract
// with a goPackage whose Layer is Manager/Engine/ResourceAccess joins to a
// SystemComponent via ComponentByContractKey. Misses are collected into
// Model.Warnings rather than treated as fatal — utilities/webClient naming
// drift must not brick codegen.
func (m *Model) validateComponentJoins() {
	for _, c := range sortedContracts(m.Contracts) {
		if c.GoPackage == "" || c.Stub {
			continue
		}
		if !isJoinableLayer(c.Layer) {
			continue
		}
		if _, ok := m.System.ComponentByContractKey(c.Key); !ok {
			m.Warnings = append(m.Warnings, fmt.Sprintf("contract %s: no systemDesign component joins", c.Key))
		}
	}
}

// isJoinableLayer reports whether Layer is one of the three layers expected
// to join a systemDesign component.
func isJoinableLayer(layer string) bool {
	switch layer {
	case "Manager", "Engine", "ResourceAccess":
		return true
	default:
		return false
	}
}

// sortedContracts returns the map's contracts as a slice sorted by Key, for
// deterministic validation-error ordering.
func sortedContracts(cs map[string]*Contract) []*Contract {
	out := make([]*Contract, 0, len(cs))
	for _, c := range cs {
		out = append(out, c)
	}
	sortContractsByKey(out)
	return out
}

// sortContractsByKey sorts contracts in place by Key.
func sortContractsByKey(cs []*Contract) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].Key < cs[j].Key })
}
