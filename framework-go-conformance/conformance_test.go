//go:build conformance

package conformance

import (
	"context"
	"testing"
	"time"
)

// TestConformance_Chain pushes every embedded fixture through the scaffolded
// command chain (scaffold, gen-models, gen-null, appgen, gen-tests, webgen, build,
// vet, typecheck) and requires every step to exit 0.
func TestConformance_Chain(t *testing.T) {
	for _, name := range FixtureNames() {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
			defer cancel()
			rep, err := RunChain(ctx, name, t.TempDir())
			for _, s := range rep.Steps {
				t.Logf("%-28s exit=%d %s", s.Name, s.ExitCode, s.Duration.Round(time.Millisecond))
			}
			if err != nil {
				t.Fatalf("RunChain(%s): %v", name, err)
			}
			if len(rep.Steps) == 0 {
				t.Fatal("chain ran no steps")
			}
			for _, s := range rep.Steps {
				if s.ExitCode != 0 {
					t.Errorf("step %s (%v) exit %d:\n%s", s.Name, s.Argv, s.ExitCode, s.Stderr)
				}
			}
		})
	}
}

// buildFixture runs the whole chain over the todomvc fixture into dir.
func buildFixture(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	rep, err := RunChain(ctx, "todomvc", dir)
	if err != nil {
		t.Fatalf("RunChain: %v (steps=%d)", err, len(rep.Steps))
	}
}

// bootBuilt boots an already built app; the caller stops it.
func bootBuilt(t *testing.T, dir string) Booted {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	b, err := Boot(ctx, dir)
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	return b
}

// TestConformance_Boot proves the composed todomvc app boots through its run contract and
// passes the smoke every integration runs (Probe is devstack.Smoke): it answers /readyz, serves
// the SPA at / and lists every tool the model generates over /mcp.
func TestConformance_Boot(t *testing.T) {
	dir := t.TempDir()
	buildFixture(t, dir)
	b := bootBuilt(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := Probe(ctx, b); err != nil {
		_ = b.Stop()
		t.Fatalf("Probe: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestConformance_BootTwice_NoOrphans proves a boot/stop cycle repeated leaves no
// container or volume behind (FIX-063).
func TestConformance_BootTwice_NoOrphans(t *testing.T) {
	ctx := context.Background()
	before, err := DockerInventory(ctx)
	if err != nil {
		t.Fatalf("docker inventory: %v", err)
	}
	dir := t.TempDir()
	buildFixture(t, dir)
	for i := range 2 {
		b := bootBuilt(t, dir)
		if err := b.Stop(); err != nil {
			t.Fatalf("Stop #%d: %v", i+1, err)
		}
	}
	after, err := DockerInventory(ctx)
	if err != nil {
		t.Fatalf("docker inventory: %v", err)
	}
	if left := DockerLeftovers(ctx, before, after); len(left) != 0 {
		t.Fatalf("boot left docker objects behind: %v", left)
	}
}
