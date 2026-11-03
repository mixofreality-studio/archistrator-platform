package webgen

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The generated ops.gen.ts typechecks under the bootstrap's own compiler options whatever tools
// its ops have. RUN-010's surface had a tool for every op and no composition route, so at the
// table's literal type `binding.tool === null` narrowed the binding to never and its own gate's
// typecheck failed on a generated file no agent may edit.
func TestOpsTypecheck(t *testing.T) {
	dir := typecheckProject(t)
	for _, tc := range []struct {
		name     string
		bindings []Binding
	}{
		{"every op has an MCP tool", []Binding{
			{OpID: "listAddTodo", Method: "POST", Path: "/api/v1/list/addTodo", Tool: "listAddTodo"},
			{OpID: "listReviewList", Method: "GET", Path: "/api/v1/list/reviewList", Tool: "listReviewList"},
		}},
		{"no op has one", []Binding{
			{OpID: "listAddTodo", Method: "POST", Path: "/api/v1/list/addTodo"},
			{OpID: "listReviewList", Method: "GET", Path: "/api/v1/list/reviewList"},
		}},
		{"a mix, with a composition route", []Binding{
			{OpID: "listAddTodo", Method: "POST", Path: "/api/v1/list/addTodo", Tool: "listAddTodo"},
			{OpID: "listDrop", Method: "DELETE", Path: "/api/v1/list/drop/{id}"},
			{OpID: "userinfo", Method: "GET", Path: "/api/userinfo", Composition: true},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops, err := EmitOps(tc.bindings)
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(dir, "src", "api", "ops.gen.ts"), ops)
			cmd := exec.Command(filepath.Join(dir, "node_modules", ".bin", "tsc"), "-p", "tsconfig.json")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("tsc: %v\n%s", err, out)
			}
		})
	}
}

// typecheckProject is a web project with the bootstrap's node version, compiler options and the
// packages ops.gen.ts imports, at the bootstrap's own ranges, and the generated errors.ts beside it.
func typecheckProject(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("npm"); err != nil {
		t.Fatal("npm is required: the generated TypeScript cannot be proven to typecheck without it")
	}
	dir := t.TempDir()
	copyTemplate(t, "bootstrap/tool-versions.tmpl", filepath.Join(dir, ".tool-versions"))
	copyTemplate(t, "errors.ts.tmpl", filepath.Join(dir, "src", "contracts", "errors.ts"))
	writeTestFile(t, filepath.Join(dir, "src", "contracts", "schema.ts"), []byte("export interface paths {\n  [path: string]: unknown;\n}\n"))
	writeTestFile(t, filepath.Join(dir, "package.json"), []byte(`{"name":"ops-typecheck","private":true,"type":"module"}`))
	writeTestFile(t, filepath.Join(dir, "tsconfig.json"), tsconfigOf(t))

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	raw, err := templates.ReadFile("templates/bootstrap/package.json.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "@@APP@@", "x")), &pkg); err != nil {
		t.Fatal(err)
	}
	args := []string{"install", "--no-audit", "--no-fund", "--prefer-offline", "--ignore-scripts"}
	for _, name := range []string{"openapi-fetch", "@modelcontextprotocol/ext-apps"} {
		args = append(args, name+"@"+pkg.Dependencies[name])
	}
	args = append(args, "typescript@"+pkg.DevDependencies["typescript"])
	cmd := exec.Command("npm", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("npm install: %v\n%s", err, out)
	}
	return dir
}

// tsconfigOf is the bootstrap's tsconfig.app.json, without the vite client types this project
// has no vite for.
func tsconfigOf(t *testing.T) []byte {
	t.Helper()
	raw, err := templates.ReadFile("templates/bootstrap/tsconfig.app.json.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	opts, ok := cfg["compilerOptions"].(map[string]any)
	if !ok || opts["strict"] != true || opts["noUncheckedIndexedAccess"] != true {
		t.Fatalf("the bootstrap tsconfig is not the strict one this test assumes: %s", raw)
	}
	delete(opts, "tsBuildInfoFile")
	opts["types"] = []string{}
	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func copyTemplate(t *testing.T, name, dest string) {
	t.Helper()
	raw, err := templates.ReadFile("templates/" + name)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, dest, raw)
}
