package methodcheck

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

// align.go is the design↔code alignment check: it cross-references the committed
// System (design components, each with a Method layer) against the app's loaded Go
// packages classified into the SAME Method layers (reusing arch.Spec's layer model
// + a minimal packages.Load walk). It reports drift between what the design DECLARES
// and what the code actually IS:
//
//   - ALIGN-MISSING-PKG   (Error)   — a design component has no code package in its
//     declared layer.
//   - ALIGN-EXTRA-PKG     (Error)   — a Method-layer code package matches no design
//     component.
//   - ALIGN-LAYER-MISMATCH (Error)  — a component's code package exists but sits in
//     a DIFFERENT Method layer than the design declares.
//
// Components are matched to packages by a normalizer (default: lowercase + strip
// non-alphanumeric). When ZERO business packages are loaded — the pure design phase,
// no code yet — the check is a graceful no-op (the design rules already ran).

// align rule ids — stable, ALIGN-prefixed (the alignment contract surface).
const (
	ruleAlignMissingPkg   RuleID = "ALIGN-MISSING-PKG"
	ruleAlignExtraPkg     RuleID = "ALIGN-EXTRA-PKG"
	ruleAlignLayerMismate RuleID = "ALIGN-LAYER-MISMATCH"
)

// classifiedPackage is one loaded internal package matched to a Method layer.
type classifiedPackage struct {
	pkgPath string // full import path
	leaf    string // the last path segment (the component-named package dir)
	layer   string // the arch.Layer.Name it classified into
}

// defaultNormalizer maps a name to a comparable match key: lowercase + strip every
// non-alphanumeric rune. So "ProjectStateAccess" and a "projectstate" package leaf
// both reduce toward the same alnum core — callers may override via the spec.
func defaultNormalizer(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// loadClassifiedPackages loads the consuming module's internal packages (via the
// supplied arch.Spec) and classifies each into its Method layer. It replicates the
// minimal packages.Load + layer-prefix classification arch.Check uses, so a code
// package's layer here is the SAME layer arch.Check would assign it. Packages that
// classify into no declared layer are dropped (arch.Check is the authority that
// FAILS on those; the alignment check only reasons about classified business code).
func loadClassifiedPackages(spec arch.Spec) ([]classifiedPackage, error) {
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedFiles,
		Dir:   spec.ModuleRoot,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, spec.Patterns...)
	if err != nil {
		return nil, fmt.Errorf("methodcheck: packages.Load: %w", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		return nil, fmt.Errorf("methodcheck: %d package load error(s); fix the build before checking alignment", n)
	}

	classify := makeSpecClassifier(spec)
	var out []classifiedPackage
	for _, pkg := range pkgs {
		// A package contributing no Go files compiles to nothing — skip (mirrors
		// arch.Check's len(pkg.Syntax)==0 guard, using GoFiles since we don't NeedSyntax).
		if len(pkg.GoFiles) == 0 {
			continue
		}
		layer, ok := classify(pkg.PkgPath)
		if !ok {
			continue
		}
		leaf := pkg.PkgPath
		if i := strings.LastIndexByte(leaf, '/'); i >= 0 {
			leaf = leaf[i+1:]
		}
		out = append(out, classifiedPackage{pkgPath: pkg.PkgPath, leaf: leaf, layer: layer})
	}
	return out, nil
}

// componentLayerName maps a design Component to the arch.Layer.Name its declared
// Method layer corresponds to, so design layers and code layers are compared in the
// SAME vocabulary. The mapping mirrors arch.MethodSpec's layer naming.
func componentLayerName(layer string) string {
	switch layer {
	case layerClient:
		return "Client"
	case layerManager:
		return "Manager"
	case layerEngine:
		return "Engine"
	case layerResourceAccess:
		return "ResourceAccess"
	case layerResource:
		return "Resource"
	case layerUtility:
		return "Utility"
	default:
		return ""
	}
}

// alignSystemToCode produces alignment findings between the design System and the
// classified code packages. normalize is the component↔package match function. When
// pkgs is empty (pure design phase, no code) it returns no findings — the design
// rules already ran. A component whose kind is Resource is NOT expected to have a
// code package (a Resource is a physical store / external system, not the app's own
// Go code), so Resource components are excluded from the missing-package check.
func alignSystemToCode(s System, pkgs []classifiedPackage, normalize func(string) string) []Finding {
	if len(pkgs) == 0 {
		return nil
	}
	if normalize == nil {
		normalize = defaultNormalizer
	}
	pkgLayers, pkgOrder := indexPackagesByLeaf(pkgs, normalize)

	matchedPkgKeys := make(map[string]bool)
	var out []Finding
	for i, c := range s.Components {
		if c.Kind == kindResource {
			continue
		}
		key := normalize(c.Name)
		section := fmt.Sprintf("component %d (%s)", i+1, c.Name)
		if key == "" {
			continue
		}
		out = append(out, checkComponentLayerAlignment(key, componentLayerName(c.Layer), section, i, pkgLayers, pkgOrder, matchedPkgKeys)...)
	}

	out = append(out, checkOrphanedPackages(pkgLayers, pkgOrder, matchedPkgKeys)...)
	return out
}

func makeSpecClassifier(spec arch.Spec) func(string) (string, bool) {
	return func(pkgPath string) (string, bool) {
		rel := strings.TrimPrefix(pkgPath, spec.ModulePrefix)
		if rel == pkgPath {
			return "", false
		}
		for _, l := range spec.Layers {
			if rel == l.DirPrefix || strings.HasPrefix(rel, l.DirPrefix+"/") {
				return l.Name, true
			}
		}
		return "", false
	}
}

func checkComponentLayerAlignment(key, declaredLayer, section string, i int, pkgLayers map[string]map[string]bool, pkgOrder map[string]string, matchedPkgKeys map[string]bool) []Finding {
	layers, exists := pkgLayers[key]
	if !exists {
		return []Finding{{
			RuleID:   ruleAlignMissingPkg,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s declares a %s but no code package matches it in any layer; the design declares a component with no implementation", section, declaredLayer),
			Location: loc(i+1, section),
		}}
	}
	matchedPkgKeys[key] = true
	if !layers[declaredLayer] {
		return []Finding{{
			RuleID:   ruleAlignLayerMismate,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s is declared in the %s layer but its code package %s is in the %s layer; design and code disagree on the component's layer", section, declaredLayer, pkgOrder[key], sortedLayers(layers)),
			Location: loc(i+1, section),
		}}
	}
	return nil
}

func indexPackagesByLeaf(pkgs []classifiedPackage, normalize func(string) string) (pkgLayers map[string]map[string]bool, pkgOrder map[string]string) {
	pkgLayers = make(map[string]map[string]bool)
	pkgOrder = make(map[string]string)
	for _, p := range pkgs {
		key := normalize(p.leaf)
		if key == "" {
			continue
		}
		if pkgLayers[key] == nil {
			pkgLayers[key] = make(map[string]bool)
		}
		pkgLayers[key][p.layer] = true
		if _, seen := pkgOrder[key]; !seen {
			pkgOrder[key] = p.pkgPath
		}
	}
	return
}

func checkOrphanedPackages(pkgLayers map[string]map[string]bool, pkgOrder map[string]string, matchedPkgKeys map[string]bool) []Finding {
	var extraKeys []string
	for key := range pkgLayers {
		if !matchedPkgKeys[key] {
			extraKeys = append(extraKeys, key)
		}
	}
	sort.Strings(extraKeys)
	var out []Finding
	for _, key := range extraKeys {
		out = append(out, Finding{
			RuleID:   ruleAlignExtraPkg,
			Severity: SeverityError,
			Message:  fmt.Sprintf("code package %s (%s layer) matches no design component; the code has a Method component the design does not declare", pkgOrder[key], sortedLayers(pkgLayers[key])),
			Location: loc(0, "alignment"),
		})
	}
	return out
}

// sortedLayers renders a layer-set deterministically for messages.
func sortedLayers(set map[string]bool) string {
	ls := make([]string, 0, len(set))
	for l := range set {
		ls = append(ls, l)
	}
	sort.Strings(ls)
	return strings.Join(ls, "/")
}
