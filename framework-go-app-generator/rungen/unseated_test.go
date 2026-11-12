package rungen_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/devstack"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/rungen"
)

// A browser surface whose web project is not yet seated (no webApp/package.json: the web client
// has not been constructed) is "not yet built", not "broken": the launcher skips the web build and
// serves the API and MCP only, so an integration that precedes the web client still boots.
const webOnlyProject = `{"serviceContracts":{
	"listManager":{"component":"listManager","layer":"Manager","goPackage":"internal/manager/list",
	  "$defs":{"T":{"type":"string"}},"interface":{"name":"ListManager","layer":"manager","operations":[{"name":"Ping","params":[],"error":false}]}}},
	"slots":{"5":{"kind":5,"model":{"components":[
	  {"id":"todo-web-app","name":"TodoWebApp","kind":"client","uiSurface":true},
	  {"id":"list-manager","name":"ListManager","kind":"manager","contractKey":"listManager"}],
	  "relationships":[{"from":"todo-web-app","to":"list-manager"}]}}}}`

// fakeServer stands in for cmd/server: it listens on the address the launcher sets, serves /readyz
// and a minimal /mcp, and serves an index at / only when the launcher handed it a web directory.
const fakeServer = `package main

import (
	"net/http"
	"os"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	if os.Getenv("%WEBDIST%") != "" {
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) })
	}
	_ = http.ListenAndServe(os.Getenv("%LISTEN%"), mux)
}
`

func TestLaunch_UnseatedWebSurfaceServesAPIOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots a sandbox module")
	}
	m := freshModel(t, webOnlyProject)
	plan, err := rungen.Derive(m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if plan.WebDir == "" || plan.WebDistEnv == "" {
		t.Fatalf("the model has a browser surface, the plan must carry its web project: %+v", plan)
	}
	dir := t.TempDir()
	server := strings.NewReplacer("%WEBDIST%", plan.WebDistEnv, "%LISTEN%", plan.ListenEnv).Replace(fakeServer)
	for name, content := range map[string]string{
		"go.mod":                                  "module example.com/unseated\n\ngo 1.25\n",
		filepath.Join(rungen.Dir, "main.gen.go"):  generate(t, m),
		filepath.Join("cmd", "server", "main.go"): server,
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The launcher needs go on PATH and, for an unseated surface, nothing else: not npm. A stub
	// temporal CLI satisfies its prerequisite check and the substrate's variables are preset, so
	// nothing is provisioned.
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	bin := t.TempDir()
	if err := os.Symlink(goBin, filepath.Join(bin, "go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "temporal"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { // #nosec G306 -- a test stub
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	var preset []string
	for _, e := range plan.Provisioned {
		preset = append(preset, e.Env+"=preset")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	app, err := devstack.Launch(ctx, dir, preset, nil)
	if err != nil {
		t.Fatalf("Launch with an unseated web surface: %v", err)
	}
	defer func() { _ = app.Stop() }()
	if !strings.Contains(app.Log(), "web surface todo-web-app not seated; serving API and MCP only") {
		t.Errorf("the launcher must say once that the surface is not seated:\n%s", app.Log())
	}
	for path, want := range map[string]int{"/readyz": http.StatusOK, "/mcp": http.StatusOK, "/": http.StatusNotFound} {
		resp, err := http.Get(app.BaseURL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("GET %s = %d, want %d", path, resp.StatusCode, want)
		}
	}
}
