//go:build conformance

package conformance

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	scenariohost "github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// Acceptance runs (FPC plan 4) drive one launched app as several actors at once: each request names
// its own principal in X-Dev-Principal, and the launch honours the header only for a subject its
// allowlist holds. The browser runtime and the generated acceptance specs rely on exactly this, so
// it is pinned here against the real composed app and the real generated edge.

// rawOpenList posts the open-list route with the header as given ("" sends none) and returns the
// status and body: the request a browser runtime's fetch makes.
func rawOpenList(ctx context.Context, t *testing.T, baseURL, header string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/list/open-list", strings.NewReader(`{"owner":"alice","view":"all"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set("X-Dev-Principal", header)
	}
	res, err := http.DefaultClient.Do(req) // #nosec G107 -- the app this test launched
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body)
}

// A per-request X-Dev-Principal is honoured only for a subject the launch allowlisted: the allowlisted
// delegated agent reaches the member's list it acts for, a subject off the list is a 401 whatever the
// header claims, and a request without the header is served as the process principal.
func TestDevPrincipals_PerRequestHeaderOnlyWhenAllowlisted(t *testing.T) {
	h, dir := launchBuilt(t)
	alice := scenariohost.Principal{Subject: "alice"}
	agent := scenariohost.Principal{Subject: "agent-1", Roles: []string{"agent"}, OnBehalfOf: "alice", Intents: []string{"read"}}
	app := h.LaunchDevstack(t, dir, alice, agent)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const wording = "alice-owns-this-todo"
	asAlice := scenariohost.MCPSession(t, app, alice)
	if _, err := asAlice.Call(ctx, "listAddTodo", `{"owner":"alice","wording":"`+wording+`","requestId":"r1"}`); err != nil {
		t.Fatalf("alice adding a todo: %v\n%s", err, app.Log())
	}

	if status, body := rawOpenList(ctx, t, app.BaseURL, "agent-1"); status != http.StatusOK || !strings.Contains(body, wording) {
		t.Errorf("X-Dev-Principal agent-1 (allowlisted): status %d body %q; want 200 and alice's list", status, body)
	}
	if status, body := rawOpenList(ctx, t, app.BaseURL, "mallory"); status != http.StatusUnauthorized || !strings.Contains(body, "dev principal not allowed") {
		t.Errorf("X-Dev-Principal mallory (not allowlisted): status %d body %q; want 401 \"dev principal not allowed\"", status, body)
	}
	if status, body := rawOpenList(ctx, t, app.BaseURL, ""); status != http.StatusOK || !strings.Contains(body, wording) {
		t.Errorf("no header: status %d body %q; want 200, served as the process principal alice", status, body)
	}
}

// ---- the acceptance plan, run for real (FPC plan 4, Task 4.5) ----
//
// The fixture records an acceptance plan beside its client plans. The residual use-case paths (the ones
// no single client's plan covers: an actor's outcome seen on another client's surface, a path through
// two clients) are bound there; the built app is launched once with the plan's principals allowlisted
// and every residual variant is run against it: the agent writes through REST or MCP and the member's
// browser, a fresh page load, shows the write (and the other way round).

const (
	acceptanceResults = "acceptance"
	// acceptanceSPAFile is the harvested SPA's todo item: the file the red proof mutates.
	acceptanceSPAFile  = "webApp/src/todo-web-app/components/TodoItem.tsx"
	acceptanceChecked  = "checked={todo.completed}"
	redVariant         = "work-the-list-P1@todoApi"
	acceptanceWebArgs  = "generated/acceptance/playwright.config.gen.ts"
	acceptanceGoPkg    = "./internal/acceptance/"
	acceptanceGoRegexp = "^TestScenario_"
)

// acceptanceFixture is the fixture's acceptance plan: every residual variant of its design, which of
// them the plan binds, and which of those it skips (and so cannot have run).
type acceptanceFixture struct {
	residual []string
	bound    map[string]bool
	skipped  map[string]bool
}

func loadAcceptanceFixture(t *testing.T) acceptanceFixture {
	t.Helper()
	raw, err := Fixture(run021Fixture)
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := methodcheck.DecodeProject(raw)
	if err != nil || !ok {
		t.Fatalf("DecodeProject: ok=%v err=%v", ok, err)
	}
	all, err := methodcheck.DeriveScenarios(p)
	if err != nil {
		t.Fatal(err)
	}
	in, err := methodcheck.ScenarioInput(p)
	if err != nil {
		t.Fatal(err)
	}
	var clients []string
	for key, c := range in.Contracts {
		if c.Client {
			clients = append(clients, key)
		}
	}
	sort.Strings(clients)
	fx := acceptanceFixture{bound: map[string]bool{}, skipped: map[string]bool{}}
	seen := map[string]bool{}
	for _, r := range scenario.AcceptanceResidual(all, clients) {
		if !seen[r.Scenario] {
			seen[r.Scenario] = true
			fx.residual = append(fx.residual, r.Scenario)
		}
	}
	sort.Strings(fx.residual)
	if p.PhaseArtifacts == nil || p.PhaseArtifacts.AcceptancePlan == nil {
		t.Fatal("the fixture records no acceptancePlan")
	}
	for _, b := range p.PhaseArtifacts.AcceptancePlan.Bindings {
		fx.bound[b.Scenario] = true
		if b.Skip != nil {
			fx.skipped[b.Scenario] = true
		}
	}
	return fx
}

// acceptanceBuild builds the fixture with impl (the harvested bodies and SPA, possibly mutated) into a
// fresh directory and seats Playwright in it.
func acceptanceBuild(t *testing.T, impl fs.FS) string {
	t.Helper()
	raw, err := Fixture(run021Fixture)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	if _, err := RunChainOn(ctx, run021Fixture, raw, dir, WithImpl(impl)); err != nil {
		t.Fatalf("RunChain (harvested bodies and SPA): %v", err)
	}
	seatUITests(t, ctx, dir)
	return dir
}

// acceptanceVerdicts is the verdict of every acceptance variant the runs recorded, by variant id.
//
// The two halves index their verdicts apart: the Go tests under the module's test-results (the scenario
// host's), the Playwright specs under the uitests directory's (the generated reporter's).
func acceptanceVerdicts(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, index := range []string{
		filepath.Join(dir, "test-results", acceptanceResults, "results.json"),
		filepath.Join(dir, "uitests", "test-results", acceptanceResults, "results.json"),
	} {
		raw, err := os.ReadFile(index) // #nosec G304 -- the scenario host's and the reporter's own indexes
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("reading %s: %v", index, err)
		}
		var vs []struct{ Scenario, Status string }
		if err := json.Unmarshal(raw, &vs); err != nil {
			t.Fatalf("%s: %v", index, err)
		}
		for _, v := range vs {
			out[v.Scenario] = v.Status
		}
	}
	if len(out) == 0 {
		t.Fatal("no acceptance verdict was recorded by either half")
	}
	return out
}

// webAcceptance runs the browser-bearing acceptance variants under the scenario host (go tool
// webscenarios), all of them or those whose title matches grep, and returns the run's output and error.
func webAcceptance(t *testing.T, ctx context.Context, dir, grep string) (string, error) {
	t.Helper()
	args := []string{"tool", "webscenarios", "-C", "uitests", "-config", acceptanceWebArgs, "-project", "scenarios"}
	if grep != "" {
		args = append(args, "-grep", grep)
	}
	return run(t, ctx, dir, "go", args...)
}

// Every residual variant of the fixture's design is bound, and ATP-BOUND is clean under the whole
// rule set. The built app, launched with every principal of the plan, is then driven by each variant:
// the machine-only ones as Go tests over the wire, the browser-bearing ones as Playwright specs
// (a fresh page load as the member shows the agent's write; an MCP read as the agent shows what the
// member's browser did). A variant that passes is a verdict "pass"; one the plan skips (the SPA cannot
// drive its path) is a verdict "skip", named, never absent. The run leaves no docker object behind.
func TestConformance_Acceptance(t *testing.T) {
	fx := loadAcceptanceFixture(t)
	if len(fx.residual) == 0 {
		t.Fatal("the fixture's design has no residual acceptance variant: the use cases it records no longer cross two clients")
	}
	for _, id := range fx.residual {
		if !fx.bound[id] {
			t.Errorf("residual variant %s has no binding in the acceptance plan (ATP-BOUND)", id)
		}
	}
	raw, err := Fixture(run021Fixture)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := methodcheck.ValidateProjectJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if strings.HasPrefix(string(f.RuleID), "ATP-") {
			t.Errorf("%s: %s", f.RuleID, f.Message)
		}
	}

	impl, err := FixtureImpl(run021Fixture)
	if err != nil {
		t.Fatal(err)
	}
	dir := acceptanceBuild(t, impl)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	before, err := DockerInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// The Go half: the variants that need no browser (and the skipped ones, which are Go tests that
	// report their skip). The directory exists only when the plan emitted one.
	if _, err := os.Stat(filepath.Join(dir, "internal", "acceptance")); err == nil {
		if out, err := run(t, ctx, dir, "go", "test", "-p", "1", "-count=1", "-timeout", "30m", "-run", acceptanceGoRegexp, acceptanceGoPkg); err != nil {
			t.Errorf("the Go acceptance variants: %v\n%s", err, tail(out))
		}
	}
	// The web half.
	if out, err := webAcceptance(t, ctx, dir, ""); err != nil {
		t.Errorf("the browser acceptance variants: %v\n%s", err, tail(out))
	}

	verdicts := acceptanceVerdicts(t, dir)
	live := 0
	for _, id := range fx.residual {
		want := "pass"
		if fx.skipped[id] {
			want = "skip"
		} else {
			live++
		}
		if got := verdicts[id]; got != want {
			t.Errorf("variant %s: verdict %q, want %q", id, got, want)
		}
	}
	if live == 0 {
		t.Error("every acceptance variant is skipped: nothing was proven")
	}
	t.Logf("%d residual variant(s): %d ran and passed, %d skipped by the plan", len(fx.residual), live, len(fx.residual)-live)

	after, err := DockerInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if left := DockerLeftovers(ctx, before, after); len(left) != 0 {
		t.Errorf("the acceptance run left docker objects behind: %v", left)
	}
}

// mutatedImpl is the harvested implementation with one substring of one file replaced, in memory:
// the committed tree is never edited (as vacuousDiscard mutates the JSON, run021_test.go).
func mutatedImpl(t *testing.T, file, from, to string) fs.FS {
	t.Helper()
	impl, err := FixtureImpl(run021Fixture)
	if err != nil {
		t.Fatal(err)
	}
	out := fstest.MapFS{}
	replaced := false
	err = fs.WalkDir(impl, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(impl, path)
		if err != nil {
			return err
		}
		if path == file {
			if !strings.Contains(string(body), from) {
				t.Fatalf("%s does not contain %q: the harvested SPA changed; update the red proof", file, from)
			}
			body = []byte(strings.Replace(string(body), from, to, 1))
			replaced = true
		}
		out[path] = &fstest.MapFile{Data: body}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Fatalf("%s is not in the harvested tree", file)
	}
	return out
}

// An acceptance run is red when a write one client made is not shown on another: the same build with
// the SPA's todo item never rendering a completed todo as checked fails the variant whose probe
// asserts the member sees the agent's marked todo checked. The verdict file names the variant as a
// failure (the app routes it: projectstate.AcceptanceRoute names todo-web-app on the path and the
// REST client's activity as the one to re-open; the app's TestAcceptanceJob_RedRunHoldsAndNamesTheRoute).
func TestConformance_Acceptance_RedRoutesToTheClient(t *testing.T) {
	impl := mutatedImpl(t, acceptanceSPAFile, acceptanceChecked, "checked={false}")
	dir := acceptanceBuild(t, impl)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	out, err := webAcceptance(t, ctx, dir, `(?:^|\s)`+redVariant+` — `)
	if err == nil {
		t.Fatalf("the variant %s passed against a SPA that never shows a todo as checked:\n%s", redVariant, tail(out))
	}
	if got := acceptanceVerdicts(t, dir)[redVariant]; got != "fail" {
		t.Errorf("verdict of %s = %q, want fail\n%s", redVariant, got, tail(out))
	}
}
