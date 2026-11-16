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
