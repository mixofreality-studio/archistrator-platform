// Command webscenarios runs an app's generated Playwright scenarios under the scenario host (Plan 3
// Task 3.5, design decision D6). It boots the host's substrates (the Temporal dev server, Postgres,
// the git and GitHub doubles), launches the composed app into them through its generated run
// contract, serves the arrange endpoint a browser scenario's advanceClock, failNext and seedResource
// reach (the app forwards /__scenariohost to it), and runs `npx playwright test` against the launched
// app. Playwright's own webServer would kill the app with SIGKILL and leak its Postgres container;
// here the app and the host are stopped by their owner, and the run fails (exit 3) naming any
// container or volume it leaves behind.
//
//	go tool webscenarios -C uitests -config generated/<contract>/playwright.config.gen.ts \
//	    [-grep <regexp>] [-project surface] [-module <dir>] [-- extra playwright args]
//
// Flags:
//
//	-C        the directory playwright runs in, where its config and node_modules live (default .)
//	-config   the Playwright config to run, relative to -C (required)
//	-grep     only the tests whose title matches
//	-project  only the Playwright project of that name (for example surface)
//	-module   the app's Go module root, holding .aiarch/state/project.json and appgen.json
//	          (default: the module of the working directory)
//
// The environment is passed to Playwright unchanged (AIARCH_NULL_OPS of a discrimination run,
// AIARCH_SCENARIO_TIMEOUT_MS, ...), plus PLAYWRIGHT_TEST_BASE_URL (the launched app) and, when the
// plan declares principals, AIARCH_DEV_PRINCIPAL: the browser's subject, which is the app's process
// principal (every plan principal is allowlisted beside it). For a surface's test plan that is the
// first in name order; for an acceptance run (-config generated/acceptance/...) it is the acceptance
// plan's browserActor, and a web spec without one is an error naming ATP-BROWSER (planPrincipals).
//
// The exit status is Playwright's. A bad invocation is 2, a launch or teardown failure is 1, and a
// leftover container or volume is 3.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"syscall"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/devstack"
	scenariohost "github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
)

// Exit statuses besides Playwright's own.
const (
	exitFailure  = 1
	exitUsage    = 2
	exitLeftover = 3
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code := run(ctx, os.Args[1:], os.Environ(), realDeps(), os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// options is one invocation.
type options struct {
	dir, config, grep, project, module string
	extra                              []string
}

// parseArgs reads the flags; what follows "--" goes to Playwright unchanged.
func parseArgs(args []string, errOut io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("webscenarios", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.StringVar(&o.dir, "C", ".", "the directory playwright runs in")
	fs.StringVar(&o.config, "config", "", "the Playwright config to run, relative to -C")
	fs.StringVar(&o.grep, "grep", "", "only the tests whose title matches")
	fs.StringVar(&o.project, "project", "", "only the Playwright project of that name")
	fs.StringVar(&o.module, "module", "", "the app's Go module root")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if o.config == "" {
		return o, errors.New("webscenarios: -config is required")
	}
	o.extra = fs.Args()
	return o, nil
}

// stack is a launched app a run drives.
type stack interface {
	// BaseURL is the launched app's origin.
	BaseURL() string
	// Principal is the subject the app runs as ("" when the surface's plan declares none).
	Principal() string
	// Stop ends the app, the arrange server and the host, returning what it could not give back.
	Stop() error
}

// command is the Playwright invocation.
type command struct {
	dir  string
	argv []string
	env  []string
}

// deps is what a run touches outside itself.
type deps struct {
	// start boots the host and launches the app into it.
	start func(ctx context.Context, o options) (stack, error)
	// exec runs a command, returning its exit status.
	exec func(ctx context.Context, c command, stdout, stderr io.Writer) (int, error)
	// inventory lists the docker containers and volumes that exist now.
	inventory func(ctx context.Context) ([]string, error)
}

func realDeps() deps {
	return deps{start: startReal, exec: execReal, inventory: devstack.DockerInventory}
}

// playwrightCommand is the invocation of o against the launched app st, under environ.
func playwrightCommand(o options, st stack, environ []string) command {
	argv := []string{"npx", "playwright", "test", "--config", o.config}
	if o.grep != "" {
		argv = append(argv, "--grep", o.grep)
	}
	if o.project != "" {
		argv = append(argv, "--project", o.project)
	}
	argv = append(argv, o.extra...)
	env := append([]string(nil), environ...)
	env = append(env, "PLAYWRIGHT_TEST_BASE_URL="+st.BaseURL())
	if p := st.Principal(); p != "" {
		env = append(env, "AIARCH_DEV_PRINCIPAL="+p)
	}
	return command{dir: o.dir, argv: argv, env: env}
}

// report writes one line to w; a diagnostic that cannot be written has nowhere else to go.
func report(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}

// run is one invocation; it returns the exit status.
func run(ctx context.Context, args, environ []string, d deps, stdout, stderr io.Writer) int {
	o, err := parseArgs(args, stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			report(stderr, "%v", err)
		}
		return exitUsage
	}
	// Teardown and the leftover check outlive a cancelled run: an interrupted run must still give
	// its containers back.
	cleanup := context.WithoutCancel(ctx)
	before, err := d.inventory(cleanup)
	if err != nil {
		report(stderr, "webscenarios: docker inventory: %v", err)
		return exitFailure
	}
	st, err := d.start(ctx, o)
	if err != nil {
		report(stderr, "webscenarios: launch: %v", err)
		return exitFailure
	}
	code := playwright(ctx, d, playwrightCommand(o, st, environ), stdout, stderr)
	if err := st.Stop(); err != nil {
		report(stderr, "webscenarios: stop: %v", err)
		if code == 0 {
			code = exitFailure
		}
	}
	return leftoverStatus(cleanup, d, before, code, stderr)
}

// playwright runs c and returns its exit status; a run that could not start is a failure.
func playwright(ctx context.Context, d deps, c command, stdout, stderr io.Writer) int {
	code, err := d.exec(ctx, c, stdout, stderr)
	if err != nil {
		report(stderr, "webscenarios: playwright: %v", err)
		return exitFailure
	}
	return code
}

// leftoverStatus is the run's exit status once its app is stopped: code, unless the run left a
// container or volume behind (exit 3, naming it), or the docker inventory could not be taken.
func leftoverStatus(ctx context.Context, d deps, before []string, code int, stderr io.Writer) int {
	after, err := d.inventory(ctx)
	if err != nil {
		report(stderr, "webscenarios: docker inventory: %v", err)
		return exitFailure
	}
	if left := devstack.DockerLeftovers(ctx, before, after); len(left) > 0 {
		report(stderr, "webscenarios: the run left behind: %v", left)
		return exitLeftover
	}
	return code
}

// execReal runs c with its output on the given writers; a command that ran and failed is its exit
// status, one that could not run is an error.
func execReal(ctx context.Context, c command, stdout, stderr io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, c.argv[0], c.argv[1:]...) // #nosec G204 -- npx playwright with arguments from the flags
	cmd.Dir = c.dir
	cmd.Env = c.env
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	}
	return 0, err
}

// launched is the real stack: the booted host and the app launched into it.
type launched struct {
	host      *scenariohost.Host
	web       *scenariohost.WebApp
	principal string
}

func (l *launched) BaseURL() string   { return l.web.App.BaseURL }
func (l *launched) Principal() string { return l.principal }

func (l *launched) Stop() error {
	err := l.web.Stop()
	l.host.Close()
	return err
}

// launchFunc boots the host and launches the app at module as principals, the first being the process
// principal; browser is the subject the browser runs as (the first principal's).
type launchFunc func(ctx context.Context, module string, principals []scenariohost.Principal, browser string) (stack, error)

// startReal boots the host and launches the app of the module o names, as the principals of the plan
// o's config belongs to (planPrincipals).
func startReal(ctx context.Context, o options) (stack, error) {
	return startWith(ctx, o, launchReal)
}

// startWith resolves the module and the plan's principals and hands them to launch.
func startWith(ctx context.Context, o options, launch launchFunc) (stack, error) {
	module := o.module
	if module == "" {
		var err error
		if module, err = moduleRoot(); err != nil {
			return nil, err
		}
	}
	// The config is relative to the directory playwright runs in.
	config := o.config
	if !filepath.IsAbs(config) {
		config = filepath.Join(o.dir, config)
	}
	principals, browser, err := planPrincipals(module, config)
	if err != nil {
		return nil, err
	}
	return launch(ctx, module, principals, browser)
}

// launchReal is the production launch: the host's substrates, then the app into them.
func launchReal(ctx context.Context, module string, principals []scenariohost.Principal, browser string) (stack, error) {
	h, err := scenariohost.Boot(ctx)
	if err != nil {
		return nil, err
	}
	web, err := h.LaunchWeb(ctx, module, principals...)
	if err != nil {
		h.Close()
		return nil, err
	}
	return &launched{host: h, web: web, principal: browser}, nil
}

// moduleRoot is the nearest directory at or above the working directory holding a go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("no go.mod at or above %s: pass -module", dir)
		}
		d = parent
	}
}

// acceptanceSurface is the directory name generated/<name>/ an acceptance run's config lives in.
const acceptanceSurface = "acceptance"

// planPrincipal is one named actor of a plan as project.json carries it.
type planPrincipal struct {
	Subject    string   `json:"subject"`
	Roles      []string `json:"roles"`
	OnBehalfOf string   `json:"onBehalfOf"`
	Intents    []string `json:"intents"`
}

// acceptancePlan is the part of the acceptance plan a launch reads.
type acceptancePlan struct {
	Principals   map[string]planPrincipal `json:"principals"`
	BrowserActor string                   `json:"browserActor"`
}

// planPrincipals is the principals a Playwright config's run launches the app as, and the subject
// the browser runs as (<module>/.aiarch/state/project.json). The surface is the config's directory
// name (generated/<surface>/playwright.config.gen.ts).
//
// An acceptance config (generated/acceptance/) takes the acceptance plan's principals
// (phaseArtifacts.acceptancePlan.principals) with the browserActor's principal first: the app's
// process principal is the SPA's own identity, because its fetches carry no header, so the actor the
// browser plays must be the first. An acceptance plan with web specs under the config's directory
// and no browserActor is an error naming ATP-BROWSER, and so is a browserActor the plan does not
// declare.
//
// Any other config takes the surface's test plan's principals (phaseArtifacts.testPlan.<surface>),
// in name order, the browser being the first. A model or a plan with none yields none: the app then
// runs as its default principal.
func planPrincipals(module, config string) (principals []scenariohost.Principal, browser string, err error) {
	raw, err := os.ReadFile(filepath.Join(module, ".aiarch", "state", "project.json")) // #nosec G304 -- the app under test
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var doc struct {
		PhaseArtifacts struct {
			TestPlan map[string]struct {
				Principals map[string]planPrincipal `json:"principals"`
			} `json:"testPlan"`
			AcceptancePlan *acceptancePlan `json:"acceptancePlan"`
		} `json:"phaseArtifacts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, "", fmt.Errorf("project.json: %w", err)
	}
	dir := filepath.Dir(config)
	surface := filepath.Base(dir)
	if surface != acceptanceSurface {
		principals, browser := principalsInOrder(doc.PhaseArtifacts.TestPlan[surface].Principals, "")
		return principals, browser, nil
	}
	return acceptancePrincipals(doc.PhaseArtifacts.AcceptancePlan, webSpecs(dir))
}

// acceptancePrincipals is planPrincipals for an acceptance run over a plan (nil: the model has none)
// whose generated specs include a browser spec when web is set.
func acceptancePrincipals(plan *acceptancePlan, web bool) ([]scenariohost.Principal, string, error) {
	if plan == nil {
		plan = &acceptancePlan{}
	}
	if plan.BrowserActor == "" && web {
		return nil, "", errors.New("webscenarios: the acceptance specs run in a browser but the acceptance plan names no browserActor: one actor owns the browser (ATP-BROWSER)")
	}
	if _, ok := plan.Principals[plan.BrowserActor]; plan.BrowserActor != "" && !ok {
		return nil, "", fmt.Errorf("webscenarios: the acceptance plan's browserActor %q is not one of its principals (ATP-BROWSER)", plan.BrowserActor)
	}
	principals, browser := principalsInOrder(plan.Principals, plan.BrowserActor)
	return principals, browser, nil
}

// principalsInOrder is the principals in name order with the one named first (when set) moved to the
// front, and the first one's subject.
func principalsInOrder(byName map[string]planPrincipal, first string) ([]scenariohost.Principal, string) {
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	if i := slices.Index(names, first); first != "" && i >= 0 {
		names = slices.Insert(slices.Delete(names, i, i+1), 0, first)
	}
	out := make([]scenariohost.Principal, 0, len(names))
	for _, name := range names {
		p := byName[name]
		out = append(out, scenariohost.Principal{Subject: p.Subject, Roles: p.Roles, OnBehalfOf: p.OnBehalfOf, Intents: p.Intents})
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out, out[0].Subject
}

// webSpecs reports whether dir holds a generated Playwright spec.
func webSpecs(dir string) bool {
	specs, err := filepath.Glob(filepath.Join(dir, "*.spec.gen.ts"))
	return err == nil && len(specs) > 0
}
