//go:build conformance

package conformance

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestConformance_IntegrationPreStepWithoutServerComposition is the tree an Engine's or a
// ResourceAccess's integration works in (ISS-031-1): its pre-step is `make gen-tests` alone, and
// no composition generator (appgen clients, config, compose) has run. `make gen-tests` writes the
// generated scenario tests together with the scenariocompose subject they import, so the tree
// builds and vets with the tests present and no cmd/server composition generated.
func TestConformance_IntegrationPreStepWithoutServerComposition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	dir := t.TempDir()
	rep, err := RunChain(ctx, "todomvc", dir, WithoutServerComposition())
	for _, s := range rep.Steps {
		t.Logf("%-32s exit=%d %s", s.Name, s.ExitCode, s.Duration.Round(time.Millisecond))
	}
	if err != nil {
		t.Fatalf("RunChain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "scenariocompose", "compose.gen.go")); err != nil {
		t.Errorf("make gen-tests did not write the scenario subject: %v", err)
	}
	tests, err := filepath.Glob(filepath.Join(dir, "internal", "*", "*", "*_scenarios.gen_test.go"))
	if err != nil || len(tests) == 0 {
		t.Fatalf("no generated scenario test is present, so the case proves nothing (%v)", err)
	}
	for _, absent := range []string{"cmd/server/main.gen.go", "cmd/server/config.gen.go", "api/openapi.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(absent))); err == nil {
			t.Errorf("the pre-step composed the server: %s exists", absent)
		}
	}
}
