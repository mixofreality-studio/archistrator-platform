package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"
)

// TestRuleIDs_CoverEveryViolationRule parses the files that construct violations and proves that
// every Rule a violation literal carries is a named constant and is listed by RuleIDs, and that
// RuleIDs is sorted and unique. A rule written as an inline string literal fails: the doctrine
// registry resolves citations against RuleIDs, so an unlisted rule would be invisible to it.
func TestRuleIDs_CoverEveryViolationRule(t *testing.T) {
	ids := RuleIDs()
	if !sort.StringsAreSorted(ids) {
		t.Errorf("RuleIDs is not sorted: %v", ids)
	}
	listed := map[string]bool{}
	for _, id := range ids {
		if listed[id] {
			t.Errorf("RuleIDs lists %q twice", id)
		}
		listed[id] = true
	}

	fset := token.NewFileSet()
	files := []string{"filelayout.go", "contractdup.go", "scenariocompose_testonly.go"}
	consts := map[string]string{}
	var parsed []*ast.File
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed = append(parsed, f)
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if bl, ok := vs.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
						if v, err := strconv.Unquote(bl.Value); err == nil {
							consts[n.Name] = v
						}
					}
				}
			}
		}
	}

	seen := 0
	check := func(expr ast.Expr, where string) {
		seen++
		id, ok := expr.(*ast.Ident)
		if !ok {
			t.Errorf("%s: a violation's Rule must be a named constant, got %T", where, expr)
			return
		}
		v, ok := consts[id.Name]
		if !ok {
			t.Errorf("%s: Rule %s is not a string constant declared in the arch gate files", where, id.Name)
			return
		}
		if !listed[v] {
			t.Errorf("%s: rule %q (%s) is not listed by RuleIDs", where, v, id.Name)
		}
	}
	for _, f := range parsed {
		ast.Inspect(f, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			var tname string
			switch ty := cl.Type.(type) {
			case *ast.Ident:
				tname = ty.Name
			}
			switch tname {
			case "fileLayoutViolation":
				if len(cl.Elts) == 4 {
					check(cl.Elts[2], fset.Position(cl.Pos()).String())
				}
			case "contractDupViolation":
				for _, e := range cl.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Rule" {
							check(kv.Value, fset.Position(cl.Pos()).String())
						}
					}
				}
			}
			return true
		})
	}
	if seen < 10 {
		t.Errorf("only %d violation literals found; the parse is not seeing the gate files", seen)
	}
	// Every listed id is also a constant the files declare (no stale entries).
	declared := map[string]bool{}
	for _, v := range consts {
		declared[v] = true
	}
	for _, id := range ids {
		if !declared[id] && id != ruleTestCompose {
			t.Errorf("RuleIDs lists %q, which no gate file declares", id)
		}
	}
}
