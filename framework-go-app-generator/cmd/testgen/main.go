// Command testgen emits a project's deterministic scenario tests from the
// per-component test plans in project.json (package testgen). Run it from the
// app's server module (the directory holding its go.mod), the way cmd/webgen
// runs from the webApp:
//
//	go run github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/cmd/testgen
//	go run github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/cmd/testgen -check
//
// Flags:
//
//	-project  the project.json to read            (default ../.aiarch/state/project.json)
//	-root     the directory output paths resolve against: the Go module whose
//	          goPackage directories the tests land in (default .)
//	-module   the Go module path of -root           (default: the module line of <root>/go.mod)
//	-uitests  the Playwright output directory, relative to -root (default ../uitests/generated)
//	-check    report drift and exit 1 instead of writing
//	-discriminate
//	          run the null-subject discrimination instead of writing: for every Go
//	          (scenario, stimulus operation) pair, re-run the scenario's generated test
//	          with AIARCH_NULL_OPS naming that operation. Each scenario is first run
//	          against its real subject (the pre-run): one that does not pass there is
//	          printed as `TP-BROKEN <component> <scenario>`, is never nulled (its null
//	          runs would fail for the wrong reason and prove nothing) and makes the exit
//	          status 1. A scenario that passes is then run once per stimulus operation
//	          with that operation nulled; a run that still passes is a survivor, printed
//	          as `TP-DISCRIMINATE <component> <scenario> <op>`, and the exit status is 1.
//	          The last line is `discriminate: N scenario(s), M null run(s), S survivor(s),
//	          B broken`. A run that did not execute the scenario (the package does not
//	          build, the test skipped, no test matched, `[no test files]` with exit 0, or
//	          the test binary died before reporting) is inconclusive: it is the tool's
//	          error (exit 1, no survivor or broken line), never a kill or a pass. Machine-client
//	          scenarios null the Manager operation a call reaches in the app the test launches;
//	          web scenarios run through the scenario host (go tool webscenarios, which boots the
//	          app and serves the arrange endpoint) with that Manager operation nulled in the
//	          server it launches. A step that reaches no single generated Manager operation has
//	          nothing to null and is announced as skipped. Run it on a current tree (-check
//	          first); it does not write.
//
// Without -check it rewrites every generated file (nothing testgen emits is agent-owned: a
// scenario's subject, probes and arrange calls, and a browser spec's locators, are all
// generated) and prunes generated files (*_scenarios.gen_test.go and null.gen_test.go under
// root; *.spec.gen.ts, elements.gen.ts, runtime.gen.ts, playwright.config.gen.ts,
// reporter.gen.ts under the Playwright directory) that the current plan no longer
// produces. With -check it prints one line per drifted or stale file, per orphan
// generated file a write run would prune, and per orphan hooks file (any Go
// *_hooks_test.go or hooks.ts: nothing emits either any more, so every one is a
// leftover of an earlier generation; agent-owned, so reported but never pruned),
// then exits 1.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/cli"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/testgen"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// errDrift is the -check verdict when anything drifted; main maps it to exit 1.
var errDrift = errors.New("testgen: generated tests are out of date; run testgen")

type options struct {
	project, root, module, uitests string
	check, discriminate            bool
}

func run(args []string, out, errOut io.Writer) error {
	return runWith(args, out, errOut, execGoTest)
}

// runWith is run with the go test runner of -discriminate supplied.
func runWith(args []string, out, errOut io.Writer, goTest goTestRunner) error {
	o, err := parseOptions(args, errOut)
	if err != nil {
		return err
	}
	raw, err := cli.ReadAdmitted(o.project) // the one loader: every problem, before any emission
	if err != nil {
		return err
	}
	gen, err := testgen.Generate(raw, testgen.Config{ModulePath: o.module, UITestsDir: filepath.ToSlash(o.uitests)})
	if err != nil {
		return err
	}
	for _, w := range gen.Warnings {
		if err := say(errOut, w); err != nil {
			return err
		}
	}
	switch {
	case o.check && o.discriminate:
		return errors.New("testgen: -check and -discriminate are separate runs")
	case o.discriminate:
		return discriminate(o, gen, out, goTest)
	case o.check:
		return check(o, gen, out)
	}
	return write(o, gen, out)
}

// parseOptions reads the flags and resolves the module path from go.mod when -module is not given.
func parseOptions(args []string, errOut io.Writer) (options, error) {
	var o options
	fl := flag.NewFlagSet("testgen", flag.ContinueOnError)
	fl.SetOutput(errOut)
	fl.StringVar(&o.project, "project", "../.aiarch/state/project.json", "the project.json to read")
	fl.StringVar(&o.root, "root", ".", "the Go module directory output paths resolve against")
	fl.StringVar(&o.module, "module", "", "the Go module path of -root (default: from go.mod)")
	fl.StringVar(&o.uitests, "uitests", "../uitests/generated", "the Playwright output directory, relative to -root")
	fl.BoolVar(&o.check, "check", false, "report drift and exit 1 instead of writing")
	fl.BoolVar(&o.discriminate, "discriminate", false, "run the scenarios against a null subject and report the survivors")
	if err := fl.Parse(args); err != nil {
		return o, err
	}
	if o.module == "" {
		m, err := modulePath(filepath.Join(o.root, "go.mod"))
		if err != nil {
			return o, err
		}
		o.module = m
	}
	return o, nil
}

// modulePath reads the module line of a go.mod.
func modulePath(goMod string) (string, error) {
	raw, err := os.ReadFile(goMod) // #nosec G304 -- the caller's own go.mod
	if err != nil {
		return "", fmt.Errorf("testgen: -module not given and %w", err)
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("testgen: %s has no module line", goMod)
}

// check reports every way the tree differs from what a write run would
// leave: Drift over the generated files, the generated files a write run
// would prune (spec §5.3: any diff in generated files fails), and every hooks
// file still on disk — nothing emits one any more, so each is a leftover of an
// earlier generation. A write run leaves those alone (agent-owned), but a Go
// one still references the input types its pruned sibling declared, so the
// package no longer compiles and the break must be attributable.
func check(o options, gen testgen.Output, out io.Writer) error {
	msgs, err := testgen.Drift(o.root, gen)
	if err != nil {
		return err
	}
	orphans, err := orphanGenerated(o, gen)
	if err != nil {
		return err
	}
	for _, p := range orphans {
		msgs = append(msgs, "orphan generated file "+p)
	}
	orphanHooks, err := orphanHooksFiles(o)
	if err != nil {
		return err
	}
	for _, p := range orphanHooks {
		msgs = append(msgs, "orphan hooks file "+p)
	}
	for _, m := range msgs {
		if err := say(out, m); err != nil {
			return err
		}
	}
	if len(msgs) > 0 {
		return errDrift
	}
	return nil
}

// say writes one line to w; the CLI's only output path, so a closed stdout is
// reported rather than ignored.
func say(w io.Writer, line string) error {
	_, err := fmt.Fprintln(w, line)
	return err
}

// write rewrites every generated file and prunes generated files the plan no
// longer produces.
func write(o options, gen testgen.Output, out io.Writer) error {
	for p, b := range gen.Generated {
		if err := writeFile(o.root, p, b); err != nil {
			return err
		}
	}
	pruned, err := prune(o, gen)
	if err != nil {
		return err
	}
	for _, p := range pruned {
		if err := say(out, "pruned "+p); err != nil {
			return err
		}
	}
	return say(out, fmt.Sprintf("wrote %d generated file(s)", len(gen.Generated)))
}

func writeFile(root, p string, b []byte) error {
	full := filepath.Join(root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return fmt.Errorf("testgen: %w", err)
	}
	if err := os.WriteFile(full, b, 0o600); err != nil {
		return fmt.Errorf("testgen: %w", err)
	}
	return nil
}

// orphanGenerated lists, root-relative with slashes, the generated files the
// plan no longer produces: Go scenario files anywhere under root, Playwright
// generated files under the uitests directory. Hooks files are never listed
// (they are agent-owned).
func orphanGenerated(o options, gen testgen.Output) ([]string, error) {
	keep := func(rel string) bool { _, ok := gen.Generated[filepath.ToSlash(rel)]; return ok }
	goStale, err := staleFiles(o.root, o.root, isGoScenarioFile, keep)
	if err != nil {
		return nil, err
	}
	tsStale, err := staleFiles(o.root, filepath.Join(o.root, filepath.FromSlash(o.uitests)), isPlaywrightGenFile, keep)
	if err != nil {
		return nil, err
	}
	var orphans []string
	for _, rel := range append(goStale, tsStale...) {
		orphans = append(orphans, filepath.ToSlash(rel))
	}
	return orphans, nil
}

// orphanHooksFiles lists, root-relative with slashes, every hooks file on disk:
// Go hooks files anywhere under root, hooks.ts under the uitests directory.
// Nothing emits a hooks file, so none is current; they are reported by -check
// and never pruned: the agent owns them.
func orphanHooksFiles(o options) ([]string, error) {
	keep := func(string) bool { return false }
	goStale, err := staleFiles(o.root, o.root, isGoHooksFile, keep)
	if err != nil {
		return nil, err
	}
	tsStale, err := staleFiles(o.root, filepath.Join(o.root, filepath.FromSlash(o.uitests)), isPlaywrightHooksFile, keep)
	if err != nil {
		return nil, err
	}
	var orphans []string
	for _, rel := range append(goStale, tsStale...) {
		orphans = append(orphans, filepath.ToSlash(rel))
	}
	return orphans, nil
}

// prune removes the orphan generated files and returns their paths.
func prune(o options, gen testgen.Output) ([]string, error) {
	orphans, err := orphanGenerated(o, gen)
	if err != nil {
		return nil, err
	}
	for _, p := range orphans {
		if err := os.Remove(filepath.Join(o.root, filepath.FromSlash(p))); err != nil { // #nosec G703 -- an orphan found by walking the caller's own -root
			return nil, fmt.Errorf("testgen: prune: %w", err)
		}
	}
	return orphans, nil
}

// staleFiles walks dir and returns, relative to root, every file match
// accepts that keep rejects. node_modules and dot-directories are skipped; a
// missing dir is simply empty.
func staleFiles(root, dir string, match func(string) bool, keep func(string) bool) ([]string, error) {
	var stale []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			return skipIfVendored(p, dir, d.Name())
		case !match(d.Name()):
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if !keep(rel) {
			stale = append(stale, rel)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return stale, err
}

// skipIfVendored skips node_modules and dot-directories below the walk root.
func skipIfVendored(p, dir, name string) error {
	if p != dir && (name == "node_modules" || strings.HasPrefix(name, ".")) {
		return filepath.SkipDir
	}
	return nil
}

// isGoScenarioFile matches the generated Go test files of a scenario package: the scenarios file and
// the null file holding its TestMain.
func isGoScenarioFile(name string) bool {
	return strings.HasSuffix(name, "_scenarios.gen_test.go") || name == "null.gen_test.go"
}

func isGoHooksFile(name string) bool { return strings.HasSuffix(name, "_hooks_test.go") }

func isPlaywrightHooksFile(name string) bool { return name == "hooks.ts" }

func isPlaywrightGenFile(name string) bool {
	return strings.HasSuffix(name, ".spec.gen.ts") || name == "playwright.config.gen.ts" || name == "reporter.gen.ts" ||
		name == "elements.gen.ts" || name == "runtime.gen.ts"
}
