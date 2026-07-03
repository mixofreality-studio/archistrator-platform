package arch

import (
	"go/ast"
	"go/types"
	"os"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// gensurface.go is the ENCAPSULATION GATE, ported from the archistrator app's
// TestGeneratedOnlyPublic into a reusable framework checker.
//
// Founder invariant: the ONLY exported (public) symbols a component may carry are
// its GENERATED CONTRACT SURFACE plus a small, DOCUMENTED allowlist of legitimate
// exceptions. No other public code may exist in a component. CheckGeneratedSurface
// is the executable form of "no other public code can exist beyond the generated
// contract."
//
// TARGETING is keyed off the *.gen.go convention: a package is subject to the gate
// iff it contains ≥1 *.gen.go file (a generated contract). Packages with no
// generated file (hand-written Clients, Utilities, pure leaf helpers) are not
// components with a generated surface and are left alone.
//
// For each target package the checker computes two sets and fails on the difference:
//
//	GENERATED SURFACE =
//	  (a) every exported top-level identifier declared in the package's *.gen.go
//	      files (the generated interface, impl struct, constructor, and contract
//	      value types), PLUS
//	  (b) the TRANSITIVE CLOSURE of in-package exported TYPES structurally reachable
//	      from (a) — a hand-written type a generated operation traffics in (a field
//	      of a generated struct, a param/result of a generated method/constructor) IS
//	      contract surface even though hand-declared.
//	  A const/var whose declared TYPE is in the surface is itself surface (enum
//	  members). A method is surface when it implements a generated interface method
//	  OR its receiver type is in the surface.
//
//	ACTUAL SURFACE = every exported top-level identifier (and exported method on an
//	  exported type) across the package's non-test, non-gen .go files.
//
// FAIL when an actual-exported symbol is neither generated surface NOR allowlisted.
// The allowlist is data — per-package symbol sets keyed by the package path relative
// to spec.ModulePrefix (e.g. "engine/validatingengine"); each value is the allowed
// identifier names (or "Recv.Method" for a specific method). Allowlisting a TYPE
// name implicitly allows its methods. Prefer UNEXPORTING over allowlisting.

// surfaceViolation is one exported symbol that is neither generated contract surface
// nor allowlisted.
type surfaceViolation struct {
	rel  string // package path relative to ModulePrefix
	kind string // "type" | "const" | "var" | "func" | "method"
	sym  string // the symbol (or "Recv.Method")
}

// CheckGeneratedSurface loads the module named by spec and reports, via t.Errorf,
// every exported symbol in a generated-contract package that is neither part of the
// generated surface nor on the allowlist. The allowlist maps a ModulePrefix-relative
// package path to its permitted exported identifiers. Setting the ENCAP_DUMP env var
// prints the flagged symbols per package (the maintenance hook to (re)generate an
// allowlist after a contract change) instead of failing.
func CheckGeneratedSurface(t *testing.T, spec Spec, allowlist map[string][]string) {
	t.Helper()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedDeps | packages.NeedImports,
		Dir:   spec.ModuleRoot,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, spec.Patterns...)
	if err != nil {
		t.Fatalf("arch: packages.Load: %v", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("arch: %d package load error(s); fix the build before checking the generated surface", n)
	}

	allow := make(map[string]map[string]bool, len(allowlist))
	for pkg, names := range allowlist {
		set := make(map[string]bool, len(names))
		for _, n := range names {
			set[n] = true
		}
		allow[pkg] = set
	}

	vios := generatedSurfaceViolations(pkgs, spec.ModulePrefix, allow)

	if os.Getenv("ENCAP_DUMP") != "" {
		dumpSurfaceViolations(t, vios)
		return
	}
	for _, v := range vios {
		t.Errorf("%s: exported %s %s is neither generated contract surface nor allowlisted "+
			"(unexport it, or add it to the allowlist with a category justification)", v.rel, v.kind, v.sym)
	}
}

// generatedSurfaceViolations is the pure core: it returns every offending exported
// symbol across the target (generated-contract) packages, deterministically ordered.
func generatedSurfaceViolations(pkgs []*packages.Package, modulePrefix string, allow map[string]map[string]bool) []surfaceViolation {
	var out []surfaceViolation
	for _, p := range pkgs {
		if !hasGeneratedFile(p) {
			continue
		}
		rel := strings.TrimPrefix(p.PkgPath, modulePrefix)
		out = append(out, packageSurfaceViolations(p, rel, allow[rel])...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].rel != out[j].rel {
			return out[i].rel < out[j].rel
		}
		return out[i].sym < out[j].sym
	})
	return out
}

// hasGeneratedFile reports whether a package carries ≥1 *.gen.go compiled file.
func hasGeneratedFile(p *packages.Package) bool {
	for _, f := range p.CompiledGoFiles {
		if strings.HasSuffix(f, ".gen.go") {
			return true
		}
	}
	return false
}

// packageSurfaceViolations flags every exported symbol in p's non-gen, non-test
// files that is neither generated surface nor allowlisted.
func packageSurfaceViolations(p *packages.Package, rel string, allow map[string]bool) []surfaceViolation {
	genNames, genIface, seeds := genSurface(p)
	closure := typeClosure(p, seeds)
	surface := func(name string) bool {
		return genNames[name] || closure[name] || allow[name]
	}
	var out []surfaceViolation
	flag := func(kind, sym string) { out = append(out, surfaceViolation{rel: rel, kind: kind, sym: sym}) }

	for i, f := range p.Syntax {
		if strings.HasSuffix(p.CompiledGoFiles[i], ".gen.go") {
			continue
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				flagGenDecl(p, d, surface, flag)
			case *ast.FuncDecl:
				flagFuncDecl(d, genIface, surface, allow, flag)
			}
		}
	}
	return out
}

func flagGenDecl(p *packages.Package, d *ast.GenDecl, surface func(string) bool, flag func(kind, sym string)) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if s.Name.IsExported() && !surface(s.Name.Name) {
				flag("type", s.Name.Name)
			}
		case *ast.ValueSpec:
			flagValueSpec(p, d, s, surface, flag)
		}
	}
}

func flagValueSpec(p *packages.Package, d *ast.GenDecl, s *ast.ValueSpec, surface func(string) bool, flag func(kind, sym string)) {
	for _, n := range s.Names {
		if !n.IsExported() || surface(n.Name) {
			continue
		}
		// A const/var whose declared TYPE is contract surface is itself contract
		// surface (enum members).
		if tn := valueTypeName(p, n.Name); tn != "" && surface(tn) {
			continue
		}
		flag(valueKeyword(d), n.Name)
	}
}

func flagFuncDecl(d *ast.FuncDecl, genIface map[string]bool, surface func(string) bool, allow map[string]bool, flag func(kind, sym string)) {
	if !d.Name.IsExported() {
		return
	}
	if d.Recv == nil {
		if !surface(d.Name.Name) {
			flag("func", d.Name.Name)
		}
		return
	}
	recv := recvTypeName(d.Recv)
	if recv == "" || !ast.IsExported(recv) {
		return // method on an unexported type is invisible externally
	}
	// A method is surface when it implements a generated interface method, its
	// receiver is contract surface, or it is explicitly allowlisted as "Recv.Method".
	if genIface[d.Name.Name] || surface(recv) || allow[recv+"."+d.Name.Name] {
		return
	}
	flag("method", recv+"."+d.Name.Name)
}

func dumpSurfaceViolations(t *testing.T, vios []surfaceViolation) {
	t.Helper()
	byRel := map[string][]string{}
	for _, v := range vios {
		byRel[v.rel] = append(byRel[v.rel], v.sym)
	}
	rels := make([]string, 0, len(byRel))
	for k := range byRel {
		rels = append(rels, k)
	}
	sort.Strings(rels)
	for _, k := range rels {
		syms := byRel[k]
		sort.Strings(syms)
		t.Logf("\t%q: {", k)
		for _, s := range syms {
			t.Logf("\t\t%q,", s)
		}
		t.Logf("\t},")
	}
}

// genSurface returns the exported names declared in *.gen.go files, the set of
// interface method names declared there, and the seed types to expand into the
// transitive closure.
func genSurface(p *packages.Package) (names map[string]bool, ifaceMethods map[string]bool, seeds []types.Type) {
	names = map[string]bool{}
	ifaceMethods = map[string]bool{}
	for i, f := range p.Syntax {
		if !strings.HasSuffix(p.CompiledGoFiles[i], ".gen.go") {
			continue
		}
		for _, decl := range f.Decls {
			seeds = append(seeds, genSurfaceDecl(p, decl, names, ifaceMethods)...)
		}
	}
	return names, ifaceMethods, seeds
}

func genSurfaceDecl(p *packages.Package, decl ast.Decl, names, ifaceMethods map[string]bool) []types.Type {
	switch d := decl.(type) {
	case *ast.GenDecl:
		return genSurfaceGenDecl(p, d, names, ifaceMethods)
	case *ast.FuncDecl:
		return genSurfaceFuncDecl(p, d, names)
	}
	return nil
}

func genSurfaceGenDecl(p *packages.Package, d *ast.GenDecl, names, ifaceMethods map[string]bool) []types.Type {
	var seeds []types.Type
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			seeds = append(seeds, genSurfaceTypeSpec(p, s, names, ifaceMethods)...)
		case *ast.ValueSpec:
			seeds = append(seeds, genSurfaceValueSpec(p, s, names)...)
		}
	}
	return seeds
}

func genSurfaceTypeSpec(p *packages.Package, s *ast.TypeSpec, names, ifaceMethods map[string]bool) []types.Type {
	if !s.Name.IsExported() {
		return nil
	}
	names[s.Name.Name] = true
	var seeds []types.Type
	if obj := p.Types.Scope().Lookup(s.Name.Name); obj != nil {
		seeds = append(seeds, obj.Type())
	}
	recordInterfaceMethods(s, ifaceMethods)
	return seeds
}

func recordInterfaceMethods(s *ast.TypeSpec, ifaceMethods map[string]bool) {
	it, ok := s.Type.(*ast.InterfaceType)
	if !ok || it.Methods == nil {
		return
	}
	for _, m := range it.Methods.List {
		for _, nm := range m.Names {
			ifaceMethods[nm.Name] = true
		}
	}
}

func genSurfaceValueSpec(p *packages.Package, s *ast.ValueSpec, names map[string]bool) []types.Type {
	var seeds []types.Type
	for _, n := range s.Names {
		if !n.IsExported() {
			continue
		}
		names[n.Name] = true
		if obj := p.Types.Scope().Lookup(n.Name); obj != nil {
			seeds = append(seeds, obj.Type())
		}
	}
	return seeds
}

func genSurfaceFuncDecl(p *packages.Package, d *ast.FuncDecl, names map[string]bool) []types.Type {
	if d.Recv != nil || !d.Name.IsExported() {
		return nil
	}
	names[d.Name.Name] = true
	if obj := p.Types.Scope().Lookup(d.Name.Name); obj != nil {
		return []types.Type{obj.Type()}
	}
	return nil
}

// typeClosure returns the set of in-package exported type names structurally
// reachable from the seed types (the data the generated contract traffics in).
func typeClosure(p *packages.Package, seeds []types.Type) map[string]bool {
	out := map[string]bool{}
	visited := map[types.Type]bool{}
	var walk func(t types.Type)
	walk = func(t types.Type) {
		if t == nil || visited[t] {
			return
		}
		visited[t] = true
		for _, child := range typeChildren(t, p, out) {
			walk(child)
		}
	}
	for _, t := range seeds {
		walk(t)
	}
	return out
}

// typeChildren records t's exported in-package name (when t is such a Named type)
// into out, and returns the immediate child types to continue the walk into.
func typeChildren(t types.Type, p *packages.Package, out map[string]bool) []types.Type {
	switch x := t.(type) {
	case *types.Named:
		return namedChildren(x, p, out)
	case *types.Pointer:
		return []types.Type{x.Elem()}
	case *types.Slice:
		return []types.Type{x.Elem()}
	case *types.Array:
		return []types.Type{x.Elem()}
	case *types.Map:
		return []types.Type{x.Key(), x.Elem()}
	case *types.Chan:
		return []types.Type{x.Elem()}
	case *types.Struct:
		return structFieldTypes(x)
	case *types.Signature:
		return append(tupleTypes(x.Params()), tupleTypes(x.Results())...)
	case *types.Interface:
		return interfaceMethodTypes(x)
	}
	return nil
}

func namedChildren(x *types.Named, p *packages.Package, out map[string]bool) []types.Type {
	if obj := x.Obj(); obj != nil && obj.Pkg() == p.Types && obj.Exported() {
		out[obj.Name()] = true
	}
	children := []types.Type{x.Underlying()}
	for i := 0; i < x.NumMethods(); i++ {
		children = append(children, x.Method(i).Type())
	}
	return children
}

func structFieldTypes(x *types.Struct) []types.Type {
	out := make([]types.Type, 0, x.NumFields())
	for i := 0; i < x.NumFields(); i++ {
		out = append(out, x.Field(i).Type())
	}
	return out
}

func interfaceMethodTypes(x *types.Interface) []types.Type {
	out := make([]types.Type, 0, x.NumMethods())
	for i := 0; i < x.NumMethods(); i++ {
		out = append(out, x.Method(i).Type())
	}
	return out
}

func tupleTypes(tup *types.Tuple) []types.Type {
	if tup == nil {
		return nil
	}
	out := make([]types.Type, 0, tup.Len())
	for i := 0; i < tup.Len(); i++ {
		out = append(out, tup.At(i).Type())
	}
	return out
}

// valueTypeName returns the simple name of the in-package named type of a top-level
// const/var, or "" if it has none in this package.
func valueTypeName(p *packages.Package, name string) string {
	obj := p.Types.Scope().Lookup(name)
	if obj == nil {
		return ""
	}
	if named, ok := obj.Type().(*types.Named); ok {
		if named.Obj() != nil && named.Obj().Pkg() == p.Types {
			return named.Obj().Name()
		}
	}
	return ""
}

func valueKeyword(d *ast.GenDecl) string {
	if d.Tok.String() == "const" {
		return "const"
	}
	return "var"
}

func recvTypeName(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	t := fl.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
