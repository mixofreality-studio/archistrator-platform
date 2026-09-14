package webgen

import (
	"path/filepath"
	"strings"
	"testing"
)

// goodTable is a tool table webgen reads; each case below adds one more table
// (or package file) beside it, so a case that loses its registrations cannot
// hide behind "the directory as a whole registered something".
const goodTable = `package a
import "github.com/modelcontextprotocol/go-sdk/mcp"
func reg(srv *mcp.Server) { mcp.AddTool(srv, &mcp.Tool{Name: "aTool"}, nil) }
`

const sdkImport = `import "github.com/modelcontextprotocol/go-sdk/mcp"
`

func toolDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a", "a_tools.gen.go"), []byte(goodTable))
	for name, src := range files {
		writeTestFile(t, filepath.Join(dir, "b", name), []byte(src))
	}
	return dir
}

// Registrations the reader resolves: an aliased SDK import, a constant Name
// (in the table or elsewhere in its package, a + of constants included), and
// explicit type arguments.
func TestLoadMCPToolsResolves(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"aliased import": {map[string]string{"b_tools.gen.go": `package b
import sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
func reg(srv *sdkmcp.Server) { sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "bAliased"}, nil) }
`}, "bAliased"},
		"constant in the table": {map[string]string{"b_tools.gen.go": "package b\n" + sdkImport + `const name = "bConst"
func reg(srv *mcp.Server) { mcp.AddTool(srv, &mcp.Tool{Name: name}, nil) }
`}, "bConst"},
		"constant elsewhere in the package": {map[string]string{
			"b_tools.gen.go": "package b\n" + sdkImport + `func reg(srv *mcp.Server) { mcp.AddTool(srv, &mcp.Tool{Name: opName}, nil) }
`,
			"names.go": "package b\nconst (\n\tprefix = \"b\"\n\topName = prefix + (\"Other\")\n)\n",
		}, "bOther"},
		"type arguments": {map[string]string{"b_tools.gen.go": "package b\n" + sdkImport + `func reg(srv *mcp.Server) { mcp.AddTool[In, Out](srv, &mcp.Tool{Name: "bGeneric"}, nil) }
`}, "bGeneric"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			tools, err := LoadMCPTools(toolDir(t, c.files))
			if err != nil {
				t.Fatal(err)
			}
			if len(tools) != 2 || !tools["aTool"] || !tools[c.want] {
				t.Errorf("want aTool and %s, got %v", c.want, tools)
			}
		})
	}
}

// Everything the reader cannot resolve fails the whole read, naming why; it is
// never a silently skipped registration (preview P2 review I3, G5).
func TestLoadMCPToolsRefuses(t *testing.T) {
	table := func(body string) map[string]string {
		return map[string]string{"b_tools.gen.go": "package b\n" + sdkImport + body}
	}
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"a table with no registration":  {table("func reg(srv *mcp.Server) {}\n"), "registers no MCP tool"},
		"a table that does not import":  {map[string]string{"b_tools.gen.go": "package b\nfunc reg() {}\n"}, "does not import"},
		"a dot import":                  {map[string]string{"b_tools.gen.go": "package b\nimport . \"github.com/modelcontextprotocol/go-sdk/mcp\"\nfunc reg(srv *Server) { AddTool(srv, &Tool{Name: \"x\"}, nil) }\n"}, `as "."`},
		"a table that does not parse":   {map[string]string{"b_tools.gen.go": "package b\nfunc {\n"}, "parse"},
		"a Name from a variable":        {table("var n = \"x\"\nfunc reg(srv *mcp.Server) { mcp.AddTool(srv, &mcp.Tool{Name: n}, nil) }\n"), "string constant"},
		"a Name from a call":            {table("func reg(srv *mcp.Server) { mcp.AddTool(srv, &mcp.Tool{Name: name()}, nil) }\n"), "string constant"},
		"a Name from another package":   {table("func reg(srv *mcp.Server) { mcp.AddTool(srv, &mcp.Tool{Name: names.X}, nil) }\n"), "string constant"},
		"an empty Name":                 {table("func reg(srv *mcp.Server) { mcp.AddTool(srv, &mcp.Tool{Name: \"\"}, nil) }\n"), "non-empty"},
		"no Name":                       {table("func reg(srv *mcp.Server) { mcp.AddTool(srv, &mcp.Tool{Description: \"d\"}, nil) }\n"), "has no Name"},
		"a tool that is not a literal":  {table("func reg(srv *mcp.Server) { t := &mcp.Tool{Name: \"x\"}; mcp.AddTool(srv, t, nil) }\n"), "literal"},
		"the Server.AddTool method":     {table("func reg(srv *mcp.Server) { srv.AddTool(&mcp.Tool{Name: \"x\"}, nil) }\n"), "not mcp.AddTool"},
		"another package's AddTool":     {table("func reg(srv *mcp.Server) { other.AddTool(srv, &mcp.Tool{Name: \"x\"}, nil) }\n"), "not mcp.AddTool"},
		"a registration after an alias": {map[string]string{"b_tools.gen.go": "package b\nimport sdkmcp \"github.com/modelcontextprotocol/go-sdk/mcp\"\nfunc reg(srv *sdkmcp.Server) { sdkmcp.AddTool(srv, &mcp.Tool{Name: \"x\"}, nil) }\n"}, "literal"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			tools, err := LoadMCPTools(toolDir(t, c.files))
			if err == nil {
				t.Fatalf("want an error, read %v", tools)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("want an error about %q, got: %v", c.want, err)
			}
		})
	}
	if _, err := LoadMCPTools(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no *tools.gen.go") {
		t.Errorf("a directory with no tool table must be loud, got %v", err)
	}
}

func twoOps(t *testing.T) Value {
	t.Helper()
	return oas(t, "paths:\n  /api/v1/x/y:\n    get: {operationId: Y}\n  /api/v1/x/z:\n    get: {operationId: Z}\n")
}

// An op binds the tool the server registers for it, and only that: an op the
// tables do not register binds none, however its name would derive (G4).
func TestDeriveBindingsBindsOnlyRegisteredTools(t *testing.T) {
	bs, err := DeriveBindings(twoOps(t), map[string]bool{"xY": true, "xQ": true})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, b := range bs {
		got[b.OpID] = b.Tool
	}
	if got["xY"] != "xY" || got["xZ"] != "" || len(got) != 2 {
		t.Errorf("want xY bound to its tool and xZ to none, got %v", got)
	}
}

// An op with no MCP tool is a decision in webgen.json ("restOnly"), never a
// side effect; a stale or doubled declaration fails too.
func TestBindingsRequireRestOnlyDeclarations(t *testing.T) {
	route := CompositionRoute{OpID: "compositionGetThing", Method: "GET", Path: "/api/thing", Result: NewObject()}
	cases := map[string]struct {
		restOnly []string
		routes   []CompositionRoute
		want     string // "" = no error
	}{
		"an undeclared OAS op without a tool":  {nil, nil, "xZ (GET /api/v1/x/z) matches no registered MCP tool"},
		"a declared OAS op":                    {[]string{"xZ"}, nil, ""},
		"a declaration naming an op with tool": {[]string{"xZ", "xY"}, nil, `restOnly names "xY"`},
		"a declaration naming no op":           {[]string{"xZ", "nope"}, nil, `restOnly names "nope"`},
		"a doubled declaration":                {[]string{"xZ", "xZ"}, nil, "twice"},
		"an undeclared composition route":      {[]string{"xZ"}, []CompositionRoute{route}, "compositionGetThing (GET /api/thing) matches no registered MCP tool"},
		"a declared composition route":         {[]string{"compositionGetThing", "xZ"}, []CompositionRoute{route}, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			in := Inputs{
				Config: Config{Composition: c.routes, RestOnly: c.restOnly},
				OAS:    twoOps(t),
				Tools:  map[string]bool{"xY": true},
			}
			bs, err := in.Bindings()
			switch {
			case c.want == "" && err != nil:
				t.Fatal(err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("want an error containing %q, got %v", c.want, err)
			case c.want == "":
				for _, b := range bs {
					if b.OpID == "xZ" && b.Tool != "" {
						t.Errorf("a REST-only op binds tool %q", b.Tool)
					}
				}
			}
		})
	}
}

func TestConfigRestOnly(t *testing.T) {
	base := `{"app":"a","surface":"web","oas":"a","mcpTools":"b","fixturesEnv":"X",`
	c, err := ParseConfig([]byte(base + `"restOnly":["xZ","compositionGetUserinfo"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.RestOnly, ",") != "xZ,compositionGetUserinfo" {
		t.Errorf("restOnly: %v", c.RestOnly)
	}
	for _, bad := range []string{`"restOnly":"xZ"}`, `"restOnly":[1]}`, `"restOnly":[""]}`} {
		if _, err := ParseConfig([]byte(base + bad)); err == nil {
			t.Errorf("want an error for %s", bad)
		}
	}
}
