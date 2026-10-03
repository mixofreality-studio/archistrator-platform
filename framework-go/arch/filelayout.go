package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
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
// (3) the scenario-test pair, and ONLY that pair (rule scenario-tests-only):
// the generated scenarios file <stereotype>_scenarios.gen_test.go and the
// agent-owned hooks file <stereotype>_hooks_test.go. Both must be black-box —
// declare package <leaf>_test (rule test-package-not-external) — and the hooks
// file may import only the component's own package, the standard library, a
// platform test double package (…/testinfra), the scenario host
// (…scenariohost) and the consuming module's Spec.HooksImportAllowlist
// prefixes (rule hooks-import-not-allowed). Any other _test.go file in a
// component package — a hand-written unit test, a white-box test, the old
// <stereotype>_test.go — is a scenario-tests-only violation: the component's
// tests are DERIVED from its use cases and test plan, never hand-authored.
// The generated file is named *.gen_test.go, not *_test.gen.go: the Go tool
// compiles ONLY files ending in _test.go as tests, so a "_test.gen.go" file
// declaring package <leaf>_test is a second package in the directory and
// breaks the build — the ".gen" marker therefore sits before the mandatory
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
// Scenario-test checking is done via os.ReadDir on the package directory plus
// an ImportsOnly parse of each allowed test file, rather than by loading with
// packages.Load(Tests:true): Tests:true would double the load (a separate
// test-variant package per production package) and, worse, FAIL the load when
// a hooks file imports a package the module does not yet depend on — when the
// rule only needs file names, the package clause and the import list.
// os.ReadDir + parser.ImportsOnly is simpler, avoids the double-load, needs no
// type information, and sees an external (foo_test package) test file for
// free since it lists names, not import graphs.
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
	hooksFile := layer.FileStereotype + "_hooks_test.go"
	testFiles := scenariosFile + " and " + hooksFile

	var out []fileLayoutViolation
	out = append(out, scenarioTestViolations(p, spec, scenariosFile, hooksFile)...)

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

// scenarioTestViolations enforces spec T2 on p's package directory: the only
// _test.go files in a component package are the generated scenarios file and
// its hooks file (scenario-tests-only), both declare the external
// <pkg>_test package (test-package-not-external), and the hooks file's
// imports stay inside hooksImportAllowed (hooks-import-not-allowed). See the
// file header for why this is a directory listing + ImportsOnly parse rather
// than a Tests:true load.
func scenarioTestViolations(p *packages.Package, spec Spec, scenariosFile, hooksFile string) []fileLayoutViolation {
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
		if name != scenariosFile && name != hooksFile {
			out = append(out, fileLayoutViolation{p.PkgPath, name, "scenario-tests-only", "want only " + scenariosFile + " and " + hooksFile})
			continue
		}
		out = append(out, allowedTestFileViolations(p, spec, filepath.Join(dir, name), name == hooksFile)...)
	}
	return out
}

// allowedTestFileViolations checks one of the two allowed test files: it must
// parse, must declare package <pkg>_test, and — when it is the hooks file —
// may import only what hooksImportAllowed permits. Only the package clause
// and the import list are needed, so the parse is ImportsOnly.
func allowedTestFileViolations(p *packages.Package, spec Spec, fpath string, isHooks bool) []fileLayoutViolation {
	name := filepath.Base(fpath)
	f, err := parser.ParseFile(token.NewFileSet(), fpath, nil, parser.ImportsOnly)
	if err != nil {
		return []fileLayoutViolation{{p.PkgPath, name, "scenario-tests-only", "unparseable: " + err.Error()}}
	}
	var out []fileLayoutViolation
	if f.Name.Name != p.Name+"_test" {
		out = append(out, fileLayoutViolation{p.PkgPath, name, "test-package-not-external", "want package " + p.Name + "_test"})
	}
	if !isHooks {
		return out
	}
	for _, imp := range f.Imports {
		ipath := strings.Trim(imp.Path.Value, `"`)
		if !hooksImportAllowed(ipath, p.PkgPath, spec) {
			out = append(out, fileLayoutViolation{p.PkgPath, name, "hooks-import-not-allowed", ipath})
		}
	}
	return out
}

// hooksImportAllowed reports whether a hooks test file in package own may
// import importPath: the component's own package and the standard library
// always; a platform test double package (path ending in "/testinfra") and
// the scenario host (any path SEGMENT ending in "scenariohost" — the module
// is framework-go-scenariohost, so a "/scenariohost" substring would never
// match its real import path) always; and any Spec.HooksImportAllowlist
// prefix — the consuming module's generated contract packages, typically.
func hooksImportAllowed(importPath, own string, spec Spec) bool {
	if importPath == own || isStdlibImport(importPath) {
		return true
	}
	if strings.HasSuffix(importPath, "/testinfra") {
		return true
	}
	for seg := range strings.SplitSeq(importPath, "/") {
		if strings.HasSuffix(seg, "scenariohost") {
			return true
		}
	}
	for _, pre := range spec.HooksImportAllowlist {
		if strings.HasPrefix(importPath, pre) {
			return true
		}
	}
	return false
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
