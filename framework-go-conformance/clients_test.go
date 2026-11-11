//go:build conformance

package conformance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// The machine clients and the browser surface of the todomvc fixture, run for real (FPC plan 3,
// Task 3.6): the fixture is built with the RUN-021 harvest's hand-written bodies, every machine-client
// scenario drives the app the test launches over the wire, as the principals of the plan, through the
// policy the composition root generated, and the null-subject run counts the client and the web
// scenarios as well as the components'.
//
// The two tests below read one run (the first to need it performs it): building, seating the UI
// tests, running the 28 client scenarios one `go test` each, and the whole -discriminate run is by far
// the slowest thing the harness does, and each test only asserts a different half of it.

// clientContracts is the machine clients of the fixture and how many scenarios each binds.
var clientContracts = map[string]int{"todoApi": 8, "todoMcpServer": 20}

// clientScenario is one generated machine-client scenario test.
type clientScenario struct{ contract, id, fn, pkg string }

// harvestRun is everything one run of the built harvest measured.
type harvestRun struct {
	clients    []clientScenario
	clientFail map[string]string // "contract/scenario" -> why it did not pass over the wire
	goCount    int               // TestScenario_ functions generated for Go components and machine clients
	webIDs     []string          // the browser scenarios, by id
	disc       discriminateRun
	left       []string // docker objects the whole run left behind
}

var harvest struct {
	once sync.Once
	run  harvestRun
	err  string
}

// harvested performs the harvest run once per test binary and returns what it measured; a run that
// could not complete fails every test that reads it.
func harvested(t *testing.T) harvestRun {
	t.Helper()
	harvest.once.Do(func() {
		// A step that fails the performing test ends it with runtime.Goexit, which still marks the
		// Once done: the error stays set unless the run completed.
		harvest.err = "the harvest run did not complete (the test that performed it reports why)"
		if run, ok := performHarvest(t); ok {
			harvest.run, harvest.err = run, ""
		}
	})
	if harvest.err != "" {
		t.Fatal(harvest.err)
	}
	return harvest.run
}

func performHarvest(t *testing.T) (harvestRun, bool) {
	t.Helper()
	raw, err := Fixture(run021Fixture)
	if err != nil {
		t.Errorf("Fixture: %v", err)
		return harvestRun{}, false
	}
	impl, err := FixtureImpl(run021Fixture)
	if err != nil {
		t.Errorf("FixtureImpl: %v", err)
		return harvestRun{}, false
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Minute)
	defer cancel()
	if _, err := RunChainOn(ctx, run021Fixture, raw, dir, WithImpl(impl)); err != nil {
		t.Errorf("RunChain (harvested bodies): %v", err)
		return harvestRun{}, false
	}
	seatUITests(t, ctx, dir)
	before, err := DockerInventory(ctx)
	if err != nil {
		t.Errorf("DockerInventory: %v", err)
		return harvestRun{}, false
	}

	var run harvestRun
	run.clients = clientScenarios(t, dir)
	run.clientFail = map[string]string{}
	for _, c := range run.clients {
		if why := runClientScenario(ctx, t, dir, c); why != "" {
			run.clientFail[c.contract+"/"+c.id] = why
		}
	}
	run.goCount = goScenarios(t, dir)
	run.webIDs = webScenarioIDs(t, dir)
	run.disc = discriminate(t, dir)

	after, err := DockerInventory(ctx)
	if err != nil {
		t.Errorf("DockerInventory: %v", err)
		return harvestRun{}, false
	}
	run.left = DockerLeftovers(ctx, before, after)
	return run, true
}

var (
	scenarioFunc = regexp.MustCompile(`^func (TestScenario_\w+)\(`)
	runScenario  = regexp.MustCompile(`h\.RunScenario\(t, "([^"]+)", "([^"]+)"`)
)

// clientScenarios lists the machine-client scenario tests generated under internal/client: the test
// function, the package it lives in, the contract it is keyed under and the scenario it runs.
func clientScenarios(t *testing.T, dir string) []clientScenario {
	t.Helper()
	var out []clientScenario
	root := filepath.Join(dir, "internal", "client")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_scenarios.gen_test.go") {
			return err
		}
		src, err := os.ReadFile(path) // #nosec G304 -- the chain's own output
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, filepath.Dir(path))
		if err != nil {
			return err
		}
		fn := ""
		for _, line := range strings.Split(string(src), "\n") {
			if m := scenarioFunc.FindStringSubmatch(line); m != nil {
				fn = m[1]
			}
			if m := runScenario.FindStringSubmatch(line); m != nil && fn != "" {
				out = append(out, clientScenario{contract: m[1], id: m[2], fn: fn, pkg: "./" + filepath.ToSlash(rel)})
				fn = ""
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("listing the generated client scenarios: %v", err)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].contract+"/"+out[i].id < out[j].contract+"/"+out[j].id
	})
	return out
}

// runClientScenario runs one scenario of one machine client as its own `go test` (the app it launches
// is the whole fixture) and returns why it did not pass: the process's verdict, and the verdict the
// scenario host recorded for it under the contract in test-results/<contract>/results.json.
func runClientScenario(ctx context.Context, t *testing.T, dir string, c clientScenario) string {
	t.Helper()
	out, err := run(t, ctx, dir, "go", "test", "-p", "1", "-count=1", "-timeout", "10m", "-run", "^"+c.fn+"$", c.pkg)
	if err != nil {
		return "go test: " + err.Error() + "\n" + tail(out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "test-results", c.contract, "results.json")) // #nosec G304 -- the scenario host's own index
	if err != nil {
		return "no verdict recorded under the contract: " + err.Error()
	}
	var verdicts []struct{ Scenario, Status string }
	if err := json.Unmarshal(raw, &verdicts); err != nil {
		return "results.json: " + err.Error()
	}
	for _, v := range verdicts {
		if v.Scenario == c.id {
			if v.Status != "pass" {
				return "recorded verdict " + v.Status
			}
			return ""
		}
	}
	return "results.json has no verdict for the scenario"
}

// webScenarioIDs lists the browser scenarios generated under uitests/generated, by id (the surface
// check is not a scenario).
func webScenarioIDs(t *testing.T, dir string) []string {
	t.Helper()
	var ids []string
	root := filepath.Join(dir, "uitests", "generated")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		base := filepath.Base(path)
		if err != nil || d.IsDir() || !strings.HasSuffix(base, ".spec.gen.ts") || base == "surface.spec.gen.ts" {
			return err
		}
		ids = append(ids, strings.TrimSuffix(base, ".spec.gen.ts"))
		return nil
	})
	if err != nil {
		t.Fatalf("listing the generated browser scenarios: %v", err)
	}
	sort.Strings(ids)
	return ids
}

// Every scenario of the fixture's two machine clients, todoApi (REST, 8) and todoMcpServer (MCP, 20),
// passes against the harvest's real bodies over the wire: the app is launched by the scenario host,
// each step is one request as the plan's principal, the generated policy decides it and the Manager
// answers. The null-subject run counts the client scenarios with the components' and finds no
// survivor (a client scenario that passes with its Manager operation nulled asserts nothing the
// operation did), and the whole run leaves no docker object behind.
func TestConformance_ClientsOverWire(t *testing.T) {
	got := harvested(t)

	perContract := map[string]int{}
	for _, c := range got.clients {
		perContract[c.contract]++
	}
	for contract, want := range clientContracts {
		if perContract[contract] != want {
			t.Errorf("%s: %d generated scenario(s), want %d", contract, perContract[contract], want)
		}
	}
	if len(got.clientFail) > 0 {
		keys := make([]string, 0, len(got.clientFail))
		for k := range got.clientFail {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t.Errorf("client scenario %s does not pass over the wire: %s", k, got.clientFail[k])
		}
	}

	if got.disc.survivors != 0 {
		for _, s := range got.disc.list {
			t.Errorf("%s scenario %s op %s survives its operation being nulled", s.component, s.scenario, s.op)
		}
	}
	for _, b := range got.disc.brokenList {
		if _, isClient := clientContracts[b.component]; isClient {
			t.Errorf("client scenario %s/%s fails against its real subject in the null-subject run (TP-BROKEN)", b.component, b.scenario)
		}
	}
	if want := implScenarios + len(got.webIDs); got.disc.scenarios != want {
		t.Errorf("-discriminate counted %d scenario(s), want %d: %d Go (78 components + 28 machine clients) and %d browser", got.disc.scenarios, want, implScenarios, len(got.webIDs))
	}
	if got.goCount != implScenarios {
		t.Errorf("%d TestScenario_ functions generated, want %d", got.goCount, implScenarios)
	}
	if len(got.left) > 0 {
		t.Errorf("the run left docker resources behind: %v", got.left)
	}
}

// The browser half of the null-subject run: the fixture's todo-web-app scenarios run through the
// scenario host (go tool webscenarios), each once against its real server and, when that passes, once
// per stimulus operation with the Manager operation it reaches nulled in the server the host launches.
// None is dropped or announced as skipped, and none survives. The fixture's SPA is the generated
// scaffold (the harvest's hand-written bodies are the server's), which cannot satisfy a scenario that
// asserts what the user sees, so those are TP-BROKEN, never nulled: the proof here is that the run
// reaches a verdict for every browser scenario through the host and leaves nothing behind.
func TestConformance_Discriminate_Web(t *testing.T) {
	got := harvested(t)
	if len(got.webIDs) == 0 {
		t.Fatal("the fixture generated no browser scenario")
	}
	web := map[string]bool{}
	for _, id := range got.webIDs {
		web[id] = true
	}
	broken := map[string]bool{}
	for _, b := range got.disc.brokenList {
		if b.component == webSurface {
			broken[b.scenario] = true
			if !web[b.scenario] {
				t.Errorf("TP-BROKEN names browser scenario %s, which the surface has no spec for", b.scenario)
			}
		}
	}
	for _, s := range got.disc.list {
		if s.component == webSurface {
			t.Errorf("browser scenario %s op %s survives its Manager operation being nulled", s.scenario, s.op)
		}
	}
	if got.disc.scenarios != implScenarios+len(got.webIDs) {
		t.Errorf("-discriminate counted %d scenario(s), want %d Go and %d browser", got.disc.scenarios, implScenarios, len(got.webIDs))
	}
	for _, id := range got.webIDs {
		if !broken[id] {
			t.Errorf("browser scenario %s passes against the scaffold SPA: the fixture's browser bindings changed; pin which ones are TP-BROKEN again", id)
		}
	}
	if len(got.left) > 0 {
		t.Errorf("the run left docker resources behind: %v", got.left)
	}
}
