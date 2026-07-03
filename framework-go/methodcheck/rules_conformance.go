package methodcheck

import (
	"fmt"
	"sort"
)

// rules_conformance.go is the code↔model conformance pass: it cross-references the
// ACTUAL Go import edges between design-matched packages against the DECLARED
// System.Relationships, in BOTH directions. It is the import-graph analogue of the
// alignment pass (which reconciles component EXISTENCE + layer); conformance
// reconciles the EDGES between them.
//
//   - CODE-EDGE-NOT-IN-MODEL (Error)   — an import between two design-matched
//     packages with no matching System.Relationships pair (any mode): the code
//     wires a dependency the architecture never declared.
//   - MODEL-EDGE-NOT-IN-CODE (Warning) — a declared SYNC relationship between two
//     code-implemented components with no import edge backing it. Restricted to the
//     edges that MUST show up as a Go import (Manager→Engine, Manager/Engine→
//     ResourceAccess, and any code component→Utility); Client→Manager (wire,
//     transport-mediated) and queued/eventPubSub (substrate-mediated) are exempt.
//
// Like alignment, this is a graceful no-op in the pure-design phase (no packages).

const (
	ruleCodeEdgeNotInModel RuleID = "CODE-EDGE-NOT-IN-MODEL"
	ruleModelEdgeNotInCode RuleID = "MODEL-EDGE-NOT-IN-CODE"
)

// codeImportEdge is one observed import between two design-matched packages.
type codeImportEdge struct {
	fromID, toID   string
	fromPkg, toPkg string
}

// conformanceCheck produces code↔model conformance findings. normalize is the
// component↔package match function (default: lowercase + strip non-alphanumeric).
// When pkgs is empty (pure design phase) it returns nothing.
func conformanceCheck(s System, pkgs []classifiedPackage, normalize func(string) string) []Finding {
	if len(pkgs) == 0 {
		return nil
	}
	if normalize == nil {
		normalize = defaultNormalizer
	}
	pkgComponent, implemented := matchPackagesToComponents(s, pkgs, normalize)
	edges := collectCodeEdges(pkgs, pkgComponent)

	var out []Finding
	out = append(out, codeEdgesNotInModel(edges, buildStaticPairs(s))...)
	out = append(out, modelEdgesNotInCode(s, componentIndex(s), implemented, edges)...)
	return out
}

// matchPackagesToComponents pairs each design-matched package to its component
// (pkgPath → Component) and records which components have a code package
// (componentID → true).
func matchPackagesToComponents(s System, pkgs []classifiedPackage, normalize func(string) string) (map[string]Component, map[string]bool) {
	componentByKey := make(map[string]Component, len(s.Components))
	for _, c := range s.Components {
		key := normalize(c.Name)
		if key == "" {
			continue
		}
		if _, seen := componentByKey[key]; !seen {
			componentByKey[key] = c
		}
	}
	pkgComponent := make(map[string]Component, len(pkgs))
	implemented := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		if c, ok := componentByKey[normalize(p.leaf)]; ok {
			pkgComponent[p.pkgPath] = c
			implemented[c.ID] = true
		}
	}
	return pkgComponent, implemented
}

// collectCodeEdges walks every design-matched package's imports and records each
// import that lands on ANOTHER design-matched component, deduped by (from,to) and
// returned in a deterministic order.
func collectCodeEdges(pkgs []classifiedPackage, pkgComponent map[string]Component) []codeImportEdge {
	seen := make(map[relPairKey]bool)
	var edges []codeImportEdge
	for _, p := range pkgs {
		from, ok := pkgComponent[p.pkgPath]
		if !ok {
			continue
		}
		edges = appendPackageCodeEdges(edges, seen, p, from, pkgComponent)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].fromPkg != edges[j].fromPkg {
			return edges[i].fromPkg < edges[j].fromPkg
		}
		return edges[i].toPkg < edges[j].toPkg
	})
	return edges
}

// appendPackageCodeEdges appends every cross-component import edge out of one
// package, deduped against seen.
func appendPackageCodeEdges(edges []codeImportEdge, seen map[relPairKey]bool, p classifiedPackage, from Component, pkgComponent map[string]Component) []codeImportEdge {
	for _, ip := range p.imports {
		to, ok := pkgComponent[ip]
		if !ok || to.ID == from.ID {
			continue
		}
		key := relPairKey{from: from.ID, to: to.ID}
		if seen[key] {
			continue
		}
		seen[key] = true
		edges = append(edges, codeImportEdge{fromID: from.ID, toID: to.ID, fromPkg: p.pkgPath, toPkg: ip})
	}
	return edges
}

// codeEdgesNotInModel emits CODE-EDGE-NOT-IN-MODEL for every observed import edge
// with no matching declared relationship (in the same direction, any mode).
func codeEdgesNotInModel(edges []codeImportEdge, staticPairs map[relPairKey]bool) []Finding {
	var out []Finding
	for _, e := range edges {
		if staticPairs[relPairKey{from: e.fromID, to: e.toID}] {
			continue
		}
		section := fmt.Sprintf("import %s→%s", e.fromPkg, e.toPkg)
		out = append(out, Finding{
			RuleID:   ruleCodeEdgeNotInModel,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: code imports across components %s→%s with no matching System.Relationships pair; the code wires a dependency the architecture never declared (code/model drift)", section, e.fromID, e.toID),
			Location: loc(0, "conformance"),
		})
	}
	return out
}

// modelEdgesNotInCode emits MODEL-EDGE-NOT-IN-CODE for every declared relationship
// that MUST be backed by a Go import (per requiresImportEdge) between two
// code-implemented components but has no observed import edge.
func modelEdgesNotInCode(s System, idx map[string]Component, implemented map[string]bool, edges []codeImportEdge) []Finding {
	codeEdgeSet := make(map[relPairKey]bool, len(edges))
	for _, e := range edges {
		codeEdgeSet[relPairKey{from: e.fromID, to: e.toID}] = true
	}
	var out []Finding
	for i, rel := range s.Relationships {
		if f := modelEdgeFinding(rel, i, idx, implemented, codeEdgeSet); f != nil {
			out = append(out, *f)
		}
	}
	return out
}

// modelEdgeFinding returns a MODEL-EDGE-NOT-IN-CODE finding for one relationship, or
// nil when the relationship needs no import edge, has an unimplemented endpoint, or
// is already backed by an import.
func modelEdgeFinding(rel Relationship, i int, idx map[string]Component, implemented map[string]bool, codeEdgeSet map[relPairKey]bool) *Finding {
	from, fromOK := idx[rel.From]
	to, toOK := idx[rel.To]
	if !fromOK || !toOK || !requiresImportEdge(from, to, rel.Mode) {
		return nil
	}
	if !implemented[from.ID] || !implemented[to.ID] {
		return nil // one side has no code yet — nothing to back the edge with
	}
	if codeEdgeSet[relPairKey{from: from.ID, to: to.ID}] {
		return nil
	}
	section := fmt.Sprintf("Relationship %s→%s", from.Name, to.Name)
	return &Finding{
		RuleID:   ruleModelEdgeNotInCode,
		Severity: SeverityWarning,
		Message:  fmt.Sprintf("%s: declared sync relationship is backed by no import edge between the components' code packages; the architecture declares a call the code does not make (code/model drift)", section),
		Location: loc(i+1, section),
	}
}

// requiresImportEdge reports whether a declared relationship is one that a faithful
// implementation MUST realize as a direct Go import. Only synchronous calls qualify
// (queued + eventPubSub are substrate-mediated, not imports). Client→Manager is
// exempt (transport-mediated wire, not a Go import). The remaining downward calls —
// Manager→Engine, Manager/Engine→ResourceAccess, and any code component→Utility —
// are realized as imports.
func requiresImportEdge(from, to Component, mode string) bool {
	if mode != modeSync {
		return false
	}
	if from.Kind == kindClient && to.Kind == kindManager {
		return false // transport-mediated wire, not a Go import
	}
	if to.Kind == kindUtility {
		return isCoreComponentKind(from.Kind)
	}
	return importEdgeKindPairs[kindPair{from: from.Kind, to: to.Kind}]
}

// kindPair is a directed (fromKind → toKind) pair.
type kindPair struct{ from, to string }

// importEdgeKindPairs are the downward business-layer calls a faithful
// implementation realizes as a direct Go import.
var importEdgeKindPairs = map[kindPair]bool{
	{from: kindManager, to: kindEngine}:         true, // Manager → Engine
	{from: kindManager, to: kindResourceAccess}: true, // Manager → ResourceAccess
	{from: kindEngine, to: kindResourceAccess}:  true, // Engine → ResourceAccess
}
