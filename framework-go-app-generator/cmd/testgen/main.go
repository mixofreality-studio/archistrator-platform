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
//
// Without -check it rewrites every generated file, writes each hooks file
// only when absent, and prunes generated files (*_scenarios.gen_test.go under
// root; *.spec.gen.ts, playwright.config.gen.ts, reporter.gen.ts under the
// Playwright directory) that the current plan no longer produces. With -check
// it prints one line per drifted or stale file, per missing/orphan hook, per
// orphan generated file a write run would prune, and per orphan hooks file
// (one whose generated sibling the plan no longer produces; agent-owned, so
// reported but never pruned), then exits 1.
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
	check                          bool
}

func run(args []string, out, errOut io.Writer) error {
	var o options
	fl := flag.NewFlagSet("testgen", flag.ContinueOnError)
	fl.SetOutput(errOut)
	fl.StringVar(&o.project, "project", "../.aiarch/state/project.json", "the project.json to read")
	fl.StringVar(&o.root, "root", ".", "the Go module directory output paths resolve against")
	fl.StringVar(&o.module, "module", "", "the Go module path of -root (default: from go.mod)")
	fl.StringVar(&o.uitests, "uitests", "../uitests/generated", "the Playwright output directory, relative to -root")
	fl.BoolVar(&o.check, "check", false, "report drift and exit 1 instead of writing")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if o.module == "" {
		m, err := modulePath(filepath.Join(o.root, "go.mod"))
		if err != nil {
			return err
		}
		o.module = m
	}
	raw, err := os.ReadFile(o.project)
	if err != nil {
		return fmt.Errorf("testgen: %w", err)
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
	if o.check {
		return check(o, gen, out)
	}
	return write(o, gen, out)
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
// leave: Drift over the generated and hooks files, the generated files a
// write run would prune (spec §5.3: any diff in generated files fails), and
// the hooks files whose generated sibling the plan no longer produces — a
// write run leaves those alone (agent-owned), but a Go one still references
// the Input_ types its pruned sibling declared, so the package no longer
// compiles and the break must be attributable.
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
	orphanHooks, err := orphanHooksFiles(o, gen)
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

// write rewrites every generated file, writes absent hooks files, and prunes
// generated files the plan no longer produces.
func write(o options, gen testgen.Output, out io.Writer) error {
	for p, b := range gen.Generated {
		if err := writeFile(o.root, p, b); err != nil {
			return err
		}
	}
	if err := writeHooks(o.root, gen.HooksOnce, out); err != nil {
		return err
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

// writeHooks writes each hooks file that is absent; an existing one is the
// agent's and is left alone.
func writeHooks(root string, hooks map[string][]byte, out io.Writer) error {
	for p, b := range hooks {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
			continue
		}
		if err := writeFile(root, p, b); err != nil {
			return err
		}
		if err := say(out, "wrote hooks "+p); err != nil {
			return err
		}
	}
	return nil
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

// orphanHooksFiles lists, root-relative with slashes, the hooks files the plan
// no longer pairs a generated file with: Go hooks files anywhere under root,
// hooks.ts under the uitests directory. They are reported by -check and
// never pruned: the agent owns them.
func orphanHooksFiles(o options, gen testgen.Output) ([]string, error) {
	keep := func(rel string) bool { _, ok := gen.HooksOnce[filepath.ToSlash(rel)]; return ok }
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
		if err := os.Remove(filepath.Join(o.root, filepath.FromSlash(p))); err != nil {
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

func isGoScenarioFile(name string) bool { return strings.HasSuffix(name, "_scenarios.gen_test.go") }

func isGoHooksFile(name string) bool { return strings.HasSuffix(name, "_hooks_test.go") }

func isPlaywrightHooksFile(name string) bool { return name == "hooks.ts" }

func isPlaywrightGenFile(name string) bool {
	return strings.HasSuffix(name, ".spec.gen.ts") || name == "playwright.config.gen.ts" || name == "reporter.gen.ts"
}
