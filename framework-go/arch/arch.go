// Package arch is a go/packages-based architecture-rules checker for Method
// systems. A consuming module runs Check from an ordinary go test, supplying a
// Spec that maps its directories to Method layers. The checker enforces, in one
// pass: (0) EVERY loaded internal package classifies into exactly one declared
// layer — there is no "other" bucket, so a rogue package (e.g. a "domain" dir)
// invented to sidestep the layer model fails the build; (1) downward-only layer
// imports (no up, no sideways); (2) Temporal is imported only by the designated
// layer; (3) each classified package exposes an exported interface matching its
// layer's suffix; (4) every method of those interfaces returns error as its
// last result; (5) — when an allowlist is supplied — every non-stdlib
// production import resolves to a sanctioned dependency prefix.
//
// The checker is deliberately closed: there are no per-call opt-outs for the
// structural rules. The only knobs are the Spec's layer set and (optional)
// dependency allowlist, both of which live in the consuming module's test where
// any change is visible in review. There is no whitelist that quietly exempts a
// package from layering, naming, or sideways rules — that absence is the point.
package arch

import (
	"go/types"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// Layer is one horizontal Method layer, ordered top→bottom in Spec.Layers.
//
// Sideways imports (a package importing a sibling in its own layer) are ALWAYS
// forbidden — Method components never call peers in their own layer — so there
// is no per-layer toggle for it.
type Layer struct {
	Name        string         // "Manager", "Engine", "ResourceAccess"
	DirPrefix   string         // path under ModulePrefix, NO trailing slash, e.g. "manager"
	IfaceSuffix *regexp.Regexp // exported-interface name pattern; nil = skip naming + return checks
}

// Spec parameterizes Check for one consuming module.
type Spec struct {
	ModuleRoot    string   // dir to run packages.Load from (the module root)
	ModulePrefix  string   // import-path prefix for internal packages, with trailing slash
	Patterns      []string // load patterns, e.g. []string{"./internal/..."}
	Layers        []Layer  // ordered top→bottom
	TemporalLayer string   // Layer.Name permitted to import go.temporal.io/*

	// TemporalExemptPackages is the explicit, minimal allowlist of NON-Manager
	// packages sanctioned to import go.temporal.io/* — the single architecturally
	// unavoidable exception to "Temporal lives only in the Manager layer". The one
	// member today is the ResourceAccess that fronts the durable-execution
	// substrate ITSELF (durableExecutionAccess): the RA whose fronted Resource IS
	// Temporal, so its concrete adapter must speak the Temporal control-plane SDK,
	// exactly as the Postgres RA's adapter speaks pgx and the Git RA's adapter
	// speaks go-git. Entries are matched by import-path SUFFIX (e.g.
	// "resourceaccess/durableexecution"), so the consuming module names the
	// package in its own test where the exemption is visible in review.
	//
	// The exemption is DELIBERATELY NARROW: it relaxes ONLY the Temporal-isolation
	// rule for a listed package. That package is still fully subject to every other
	// rule — classification (Rule 0), downward-only / no-sideways imports (Rule 1),
	// interface naming + error returns (Rules 3/4), and the dependency allowlist
	// (Rule 5). So a listed RA may import the Temporal SDK in its concrete adapter,
	// but it CANNOT import a business layer, expose a non-Access port, or pull in an
	// unsanctioned dependency. The contract surface staying Temporal-free (the port
	// interface + value types carry zero Temporal lexeme) is the component
	// contract's own promise, enforced by its design + review; the allowlist here
	// grants the import, it does not certify opacity. Leave nil/empty (the default)
	// and the rule stays strict for every package — existing consumers unaffected.
	TemporalExemptPackages []string

	// AllowedImportPrefixes is the dependency allowlist for the consuming
	// module's PRODUCTION code (Check loads with Tests:false, so test-only
	// imports are never scanned). When non-empty, every non-stdlib import of
	// every loaded package must be matched — as a string prefix — by one of
	// these entries, else it is reported as a disallowed dependency. The
	// module's own internal packages must be covered too (typically by the org
	// prefix). Leave nil/empty to DISABLE the allowlist (the default; existing
	// consumers are unaffected). This is how a Method system pins itself to a
	// fixed, operator-curated infrastructure menu — an unsanctioned driver
	// (e.g. a MongoDB client) fails the build.
	AllowedImportPrefixes []string
}

// MethodSpec returns the standard Method layer configuration: Client (entry
// point, no interface requirement) over Manager (façade, no interface
// requirement) over Engine (Engine$) over ResourceAccess (Access$) over the
// Utility bar (utility/, no interface requirement), with Temporal permitted only
// in Manager. Sideways imports are always forbidden (Method components never call
// peers in their own layer), so the ONLY permitted internal packages are client/,
// manager/, engine/, resourceaccess/ and utility/ (plus their component
// sub-packages). Anything else — a "domain", "shared", "common", or generic
// "util" dir invented to dodge the layer model — is unclassified and fails
// Check's classification rule.
//
// The Utility bar is modelled as the BOTTOM-most layer so the Utility-bar
// exception to closed layering ([[the-method-layers]]) falls out of the existing
// downward-import rule: any layer importing a utility/ package is a legal
// downward edge, while a utility/ package importing a business-layer package
// (manager/engine/resourceaccess/client) is an upward import and is rejected — a
// Utility is cross-cutting infrastructure and must not depend on the business
// layers. Utilities expose concern-named ports (e.g. Security), not Engine$/
// Access$ ports, so the Utility layer carries no IfaceSuffix. A Utility imports
// no Temporal (it is not the TemporalLayer), so a utility/ package that pulls in
// go.temporal.io still fails the Temporal-isolation rule.
func MethodSpec(moduleRoot, modulePrefix string) Spec {
	return Spec{
		ModuleRoot:   moduleRoot,
		ModulePrefix: modulePrefix,
		Patterns:     []string{"./internal/..."},
		Layers: []Layer{
			{Name: "Client", DirPrefix: "client", IfaceSuffix: nil},
			{Name: "Manager", DirPrefix: "manager", IfaceSuffix: nil},
			{Name: "Engine", DirPrefix: "engine", IfaceSuffix: regexp.MustCompile(`Engine$`)},
			{Name: "ResourceAccess", DirPrefix: "resourceaccess", IfaceSuffix: regexp.MustCompile(`Access$`)},
			{Name: "Utility", DirPrefix: "utility", IfaceSuffix: nil},
		},
		TemporalLayer: "Manager",
	}
}

// Check loads the module and reports every architecture violation via t.Errorf.
func Check(t *testing.T, spec Spec) {
	t.Helper()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedSyntax,
		Dir:   spec.ModuleRoot,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, spec.Patterns...)
	if err != nil {
		t.Fatalf("arch: packages.Load: %v", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("arch: %d package load error(s); fix the build before checking architecture", n)
	}
	// Guard against a vacuously-passing test: if the patterns matched nothing
	// (e.g. someone narrowed Patterns to a path that no longer exists), there is
	// no architecture to check and the test would silently pass. Fail loudly.
	if len(pkgs) == 0 {
		t.Fatalf("arch: patterns %v matched no packages; the architecture test would pass vacuously — fix the load patterns", spec.Patterns)
	}

	// Rule 5: dependency allowlist (opt-in). Scanned across ALL loaded packages
	// independently of layer classification, so an unclassified internal package
	// cannot smuggle in an unsanctioned dependency.
	if len(spec.AllowedImportPrefixes) > 0 {
		checkAllowlist(t, pkgs, spec.AllowedImportPrefixes)
	}

	layerIndex := func(pkgPath string) (int, bool) {
		rel := strings.TrimPrefix(pkgPath, spec.ModulePrefix)
		if rel == pkgPath {
			return 0, false // not an internal package of this module
		}
		for i, l := range spec.Layers {
			if rel == l.DirPrefix || strings.HasPrefix(rel, l.DirPrefix+"/") {
				return i, true
			}
		}
		return 0, false
	}

	// Permitted directory prefixes, for the classification-failure message.
	dirs := make([]string, len(spec.Layers))
	for i, l := range spec.Layers {
		dirs[i] = l.DirPrefix + "/"
	}
	permitted := strings.Join(dirs, ", ")

	for _, pkg := range pkgs {
		// A directory that contributes no production Go files (e.g. one holding
		// only an external _test.go package) compiles to nothing and cannot smuggle
		// logic — Check loads with Tests:false, so test code is never importable by
		// production. Skip it; a rogue package hiding real code still has parsed
		// syntax and is caught below.
		if len(pkg.Syntax) == 0 {
			continue
		}
		idx, ok := layerIndex(pkg.PkgPath)
		if !ok {
			// Rule 0: every loaded internal package MUST classify into a declared
			// layer. There is no "unclassified" escape — a rogue package invented
			// to sit outside the layer model (a "domain", "shared", or "common"
			// dir) lands here and fails the build. Method has only Clients /
			// Managers / Engines / ResourceAccess / Resources / Utilities; shared
			// typed models belong to the ResourceAccess that fronts them.
			t.Errorf("arch: %s is not part of any Method layer — it sits outside the layer model; the only permitted internal package roots are: %s. Move it into the layer that owns it (shared typed models belong to their fronting ResourceAccess), do not create an out-of-band package.",
				pkg.PkgPath, permitted)
			continue
		}
		layer := spec.Layers[idx]

		// Rule 1 & 2: import edges.
		importPaths := make([]string, 0, len(pkg.Imports))
		for ip := range pkg.Imports {
			importPaths = append(importPaths, ip)
		}
		sort.Strings(importPaths)
		temporalExempt := temporalExempt(pkg.PkgPath, spec.TemporalExemptPackages)
		for _, ip := range importPaths {
			if strings.Contains(ip, "go.temporal.io") {
				// Temporal is permitted only in the designated layer, OR in a package
				// on the explicit TemporalExemptPackages allowlist (the durable-execution
				// RA that fronts Temporal itself). The exemption relaxes ONLY this rule;
				// every other rule below still applies to the package.
				if layer.Name != spec.TemporalLayer && !temporalExempt {
					t.Errorf("arch: %s (%s layer) imports Temporal %q; only the %s layer may (or a package explicitly listed in Spec.TemporalExemptPackages)",
						pkg.PkgPath, layer.Name, ip, spec.TemporalLayer)
				}
				continue
			}
			j, ok := layerIndex(ip)
			if !ok {
				continue
			}
			switch {
			case j < idx:
				t.Errorf("arch: %s (%s) imports %s (%s) — upward import forbidden",
					pkg.PkgPath, layer.Name, ip, spec.Layers[j].Name)
			case j == idx:
				t.Errorf("arch: %s imports sibling %s in the same %s layer — sideways import forbidden; Method components do not call peers in their own layer",
					pkg.PkgPath, ip, layer.Name)
			}
		}

		// Rules 3 & 4: interface naming + return types (skipped only when the
		// layer declares no interface suffix, e.g. Client/Manager façades).
		if layer.IfaceSuffix == nil {
			continue
		}
		ifaces := exportedInterfaces(pkg)
		matched := false
		for name, iface := range ifaces {
			if !layer.IfaceSuffix.MatchString(name) {
				continue
			}
			matched = true
			for i := 0; i < iface.NumMethods(); i++ {
				m := iface.Method(i)
				sig := m.Type().(*types.Signature)
				res := sig.Results()
				if res.Len() == 0 || res.At(res.Len()-1).Type().String() != "error" {
					t.Errorf("arch: %s.%s.%s last result is not error",
						pkg.PkgPath, name, m.Name())
				}
			}
		}
		if !matched {
			t.Errorf("arch: %s (%s) exposes no exported interface matching %q",
				pkg.PkgPath, layer.Name, layer.IfaceSuffix.String())
		}
	}
}

// checkAllowlist reports every non-stdlib production import that no allowed
// prefix covers. Stdlib imports (first path segment carries no dot) are always
// permitted; the allowlist governs only third-party / cross-module dependencies.
func checkAllowlist(t *testing.T, pkgs []*packages.Package, allowed []string) {
	t.Helper()
	for _, pkg := range pkgs {
		importPaths := make([]string, 0, len(pkg.Imports))
		for ip := range pkg.Imports {
			importPaths = append(importPaths, ip)
		}
		sort.Strings(importPaths)
		for _, ip := range importPaths {
			if isStdlibImport(ip) {
				continue
			}
			ok := false
			for _, prefix := range allowed {
				if strings.HasPrefix(ip, prefix) {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("arch: %s imports %q — disallowed dependency (not on the sanctioned allowlist; an aiarch operator must add it before it may be used)",
					pkg.PkgPath, ip)
			}
		}
	}
}

// temporalExempt reports whether pkgPath is on the Temporal-isolation allowlist,
// matched by import-path suffix. An entry of "resourceaccess/durableexecution"
// matches ".../internal/resourceaccess/durableexecution" but NOT a sibling — the
// match is anchored on a path boundary so a prefix like "durableexecutionx" or a
// substring elsewhere in the path cannot accidentally exempt a package.
func temporalExempt(pkgPath string, exempt []string) bool {
	for _, e := range exempt {
		if e == "" {
			continue
		}
		if pkgPath == e || strings.HasSuffix(pkgPath, "/"+e) {
			return true
		}
	}
	return false
}

// isStdlibImport reports whether an import path is part of the Go standard
// library. The convention (shared with goimports/golang.org/x/tools) is that a
// stdlib path's first segment contains no dot — "context", "net/http",
// "encoding/json" — whereas a third-party path's first segment is a host like
// "github.com" or "go.temporal.io".
func isStdlibImport(importPath string) bool {
	first := importPath
	if i := strings.IndexByte(importPath, '/'); i >= 0 {
		first = importPath[:i]
	}
	return !strings.Contains(first, ".")
}

func exportedInterfaces(pkg *packages.Package) map[string]*types.Interface {
	out := map[string]*types.Interface{}
	if pkg.Types == nil {
		return out
	}
	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		tn, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		if iface, ok := tn.Type().Underlying().(*types.Interface); ok {
			out[name] = iface
		}
	}
	return out
}
