package github_test

// Satellite-level regression tests for the GitHub Actions wire ops (actions.go),
// exercised against the stateful FakeActions boundary IN ISOLATION — so the
// satellite carries its own coverage of dispatch / list-by-name / get / cancel and
// the non-dedup dispatch semantics the RA's idempotency analog relies on.

import (
	"context"
	"io"
	"sync"
	"testing"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
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

	inputs := map[string]string{fwgithub.DispatchInputKeyIdempotency: "abc123"}
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
	inputs := map[string]string{fwgithub.DispatchInputKeyIdempotency: "dup"}
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
		map[string]string{fwgithub.DispatchInputKeyIdempotency: "k"}, "t")
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
		map[string]string{fwgithub.DispatchInputKeyIdempotency: "k"}, "t")
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
	inputs := map[string]string{fwgithub.DispatchInputKeyIdempotency: "k"}

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
	inputs := map[string]string{fwgithub.DispatchInputKeyIdempotency: "race"}

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

// TestListArtifacts lists a run's artifacts via the Actions artifacts route and
// maps the wire DTO onto the satellite's Artifact value.
func TestListArtifacts(t *testing.T) {
	fake := gh.StartActions()
	defer fake.Close()
	fake.AddArtifact(42, "test-results-C-BG-3", []byte("zipbytes"))
	c := actionsClient(t, fake.BaseURL())
	ctx := context.Background()

	arts, err := c.ListArtifacts(ctx, "o", "r", 7, "tok")
	if err != nil || len(arts) != 1 || arts[0].Name != "test-results-C-BG-3" {
		t.Fatalf("ListArtifacts = %v %+v, want one artifact named test-results-C-BG-3", err, arts)
	}
	if arts[0].ID != 42 || arts[0].SizeBytes != int64(len("zipbytes")) || arts[0].Expired {
		t.Fatalf("artifact = %+v, want id 42, size 8, not expired", arts[0])
	}
	req := fake.Requests()[0]
	if req.Auth != "token tok" {
		t.Fatalf("list auth = %q, want installation token", req.Auth)
	}
}

// TestDownloadArtifactFollowsRedirectWithoutToken pins Review Focus 5: the zip
// endpoint answers 302 to a DIFFERENT host (blob storage) and the client must follow
// it while NOT forwarding the installation token to that host.
func TestDownloadArtifactFollowsRedirectWithoutToken(t *testing.T) {
	fake := gh.StartActions()
	defer fake.Close()
	fake.AddArtifact(42, "a", []byte("zipbytes"))
	c := actionsClient(t, fake.BaseURL())
	ctx := context.Background()

	rc, err := c.DownloadArtifact(ctx, "o", "r", 42, "tok")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "zipbytes" {
		t.Fatalf("body = %q, want zipbytes", b)
	}
	// the API host saw the token …
	if got := fake.Requests()[0].Auth; got != "token tok" {
		t.Fatalf("api auth = %q, want installation token", got)
	}
	// … and the blob host did NOT.
	blob := fake.BlobRequests()
	if len(blob) != 1 {
		t.Fatalf("blob requests = %d, want 1", len(blob))
	}
	if blob[0].Auth != "" {
		t.Fatalf("token leaked to blob host: Authorization=%q", blob[0].Auth)
	}
}

// TestDownloadArtifactMissingIsNotFound maps the zip endpoint's 404 onto
// fwra.NotFound via ClassifyStatus (an expired/absent artifact).
func TestDownloadArtifactMissingIsNotFound(t *testing.T) {
	fake := gh.StartActions()
	defer fake.Close()
	c := actionsClient(t, fake.BaseURL())
	ctx := context.Background()

	rc, err := c.DownloadArtifact(ctx, "o", "r", 99, "tok")
	if rc != nil {
		_ = rc.Close()
	}
	if kindOf(err) != fwra.NotFound {
		t.Fatalf("download of missing artifact kind = %v (%v), want NotFound", kindOf(err), err)
	}
}
