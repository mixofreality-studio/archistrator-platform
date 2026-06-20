package github_test

// Satellite-level regression tests for the GitHub Actions wire ops (actions.go),
// exercised against the stateful FakeActions boundary IN ISOLATION — so the
// satellite carries its own coverage of dispatch / list-by-name / get / cancel and
// the non-dedup dispatch semantics the RA's idempotency analog relies on.

import (
	"context"
	"sync"
	"testing"

	fwgithub "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

func actionsClient(t *testing.T, baseURL string) *fwgithub.AppClient {
	t.Helper()
	keyPEM, err := gh.GenerateAppKeyPEM()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	c, err := fwgithub.NewAppClient("42", keyPEM, baseURL)
	if err != nil {
		t.Fatalf("NewAppClient: %v", err)
	}
	return c
}

func TestDispatchCreatesQueryableRun(t *testing.T) {
	fake := gh.StartActions()
	defer fake.Close()
	c := actionsClient(t, fake.BaseURL())
	ctx := context.Background()

	inputs := map[string]string{fwgithub.DispatchInputKeyIdempotencyToken: "abc123"}
	if err := c.DispatchWorkflow(ctx, "acme", "proj", "construct.yml", "main", inputs, "ghs_tok"); err != nil {
		t.Fatalf("DispatchWorkflow: %v", err)
	}
	runName := fwgithub.RunNamePrefix + "abc123"
	runs, err := c.ListRunsByName(ctx, "acme", "proj", "construct.yml", runName, "ghs_tok")
	if err != nil {
		t.Fatalf("ListRunsByName: %v", err)
	}
	if len(runs) != 1 || runs[0].Name != runName || runs[0].Status != fwgithub.RunQueued {
		t.Fatalf("runs = %+v, want one queued run named %q", runs, runName)
	}
	// the call authenticated with the installation token (not an App-JWT bearer)
	req := fake.Requests()[0]
	if req.Auth != "token ghs_tok" {
		t.Fatalf("dispatch auth = %q, want installation token", req.Auth)
	}
}

func TestDispatchIsNotDeduped(t *testing.T) {
	// The load-bearing GitHub fact the RA must compensate for: two dispatches with
	// the SAME token create TWO runs. (The RA converges them; the satellite does not.)
	fake := gh.StartActions()
	defer fake.Close()
	c := actionsClient(t, fake.BaseURL())
	ctx := context.Background()
	inputs := map[string]string{fwgithub.DispatchInputKeyIdempotencyToken: "dup"}
	for i := 0; i < 2; i++ {
		if err := c.DispatchWorkflow(ctx, "acme", "proj", "construct.yml", "main", inputs, "t"); err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
	}
	runs, err := c.ListRunsByName(ctx, "acme", "proj", "construct.yml", fwgithub.RunNamePrefix+"dup", "t")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2 (dispatch is NOT deduped by GitHub)", len(runs))
	}
}

func TestGetRunAndTerminalMapping(t *testing.T) {
	fake := gh.StartActions()
	defer fake.Close()
	c := actionsClient(t, fake.BaseURL())
	ctx := context.Background()
	_ = c.DispatchWorkflow(ctx, "acme", "proj", "construct.yml", "main",
		map[string]string{fwgithub.DispatchInputKeyIdempotencyToken: "k"}, "t")
	runs, _ := c.ListRunsByName(ctx, "acme", "proj", "construct.yml", fwgithub.RunNamePrefix+"k", "t")
	id := runs[0].ID
	fake.SetRunTerminal(id, "failure")

	run, err := c.GetRun(ctx, "acme", "proj", id, "t")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != fwgithub.RunCompleted || run.Conclusion != fwgithub.RunFailure {
		t.Fatalf("run = %+v, want completed/failure", run)
	}
}

func TestGetRunNotFound(t *testing.T) {
	fake := gh.StartActions()
	defer fake.Close()
	c := actionsClient(t, fake.BaseURL())
	if _, err := c.GetRun(context.Background(), "acme", "proj", 999, "t"); kindOf(err) != fwra.NotFound {
		t.Fatalf("GetRun missing kind = %v, want NotFound", kindOf(err))
	}
}

func TestCancelRunIdempotent(t *testing.T) {
	fake := gh.StartActions()
	defer fake.Close()
	c := actionsClient(t, fake.BaseURL())
	ctx := context.Background()
	_ = c.DispatchWorkflow(ctx, "acme", "proj", "construct.yml", "main",
		map[string]string{fwgithub.DispatchInputKeyIdempotencyToken: "k"}, "t")
	runs, _ := c.ListRunsByName(ctx, "acme", "proj", "construct.yml", fwgithub.RunNamePrefix+"k", "t")
	id := runs[0].ID

	if err := c.CancelRun(ctx, "acme", "proj", id, "t"); err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	// second cancel — run is now completed → 409 → mapped to success
	if err := c.CancelRun(ctx, "acme", "proj", id, "t"); err != nil {
		t.Fatalf("CancelRun (already terminal) = %v, want nil (idempotent)", err)
	}
	// cancel an absent run → 404 → success
	if err := c.CancelRun(ctx, "acme", "proj", 4242, "t"); err != nil {
		t.Fatalf("CancelRun (absent) = %v, want nil", err)
	}
}

func TestDispatchErrorKindMapping(t *testing.T) {
	fake := gh.StartActions()
	defer fake.Close()
	c := actionsClient(t, fake.BaseURL())
	ctx := context.Background()
	inputs := map[string]string{fwgithub.DispatchInputKeyIdempotencyToken: "k"}

	fake.ForceNext("dispatch", 403)
	if err := c.DispatchWorkflow(ctx, "acme", "proj", "construct.yml", "main", inputs, "t"); kindOf(err) != fwra.Auth {
		t.Fatalf("dispatch 403 kind = %v, want Auth", kindOf(err))
	}
	fake.ForceNext("dispatch", 503)
	if err := c.DispatchWorkflow(ctx, "acme", "proj", "construct.yml", "main", inputs, "t"); kindOf(err) != fwra.Transient {
		t.Fatalf("dispatch 503 kind = %v, want Transient", kindOf(err))
	}
}

// TestDispatchRace proves the fake's dispatch is concurrency-safe and that
// concurrent same-token dispatches create exactly N runs all carrying the same
// name (the substrate the RA converges over).
func TestDispatchRace(t *testing.T) {
	fake := gh.StartActions()
	defer fake.Close()
	c := actionsClient(t, fake.BaseURL())
	ctx := context.Background()
	inputs := map[string]string{fwgithub.DispatchInputKeyIdempotencyToken: "race"}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.DispatchWorkflow(ctx, "acme", "proj", "construct.yml", "main", inputs, "t")
		}()
	}
	wg.Wait()
	runs, _ := c.ListRunsByName(ctx, "acme", "proj", "construct.yml", fwgithub.RunNamePrefix+"race", "t")
	if len(runs) != 5 {
		t.Fatalf("got %d runs, want 5", len(runs))
	}
}
