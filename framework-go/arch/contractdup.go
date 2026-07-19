package arch

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// contractdup.go implements two CONTRACT-DUPLICATION gates that flag a
// hand-written Go interface whose method set is a full structural duplicate of
// a generated service contract — the point of both rules is that the caller
// should CONSUME the generated contract type directly instead of re-declaring
// its shape by hand:
//
//   - Rule c (no-exported-hand-iface, SAME package): an EXPORTED interface
//     declared in a non-*.gen.go file, in a package that hasGeneratedFile, whose
//     method set duplicates that package's OWN generated contract interface.
//   - Rule d (no-foreign-contract-redecl, CROSS package): an EXPORTED interface
//     (in any package) whose method set structurally equals a generated
//     contract interface owned by a DIFFERENT package.
//
// THE MATCH CRITERION (deliberately narrow — this is what keeps the Löwy
// narrow-accepted-interface idiom, used throughout this codebase, out of
// scope): a candidate interface duplicates a generated contract interface iff
// BOTH hold — (1) EXACT method-NAME-SET equality (not "any overlap", not
// subset, not superset — equal sets) and (2) per-method signature equality via
// types.Identical on each method's *types.Signature — a structural go/types
// comparison of parameter and result types, NEVER a type-NAME string
// comparison, so a locally-defined mirror type standing in for the generated
// contract's own parameter/result type FAILS this test. A narrower
// accepted-interface (fewer methods than the contract), or the same handful of
// methods but using a local mirror type in place of the generated contract's
// type (the real app's durableExecutionAccess seam:
// `RegisterSchedule(ctx context.Context, spec scheduleSpec) error` vs. a
// generated 4-method `RegisterSchedule(rc fwra.Context, id ScheduleID, spec
// ScheduleSpec) error`), fails BOTH tests and is automatically exempt — no
// separate unexported/exported carve-out is needed to protect it. Rule c is
// ALSO scoped to exported candidates per its own definition; reusing
// exportedInterfaces (which already filters to exported names) gets that for
// free rather than needing a second explicit check.
//
// SELF-PACKAGE EXCLUSION (mandatory for rule c, and applied to rule d too): a
// package's own generated contract interface(s) — the ones
// genContractInterfaces collects from its *.gen.go files — are explicitly
// excluded from the "other interfaces in this package" candidate set by name,
// so a generated interface can never self-match itself under rule c. The same
// exclusion removes a package's own generated interfaces from rule d's
// per-package candidate set (generated-vs-generated is a codegen concern, not
// a hand-written-duplication one); same-package matches are never considered
// by rule d at all — that is rule c's territory, so d skips any corpus entry
// owned by the candidate's own package.
//
// There is deliberately no "unique Go package per component" rule here (a
// prior idea, founder-overridden as wrong): several generated contracts
// legitimately front ONE shared Go package (align.go's alignViaSharedGoPackage
// exists for exactly this — e.g. the app's projectstate package fronting four
// generated contracts). Treating "this package's generated contracts" as a SET
// keyed by interface name — rather than assuming one contract per package —
// is what keeps that shape correctly handled instead of accidentally broken.

// contractDupViolation is one hand-written interface flagged as duplicating a
// generated contract's method set.
type contractDupViolation struct{ Pkg, Iface, Rule, Detail string }

// contractCorpusEntry is one generated contract interface in the cross-package
// corpus rule d searches: the package that owns it, its name, and its
// *types.Interface for structural comparison.
type contractCorpusEntry struct {
	pkgPath string
	name    string
	iface   *types.Interface
}

const (
	// ruleNoExportedHandIface is rule c: an exported hand-written interface
	// duplicating its own package's generated contract.
	ruleNoExportedHandIface = "no-exported-hand-iface"
	// ruleNoForeignContractRedecl is rule d: an exported interface duplicating
	// a DIFFERENT package's generated contract.
	ruleNoForeignContractRedecl = "no-foreign-contract-redecl"
)

// CheckContractDuplication loads the module named by spec and reports, via
// t.Errorf, every hand-written interface that duplicates a generated service
// contract's method set — see the file header for the two rules and the exact
// match criterion. Mirrors CheckFileLayout's signature and reporting
// convention: unconditional, no allowlist parameter, since the
// legitimate-idiom-vs-violation distinction here is purely structural — there
// is nothing for a consuming repo to configure.
func CheckContractDuplication(t *testing.T, spec Spec) {
	t.Helper()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:   spec.ModuleRoot,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, spec.Patterns...)
	if err != nil {
		t.Fatalf("arch: packages.Load: %v", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("arch: %d package load error(s); fix the build before checking contract duplication", n)
	}

	for _, v := range contractDuplicationViolations(pkgs) {
		t.Errorf("%s: %s %s: %s", v.Pkg, v.Rule, v.Iface, v.Detail)
	}
}

// contractDuplicationViolations is the pure core: it returns every rule-c and
// rule-d violation across pkgs, deterministically ordered by package, then
// interface, then rule.
func contractDuplicationViolations(pkgs []*packages.Package) []contractDupViolation {
	genByPkg, corpus := buildContractCorpus(pkgs)

	var out []contractDupViolation
	for _, p := range pkgs {
		out = append(out, ruleCViolations(p, genByPkg[p.PkgPath])...)
		out = append(out, ruleDViolations(p, genByPkg[p.PkgPath], corpus)...)
	}
	sortContractDupViolations(out)
	return out
}

// buildContractCorpus scans pkgs for every generated-contract package
// (hasGeneratedFile) and returns two views of the same data: genByPkg, keyed
// by owning package path, for rule c's per-package lookup, and corpus, a
// flat, deterministically sorted list for rule d's cross-package search.
func buildContractCorpus(pkgs []*packages.Package) (genByPkg map[string]map[string]*types.Interface, corpus []contractCorpusEntry) {
	genByPkg = make(map[string]map[string]*types.Interface, len(pkgs))
	for _, p := range pkgs {
		if !hasGeneratedFile(p) {
			continue
		}
		gen := genContractInterfaces(p)
		if len(gen) == 0 {
			continue
		}
		genByPkg[p.PkgPath] = gen
		for name, iface := range gen {
			corpus = append(corpus, contractCorpusEntry{pkgPath: p.PkgPath, name: name, iface: iface})
		}
	}
	sort.Slice(corpus, func(i, j int) bool {
		if corpus[i].pkgPath != corpus[j].pkgPath {
			return corpus[i].pkgPath < corpus[j].pkgPath
		}
		return corpus[i].name < corpus[j].name
	})
	return genByPkg, corpus
}

// sortContractDupViolations orders out by package, then interface, then rule
// — the deterministic order contractDuplicationViolations reports in.
func sortContractDupViolations(out []contractDupViolation) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pkg != out[j].Pkg {
			return out[i].Pkg < out[j].Pkg
		}
		if out[i].Iface != out[j].Iface {
			return out[i].Iface < out[j].Iface
		}
		return out[i].Rule < out[j].Rule
	})
}

// ruleCViolations implements rule c (no-exported-hand-iface): every EXPORTED
// interface declared anywhere in p — other than p's own generated contract
// interface(s), which are explicitly excluded by name (the mandatory
// self-package exclusion) — whose method set structurally equals one of them.
// A package that owns no generated contract (ownGen empty) has nothing to
// compare against and produces no findings.
func ruleCViolations(p *packages.Package, ownGen map[string]*types.Interface) []contractDupViolation {
	if len(ownGen) == 0 {
		return nil
	}
	var out []contractDupViolation
	for name, cand := range exportedInterfaces(p) {
		if _, isOwnGen := ownGen[name]; isOwnGen {
			continue // self-package exclusion: the generated contract can never self-match
		}
		if genName, ok := matchingContract(cand, ownGen); ok {
			out = append(out, contractDupViolation{
				Pkg:   p.PkgPath,
				Iface: name,
				Rule:  ruleNoExportedHandIface,
				Detail: "duplicates this package's own generated contract " + genName +
					"'s method set (same method names, identical signatures) — consume " + genName + " instead of re-declaring it",
			})
		}
	}
	return out
}

// ruleDViolations implements rule d (no-foreign-contract-redecl): every
// EXPORTED interface declared in p — other than p's own generated contract
// interface(s) — whose method set structurally equals a generated contract
// interface owned by a DIFFERENT package in corpus. Same-package matches are
// skipped here (that is rule c's territory), so corpus entries owned by p
// itself never participate.
func ruleDViolations(p *packages.Package, ownGen map[string]*types.Interface, corpus []contractCorpusEntry) []contractDupViolation {
	var out []contractDupViolation
	for name, cand := range exportedInterfaces(p) {
		if _, isOwnGen := ownGen[name]; isOwnGen {
			continue // p's own generated interface is never a rule-d candidate
		}
		if entry, ok := matchingForeignContract(cand, p.PkgPath, corpus); ok {
			out = append(out, contractDupViolation{
				Pkg:   p.PkgPath,
				Iface: name,
				Rule:  ruleNoForeignContractRedecl,
				Detail: "duplicates the generated contract " + entry.name + " owned by package " + entry.pkgPath +
					" (same method names, identical signatures) — consume that generated type instead of re-declaring it",
			})
		}
	}
	return out
}

// matchingForeignContract returns the first corpus entry NOT owned by
// ownPkgPath whose method set structurally equals cand — same-package
// entries are skipped here since that duplication is rule c's territory, not
// rule d's.
func matchingForeignContract(cand *types.Interface, ownPkgPath string, corpus []contractCorpusEntry) (contractCorpusEntry, bool) {
	for _, entry := range corpus {
		if entry.pkgPath == ownPkgPath {
			continue
		}
		if ifaceMethodSetEqual(cand, entry.iface) {
			return entry, true
		}
	}
	return contractCorpusEntry{}, false
}

// matchingContract returns the name of the first entry in gen (in
// deterministic, sorted-name order) whose method set structurally equals
// cand, or ("", false) if none match.
func matchingContract(cand *types.Interface, gen map[string]*types.Interface) (string, bool) {
	names := make([]string, 0, len(gen))
	for name := range gen {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if ifaceMethodSetEqual(cand, gen[name]) {
			return name, true
		}
	}
	return "", false
}

// ifaceMethodSetEqual reports whether a and b carry EXACTLY the same set of
// method names AND, for every method, an identical *types.Signature per
// types.Identical — a structural go/types comparison of parameter and result
// types, never a type-NAME string comparison. This is THE match criterion (see
// file header): a narrower interface (fewer or more methods), or one using a
// local mirror type in place of the other's own parameter/result type, fails
// here. Two empty (zero-method) interfaces are never considered equal under
// this rule — an empty method set is not a "duplicated contract".
func ifaceMethodSetEqual(a, b *types.Interface) bool {
	if a.NumMethods() == 0 || a.NumMethods() != b.NumMethods() {
		return false
	}
	bMethods := make(map[string]*types.Func, b.NumMethods())
	for i := 0; i < b.NumMethods(); i++ {
		bMethods[b.Method(i).Name()] = b.Method(i)
	}
	for i := 0; i < a.NumMethods(); i++ {
		am := a.Method(i)
		bm, ok := bMethods[am.Name()]
		if !ok {
			return false // name-set mismatch
		}
		if !types.Identical(am.Type(), bm.Type()) {
			return false // signature mismatch (e.g. a local mirror type in place of the generated one)
		}
	}
	return true
}

// genContractInterfaces returns every EXPORTED interface type declared in p's
// *.gen.go files, keyed by name — "this package's generated contract corpus".
// A package normally owns exactly one, but the alignViaSharedGoPackage shape
// (several generated contracts fronting one Go package) means a package can
// legitimately own more than one; every entry is checked independently by both
// rules. Sibling to genSurface (gensurface.go), which returns method NAMES
// only (sufficient for the encapsulation gate); this returns the actual
// *types.Interface so callers can do full per-method SIGNATURE equality.
func genContractInterfaces(p *packages.Package) map[string]*types.Interface {
	out := map[string]*types.Interface{}
	if p.Types == nil {
		return out
	}
	for i, f := range p.Syntax {
		if !strings.HasSuffix(p.CompiledGoFiles[i], ".gen.go") {
			continue
		}
		collectGenContractInterfaces(p, f, out)
	}
	return out
}

func collectGenContractInterfaces(p *packages.Package, f *ast.File, out map[string]*types.Interface) {
	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range genDecl.Specs {
			if name, iface, ok := genContractInterfaceSpec(p, spec); ok {
				out[name] = iface
			}
		}
	}
}

// genContractInterfaceSpec reports the name and *types.Interface of spec when
// it is an exported top-level interface TypeSpec with resolved type
// information — the single-TypeSpec check factored out of
// collectGenContractInterfaces to keep both functions' branching shallow.
func genContractInterfaceSpec(p *packages.Package, spec ast.Spec) (string, *types.Interface, bool) {
	ts, ok := spec.(*ast.TypeSpec)
	if !ok || !ts.Name.IsExported() {
		return "", nil, false
	}
	if _, ok := ts.Type.(*ast.InterfaceType); !ok {
		return "", nil, false
	}
	obj := p.Types.Scope().Lookup(ts.Name.Name)
	if obj == nil {
		return "", nil, false
	}
	iface, ok := obj.Type().Underlying().(*types.Interface)
	if !ok {
		return "", nil, false
	}
	return ts.Name.Name, iface, true
}
