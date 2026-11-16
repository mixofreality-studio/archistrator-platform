package appgen

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/plan"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// Severity is how an admission finding is handled: it refuses the write or it waits for Tier 2.
//
//sumtype:decl
type Severity interface{ isSeverity() }

// Local is a defect in this artifact: the write is refused.
type Local struct{}

// Incomplete is a finding that needs a part of the design that has not been recorded yet: the
// write is accepted with a warning, and Tier 2 enforces it at commit.
type Incomplete struct{}

func (Local) isSeverity()      {}
func (Incomplete) isSeverity() {}

// SeverityName is "local" or "incomplete". The switch has no default (gochecksumtype keeps it
// exhaustive).
func SeverityName(s Severity) string {
	switch s.(type) {
	case Local:
		return "local"
	case Incomplete:
		return "incomplete"
	}
	panic("unreachable: sealed Severity")
}

// Problem is one thing the generators did wrong, or could not do, over an artifact.
type Problem struct {
	Consumer, Path, Message string
	Severity                Severity
}

// AdmitTier1 emits every consumer of kind over in (Tree nil: the emission of a fully built
// module), parses every emitted .go file and typechecks each emitted package with go/types
// against an importer that resolves only the emitted siblings (D8). It returns every problem at
// once, sorted by consumer then path. A consumer whose Emit fails is one problem: Incomplete
// when its error names an id absent from the project (a component, contract or operation the
// design has not recorded yet), else Local.
//
// It is pure: no file system, no toolchain, no module cache. A package outside the emitted set
// (the standard library, framework-go, Temporal) is opaque, and a type error whose operand is a
// selector on an opaque package is dropped; it is Tier 2's. So is a name taken from the
// hand-written code of a contract's package, and the arguments of a variant constructor, which
// the app's appgen.json decides.
//
// Every consumer emits, not only those of kind, so that a package the artifact's consumers write
// resolves the siblings other consumers write beside it (cmd/server's main.gen.go names the
// Config `appgen config` writes). Only the failures of the artifact's own consumers, and the
// findings in their files, are reported.
func AdmitTier1(in Input, kind ArtifactKind) []Problem {
	in.Tree, in.Only = nil, nil
	reading := Consumers(kind)
	if len(reading) == 0 {
		return nil
	}
	src := &admitSources{fset: token.NewFileSet(), module: in.ModulePath, moduleDir: in.ModuleDir, byDir: map[string][]*admitFile{}, reporting: map[string]bool{}, partial: handWrittenPackages(in), designIncomplete: designIncomplete(in.Project)}
	for _, c := range reading {
		src.reporting[c.Name] = true
	}
	// A Manager the composition leaves unbuilt (a dependency with no arm in the deployment model yet)
	// has no scenario subject, which the tests that name it cannot find: the composition is not whole.
	var warn strings.Builder
	in.Warn = &warn
	problems := src.emitAll(in)
	src.designIncomplete = src.designIncomplete || strings.Contains(warn.String(), "manager left unbuilt")
	problems = append(problems, src.check()...)
	sort.SliceStable(problems, func(i, j int) bool { return problemLess(problems[i], problems[j]) })
	return problems
}

func problemLess(a, b Problem) bool {
	if a.Consumer != b.Consumer {
		return a.Consumer < b.Consumer
	}
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	return a.Message < b.Message
}

// absentID matches the generators' and the project model's own wording for an id the design has
// not recorded yet: a component, contract or operation that a record names and the project does
// not hold ("unknown component "ghostEngine""), a Manager contract an mcpApp client fronts that is
// still to be recorded, a test plan filed under a contract that is not there yet. Every other emit
// failure (an unknown layer, a source gofmt cannot parse) is a defect in the artifact itself.
var absentID = regexp.MustCompile(`\bunknown (component|contract|operation)\b|\bfronts no Manager\b|\bis not a serviceContracts key\b|\bthe deployment declares no "[^"]+" setting\b`)

// compositionConsumers are the consumers that wire the whole System together: what they emit
// needs every contract and the deployment model, so over a design still being recorded their
// findings name what is still to come.
var compositionConsumers = map[string]bool{
	"appgen compose": true, "appgen config": true, "appgen clients": true, "webgen bootstrap": true, "webgen generate": true,
}

// scenariocomposeRef matches a finding that names a subject the composition's scenario package builds.
var scenariocomposeRef = regexp.MustCompile(`\bscenariocompose\.`)

// severityOf is how a failure of consumer is handled: Incomplete when it names a part of the design
// not recorded yet, when it is a configuration the app's appgen.json decides, or when the design is
// still being recorded and the failure is the composition's (or a test's reference to it).
func (s *admitSources) severityOf(consumer, msg string) Severity {
	switch {
	case absentID.MatchString(msg), configBound(consumer, msg):
		return Incomplete{}
	case s.designIncomplete && (compositionConsumers[consumer] || scenariocomposeRef.MatchString(msg)):
		return Incomplete{}
	}
	return Local{}
}

// designIncomplete reports whether the System still has a built, contract-bearing component with no
// recorded contract: contracts are recorded one at a time, and the composition is only whole once
// the last one is. A project that does not load is not incomplete: its failure is its own.
func designIncomplete(raw []byte) bool {
	m, err := projectmodel.Load(raw)
	if err != nil || m.System == nil {
		return false
	}
	have := map[string]bool{}
	for key := range m.Contracts {
		if c, ok := m.System.ComponentByContractKey(key); ok {
			have[c.ID] = true
		}
	}
	return slices.ContainsFunc(m.System.Components, func(c projectmodel.SystemComponent) bool {
		return c.BuildStatus == "" && bearsContract(c.Kind) && !have[c.ID]
	})
}

// bearsContract reports whether a component kind records a service contract: a Client's is derived
// and a Resource has none.
func bearsContract(kind string) bool {
	switch strings.ToLower(strings.ReplaceAll(kind, " ", "")) {
	case "manager", "engine", "resourceaccess", "utility":
		return true
	}
	return false
}

// firstLine is an emit error's message up to its first line break: a generator that fails on its
// own output appends the whole source to the error.
func firstLine(err error) string {
	msg, _, _ := strings.Cut(err.Error(), "\n")
	return msg
}

// admitFile is one parsed emitted Go file.
type admitFile struct {
	consumer string
	path     string // repo-relative
	ast      *ast.File
}

type admitSources struct {
	fset      *token.FileSet
	module    string
	moduleDir string
	byDir     map[string][]*admitFile // repo-relative directory -> its parsed files
	reporting map[string]bool         // the consumers of the artifact: only their files' findings are reported
	partial   map[string]bool         // import paths of the packages that also hold hand-written code (a contract's goPackage)

	designIncomplete bool // the System still has contracts to record, or no deployment model: see designIncomplete
}

// handWrittenPackages is the import path of every contract package that holds code a person or
// an agent writes beside the generated files: every non-Client contract's goPackage. A Client's
// package is generated end to end.
func handWrittenPackages(in Input) map[string]bool {
	var doc struct {
		ServiceContracts map[string]struct {
			GoPackage string `json:"goPackage"`
			Layer     string `json:"layer"`
		} `json:"serviceContracts"`
	}
	out := map[string]bool{}
	if json.Unmarshal(in.Project, &doc) != nil {
		return out
	}
	for _, c := range doc.ServiceContracts {
		if c.GoPackage != "" && !strings.EqualFold(c.Layer, "client") {
			out[in.ModulePath+"/"+path.Clean(c.GoPackage)] = true
		}
	}
	return out
}

// emitAll runs every consumer, parses what it emits, and returns the failures that are the
// artifact's own: a consumer's emit error, a file that does not parse.
func (s *admitSources) emitAll(in Input) []Problem {
	var problems []Problem
	for _, c := range AllConsumers() {
		files, err := c.Emit(in)
		if err != nil {
			if s.reporting[c.Name] {
				problems = append(problems, Problem{Consumer: c.Name, Message: firstLine(err), Severity: s.severityOf(c.Name, err.Error())})
			}
			continue
		}
		problems = append(problems, s.addAll(c.Name, files)...)
	}
	return problems
}

// addAll parses the Go files of one consumer's emission. A Seam is the app's own once written:
// its seed is not what the generators emit, and what the app made of it is Tier 2's.
func (s *admitSources) addAll(consumer string, files []plan.File) []Problem {
	var problems []Problem
	for _, f := range files {
		if strings.HasSuffix(f.Path, ".go") && !plan.IsSeam(f.Role) {
			problems = append(problems, s.add(consumer, f)...)
		}
	}
	return problems
}

// add parses f; a parse error is a Local problem and the file is left out of the typecheck.
func (s *admitSources) add(consumer string, f plan.File) []Problem {
	parsed, err := parser.ParseFile(s.fset, f.Path, f.Content, parser.SkipObjectResolution)
	if err != nil {
		if !s.reporting[consumer] {
			return nil
		}
		return []Problem{{Consumer: consumer, Path: f.Path, Message: "parse: " + err.Error(), Severity: Local{}}}
	}
	dir := path.Dir(f.Path)
	s.byDir[dir] = append(s.byDir[dir], &admitFile{consumer: consumer, path: f.Path, ast: parsed})
	return nil
}

// importPath is the Go import path of a repo-relative directory inside the module; ok is false
// for a directory outside it.
func (s *admitSources) importPath(dir string) (string, bool) {
	rel := dir
	if s.moduleDir != "" && s.moduleDir != "." {
		switch {
		case dir == s.moduleDir:
			rel = "."
		case strings.HasPrefix(dir, s.moduleDir+"/"):
			rel = strings.TrimPrefix(dir, s.moduleDir+"/")
		default:
			return "", false
		}
	}
	if rel == "." {
		return s.module, true
	}
	return s.module + "/" + rel, true
}

// selectedNames is every identifier some selector expression of f selects from.
func selectedNames(f *ast.File) map[string]bool {
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	return used
}

// pathTokens are the words of an import path, last first.
func pathTokens(p string) []string {
	var tokens []string
	for _, seg := range strings.Split(p, "/") {
		tokens = append(tokens, strings.FieldsFunc(seg, func(r rune) bool { return r == '-' || r == '.' })...)
	}
	slices.Reverse(tokens)
	return tokens
}

// nameOpaqueImports gives every unnamed import of an opaque package the name the file actually
// uses for it. The importer cannot know a package's declared name (it never reads the package),
// and a wrong guess would make every use of the package an error. The name is the first word of
// the path, last first, that the file selects from; an explicit alias is exact for any declared
// name.
func nameOpaqueImports(f *ast.File, siblings map[string]string) {
	used := selectedNames(f)
	for _, spec := range f.Imports {
		p := strings.Trim(spec.Path.Value, "`\"")
		if _, sibling := siblings[p]; sibling || spec.Name != nil {
			continue
		}
		for _, t := range pathTokens(p) {
			if used[t] && token.IsIdentifier(t) {
				spec.Name = &ast.Ident{NamePos: spec.Path.Pos(), Name: t}
				break
			}
		}
	}
}

func isTestFile(f *admitFile) bool { return strings.HasSuffix(f.path, "_test.go") }

// siblings maps the import path of every emitted package (a directory holding a non-test file) to its directory.
func (s *admitSources) siblings() map[string]string {
	out := map[string]string{}
	for dir, files := range s.byDir {
		if ip, ok := s.importPath(dir); ok && slices.ContainsFunc(files, func(f *admitFile) bool { return !isTestFile(f) }) {
			out[ip] = dir
		}
	}
	return out
}

// reads reports whether any file of dir is one of the artifact's consumers'.
func (s *admitSources) reads(dir string) bool {
	return slices.ContainsFunc(s.byDir[dir], func(f *admitFile) bool { return s.reporting[f.consumer] })
}

// groupsOf groups a directory's files by package clause: the package itself (its files and its
// in-package tests) and its external test package are checked apart. The names come back sorted.
func (s *admitSources) groupsOf(dir string) (names []string, groups map[string][]*admitFile) {
	groups = map[string][]*admitFile{}
	for _, f := range s.byDir[dir] {
		groups[f.ast.Name.Name] = append(groups[f.ast.Name.Name], f)
	}
	for n := range groups {
		names = append(names, n)
	}
	slices.Sort(names)
	return names, groups
}

// check typechecks every package the artifact's consumers write (and its external test package)
// and returns the problems that are not an opaque package's to report.
func (s *admitSources) check() []Problem {
	byImport := s.siblings()
	for _, files := range s.byDir {
		for _, f := range files {
			nameOpaqueImports(f.ast, byImport)
		}
	}
	ck := &admitChecker{s: s, byImport: byImport, done: map[string]*types.Package{}, busy: map[string]bool{}, opaque: map[string]*types.Package{}}
	dirs := make([]string, 0, len(s.byDir))
	for dir := range s.byDir {
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	for _, dir := range dirs {
		ip, ok := s.importPath(dir)
		if !ok || !s.reads(dir) {
			continue // outside the Go module, or nothing here is the artifact's: checked only when imported
		}
		names, groups := s.groupsOf(dir)
		for _, n := range names {
			ck.checkGroup(ip, n, groups[n])
		}
	}
	return ck.problems
}

type admitChecker struct {
	s        *admitSources
	byImport map[string]string
	done     map[string]*types.Package // sibling import path -> its typechecked package (non-test files only)
	busy     map[string]bool
	opaque   map[string]*types.Package
	problems []Problem
}

// Import resolves a sibling emitted package, or an opaque stand-in for any other.
func (c *admitChecker) Import(p string) (*types.Package, error) {
	if dir, ok := c.byImport[p]; ok {
		return c.importSibling(p, dir)
	}
	if pkg, ok := c.opaque[p]; ok {
		return pkg, nil
	}
	pkg := types.NewPackage(p, path.Base(p))
	pkg.MarkComplete()
	c.opaque[p] = pkg
	return pkg, nil
}

// importSibling typechecks the non-test files of an emitted package, once, for its importers.
func (c *admitChecker) importSibling(p, dir string) (*types.Package, error) {
	if pkg, ok := c.done[p]; ok {
		return pkg, nil
	}
	if c.busy[p] {
		return nil, fmt.Errorf("import cycle through %s", p)
	}
	c.busy[p] = true
	defer delete(c.busy, p)
	var own []*admitFile
	for _, f := range c.s.byDir[dir] {
		if !isTestFile(f) {
			own = append(own, f)
		}
	}
	pkg, _ := c.typecheck(p, own, false)
	c.done[p] = pkg
	return pkg, nil
}

// checkGroup typechecks the files sharing a package clause in one directory, reporting what the
// emission got wrong. An external test package is checked under its own path.
func (c *admitChecker) checkGroup(importPath, name string, files []*admitFile) {
	if strings.HasSuffix(name, "_test") && slices.ContainsFunc(files, isTestFile) {
		importPath += "_test"
	}
	_, probs := c.typecheck(importPath, files, true)
	c.problems = append(c.problems, probs...)
}

// typecheck checks files as one package and returns it with the problems that are not opaque.
// report is false when the package is typechecked only to be imported: its problems are reported
// when its own directory is checked.
func (c *admitChecker) typecheck(importPath string, files []*admitFile, report bool) (*types.Package, []Problem) {
	var (
		probs []Problem
		asts  = make([]*ast.File, len(files))
		byTok = map[*token.File]*admitFile{}
	)
	for i, f := range files {
		asts[i] = f.ast
		byTok[c.s.fset.File(f.ast.Pos())] = f
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{
		Importer: c,
		Error: func(err error) {
			if !report {
				return
			}
			if p, ok := c.problem(err, byTok, info, strings.TrimSuffix(importPath, "_test")); ok {
				probs = append(probs, p)
			}
		},
	}
	pkg, _ := conf.Check(importPath, c.s.fset, asts, info)
	return pkg, probs
}

// problem turns a type error into a Problem, or reports false when it is not the artifact's: a
// soft error (an unused import), one in a file of another artifact's consumer, or one an opaque
// package or hand-written code explains.
func (c *admitChecker) problem(err error, byTok map[*token.File]*admitFile, info *types.Info, pkgPath string) (Problem, bool) {
	terr, ok := err.(types.Error)
	if !ok || terr.Soft {
		return Problem{}, false
	}
	f := byTok[c.s.fset.File(terr.Pos)]
	if f == nil || !c.s.reporting[f.consumer] || c.isOpaqueOperand(f.ast, info, terr, pkgPath) {
		return Problem{}, false
	}
	at := c.s.fset.Position(terr.Pos)
	return Problem{Consumer: f.consumer, Path: f.path, Message: fmt.Sprintf("%d:%d: %s", at.Line, at.Column, terr.Msg), Severity: c.s.severityOf(f.consumer, terr.Msg)}, true
}

var callArgs = regexp.MustCompile(`arguments in call to|in argument to`)

// configBound reports whether a finding depends on a fact Tier 1 cannot see: the arguments
// `appgen compose` passes a variant's constructor come from the app's appgen.json
// (variantHookArgs), which a pure check over the project document does not have. Tier 2 builds
// with the real file, so such a finding waits for it (Incomplete) instead of refusing the write.
func configBound(consumer, msg string) bool {
	return consumer == "appgen compose" && callArgs.MatchString(msg)
}

// enclosing is the nodes of f whose range holds pos, outermost first.
func enclosing(f *ast.File, pos token.Pos) []ast.Node {
	var out []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil || pos < n.Pos() || pos >= n.End() {
			return false
		}
		out = append(out, n)
		return true
	})
	return out
}

// isOpaqueOperand reports whether the error is an opaque package's to report: its position lies
// inside a selector expression whose operand is a package this check cannot see into, or it is an
// elided composite literal whose type is such a selector (the elision cannot be resolved without
// the opaque type). A name undefined in a package that holds hand-written code beside the emitted
// files (a contract's goPackage) is the hand-written code's: it is the one thing this check
// cannot see, so it is not a finding.
func (c *admitChecker) isOpaqueOperand(f *ast.File, info *types.Info, terr types.Error, pkgPath string) bool {
	undefined := strings.HasPrefix(terr.Msg, "undefined: ")
	if undefined && !strings.Contains(terr.Msg, ".") && c.s.partial[pkgPath] {
		return true // an unqualified name of the package's own hand-written code
	}
	elided := strings.Contains(terr.Msg, "composite literal")
	for _, n := range enclosing(f, terr.Pos) {
		if c.explains(info, n, terr.Pos, undefined, elided) {
			return true
		}
	}
	return false
}

// explains reports whether node n, which encloses the error at pos, accounts for it.
func (c *admitChecker) explains(info *types.Info, n ast.Node, pos token.Pos, undefined, elided bool) bool {
	switch n := n.(type) {
	case *ast.SelectorExpr:
		return c.opaqueSelector(info, n) || (undefined && c.partialSelector(info, n))
	case *ast.CompositeLit:
		return elided && n.Type != nil && pos >= n.Lbrace && c.mentionsOpaque(info, n.Type)
	}
	return false
}

// importedBy is the import path of the package a selector's operand names; ok is false when the
// operand is not a package.
func importedBy(info *types.Info, sel *ast.SelectorExpr) (string, bool) {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	pn, ok := info.Uses[id].(*types.PkgName)
	if !ok {
		return "", false
	}
	return pn.Imported().Path(), true
}

// partialSelector reports whether sel selects from a sibling package that holds hand-written code.
func (c *admitChecker) partialSelector(info *types.Info, sel *ast.SelectorExpr) bool {
	p, ok := importedBy(info, sel)
	return ok && c.s.partial[p]
}

// opaqueSelector reports whether sel selects from an imported package that is not an emitted sibling.
func (c *admitChecker) opaqueSelector(info *types.Info, sel *ast.SelectorExpr) bool {
	p, ok := importedBy(info, sel)
	if !ok {
		return false
	}
	_, sibling := c.byImport[p]
	return !sibling
}

// mentionsOpaque reports whether a type expression names a type of an opaque package anywhere in it.
func (c *admitChecker) mentionsOpaque(info *types.Info, e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && c.opaqueSelector(info, sel) {
			found = true
		}
		return !found
	})
	return found
}
