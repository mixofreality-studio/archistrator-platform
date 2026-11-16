package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A bootstrap keeps what exists, except the mcpApp host-bridge seat: that file is generated, so a
// rerun restores it, while every other seat is the app's own once written.
func TestBootstrap_SeatsStayButTheHostBridgeIsRegenerated(t *testing.T) {
	module := t.TempDir() // `go tool` runs in the app's module: the app's name is its module's
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.com/todomvc/server\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(module)
	dir := filepath.Join(module, "webApp")
	args := []string{"bootstrap", "-C", dir, "-surface", "todo-mcp-app", "-kind", "mcpApp"}
	var out bytes.Buffer
	if err := run(args, &out); err != nil {
		t.Fatal(err)
	}
	bridge := filepath.Join(dir, "src", "todo-mcp-app", "main.tsx")
	seat := filepath.Join(dir, "src", "todo-mcp-app", "routes", "App.tsx")
	want, err := os.ReadFile(bridge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "bootstrapped src/todo-mcp-app/main.tsx") {
		t.Errorf("the first run does not report the bridge: %s", out.String())
	}
	for p, body := range map[string]string{bridge: "// hand edit\n", seat: "// the app's own\n"} {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	if err := run(args, &out); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(bridge); !bytes.Equal(got, want) {
		t.Errorf("the generated host-bridge seat was not restored:\n%s", got)
	}
	if got, _ := os.ReadFile(seat); string(got) != "// the app's own\n" {
		t.Errorf("a seat the app owns was overwritten:\n%s", got)
	}
	if strings.Contains(out.String(), "App.tsx") {
		t.Errorf("a kept seat was reported as written: %s", out.String())
	}
	out.Reset()
	if err := run(args, &out); err != nil || out.Len() != 0 {
		t.Errorf("a third run must write nothing: %v %q", err, out.String())
	}
}
