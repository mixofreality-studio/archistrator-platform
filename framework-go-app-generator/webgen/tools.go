package webgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// mcpSDK is the import path of the MCP Go SDK whose AddTool the tables call.
const mcpSDK = "github.com/modelcontextprotocol/go-sdk/mcp"

// LoadMCPTools returns the names of the MCP tools a server REALLY registers, read
// from its generated Go tool tables: every *tools.gen.go file under dir (the
// archistrator layout is dir/<mgr>/<mgr>_tools.gen.go, mcpgen's is
// dir/[<mgr>/]tools.gen.go). The name is decided there (mcpemit and mcpgen:
// `toolName := mgrPrefix + op.Name`), and it is not always the name a path would
// yield: /project-design/request-sdp-commit is projectDesignRequestSDPCommit.
//
// Each registration is found by parsing the Go, not by pattern matching: a call
// <sdk>.AddTool(<server>, &<sdk>.Tool{Name: <name>, ...}, <handler>), where <sdk>
// is whatever name the file imports the SDK under and <name> is a string literal
// or a string constant declared in the table's package.
//
// The read is strict, because a silently missed registration binds its op
// `tool: null` and switches the MCP transport off for it. Every one of these is
// an error, never a skipped line:
//   - no directory, or no *tools.gen.go under it;
//   - a table that does not parse, or registers nothing;
//   - a table that imports the SDK as `.` or `_`;
//   - an AddTool call that is not the SDK's (a Server.AddTool method, another
//     package's AddTool), whose tool is not a &<sdk>.Tool literal, or whose Name
//     is missing or not a resolvable string constant.
func LoadMCPTools(dir string) (map[string]bool, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("webgen: no generated MCP tool directory at %s", dir)
	}
	tools := map[string]bool{}
	var tables int
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "tools.gen.go") {
			return err
		}
		tables++
		return addFileTools(p, tools)
	})
	if err != nil {
		return nil, err
	}
	if tables == 0 {
		return nil, fmt.Errorf("webgen: no *tools.gen.go tool table under %s", dir)
	}
	return tools, nil
}

// addFileTools adds one table's registrations; a table with none is an error.
func addFileTools(file string, tools map[string]bool) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("webgen: parse %s: %w", file, err)
	}
	sdk, err := sdkName(f)
	if err != nil {
		return fmt.Errorf("webgen: %s: %w", file, err)
	}
	consts, err := packageConsts(filepath.Dir(file))
	if err != nil {
		return err
	}
	r := reader{sdk: sdk, consts: consts}
	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		if r.err != nil {
			return false
		}
		if name, ok := r.registration(n); ok {
			names = append(names, name)
		}
		return true
	})
	if r.err != nil {
		return fmt.Errorf("webgen: %s:%s: %w", file, lineOf(fset, r.at), r.err)
	}
	if len(names) == 0 {
		return fmt.Errorf("webgen: %s registers no MCP tool (want at least one %s.AddTool)", file, sdk)
	}
	for _, name := range names {
		tools[name] = true
	}
	return nil
}

// sdkName is the name a table refers to the MCP SDK by: its import alias, or
// "mcp". A table that does not import the SDK registers nothing.
func sdkName(f *ast.File) (string, error) {
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p != mcpSDK {
			continue
		}
		if imp.Name == nil {
			return "mcp", nil
		}
		if imp.Name.Name == "." || imp.Name.Name == "_" {
			return "", fmt.Errorf("imports %s as %q; its AddTool calls cannot be read", mcpSDK, imp.Name.Name)
		}
		return imp.Name.Name, nil
	}
	return "", fmt.Errorf("does not import %s, so it registers no MCP tool", mcpSDK)
}

// packageConsts is every constant declared in the Go files of dir (the table's
// package), for a Name given as a constant. constString evaluates one; a
// constant that is not a string literal (or a + of them) does not evaluate, so
// naming it fails.
func packageConsts(dir string) (map[string]ast.Expr, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	raw := map[string]ast.Expr{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("webgen: parse %s: %w", file, err)
		}
		collectConsts(f, raw)
	}
	return raw, nil
}

func collectConsts(f *ast.File, raw map[string]ast.Expr) {
	for _, decl := range f.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.CONST {
			for _, spec := range gen.Specs {
				specConsts(spec, raw)
			}
		}
	}
}

// specConsts records `name = value` pairs; an implicit (iota-style) value is
// not a string literal and is left out.
func specConsts(spec ast.Spec, raw map[string]ast.Expr) {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok || len(vs.Values) != len(vs.Names) {
		return
	}
	for i, n := range vs.Names {
		raw[n.Name] = vs.Values[i]
	}
}

// constString evaluates a string-literal constant expression: a literal, a
// constant naming one, or a + of those.
func constString(raw map[string]ast.Expr, e ast.Expr, depth int) (string, bool) {
	if depth > 32 {
		return "", false
	}
	switch v := e.(type) {
	case *ast.BasicLit:
		return stringLit(v)
	case *ast.ParenExpr:
		return constString(raw, v.X, depth+1)
	case *ast.Ident:
		def, ok := raw[v.Name]
		if !ok {
			return "", false
		}
		return constString(raw, def, depth+1)
	case *ast.BinaryExpr:
		return concat(raw, v, depth)
	}
	return "", false
}

func stringLit(v *ast.BasicLit) (string, bool) {
	if v.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(v.Value)
	return s, err == nil
}

func concat(raw map[string]ast.Expr, v *ast.BinaryExpr, depth int) (string, bool) {
	if v.Op != token.ADD {
		return "", false
	}
	l, okL := constString(raw, v.X, depth+1)
	r, okR := constString(raw, v.Y, depth+1)
	return l + r, okL && okR
}

// reader walks one table; the first registration it cannot read stops it.
type reader struct {
	sdk    string
	consts map[string]ast.Expr
	err    error
	at     token.Pos
}

func (r *reader) fail(at token.Pos, format string, args ...any) {
	r.err, r.at = fmt.Errorf(format, args...), at
}

// registration reads <sdk>.AddTool(_, &<sdk>.Tool{Name: …}, …). Any other call
// named AddTool is refused, not skipped.
func (r *reader) registration(n ast.Node) (string, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := unindex(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "AddTool" {
		return "", false
	}
	if !isSelector(sel, r.sdk, "AddTool") {
		r.fail(call.Pos(), "an AddTool call that is not %s.AddTool (the SDK's function) cannot be read; register tools as %s.AddTool(srv, &%s.Tool{Name: ...}, h)", r.sdk, r.sdk, r.sdk)
		return "", false
	}
	if len(call.Args) < 2 {
		r.fail(call.Pos(), "%s.AddTool with fewer than two arguments", r.sdk)
		return "", false
	}
	lit := toolLiteral(call.Args[1], r.sdk)
	if lit == nil {
		r.fail(call.Args[1].Pos(), "the tool passed to %s.AddTool is not a &%s.Tool{...} literal, so its name cannot be read", r.sdk, r.sdk)
		return "", false
	}
	return r.nameField(lit)
}

// unindex strips explicit type arguments: AddTool[In, Out](...) is AddTool(...).
func unindex(e ast.Expr) ast.Expr {
	switch v := e.(type) {
	case *ast.IndexExpr:
		return v.X
	case *ast.IndexListExpr:
		return v.X
	}
	return e
}

func toolLiteral(e ast.Expr, sdk string) *ast.CompositeLit {
	addr, ok := e.(*ast.UnaryExpr)
	if !ok || addr.Op != token.AND {
		return nil
	}
	lit, ok := addr.X.(*ast.CompositeLit)
	if !ok || !isSelector(lit.Type, sdk, "Tool") {
		return nil
	}
	return lit
}

func (r *reader) nameField(lit *ast.CompositeLit) (string, bool) {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "Name" {
			continue
		}
		name, ok := constString(r.consts, kv.Value, 0)
		if !ok || name == "" {
			r.fail(kv.Value.Pos(), "the tool's Name is not a non-empty string literal or string constant of this package")
			return "", false
		}
		return name, true
	}
	r.fail(lit.Pos(), "the %s.Tool literal has no Name", r.sdk)
	return "", false
}

func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

func lineOf(fset *token.FileSet, at token.Pos) string {
	return strconv.Itoa(fset.Position(at).Line)
}
