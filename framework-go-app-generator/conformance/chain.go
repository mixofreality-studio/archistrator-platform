package conformance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

const (
	platformModulePrefix = "github.com/mixofreality-studio/archistrator-platform/"
	appGeneratorModule   = platformModulePrefix + "framework-go-app-generator"
	nullgenTool          = appGeneratorModule + "/cmd/nullgen"
	statePath            = ".aiarch/state/project.json"
	webDir               = "webApp"
	stderrTail           = 16 << 10
)

// Report is the record of one chain run.
type Report struct {
	Fixture, Dir string
	Steps        []StepResult
}

// StepResult is one command the chain ran (or the in-process scaffold step).
type StepResult struct {
	Name     string
	Argv     []string
	ExitCode int
	Stderr   string
	Duration time.Duration
}

// platformRoot is the platform checkout this package was built from: the chain
// builds the scaffolded app against the generators and runtime in the tree under
// test, never against the published tags (which lack the change being proven).
func platformRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("conformance: cannot locate the platform checkout (runtime.Caller)")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "framework-go-app-generator", "go.mod")); err != nil {
		return "", fmt.Errorf("conformance: %s is not the platform checkout: %w", root, err)
	}
	return root, nil
}

// RunChain scaffolds fixture into dir and runs the exact command chain a scaffolded
// app runs: make gen-models, make gen-null, go tool appgen {temporal,clients,config,
// compose}, make gen-tests, go tool webgen bootstrap+generate (browser surfaces),
// go build, go vet and the web typecheck. The scaffolded Makefile targets are run by
// name, so the pipeline follows whatever method-assets ships. It stops at the first
// failing step; the Report holds every step that ran, the failing one last.
func RunChain(ctx context.Context, fixture string, dir string) (Report, error) {
	raw, err := Fixture(fixture)
	if err != nil {
		return Report{Fixture: fixture, Dir: dir}, err
	}
	return RunChainOn(ctx, fixture, raw, dir)
}

// RunChainOn is RunChain over project.json bytes the caller supplies (a fixture with one
// binding changed, say); fixture only names the scaffold's module and project.
func RunChainOn(ctx context.Context, fixture string, raw []byte, dir string) (Report, error) {
	rep := Report{Fixture: fixture, Dir: dir}
	model, err := projectmodel.Load(raw)
	if err != nil {
		return rep, fmt.Errorf("fixture %s does not load: %w", fixture, err)
	}
	root, err := platformRoot()
	if err != nil {
		return rep, err
	}

	start := time.Now()
	scaffoldErr := scaffold(dir, fixture, raw, root)
	rep.Steps = append(rep.Steps, stepOf("scaffold", []string{"scaffold", fixture}, scaffoldErr, start))
	if scaffoldErr != nil {
		return rep, scaffoldErr
	}

	run := &runner{ctx: ctx, dir: dir, rep: &rep}
	for _, st := range chainSteps(model) {
		if err := run.step(st); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// chainStep is one command of the chain; sub is its working directory under the
// scaffold ("" is the module root).
type chainStep struct {
	name string
	sub  string
	argv []string
}

// chainSteps is the ordered command list for model. gen-null comes first: modelgen
// and appgen generate for a contract package only once it holds code (null.gen.go is
// that code in a model with nothing built), and appgen compose seeds hooks.go only
// when absent, which nullgen would then refuse to replace.
func chainSteps(model *projectmodel.Model) []chainStep {
	steps := []chainStep{
		{name: "go mod tidy", argv: []string{"go", "mod", "tidy"}},
		{name: "make gen-null", argv: []string{"make", "gen-null"}},
		{name: "make gen-models", argv: []string{"make", "gen-models"}},
	}
	for _, sub := range []string{"temporal", "clients", "config", "compose"} {
		steps = append(steps, chainStep{name: "appgen " + sub,
			argv: []string{"go", "tool", "appgen", sub, "-project", statePath, "-root", "."}})
	}
	steps = append(steps, chainStep{name: "make gen-tests", argv: []string{"make", "gen-tests"}})
	surfaces := model.System.BuiltBrowserSurfaces()
	for _, s := range surfaces {
		steps = append(steps, chainStep{name: "webgen bootstrap " + s.ID,
			argv: []string{"go", "tool", "webgen", "bootstrap", "-C", webDir, "-surface", s.ID}})
	}
	if len(surfaces) > 0 {
		steps = append(steps, chainStep{name: "webgen generate",
			argv: []string{"go", "tool", "webgen", "generate", "-C", webDir}})
	}
	steps = append(steps,
		chainStep{name: "go mod tidy (generated imports)", argv: []string{"go", "mod", "tidy"}},
		chainStep{name: "go build", argv: []string{"go", "build", "./..."}},
		chainStep{name: "go vet", argv: []string{"go", "vet", "./..."}},
	)
	if len(surfaces) > 0 {
		// bootstrap seats a package.json but no lockfile, so npm ci has nothing to
		// install from; npm install writes the lockfile the typecheck then uses.
		steps = append(steps,
			chainStep{name: "npm install", sub: webDir, argv: []string{"npm", "install", "--no-audit", "--no-fund"}},
			chainStep{name: "npm run gen:api", sub: webDir, argv: []string{"npm", "run", "gen:api"}},
			chainStep{name: "npm run typecheck", sub: webDir, argv: []string{"npm", "run", "typecheck"}},
		)
	}
	return steps
}

// runner executes steps in a scaffold with GOWORK=off, recording each.
type runner struct {
	ctx context.Context
	dir string
	rep *Report
}

func (r *runner) step(st chainStep) error {
	start := time.Now()
	cmd := exec.CommandContext(r.ctx, st.argv[0], st.argv[1:]...) // #nosec G204 -- fixed argv built above
	cmd.Dir = filepath.Join(r.dir, st.sub)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	res := StepResult{Name: st.name, Argv: st.argv, Duration: time.Since(start), Stderr: tail(out.String())}
	if err != nil {
		res.ExitCode = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		}
	}
	r.rep.Steps = append(r.rep.Steps, res)
	if err != nil {
		return fmt.Errorf("conformance: step %q failed (exit %d): %w\n%s", st.name, res.ExitCode, err, res.Stderr)
	}
	return nil
}

func stepOf(name string, argv []string, err error, start time.Time) StepResult {
	res := StepResult{Name: name, Argv: argv, Duration: time.Since(start)}
	if err != nil {
		res.ExitCode = 1
		res.Stderr = err.Error()
	}
	return res
}

// tail keeps the end of a command's combined output, where its error is.
func tail(s string) string {
	if len(s) <= stderrTail {
		return s
	}
	return "..." + s[len(s)-stderrTail:]
}

// scaffold seats the managed app scaffold (the same file set a project is born with),
// the fixture as its project.json, and points every platform module the app requires at
// the checkout under test, adding the nullgen tool the published scaffold lacks until
// the release carrying cmd/nullgen.
func scaffold(dir, fixture string, raw []byte, platform string) error {
	files, err := methodassets.ScaffoldFiles(methodassets.ScaffoldData{
		ModulePath: "example.com/conformance/" + fixture, AppSlug: "aiarch-app",
		ProjectID: fixture, Owner: "conformance", Name: fixture,
		StateMcpModulePath:    "example.com/conformance/aiarch-state-mcp",
		StateMcpModuleVersion: "v0.0.0-conformance",
	})
	if err != nil {
		return err
	}
	files[statePath] = raw
	for path, content := range files {
		dest := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(dest, content, 0o600); err != nil {
			return err
		}
	}
	return localizeModule(dir, platform)
}

// localizeModule rewrites the scaffolded go.mod to build against the platform checkout.
func localizeModule(dir, platform string) error {
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod")) // #nosec G304 -- the scaffold just written
	if err != nil {
		return err
	}
	args := []string{"mod", "edit", "-tool=" + nullgenTool}
	var mods []string
	for _, line := range strings.Split(string(gomod), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.HasPrefix(f[0], platformModulePrefix) && !strings.Contains(f[0], "/cmd/") {
			mods = append(mods, f[0])
		}
	}
	sort.Strings(mods)
	for _, m := range mods {
		local := filepath.Join(platform, strings.TrimPrefix(m, platformModulePrefix))
		if _, err := os.Stat(filepath.Join(local, "go.mod")); err != nil { // #nosec G703 -- a sibling of this checkout, named by the scaffold's own go.mod
			continue // a module the checkout does not carry stays at its pinned release
		}
		args = append(args, "-replace="+m+"="+local)
	}
	cmd := exec.Command("go", args...) // #nosec G204 -- fixed verb, paths from the scaffold
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}
