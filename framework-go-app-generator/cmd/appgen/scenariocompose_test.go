package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// compose emits the generated test subject beside the composition root: the scenario tests
// build their components from it, and -check holds it to the model like every other generated
// file. It exists even for an app with no test plans (Workflows() feeds the replay gate).
func TestCompose_WritesScenarioCompose(t *testing.T) {
	root, base := appFixture(t, "internal/resourceaccess/todo", "internal/resourceaccess/caller", "internal/manager/list")
	if err := run(append([]string{"compose"}, base...), io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "internal/scenariocompose/compose.gen.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("compose did not write the scenario subject: %v", err)
	}
	for _, want := range []string{"package scenariocompose", "func NewListManager(t *testing.T, h *scenariohost.Host)", "func Workflows() scenariohost.Workflows", `s.h.Input(s.t, "caller-access", "external-http", "URL"), &http.Client{Timeout: 30 * time.Second}`} {
		if !strings.Contains(string(src), want) {
			t.Errorf("compose.gen.go lacks %q:\n%s", want, src)
		}
	}
	if err := run(append([]string{"compose", "-check"}, base...), io.Discard); err != nil {
		t.Errorf("compose -check after compose: %v", err)
	}
	if err := os.WriteFile(path, append(src, "\n// drift\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(append([]string{"compose", "-check"}, base...), io.Discard); !errors.Is(err, errDrift) {
		t.Errorf("a drifted scenario subject must fail -check, got %v", err)
	}
}
