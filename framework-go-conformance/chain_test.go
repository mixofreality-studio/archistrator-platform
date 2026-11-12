package conformance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RunChain consumed from the module cache (the app builds it under GOWORK=off) has no platform
// checkout to localize against: platformRootAt reports none instead of failing, and the scaffold
// is left on the published pins, with no local replace directive.
func TestRunChain_NoCheckoutUsesPublishedPins(t *testing.T) {
	if _, ok := platformRootAt(filepath.Join(t.TempDir(), "pkg", "mod", "conformance@v0.2.0", "chain.go")); ok {
		t.Fatal("a directory with no framework-go-app-generator beside it is not a platform checkout")
	}
	if _, ok := platformRoot(); !ok {
		t.Fatal("this package is built from the platform checkout, which platformRoot must find")
	}

	raw, err := Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := scaffold(dir, "todomvc", raw, ""); err != nil {
		t.Fatalf("scaffold without a checkout: %v", err)
	}
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gomod), "replace") {
		t.Errorf("without a checkout the scaffold builds against the published pins; go.mod has a replace:\n%s", gomod)
	}
	if !strings.Contains(string(gomod), nullgenTool) {
		t.Errorf("go.mod lacks the nullgen tool the chain runs:\n%s", gomod)
	}

	// The chain's first step resolves the pins from the proxy. Until the release carrying them is
	// pushed the proxy cannot serve them, which is the release gate's to prove, not this test's.
	pin := ""
	for _, line := range strings.Split(string(gomod), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == platformModulePrefix+"framework-go" {
			pin = f[0] + "@" + f[1]
		}
	}
	if pin == "" {
		t.Fatalf("the scaffold pins no framework-go:\n%s", gomod)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	probe := exec.CommandContext(ctx, "go", "list", "-m", pin)
	probe.Dir = dir
	probe.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("%s is not on the module proxy yet (%v): %s", pin, err, out)
	}
	tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidy.Dir = dir
	tidy.Env = append(os.Environ(), "GOWORK=off")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy against the published pins: %v\n%s", err, out)
	}
}
