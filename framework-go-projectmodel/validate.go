package projectmodel

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// knownLayers enumerates all valid contract layer values.
var knownLayers = map[string]bool{
	"Client":         true,
	"Manager":        true,
	"Engine":         true,
	"ResourceAccess": true,
	"Utility":        true,
}

// CanonicalLayer is layer spelled as projectmodel spells it (Client, Manager, Engine,
// ResourceAccess, Utility), matched ignoring case, and whether it is a layer at all. Load
// refuses any other spelling, and every generator loads through Load, so a writer that
// stores a contract's layer through this stores one every generator can read.
func CanonicalLayer(layer string) (string, bool) {
	for known := range knownLayers {
		if strings.EqualFold(known, layer) {
			return known, true
		}
	}
	return "", false
}

// legalDepTargets is the Method's downward-only call legality for COMPONENT
// deps ([[the-method-layers]]): who may depend on whom. Utility targets are
// exempt (anyone calls Utilities); plain goType deps are exempt (no contract).
var legalDepTargets = map[string]map[string]bool{
	"Client":         {"Manager": true},
	"Manager":        {"Engine": true, "ResourceAccess": true},
	"Engine":         {"ResourceAccess": true},
	"ResourceAccess": {},
}

// validate cross-checks the parsed contracts and systemDesign slot against one another and
// reports every problem it finds, in a stable order: dep legality, then deployment, then
// relationships (only when a systemDesign slot was present). The warn-level component joins
// land in Warnings and are never problems.
func (m *Model) validate() Problems {
	var probs Problems
	probs = append(probs, m.validateDeps()...)
	probs = append(probs, m.validateDeployment()...)
	if m.System == nil {
		return probs
	}
	probs = append(probs, systemProblems(m.System)...)
	m.validateComponentJoins()
	return probs
}

// validateDeps checks rule 1 (every Dep.Component resolves to a Contracts key) and rule 2
// (dep-layer legality) for every contract's deps. A contract's own layer spelling is
// ValidateContract's.
func (m *Model) validateDeps() Problems {
	var probs Problems
	for _, c := range sortedContracts(m.Contracts) {
		probs = append(probs, m.validateContractDeps(c)...)
	}
	return probs
}

// validateContractDeps validates one contract's deps against rules 1 and 2.
func (m *Model) validateContractDeps(c *Contract) Problems {
	var probs Problems
	for i, d := range c.Deps {
		if d.Component == "" {
			continue // plain dep: exempt
		}
		path := fmt.Sprintf("serviceContracts.%s.deps[%d]", c.Key, i)
		target, ok := m.Contracts[d.Component]
		if !ok {
			probs = append(probs, problem(path, CodeDepTarget, "contract %s dep %q: unknown component %q", c.Key, d.Name, d.Component))
			continue
		}
		if target.Layer == "Utility" {
			continue // Utility targets: exempt
		}
		// Utility sources have no dep restrictions; others have legality rules.
		allowed, known := legalDepTargets[c.Layer]
		if known && !allowed[target.Layer] {
			probs = append(probs, problem(path, CodeDepTarget, "contract %s (%s) dep %q -> %s (%s): layer rule violation", c.Key, c.Layer, d.Name, d.Component, target.Layer))
		}
	}
	return probs
}

// systemProblems checks rule 3: every systemDesign relationship endpoint resolves to a
// SystemComponent ID.
func systemProblems(s *System) Problems {
	ids := make(map[string]bool, len(s.Components))
	for _, sc := range s.Components {
		ids[sc.ID] = true
	}
	var probs Problems
	for i, r := range s.Relationships {
		if !ids[r.From] || !ids[r.To] {
			probs = append(probs, problem(fmt.Sprintf("systemDesign.relationships[%d]", i), CodeRelEnds, "relationship %s -> %s: unresolved endpoint", r.From, r.To))
		}
	}
	return probs
}

// ValidateSystem parses a systemDesign slot model and reports every problem in it.
func ValidateSystem(raw json.RawMessage) Problems {
	s, err := ParseSystem(raw)
	if err != nil {
		return Problems{problem("systemDesign", CodeDecode, "systemDesign slot: %v", err)}
	}
	return systemProblems(s)
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
