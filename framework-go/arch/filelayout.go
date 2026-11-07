package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// filelayout.go is the FILE-LAYOUT GATE: for every leaf package of a layer
// carrying a non-empty Layer.FileStereotype, handwritten (non *.gen.go) .go
// files are limited to a closed set — (1) the impl file
// <leaf><stereotype>.go, carrying all contract methods and shared/non-workflow
// code; (2) on the Manager layer only, one per-workflow file per workflow
// ENTRY func, named after the func (minus the "Workflow" suffix, lowercased);
// (3) the generated test pair, and ONLY that pair (rule scenario-tests-only):
// the generated scenarios file <stereotype>_scenarios.gen_test.go and the
// generated null-subject file null.gen_test.go. Both must be black-box -
// declare package <leaf>_test (rule test-package-not-external). Anything else
// in scope is ARCH-TEST-COMPOSE: an agent never writes code that runs inside a
// test, so a <stereotype>_hooks_test.go is a violation whatever it contains,
// and so is a _test package that declares a type whose method set implements
// a generated contract interface (an in-file fake: the scenario would exercise
// the fake, not the component; the subject comes from the generated
// scenariocompose package). Any other _test.go file in a component package - a
// hand-written unit test, a white-box test, the old <stereotype>_test.go - is a
// scenario-tests-only violation: the component's tests are DERIVED from its
// use cases and test plan, never hand-authored.
// The generated file is named *.gen_test.go, not *_test.gen.go: the Go tool
// compiles ONLY files ending in _test.go as tests, so a "_test.gen.go" file
// declaring package <leaf>_test is a second package in the directory and
// breaks the build - the ".gen" marker therefore sits before the mandatory
// "_test.go" suffix. Anywhere in scope, a hand call to RegisterActivity/
// RegisterActivityWithOptions is forbidden (registration is reserved for the
// generated worker), and a workflow.Context func outside the Manager layer is
// forbidden regardless of its file.
//
// A workflow ENTRY func is a func whose name ends in "Workflow" AND takes a
// workflow.Context param (both conditions required — a func merely named
// "...Workflow" without the param, or one taking workflow.Context without the
// name suffix, is not an entry func). Rule 2 keys on entry funcs, not on
// every workflow.Context-taking func: workflow-file-multiple-funcs fires only
// when a file declares MORE THAN ONE entry func, and the required filename is
// derived from THE entry func. A non-entry, context-taking helper is legal in
// any file that has exactly one entry func (real workflow packages fold an
// entry point plus its context-taking helpers into one file). Such a helper
// remains illegal in the impl file (workflow-in-impl-file fires for ANY
// context-taking func there, entry or helper) and in a file with NO entry
// func — a helpers-only file is not a workflow file, so it falls through to
// file-not-allowed rather than being silently passed.
//
// workflow.Context is matched by the AST selector's NAME ("workflow.Context"),
// not by resolved import path, so both a real go.temporal.io/sdk/workflow
// import and a fixture's local stub package satisfy the rule identically —
// deliberately, since the check must work for consumers without pulling the
// real Temporal SDK into this framework module or its testdata fixtures.
//
// The file-name half of the test-file rule is done via os.ReadDir on the
// package directory plus a package-clause parse of each allowed test file: it
// needs only names. The in-file-fake half (contractFakeViolations) needs type
// information and the test variants of every package, so it is a second load
// (Tests:true) behind CheckFileLayout; fileLayoutViolations stays a
// syntax-level core.
//
// fileLayoutViolations is the pure core: it takes an already-loaded package
// set (Tests:false, syntax-level load — no type info needed) and returns every
// violation, deterministically ordered by iteration over pkgs and their
// CompiledGoFiles. CheckFileLayout below is the t.Errorf-routing wrapper.
type fileLayoutViolation struct{ Pkg, File, Rule, Detail string }

// CheckFileLayout loads the module named by spec and reports, via t.Errorf,
// every file-layout violation across its classified packages — see the file
// header for the closed set of rules enforced. A package that does not
// classify into a spec layer, or whose layer has an empty FileStereotype, is
// silently skipped (file-layout enforcement is opt-in per layer via
// Layer.FileStereotype).
func CheckFileLayout(t *testing.T, spec Spec) {
	t.Helper()
	spec.requireSpec(t)
	cfg := &packages.Config{
		// The check is AST-name-based (file names + parsed syntax); it never
		// touches type information, so Types/TypesInfo are deliberately not loaded.
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax,
		Dir:   spec.ModuleRoot,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, spec.Patterns...)
	if err != nil {
		t.Fatalf("arch: packages.Load: %v", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("arch: %d package load error(s); fix the build before checking the file layout", n)
	}

	for _, v := range fileLayoutViolations(pkgs, spec) {
		t.Errorf("%s: %s %s: %s", v.Pkg, v.Rule, v.File, v.Detail)
	}

	// ARCH-TEST-COMPOSE, in-file-fake half: needs the test variants and types.
	testCfg := &packages.Config{Mode: contractFakeLoadMode, Dir: spec.ModuleRoot, Tests: true}
	testPkgs, err := packages.Load(testCfg, spec.Patterns...)
	if err != nil {
		t.Fatalf("arch: packages.Load (tests): %v", err)
	}
	if n := packages.PrintErrors(testPkgs); n > 0 {
		t.Fatalf("arch: %d package load error(s) in test code; fix the test build before checking for in-file fakes", n)
	}
	for _, v := range contractFakeViolations(testPkgs, spec) {
		t.Errorf("%s: %s %s: %s", v.Pkg, v.Rule, v.File, v.Detail)
	}

	// ARCH-SCENARIOCOMPOSE-TEST-ONLY: the test substrates stay out of the binary.
	CheckScenarioComposeTestOnly(t, spec)
}

// fileLayoutViolations returns every file-layout violation across pkgs. A
// package that does not classify into a spec layer, or whose layer has an
// empty FileStereotype (file-layout enforcement disabled), is skipped.
func fileLayoutViolations(pkgs []*packages.Package, spec Spec) []fileLayoutViolation {
	idx := makeLayerIndex(spec)
	var out []fileLayoutViolation
	for _, p := range pkgs {
		layer, _, ok := layerFor(idx, spec, p.PkgPath)
		if !ok || layer.FileStereotype == "" {
			continue
		}
		out = append(out, packageFileLayoutViolations(p, spec, layer)...)
	}
	return out
}

// packageFileLayoutViolations checks one already-classified package.
func packageFileLayoutViolations(p *packages.Package, spec Spec, layer Layer) []fileLayoutViolation {
	if len(p.CompiledGoFiles) == 0 {
		return nil
	}
	leaf := path.Base(p.PkgPath)
	implFile := leaf + layer.FileStereotype + ".go"
	scenariosFile := layer.FileStereotype + "_scenarios.gen_test.go"
	testFiles := scenariosFile + " and " + nullTestFile

	var out []fileLayoutViolation
	out = append(out, scenarioTestViolations(p, scenariosFile)...)

	for i, fpath := range p.CompiledGoFiles {
		base := filepath.Base(fpath)
		if strings.HasSuffix(base, ".gen.go") {
			continue
		}
		f := p.Syntax[i]
		for _, call := range registerActivityCalls(f) {
			out = append(out, fileLayoutViolation{p.PkgPath, base, "hand-activity-registration", call})
		}
		out = append(out, fileHandwrittenViolations(p.PkgPath, base, implFile, testFiles, layer, spec, workflowFuncs(f), workflowEntryFuncs(f))...)
	}
	return out
}

// fileHandwrittenViolations classifies a single handwritten (non *.gen.go)
// file against the closed allowed set and returns every structural violation
// for it (a workflow file can fail both the one-entry-func-per-file rule and
// the filename rule at once). wfFuncs is the file's list of ALL
// workflow.Context func names (entry funcs and non-entry helpers alike) —
// used by workflow-in-impl-file and workflow-func-outside-manager, which
// apply regardless of entry-func status. entryFuncs is the subset that are
// workflow ENTRY funcs (name ends "Workflow" AND takes workflow.Context) —
// the rule-2 classification key: a file is a "workflow file" iff it has
// exactly one entry func, and the required filename and the
// multiple-funcs check are both derived from entryFuncs, not wfFuncs.
func fileHandwrittenViolations(pkgPath, base, implFile, testFiles string, layer Layer, spec Spec, wfFuncs, entryFuncs []string) []fileLayoutViolation {
	var out []fileLayoutViolation
	switch {
	case base == implFile:
		if len(wfFuncs) > 0 {
			out = append(out, fileLayoutViolation{pkgPath, base, "workflow-in-impl-file",
				"workflow func " + wfFuncs[0] + " must live in its own per-workflow file, not " + implFile})
		}
	case len(wfFuncs) > 0:
		out = append(out, workflowFileViolations(pkgPath, base, implFile, testFiles, layer, spec, wfFuncs, entryFuncs)...)
	default:
		out = append(out, fileLayoutViolation{pkgPath, base, "file-not-allowed",
			"handwritten files are limited to " + implFile + ", per-workflow files, " + testFiles})
	}
	return out
}

// workflowFileViolations classifies a handwritten file that contains at least
// one workflow.Context-taking func (wfFuncs) — the rule-2 branch of
// fileHandwrittenViolations. It applies the workflow-func-outside-manager,
// file-not-allowed (helpers-only file, no entry func), workflow-file-multiple-funcs,
// and workflow-file-name checks, in that order, matching the classification
// described in the fileHandwrittenViolations doc comment.
func workflowFileViolations(pkgPath, base, implFile, testFiles string, layer Layer, spec Spec, wfFuncs, entryFuncs []string) []fileLayoutViolation {
	var out []fileLayoutViolation
	if layer.Name != spec.TemporalLayer {
		out = append(out, fileLayoutViolation{pkgPath, base, "workflow-func-outside-manager",
			"workflow func " + wfFuncs[0] + " found outside the " + spec.TemporalLayer + " layer"})
		return out
	}
	if len(entryFuncs) == 0 {
		// Context-taking func(s) present, but none is a workflow ENTRY
		// func — this is not a workflow file (e.g. a helpers-only file),
		// so it falls into the closed set's default: file-not-allowed.
		out = append(out, fileLayoutViolation{pkgPath, base, "file-not-allowed",
			"handwritten files are limited to " + implFile + ", per-workflow files, " + testFiles})
		return out
	}
	if len(entryFuncs) > 1 {
		out = append(out, fileLayoutViolation{pkgPath, base, "workflow-file-multiple-funcs", strings.Join(entryFuncs, ",")})
	}
	want := strings.ToLower(strings.TrimSuffix(entryFuncs[0], "Workflow")) + ".go"
	if base != want {
		out = append(out, fileLayoutViolation{pkgPath, base, "workflow-file-name", "want " + want})
	}
	return out
}

// ruleTestCompose is the rule id of the test-composition gate: the subject of
// every generated test is built by the generated scenariocompose package, never
// by code an agent wrote.
const ruleTestCompose = "ARCH-TEST-COMPOSE"

// nullTestFile is the second generated test file a component package may carry:
// the null-subject run that proves each scenario can fail.
const nullTestFile = "null.gen_test.go"

// hooksTestSuffix names the retired agent-owned hooks file. Any file ending in
// it is ARCH-TEST-COMPOSE.
const hooksTestSuffix = "_hooks_test.go"

// scenarioTestViolations enforces the test-file half of ARCH-TEST-COMPOSE on
// p's package directory: the only _test.go files in a component package are the
// generated scenarios file and the generated null file (scenario-tests-only),
// both declare the external <pkg>_test package (test-package-not-external), and
// no <stereotype>_hooks_test.go exists (ARCH-TEST-COMPOSE). See the file header
// for why this is a directory listing + package-clause parse.
func scenarioTestViolations(p *packages.Package, scenariosFile string) []fileLayoutViolation {
	dir := filepath.Dir(p.CompiledGoFiles[0])
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []fileLayoutViolation
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		switch {
		case strings.HasSuffix(name, hooksTestSuffix):
			out = append(out, fileLayoutViolation{p.PkgPath, name, ruleTestCompose,
				"an agent never writes test code: the hooks file is retired, the subject comes from the generated scenariocompose package"})
		case name != scenariosFile && name != nullTestFile:
			out = append(out, fileLayoutViolation{p.PkgPath, name, "scenario-tests-only", "want only " + scenariosFile + " and " + nullTestFile})
		default:
			out = append(out, externalTestPackageViolations(p, filepath.Join(dir, name))...)
		}
	}
	return out
}

// externalTestPackageViolations checks one of the two allowed test files: it
// must parse and declare package <pkg>_test. Only the package clause is needed,
// so the parse is PackageClauseOnly.
func externalTestPackageViolations(p *packages.Package, fpath string) []fileLayoutViolation {
	name := filepath.Base(fpath)
	f, err := parser.ParseFile(token.NewFileSet(), fpath, nil, parser.PackageClauseOnly)
	if err != nil {
		return []fileLayoutViolation{{p.PkgPath, name, "scenario-tests-only", "unparseable: " + err.Error()}}
	}
	if f.Name.Name != p.Name+"_test" {
		return []fileLayoutViolation{{p.PkgPath, name, "test-package-not-external", "want package " + p.Name + "_test"}}
	}
	return nil
}

// contractFakeLoadMode is what contractFakeViolations needs from a Tests:true
// load: the test variants with their syntax and full type information.
const contractFakeLoadMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
	packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax

// contractFakeViolations is the in-file-fake half of ARCH-TEST-COMPOSE: it
// reports every type declared in a _test.go file of a classified component
// package (in-package or external test variant) whose method set implements a
// generated contract interface (go/types Implements, pointer receivers
// included). A contract interface is an exported interface with at least one
// method, declared in a generated (*.gen.go) file of a classified component
// package that the test package is, or imports directly - a fake has to name
// the contract's types to implement it, so a direct import is necessary.
// pkgs must come from a Tests:true load with contractFakeLoadMode.
func contractFakeViolations(pkgs []*packages.Package, spec Spec) []fileLayoutViolation {
	idx := makeLayerIndex(spec)
	var out []fileLayoutViolation
	for _, p := range pkgs {
		out = append(out, packageFakeViolations(p, idx, spec)...)
	}
	return out
}

// packageFakeViolations is contractFakeViolations for one loaded package (a production
// package, an in-package test variant or an external test package).
func packageFakeViolations(p *packages.Package, idx func(string) (int, bool), spec Spec) []fileLayoutViolation {
	if p.Types == nil || strings.HasSuffix(p.PkgPath, ".test") {
		return nil
	}
	base := strings.TrimSuffix(p.PkgPath, "_test")
	layer, _, ok := layerFor(idx, spec, base)
	if !ok || layer.FileStereotype == "" {
		return nil
	}
	fakes := testDeclaredTypes(p)
	var out []fileLayoutViolation
	for _, c := range contractInterfaces(p, idx) {
		for _, f := range fakes {
			if implementsEither(f.typ, c.iface) {
				out = append(out, fileLayoutViolation{base, f.file, ruleTestCompose,
					"type " + f.name + " implements the generated contract " + c.pkg + "." + c.name + "; the subject comes from scenariocompose"})
			}
		}
	}
	return out
}

// declaredType is one non-interface named type declared in a _test.go file.
type declaredType struct {
	name, file string
	typ        types.Type
}

// testDeclaredTypes lists the package-scope named non-interface, non-generic
// types p declares in its _test.go files, in scope order.
func testDeclaredTypes(p *packages.Package) []declaredType {
	var out []declaredType
	scope := p.Types.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		file := p.Fset.Position(tn.Pos()).Filename
		if !strings.HasSuffix(file, "_test.go") {
			continue
		}
		named, ok := tn.Type().(*types.Named)
		if !ok || named.TypeParams().Len() > 0 || types.IsInterface(named) {
			continue
		}
		out = append(out, declaredType{name: name, file: filepath.Base(file), typ: named})
	}
	return out
}

// implementsEither reports whether t or *t implements iface.
func implementsEither(t types.Type, iface *types.Interface) bool {
	return types.Implements(t, iface) || types.Implements(types.NewPointer(t), iface)
}

// contractIface is one generated contract interface visible to a test package.
type contractIface struct {
	pkg, name string
	iface     *types.Interface
}

// contractInterfaces collects the generated contract interfaces of the classified
// component packages p is or directly imports.
func contractInterfaces(p *packages.Package, idx func(string) (int, bool)) []contractIface {
	cands := []*packages.Package{p}
	for _, ip := range p.Imports {
		cands = append(cands, ip)
	}
	var out []contractIface
	seen := map[string]bool{}
	for _, c := range cands {
		if c.Types == nil || seen[c.Types.Path()] {
			continue
		}
		seen[c.Types.Path()] = true
		if _, ok := idx(strings.TrimSuffix(c.PkgPath, "_test")); ok {
			out = append(out, generatedInterfaces(c)...)
		}
	}
	return out
}

// generatedInterfaces lists the exported, non-empty interfaces c declares in generated
// (*.gen.go) files: the service-contract interfaces.
func generatedInterfaces(c *packages.Package) []contractIface {
	var out []contractIface
	scope := c.Types.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !tn.Exported() || !strings.HasSuffix(c.Fset.Position(tn.Pos()).Filename, ".gen.go") {
			continue
		}
		if it, ok := tn.Type().Underlying().(*types.Interface); ok && it.NumMethods() > 0 {
			out = append(out, contractIface{pkg: c.Types.Name(), name: name, iface: it})
		}
	}
	return out
}

// workflowFuncs returns the name of every top-level func in f whose parameter
// list includes a workflow.Context-typed parameter (matched by selector NAME,
// not import path — see file header). This includes both workflow ENTRY
// funcs and non-entry context-taking helpers; it feeds the two rules that
// apply to any context-taking func regardless of entry status
// (workflow-in-impl-file, workflow-func-outside-manager). Use
// workflowEntryFuncs for the rule-2 (one-workflow-file) classification.
func workflowFuncs(f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || !hasWorkflowContextParam(fd) {
			continue
		}
		out = append(out, fd.Name.Name)
	}
	return out
}

// workflowEntryFuncs returns the name of every top-level func in f that is a
// workflow ENTRY func: its name ends in "Workflow" AND it takes a
// workflow.Context param — both conditions required (see file header). This
// is the rule-2 classification key.
func workflowEntryFuncs(f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || !hasWorkflowContextParam(fd) || !strings.HasSuffix(fd.Name.Name, "Workflow") {
			continue
		}
		out = append(out, fd.Name.Name)
	}
	return out
}

func hasWorkflowContextParam(fd *ast.FuncDecl) bool {
	if fd.Type.Params == nil {
		return false
	}
	for _, param := range fd.Type.Params.List {
		if isWorkflowContextType(param.Type) {
			return true
		}
	}
	return false
}

// isWorkflowContextType reports whether expr is the selector expression
// "workflow.Context".
func isWorkflowContextType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == "workflow" && sel.Sel.Name == "Context"
}

// registerActivityCalls returns the name of every RegisterActivity /
// RegisterActivityWithOptions call in f, regardless of receiver — hand files
// must never register Activities directly; registration flows exclusively
// through the generated worker entrypoint.
func registerActivityCalls(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "RegisterActivity" || sel.Sel.Name == "RegisterActivityWithOptions" {
			out = append(out, sel.Sel.Name)
		}
		return true
	})
	return out
}
