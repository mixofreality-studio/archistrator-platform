// Package pushgate is a go/packages, TYPE-based architecture check that every git
// push a module's production code performs runs inside a sanctioned per-repo
// lock wrapper — github.PushLocal — so no in-process writer can race another
// into a local, checked-out state repo (FIX-002). A consuming module runs Check
// from an ordinary go test, in the style of framework-go/arch.
//
// It recognises a push by TYPE, not by text:
//
//   - any reference (call OR method value) to Push / PushContext on go-git's
//     *Repository or *Remote;
//   - an os/exec Command / CommandContext call whose program is the constant
//     "git" and one of whose constant arguments is "push";
//   - a call carrying a constant "push" to a GIT RUNNER: a function that spreads
//     its own arguments into exec.Command("git", ...), found to a fixpoint (a
//     function that spreads into a runner is a runner too).
//
// A push site is WRAPPED when it is lexically inside a function literal passed
// directly as an argument to a sanctioned wrapper call, or when its enclosing
// function is UNEXPORTED, has at least one reference in the loaded packages, and
// every reference is a call that is itself wrapped (recursively). An exported
// function, a function referenced as a value, or one reached only through an
// interface is never wrapped through its callers — the gate fails closed.
//
// The only opt-out is Spec.Exempt: named functions that push to a NON-local
// remote (e.g. a GitHub origin). Each entry must name a function that really
// pushes, or the stale exemption is itself a violation.
package pushgate

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// goGitPkg is the go-git v5 package whose Repository/Remote push.
const goGitPkg = "github.com/go-git/go-git/v5"

// Func names a function or method by its go/types FullName, e.g.
// "github.com/x/github.PushLocal" or "(*github.com/x/pkg.T).method".
type Func string

// Spec parameterises Check for one consuming module.
type Spec struct {
	ModuleRoot string   // dir to run packages.Load from
	Patterns   []string // load patterns, e.g. "./internal/resourceaccess/..."
	// Wrappers are the sanctioned lock wrappers: a function literal passed to one
	// of these runs under the per-repo lock. Typically just PushLocalFunc.
	Wrappers []Func
	// Exempt are functions allowed to push unwrapped because their remote is not
	// local. Every entry must contain a push site.
	Exempt []Func
}

// PushLocalFunc is the FullName of github.PushLocal, the canonical wrapper.
const PushLocalFunc Func = "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github.PushLocal"

// Violation is one push that is not provably under a sanctioned wrapper.
type Violation struct {
	Pos  token.Position
	Func string // enclosing function's FullName ("" at package scope)
	What string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s in %s", v.Pos, v.What, v.Func)
}

// Check loads spec.Patterns and fails t for every Violation.
func Check(t testing.TB, spec Spec) {
	t.Helper()
	vs, err := Find(spec)
	if err != nil {
		t.Fatalf("pushgate: %v", err)
	}
	for _, v := range vs {
		t.Errorf("pushgate: %s — run every local push inside github.PushLocal (FIX-002)", v)
	}
}

// Find loads spec.Patterns (production code only) and returns every violation,
// sorted by position.
func Find(spec Spec) ([]Violation, error) {
	if len(spec.Wrappers) == 0 {
		return nil, fmt.Errorf("spec names no wrapper")
	}
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir: spec.ModuleRoot,
	}, spec.Patterns...)
	if err != nil {
		return nil, err
	}
	var loadErrs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			loadErrs = append(loadErrs, e.Error())
		}
	})
	if len(loadErrs) > 0 {
		return nil, fmt.Errorf("load errors:\n%s", strings.Join(loadErrs, "\n"))
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("patterns %v matched no packages", spec.Patterns)
	}
	g := newGate(spec, pkgs)
	return g.run(), nil
}

// node is an AST node with the file context needed to walk its ancestry.
type node struct {
	n       ast.Node
	parents map[ast.Node]ast.Node
	info    *types.Info
}

type gate struct {
	wrappers map[string]bool
	exempt   map[string]bool
	files    []fileCtx
	refs     map[string][]node // FullName → every Ident referring to it
	exported map[string]bool   // FullName → declared exported
	runners  map[string]bool   // FullName → git runner
	memo     map[string]int    // covered memo: 0 unknown, 1 in progress, 2 yes, 3 no
}

type fileCtx struct {
	file    *ast.File
	parents map[ast.Node]ast.Node
	info    *types.Info
	fset    *token.FileSet
}

func newGate(spec Spec, pkgs []*packages.Package) *gate {
	g := &gate{
		wrappers: setOf(spec.Wrappers), exempt: setOf(spec.Exempt),
		refs: map[string][]node{}, exported: map[string]bool{},
		runners: map[string]bool{}, memo: map[string]int{},
	}
	for _, p := range pkgs {
		g.indexPackage(p)
	}
	g.findRunners()
	return g
}

// indexPackage records p's files and its function references and declarations.
// Uses/Defs are per PACKAGE, so each referring Ident is attributed to the file
// that holds it (its ancestry lives in that file's parent map).
func (g *gate) indexPackage(p *packages.Package) {
	pkgFiles := make([]fileCtx, 0, len(p.Syntax))
	for _, f := range p.Syntax {
		pkgFiles = append(pkgFiles, fileCtx{file: f, parents: parentMap(f), info: p.TypesInfo, fset: p.Fset})
	}
	g.files = append(g.files, pkgFiles...)
	for id, obj := range p.TypesInfo.Uses {
		fn, ok := obj.(*types.Func)
		if !ok {
			continue
		}
		if fc, ok := fileOf(pkgFiles, id.Pos()); ok {
			g.refs[fn.FullName()] = append(g.refs[fn.FullName()], node{n: id, parents: fc.parents, info: fc.info})
		}
	}
	for id, obj := range p.TypesInfo.Defs {
		if fn, ok := obj.(*types.Func); ok {
			g.exported[fn.FullName()] = id.IsExported()
		}
	}
}

func fileOf(files []fileCtx, pos token.Pos) (fileCtx, bool) {
	for _, fc := range files {
		if fc.file.FileStart <= pos && pos < fc.file.FileEnd {
			return fc, true
		}
	}
	return fileCtx{}, false
}

func setOf(fs []Func) map[string]bool {
	m := map[string]bool{}
	for _, f := range fs {
		m[string(f)] = true
	}
	return m
}

func parentMap(f *ast.File) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return parents
}

// calleeFunc resolves a call's static callee, or nil.
func calleeFunc(info *types.Info, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch fn := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = fn
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.IndexExpr: // generic instantiation
		if s, ok := fn.X.(*ast.SelectorExpr); ok {
			id = s.Sel
		} else if i, ok := fn.X.(*ast.Ident); ok {
			id = i
		}
	}
	if id == nil {
		return nil
	}
	f, _ := info.Uses[id].(*types.Func)
	return f
}

func constString(info *types.Info, e ast.Expr) (string, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

// execGitArgs returns the arguments after the program name when call is
// os/exec.Command/CommandContext with the constant program "git".
func execGitArgs(info *types.Info, call *ast.CallExpr) ([]ast.Expr, bool) {
	f := calleeFunc(info, call)
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != "os/exec" {
		return nil, false
	}
	prog := 0
	switch f.Name() {
	case "Command":
	case "CommandContext":
		prog = 1
	default:
		return nil, false
	}
	if len(call.Args) <= prog {
		return nil, false
	}
	if name, ok := constString(info, call.Args[prog]); !ok || name != "git" {
		return nil, false
	}
	return call.Args[prog+1:], true
}

func hasConstPush(info *types.Info, args []ast.Expr) bool {
	for _, a := range args {
		if s, ok := constString(info, a); ok && s == "push" {
			return true
		}
	}
	return false
}

// enclosingFunc returns the FullName of the FuncDecl containing n ("" if none).
func enclosingFunc(nd node) string {
	for cur := nd.n; cur != nil; cur = nd.parents[cur] {
		if fd, ok := cur.(*ast.FuncDecl); ok {
			if fn, ok := nd.info.Defs[fd.Name].(*types.Func); ok {
				return fn.FullName()
			}
			return ""
		}
	}
	return ""
}

// findRunners computes the git-runner set to a fixpoint.
func (g *gate) findRunners() {
	for g.findRunnersOnce() {
	}
}

// findRunnersOnce adds every function that spreads arguments into a git exec or
// a known runner; it reports whether the set grew.
func (g *gate) findRunnersOnce() bool {
	changed := false
	for _, fc := range g.files {
		ast.Inspect(fc.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !call.Ellipsis.IsValid() || !g.isGitInvocation(fc.info, call) {
				return true
			}
			encl := enclosingFunc(node{n: call, parents: fc.parents, info: fc.info})
			if encl != "" && !g.runners[encl] {
				g.runners[encl] = true
				changed = true
			}
			return true
		})
	}
	return changed
}

// isGitInvocation: call is exec.Command("git", ...) or a call to a git runner.
func (g *gate) isGitInvocation(info *types.Info, call *ast.CallExpr) bool {
	if _, ok := execGitArgs(info, call); ok {
		return true
	}
	f := calleeFunc(info, call)
	return f != nil && g.runners[f.FullName()]
}

// pushSite classifies n as a push, returning a description.
func (g *gate) pushSite(fc fileCtx, n ast.Node) (string, bool) {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		return goGitPush(fc.info, x)
	case *ast.CallExpr:
		if args, ok := execGitArgs(fc.info, x); ok && hasConstPush(fc.info, args) {
			return `exec git "push"`, true
		}
		if f := calleeFunc(fc.info, x); f != nil && g.runners[f.FullName()] && hasConstPush(fc.info, x.Args) {
			return `git runner ` + f.Name() + `("push")`, true
		}
	}
	return "", false
}

// goGitPush: x selects Push/PushContext on go-git's *Repository or *Remote (as a
// call or as a method value).
func goGitPush(info *types.Info, x *ast.SelectorExpr) (string, bool) {
	sel, ok := info.Selections[x]
	if !ok {
		return "", false
	}
	f, ok := sel.Obj().(*types.Func)
	if !ok || !isGoGitPushName(f) {
		return "", false
	}
	t := f.Signature().Recv().Type()
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || (named.Obj().Name() != "Repository" && named.Obj().Name() != "Remote") {
		return "", false
	}
	return "go-git (*" + named.Obj().Name() + ")." + f.Name(), true
}

func isGoGitPushName(f *types.Func) bool {
	return f.Pkg() != nil && f.Pkg().Path() == goGitPkg && (f.Name() == "Push" || f.Name() == "PushContext")
}

// lexicallyWrapped: some ancestor is a FuncLit passed directly to a wrapper call.
func (g *gate) lexicallyWrapped(nd node) bool {
	for cur := nd.n; cur != nil; cur = nd.parents[cur] {
		if lit, ok := cur.(*ast.FuncLit); ok && g.passedToWrapper(nd, lit) {
			return true
		}
	}
	return false
}

func (g *gate) passedToWrapper(nd node, lit *ast.FuncLit) bool {
	call, ok := nd.parents[lit].(*ast.CallExpr)
	if !ok {
		return false
	}
	f := calleeFunc(nd.info, call)
	return f != nil && g.wrappers[f.FullName()] && slices.Contains(call.Args, ast.Expr(lit))
}

func (g *gate) wrapped(nd node) bool {
	if g.lexicallyWrapped(nd) {
		return true
	}
	encl := enclosingFunc(nd)
	return encl != "" && g.covered(encl)
}

// covered: fn is unexported, referenced, and every reference is a wrapped call.
func (g *gate) covered(fn string) bool {
	switch g.memo[fn] {
	case 1, 3:
		return false
	case 2:
		return true
	}
	g.memo[fn] = 1
	ok := !g.exported[fn] && len(g.refs[fn]) > 0
	for _, ref := range g.refs[fn] {
		if !ok {
			break
		}
		ok = isCallOf(ref) && g.wrapped(ref)
	}
	if ok {
		g.memo[fn] = 2
	} else {
		g.memo[fn] = 3
	}
	return ok
}

// isCallOf: the referring Ident is the callee of a call (not a value use).
func isCallOf(ref node) bool {
	fun := ast.Node(ref.n)
	if sel, ok := ref.parents[fun].(*ast.SelectorExpr); ok && sel.Sel == ref.n {
		fun = sel
	}
	call, ok := ref.parents[fun].(*ast.CallExpr)
	return ok && call.Fun == fun
}

func (g *gate) run() []Violation {
	var vs []Violation
	used := map[string]bool{}
	for _, fc := range g.files {
		vs = append(vs, g.checkFile(fc, used)...)
	}
	for e := range g.exempt {
		if !used[e] {
			vs = append(vs, Violation{Func: e, What: "stale exemption: function has no push site (or does not exist)"})
		}
	}
	sort.Slice(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		if a.Pos.Filename != b.Pos.Filename {
			return a.Pos.Filename < b.Pos.Filename
		}
		if a.Pos.Offset != b.Pos.Offset {
			return a.Pos.Offset < b.Pos.Offset
		}
		return a.Func < b.Func
	})
	return vs
}

// checkFile reports fc's unwrapped push sites, marking exemptions it uses.
func (g *gate) checkFile(fc fileCtx, used map[string]bool) []Violation {
	var vs []Violation
	ast.Inspect(fc.file, func(n ast.Node) bool {
		what, ok := g.pushSite(fc, n)
		if !ok {
			return true
		}
		nd := node{n: n, parents: fc.parents, info: fc.info}
		encl := enclosingFunc(nd)
		switch {
		case g.exempt[encl]:
			used[encl] = true
		case !g.wrapped(nd):
			vs = append(vs, Violation{Pos: fc.fset.Position(n.Pos()), Func: encl, What: "unwrapped push: " + what})
		}
		return true
	})
	return vs
}
