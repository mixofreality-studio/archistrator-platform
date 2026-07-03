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

	// ruleAlignStalePlanned flags a component marked buildStatus=planned that
	// nonetheless HAS a code package — the planned list must self-expire once the code
	// lands (else it silently masks real drift).
	ruleAlignStalePlanned RuleID = "ALIGN-STALE-PLANNED"
	// ruleAlignExternalNonUtility flags a component marked buildStatus=external whose
	// kind is not Utility — external is only legal for framework-provided Utilities.
	ruleAlignExternalNonUtility RuleID = "ALIGN-EXTERNAL-NONUTILITY"
	// ruleAlignExternalUnwired flags an external Utility that no loaded package imports
	// from framework-go/utilities/… — provenance hardening (assert wired, don't waive).
	ruleAlignExternalUnwired RuleID = "ALIGN-EXTERNAL-UNWIRED"
)

// frameworkUtilitiesMarker is the import-path substring that identifies a framework
// utility package (…/framework-go/utilities/<name>). An external Utility is PASSED
// only when a loaded app package imports one whose name matches the component.
const frameworkUtilitiesMarker = "framework-go/utilities/"

// compKey is a layer-scoped match key: matching between design components and code
// packages keys on (normalized name, Method layer), NOT leaf-name alone. With a
// suffix-stripping normalizer, manager/settlement and engine/settlement both reduce
// to the name "settlement" — only the layer keeps them distinct, so import
// attribution and component matching must carry the layer in the key.
type compKey struct {
	name  string
	layer string
}

// StereotypeSuffixNormalizer is a component↔package match-key normalizer suitable as
// a ProjectSpec.NameNormalizer. It applies the defaultNormalizer core (lowercase +
// strip every non-alphanumeric rune) and THEN strips exactly ONE trailing Method
// stereotype suffix — access | engine | manager | client — when doing so leaves a
// non-empty remainder. So "SettlementManager", "SettlementEngine", and a "settlement"
// package leaf all reduce to "settlement" (kept distinct only by their layer, via
// compKey). A name whose WHOLE value IS a bare suffix ("Manager", "Client") is left
// intact rather than collapsing to "" ; a non-suffixed name ("Security") is unchanged;
// only ONE suffix is stripped ("AccessManager" → "access", never "").
func StereotypeSuffixNormalizer(s string) string {
	base := defaultNormalizer(s)
	for _, suf := range stereotypeSuffixes {
		// len(base) > len(suf) guarantees a non-empty remainder AND that base is not
		// itself the bare suffix — both required so "Manager" stays "manager".
		if len(base) > len(suf) && strings.HasSuffix(base, suf) {
			return base[:len(base)-len(suf)]
		}
	}
	return base
}

// stereotypeSuffixes are the Method layer stereotype suffixes a component or package
// name may carry. ResourceAccess carries "access" (the trailing stereotype), not the
// full "resourceaccess".
var stereotypeSuffixes = []string{"access", "engine", "manager", "client"}

// classifiedPackage is one loaded internal package matched to a Method layer.
type classifiedPackage struct {
	pkgPath string   // full import path
	leaf    string   // the last path segment (the component-named package dir)
	layer   string   // the arch.Layer.Name it classified into
	imports []string // full import paths this package imports (for conformance)
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
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedImports,
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
		if cp, ok := classifyLoadedPackage(pkg, classify); ok {
			out = append(out, cp)
		}
	}
	return out, nil
}

// classifyLoadedPackage turns one loaded package into a classifiedPackage, or
// (_, false) when it contributes no Go files or classifies into no declared layer.
func classifyLoadedPackage(pkg *packages.Package, classify func(string) (string, bool)) (classifiedPackage, bool) {
	// A package contributing no Go files compiles to nothing — skip (mirrors
	// arch.Check's len(pkg.Syntax)==0 guard, using GoFiles since we don't NeedSyntax).
	if len(pkg.GoFiles) == 0 {
		return classifiedPackage{}, false
	}
	layer, ok := classify(pkg.PkgPath)
	if !ok {
		return classifiedPackage{}, false
	}
	leaf := pkg.PkgPath
	if i := strings.LastIndexByte(leaf, '/'); i >= 0 {
		leaf = leaf[i+1:]
	}
	imports := make([]string, 0, len(pkg.Imports))
	for ip := range pkg.Imports {
		imports = append(imports, ip)
	}
	sort.Strings(imports)
	return classifiedPackage{pkgPath: pkg.PkgPath, leaf: leaf, layer: layer, imports: imports}, true
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
//
// Matching is LAYER-SCOPED: a component matches a package on (normalized name, layer),
// so two same-named packages in different layers (manager/settlement vs
// engine/settlement) never collide. The component's buildStatus modulates the check:
//   - planned  → no missing-package failure; instead ALIGN-STALE-PLANNED if a package
//     already exists (the planned marker must self-expire).
//   - external → no missing-package failure; ALIGN-EXTERNAL-NONUTILITY for a non-Utility,
//     else ALIGN-EXTERNAL-UNWIRED (Warning) unless a framework utility import backs it.
//   - built (absent) → the ordinary missing/mismatch/extra reconciliation.
func alignSystemToCode(s System, pkgs []classifiedPackage, normalize func(string) string) []Finding {
	if len(pkgs) == 0 {
		return nil
	}
	if normalize == nil {
		normalize = defaultNormalizer
	}
	byNameLayer, layersByName, pkgByName := indexPackages(pkgs, normalize)

	matched := make(map[compKey]bool)
	var out []Finding
	for i, c := range s.Components {
		if c.Kind == kindResource {
			continue
		}
		key := normalize(c.Name)
		if key == "" {
			continue
		}
		section := fmt.Sprintf("component %d (%s)", i+1, c.Name)
		out = append(out, alignComponent(c, key, section, i, pkgs, byNameLayer, layersByName, pkgByName, matched, normalize)...)
	}

	out = append(out, checkOrphanedPackages(byNameLayer, matched)...)
	return out
}

// alignComponent dispatches a single component to the build-status–appropriate check.
func alignComponent(c Component, key, section string, i int, pkgs []classifiedPackage, byNameLayer map[compKey]string, layersByName map[string]map[string]bool, pkgByName map[string]string, matched map[compKey]bool, normalize func(string) string) []Finding {
	declaredLayer := componentLayerName(c.Layer)
	switch c.BuildStatus {
	case buildStatusPlanned:
		return alignPlannedComponent(key, declaredLayer, section, i, pkgs, byNameLayer, matched, normalize)
	case buildStatusExternal:
		return alignExternalComponent(c, key, section, i, pkgs, normalize)
	default:
		return alignBuiltComponent(key, declaredLayer, section, i, byNameLayer, layersByName, pkgByName, matched)
	}
}

// alignBuiltComponent is the ordinary reconciliation for a to-be-built component: an
// exact (name, layer) package match passes; the name present only in ANOTHER layer is
// ALIGN-LAYER-MISMATCH; absence is ALIGN-MISSING-PKG.
func alignBuiltComponent(key, declaredLayer, section string, i int, byNameLayer map[compKey]string, layersByName map[string]map[string]bool, pkgByName map[string]string, matched map[compKey]bool) []Finding {
	if _, ok := byNameLayer[compKey{name: key, layer: declaredLayer}]; ok {
		matched[compKey{name: key, layer: declaredLayer}] = true
		return nil
	}
	if layers, exists := layersByName[key]; exists && len(layers) > 0 {
		// The name exists, just in a different layer — account those packages so they
		// are not ALSO reported as orphaned, and report the disagreement.
		for l := range layers {
			matched[compKey{name: key, layer: l}] = true
		}
		return []Finding{{
			RuleID:   ruleAlignLayerMismate,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s is declared in the %s layer but its code package %s is in the %s layer; design and code disagree on the component's layer", section, declaredLayer, pkgByName[key], sortedLayers(layers)),
			Location: loc(i+1, section),
		}}
	}
	return []Finding{{
		RuleID:   ruleAlignMissingPkg,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s declares a %s but no code package matches it in any layer; the design declares a component with no implementation", section, declaredLayer),
		Location: loc(i+1, section),
	}}
}

// alignPlannedComponent skips the missing-package failure (a planned component has no
// code yet by definition) but flags the STALE case: a planned component that already
// HAS a package. "Has a package" is matched as any package under the component's mapped
// directory in its layer — an exact (name, layer) leaf OR any ancestor directory
// segment normalizing to the component name — so the MCPClient shape (generated
// subpackages under client/mcp/* with NO root package) is detected too.
func alignPlannedComponent(key, declaredLayer, section string, i int, pkgs []classifiedPackage, byNameLayer map[compKey]string, matched map[compKey]bool, normalize func(string) string) []Finding {
	impl, hasImpl := packageUnderComponent(pkgs, key, declaredLayer, normalize)
	if !hasImpl {
		return nil
	}
	// Account every under-directory package (and the exact leaf) so a stale-but-present
	// package is not ALSO reported as orphaned — it does map to this (planned) component.
	if _, ok := byNameLayer[compKey{name: key, layer: declaredLayer}]; ok {
		matched[compKey{name: key, layer: declaredLayer}] = true
	}
	for _, p := range pkgs {
		if p.layer == declaredLayer && pathHasSegmentKey(p.pkgPath, key, normalize) {
			matched[compKey{name: normalize(p.leaf), layer: p.layer}] = true
		}
	}
	return []Finding{{
		RuleID:   ruleAlignStalePlanned,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s is marked buildStatus=planned but a code package (%s) already implements it; a planned component that has a package is stale — drop the planned marker so the list self-expires", section, impl),
		Location: loc(i+1, section),
	}}
}

// alignExternalComponent validates a component marked external. external is legal ONLY
// for a Utility (framework-provided Security/Logging/Diagnostics); an external
// non-Utility is a contract-misuse Error. For an external Utility, provenance is
// asserted (not waived): PASS only when some loaded package imports a
// framework-go/utilities/<name> matching the utility; otherwise a Warning.
func alignExternalComponent(c Component, key, section string, i int, pkgs []classifiedPackage, normalize func(string) string) []Finding {
	if c.Kind != kindUtility {
		return []Finding{{
			RuleID:   ruleAlignExternalNonUtility,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s is marked buildStatus=external but its kind is %s; only a Utility (framework-provided Security/Logging/Diagnostics) may be external — an external non-Utility is a contract misuse", section, c.Kind),
			Location: loc(i+1, section),
		}}
	}
	if importsFrameworkUtility(pkgs, key, normalize) {
		return nil
	}
	return []Finding{{
		RuleID:   ruleAlignExternalUnwired,
		Severity: SeverityWarning,
		Message:  fmt.Sprintf("%s is an external Utility but no loaded package imports a %s%s package; assert the framework utility is actually wired in rather than waiving it", section, frameworkUtilitiesMarker, key),
		Location: loc(i+1, section),
	}}
}

// packageUnderComponent reports whether any classified package in declaredLayer sits
// under the component's mapped directory — either its leaf normalizes to the component
// key (the ordinary single-package case) OR some ancestor path segment does (the
// MCPClient shape: subpackages under client/mcp/* with no root package). Returns the
// lexicographically first such package path for deterministic messages.
func packageUnderComponent(pkgs []classifiedPackage, key, declaredLayer string, normalize func(string) string) (string, bool) {
	best := ""
	for _, p := range pkgs {
		if p.layer != declaredLayer || !pathHasSegmentKey(p.pkgPath, key, normalize) {
			continue
		}
		if best == "" || p.pkgPath < best {
			best = p.pkgPath
		}
	}
	return best, best != ""
}

// pathHasSegmentKey reports whether any '/'-separated segment of pkgPath normalizes to
// key — so a component "MCPClient" (key "mcp") matches …/client/mcp/designtools on its
// "mcp" ancestor segment, not just an exact leaf.
func pathHasSegmentKey(pkgPath, key string, normalize func(string) string) bool {
	for _, seg := range strings.Split(pkgPath, "/") {
		if normalize(seg) == key {
			return true
		}
	}
	return false
}

// importsFrameworkUtility reports whether any loaded package imports a
// framework-go/utilities/<name> whose first path segment normalizes to key.
func importsFrameworkUtility(pkgs []classifiedPackage, key string, normalize func(string) string) bool {
	for _, p := range pkgs {
		for _, ip := range p.imports {
			if frameworkUtilityImportMatches(ip, key, normalize) {
				return true
			}
		}
	}
	return false
}

// frameworkUtilityImportMatches reports whether import path ip is a
// framework-go/utilities/<name> whose <name> segment normalizes to key.
func frameworkUtilityImportMatches(ip, key string, normalize func(string) string) bool {
	idx := strings.Index(ip, frameworkUtilitiesMarker)
	if idx < 0 {
		return false
	}
	seg := ip[idx+len(frameworkUtilitiesMarker):]
	if j := strings.IndexByte(seg, '/'); j >= 0 {
		seg = seg[:j]
	}
	return normalize(seg) == key
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

// indexPackages builds the LAYER-SCOPED package indexes:
//   - byNameLayer: (normalized leaf, layer) → pkgPath (first seen) — the exact match
//     key, so manager/settlement and engine/settlement remain distinct entries.
//   - layersByName: normalized leaf → set of layers it appears in — used to explain a
//     layer mismatch (the name exists, but not in the declared layer).
//   - pkgByName: normalized leaf → the lexicographically first pkgPath carrying it —
//     a representative package path for the mismatch message.
func indexPackages(pkgs []classifiedPackage, normalize func(string) string) (byNameLayer map[compKey]string, layersByName map[string]map[string]bool, pkgByName map[string]string) {
	byNameLayer = make(map[compKey]string)
	layersByName = make(map[string]map[string]bool)
	pkgByName = make(map[string]string)
	for _, p := range pkgs {
		key := normalize(p.leaf)
		if key == "" {
			continue
		}
		ck := compKey{name: key, layer: p.layer}
		if _, seen := byNameLayer[ck]; !seen {
			byNameLayer[ck] = p.pkgPath
		}
		if layersByName[key] == nil {
			layersByName[key] = make(map[string]bool)
		}
		layersByName[key][p.layer] = true
		if cur, ok := pkgByName[key]; !ok || p.pkgPath < cur {
			pkgByName[key] = p.pkgPath
		}
	}
	return
}

// checkOrphanedPackages emits ALIGN-EXTRA-PKG for every (name, layer) package that no
// component matched — deterministically ordered by name then layer.
func checkOrphanedPackages(byNameLayer map[compKey]string, matched map[compKey]bool) []Finding {
	var extra []compKey
	for ck := range byNameLayer {
		if !matched[ck] {
			extra = append(extra, ck)
		}
	}
	sort.Slice(extra, func(i, j int) bool {
		if extra[i].name != extra[j].name {
			return extra[i].name < extra[j].name
		}
		return extra[i].layer < extra[j].layer
	})
	var out []Finding
	for _, ck := range extra {
		out = append(out, Finding{
			RuleID:   ruleAlignExtraPkg,
			Severity: SeverityError,
			Message:  fmt.Sprintf("code package %s (%s layer) matches no design component; the code has a Method component the design does not declare", byNameLayer[ck], ck.layer),
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
