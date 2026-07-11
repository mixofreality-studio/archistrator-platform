package arch

import (
	"go/ast"
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
// code; (2) on the Manager layer only, one per-workflow file per exported
// func taking a workflow.Context param, named after the func (minus the
// "Workflow" suffix, lowercased); (3) the single test file
// <stereotype>_test.go. Anywhere in scope, a hand call to
// RegisterActivity/RegisterActivityWithOptions is forbidden (registration is
// reserved for the generated worker), and a workflow.Context func outside the
// Manager layer is forbidden regardless of its file.
//
// workflow.Context is matched by the AST selector's NAME ("workflow.Context"),
// not by resolved import path, so both a real go.temporal.io/sdk/workflow
// import and a fixture's local stub package satisfy the rule identically —
// deliberately, since the check must work for consumers without pulling the
// real Temporal SDK into this framework module or its testdata fixtures.
//
// Test-file-name checking is done via os.ReadDir on the package directory
// rather than by loading with packages.Load(Tests:true): Tests:true would
// double the load (a separate test-variant package per production package)
// just to observe file NAMES, when the rule only needs to know what _test.go
// files exist on disk — never their parsed contents. os.ReadDir is simpler,
// avoids the double-load, and picks up an external (foo_test package) test
// file for free since it lists names, not import graphs.
//
// fileLayoutViolations is the pure core: it takes an already-loaded package
// set (Tests:false, matching gensurface.go's load mode) and returns every
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
	testFile := layer.FileStereotype + "_test.go"

	var out []fileLayoutViolation
	out = append(out, testFileNameViolations(p, testFile)...)

	for i, fpath := range p.CompiledGoFiles {
		base := filepath.Base(fpath)
		if strings.HasSuffix(base, ".gen.go") {
			continue
		}
		f := p.Syntax[i]
		for _, call := range registerActivityCalls(f) {
			out = append(out, fileLayoutViolation{p.PkgPath, base, "hand-activity-registration", call})
		}
		out = append(out, fileHandwrittenViolations(p.PkgPath, base, implFile, testFile, layer, spec, workflowFuncs(f))...)
	}
	return out
}

// fileHandwrittenViolations classifies a single handwritten (non *.gen.go)
// file against the closed allowed set and returns every structural violation
// for it (a workflow file can fail both the one-func-per-file rule and the
// filename rule at once). wfFuncs is the file's list of workflow.Context func
// names.
func fileHandwrittenViolations(pkgPath, base, implFile, testFile string, layer Layer, spec Spec, wfFuncs []string) []fileLayoutViolation {
	var out []fileLayoutViolation
	switch {
	case base == implFile:
		if len(wfFuncs) > 0 {
			out = append(out, fileLayoutViolation{pkgPath, base, "workflow-in-impl-file",
				"workflow func " + wfFuncs[0] + " must live in its own per-workflow file, not " + implFile})
		}
	case len(wfFuncs) > 0:
		if layer.Name != spec.TemporalLayer {
			out = append(out, fileLayoutViolation{pkgPath, base, "workflow-func-outside-manager",
				"workflow func " + wfFuncs[0] + " found outside the " + spec.TemporalLayer + " layer"})
			return out
		}
		if len(wfFuncs) > 1 {
			out = append(out, fileLayoutViolation{pkgPath, base, "workflow-file-multiple-funcs", strings.Join(wfFuncs, ",")})
		}
		want := strings.ToLower(strings.TrimSuffix(wfFuncs[0], "Workflow")) + ".go"
		if base != want {
			out = append(out, fileLayoutViolation{pkgPath, base, "workflow-file-name", "want " + want})
		}
	default:
		out = append(out, fileLayoutViolation{pkgPath, base, "file-not-allowed",
			"handwritten files are limited to " + implFile + ", per-workflow files, " + testFile})
	}
	return out
}

// testFileNameViolations flags every _test.go file in p's package directory
// whose name is not testFile. See the file header for why this is a directory
// listing rather than a Tests:true load.
func testFileNameViolations(p *packages.Package, testFile string) []fileLayoutViolation {
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
		if name != testFile {
			out = append(out, fileLayoutViolation{p.PkgPath, name, "test-file-name", "want " + testFile})
		}
	}
	return out
}

// workflowFuncs returns the name of every top-level func in f whose parameter
// list includes a workflow.Context-typed parameter (matched by selector NAME,
// not import path — see file header).
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
