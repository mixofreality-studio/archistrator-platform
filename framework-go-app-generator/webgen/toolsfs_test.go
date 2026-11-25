package webgen

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// The generator reads a project's tool tables from the emission it holds in memory: the same
// registrations, resolved the same way (a constant Name declared in a sibling file of the
// table's package included), as from disk.
func TestLoadMCPToolsFS_MatchesTheDiskReader(t *testing.T) {
	files := map[string]string{
		"consts.go":      "package b\nconst prefix = \"b\"\n",
		"b_tools.gen.go": "package b\n" + sdkImport + "func reg(srv *mcp.Server) { mcp.AddTool(srv, &mcp.Tool{Name: prefix + \"Tool\"}, nil) }\n",
	}
	disk, err := LoadMCPTools(toolDir(t, files))
	if err != nil {
		t.Fatal(err)
	}
	mem := fstest.MapFS{"internal/client/a/a_tools.gen.go": &fstest.MapFile{Data: []byte(goodTable)}}
	for name, src := range files {
		mem["internal/client/b/"+name] = &fstest.MapFile{Data: []byte(src)}
	}
	got, err := LoadMCPToolsFS(mem, "internal/client")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, disk) || !got["aTool"] || !got["bTool"] {
		t.Fatalf("LoadMCPToolsFS = %v, LoadMCPTools = %v", got, disk)
	}
}

func TestLoadMCPToolsFS_Refuses(t *testing.T) {
	if _, err := LoadMCPToolsFS(fstest.MapFS{}, "internal/client"); err == nil || !strings.Contains(err.Error(), "no generated MCP tool directory") {
		t.Errorf("an absent directory: %v", err)
	}
	empty := fstest.MapFS{"internal/client/x.go": &fstest.MapFile{Data: []byte("package x\n")}}
	if _, err := LoadMCPToolsFS(empty, "internal/client"); err == nil || !strings.Contains(err.Error(), "no *tools.gen.go") {
		t.Errorf("a directory with no table: %v", err)
	}
}

// LoadInputsFS resolves the config's paths against the webApp root inside the repository, and a
// path that climbs out of it is an error, not a read of something else.
func TestLoadInputsFS(t *testing.T) {
	oas := "openapi: 3.0.0\ninfo: {title: t, version: '1'}\npaths: {}\n"
	repo := fstest.MapFS{
		"server/api/openapi.yaml":                 &fstest.MapFile{Data: []byte(oas)},
		"server/internal/client/a/a_tools.gen.go": &fstest.MapFile{Data: []byte(goodTable)},
	}
	c := Config{App: "app", OAS: "../server/api/openapi.yaml", MCPTools: "../server/internal/client", FixturesEnv: "APP_PREVIEW_FIXTURES"}
	in, err := LoadInputsFS(repo, "webApp", c)
	if err != nil {
		t.Fatal(err)
	}
	if !in.Tools["aTool"] {
		t.Errorf("tools = %v", in.Tools)
	}
	c.OAS = "../../outside.yaml"
	if _, err := LoadInputsFS(repo, "webApp", c); err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Errorf("a path above the repository: %v", err)
	}
	c.OAS = "../server/api/missing.yaml"
	if _, err := LoadInputsFS(repo, "webApp", c); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing OAS must report fs.ErrNotExist, got %v", err)
	}
	c.OAS, c.MCPTools, c.MCPOptional = "../server/api/openapi.yaml", "../server/nothing", true
	in, err = LoadInputsFS(repo, "webApp", c)
	if err != nil || len(in.Tools) != 0 {
		t.Errorf("an optional tool directory: %v, %v", in.Tools, err)
	}
}

// A design whose browser surface fronts no exposed Manager yet has an OAS with no paths and no
// generated tool table: that is an empty binding set, not a refusal. Anything else stays one.
func TestLoadInputsNoOperationsNeedsNoTools(t *testing.T) {
	c := Config{App: "app", OAS: "../api/openapi.yaml", MCPTools: "../internal/client", FixturesEnv: "APP_PREVIEW_FIXTURES"}
	empty := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n"
	withOp := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n  /api/v1/list/recall:\n    get: {operationId: Recall}\n"
	table := func(src string) fstest.MapFS {
		return fstest.MapFS{"internal/client/a/a_tools.gen.go": &fstest.MapFile{Data: []byte(src)}}
	}
	cases := []struct {
		name    string
		oas     string
		repo    fstest.MapFS
		wantErr bool
	}{
		{"no tool directory, no operations", empty, fstest.MapFS{}, false},
		{"empty tool directory, no operations", empty, fstest.MapFS{"internal/client/.keep": &fstest.MapFile{}}, false},
		{"no tool directory, operations", withOp, fstest.MapFS{}, true},
		{"unreadable table, no operations", empty, table("package a\nfunc ("), true},
		{"table registering nothing, no operations", empty, table("package a\n" + sdkImport), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := tc.repo
			repo["api/openapi.yaml"] = &fstest.MapFile{Data: []byte(tc.oas)}
			in, err := LoadInputsFS(repo, "webApp", c)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want a refusal, got tools %v", in.Tools)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			bs, err := in.Bindings()
			if err != nil || len(bs) != 0 {
				t.Errorf("want an empty binding set, got %v, %v", bs, err)
			}
		})
	}
}
