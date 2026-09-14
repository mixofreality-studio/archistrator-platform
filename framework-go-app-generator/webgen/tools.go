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

// LoadMCPTools returns the names of the MCP tools a server REALLY registers, read
// from its generated Go tool tables: every *tools.gen.go file under dir (the
// archistrator layout is dir/<mgr>/<mgr>_tools.gen.go, mcpgen's is
// dir/[<mgr>/]tools.gen.go). The name is decided there (mcpemit and mcpgen:
// `toolName := mgrPrefix + op.Name`), and it is not always the name a path would
// yield: /project-design/request-sdp-commit is projectDesignRequestSDPCommit.
//
// Each registration is found by parsing the Go, not by pattern matching: a
// call mcp.AddTool(<server>, &mcp.Tool{Name: "<literal>", ...}, <handler>).
// A blind read — no directory, or no registration in it — is an error, because
// binding every op `tool: null` would silently switch the MCP transport off.
func LoadMCPTools(dir string) (map[string]bool, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("webgen: no generated MCP tool directory at %s", dir)
	}
	tools := map[string]bool{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "tools.gen.go") {
			return err
		}
		return addFileTools(p, tools)
	})
	if err != nil {
		return nil, err
	}
	if len(tools) == 0 {
		return nil, fmt.Errorf("webgen: read no mcp.AddTool registration under %s", dir)
	}
	return tools, nil
}

func addFileTools(file string, tools map[string]bool) error {
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("webgen: parse %s: %w", file, err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if name, ok := addToolName(n); ok {
			tools[name] = true
		}
		return true
	})
	return nil
}

// addToolName recognizes mcp.AddTool(_, &mcp.Tool{Name: "x", ...}, ...).
func addToolName(n ast.Node) (string, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok || !isSelector(call.Fun, "mcp", "AddTool") || len(call.Args) < 2 {
		return "", false
	}
	addr, ok := call.Args[1].(*ast.UnaryExpr)
	if !ok || addr.Op != token.AND {
		return "", false
	}
	lit, ok := addr.X.(*ast.CompositeLit)
	if !ok || !isSelector(lit.Type, "mcp", "Tool") {
		return "", false
	}
	return nameField(lit)
}

func nameField(lit *ast.CompositeLit) (string, bool) {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		val, isLit := kv.Value.(*ast.BasicLit)
		if !ok || key.Name != "Name" || !isLit || val.Kind != token.STRING {
			continue
		}
		name, err := strconv.Unquote(val.Value)
		return name, err == nil && name != ""
	}
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
