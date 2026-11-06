package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/conformance"
)

func freshApp(t *testing.T) (root, project string) {
	t.Helper()
	root = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/todomvc\n\ngo 1.26.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := conformance.Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	project = filepath.Join(root, "project.json")
	if err := os.WriteFile(project, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, project
}

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

// binary builds the command once; `go run` would report its own exit 1 for the child's exit 2.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "nullgen-bin")
		if err != nil {
			buildErr = err
			return
		}
		builtBin = filepath.Join(dir, "nullgen")
		cmd := exec.Command("go", "build", "-o", builtBin, ".")
		cmd.Env = append(os.Environ(), "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build: %w\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtBin
}

func runNullgen(t *testing.T, args ...string) (stderr string, exit int) {
	t.Helper()
	cmd := exec.Command(binary(t), args...)
	var buf bytes.Buffer
	cmd.Stderr = &buf
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return buf.String(), ee.ExitCode()
	}
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, buf.String())
	}
	return buf.String(), 0
}

// A fresh app gets the null files; run again, nullgen replaces only its own output.
func TestNullgen_WritesAFreshApp(t *testing.T) {
	root, project := freshApp(t)
	if stderr, exit := runNullgen(t, "-project", project, "-root", root); exit != 0 {
		t.Fatalf("exit %d\n%s", exit, stderr)
	}
	for _, path := range []string{"cmd/server/hooks.go", "internal/manager/list/null.gen.go"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Errorf("%s not written: %v", path, err)
		}
	}
	if stderr, exit := runNullgen(t, "-project", project, "-root", root); exit != 0 {
		t.Fatalf("second run exit %d\n%s", exit, stderr)
	}
}

// A hooks.go the app wrote makes the binary exit 2 before it writes anything.
func TestNullgen_ExitsTwoOnHandwrittenHooks(t *testing.T) {
	root, project := freshApp(t)
	hooks := filepath.Join(root, "cmd", "server", "hooks.go")
	if err := os.MkdirAll(filepath.Dir(hooks), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooks, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr, exit := runNullgen(t, "-project", project, "-root", root)
	if exit != 2 || !strings.Contains(stderr, "refusing to overwrite") {
		t.Fatalf("want exit 2 naming the refusal, got %d:\n%s", exit, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "internal")); err == nil {
		t.Error("a refused run must write nothing")
	}
}
