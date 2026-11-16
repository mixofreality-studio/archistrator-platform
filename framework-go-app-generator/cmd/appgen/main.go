// Command appgen generates an app's composition layer from project.json, reading the
// module path from go.mod. Run it from the app's Go module with `go tool appgen`:
//
//	go tool appgen temporal [-check]   per-Manager activities, invokers and worker
//	go tool appgen config   [-check]   cmd/server/config.gen.go
//	go tool appgen compose  [-check]   cmd/server/main.gen.go, the generated authorization table
//	                                   and its policy test, the browser surfaces' static handler,
//	                                   hooks.go if absent, cmd/devstack/main.gen.go, the
//	                                   app's local run contract (go run ./cmd/devstack), and
//	                                   internal/scenariocompose/compose.gen.go, the components
//	                                   the generated scenario tests drive
//	go tool appgen clients  [-check]   internal/client/** and api/openapi.yaml
//
// A contract is wired only once its package holds code, so the generated composition
// root always builds at whatever point construction has reached. The knobs an app may
// need live in a birth-seated appgen.json; a fresh app's is empty and every knob has a
// default derived from the contracts and the design.
//
// Each subcommand writes exactly what its consumer in package appgen emits (appgen.Consumer):
// the same emission is the manifest the platform plans edit scope and drift from. A Generated
// output is rewritten on every run and compared by -check; a Seam (hooks.go) is written once,
// when absent, and is the app's after.
//
// Flags: -project (default .aiarch/state/project.json), -root (default .), -config (default
// <root>/appgen.json, optional).
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/plan"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

// errDrift is the -check verdict when anything drifted.
var errDrift = errors.New("appgen: generated files are out of date; run appgen")

type options struct {
	project, root, configPath string
	check                     bool
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: appgen temporal|config|compose|clients [-check]")
	}
	sub, args := args[0], args[1:]
	var o options
	fl := flag.NewFlagSet("appgen "+sub, flag.ContinueOnError)
	fl.SetOutput(out)
	fl.StringVar(&o.project, "project", ".aiarch/state/project.json", "the project.json to read")
	fl.StringVar(&o.root, "root", ".", "the Go module to generate into")
	fl.StringVar(&o.configPath, "config", "", "the knobs file (default <root>/appgen.json)")
	fl.BoolVar(&o.check, "check", false, "report drift and exit 1 instead of writing")
	if err := fl.Parse(args); err != nil {
		return err
	}
	consumer, ok := appgen.ConsumerNamed("appgen " + sub)
	if !ok {
		return fmt.Errorf("appgen: unknown subcommand %q", sub)
	}
	module, err := arch.ModulePathOf(o.root)
	if err != nil {
		return err
	}
	cfg, err := readConfig(o)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(o.project) // #nosec G304 G703 -- the CLI's own argument; no trust boundary
	if err != nil {
		return err
	}
	files, err := consumer.Emit(appgen.Input{Project: raw, ModulePath: module, Config: cfg, Tree: os.DirFS(o.root), Warn: out})
	if err != nil {
		return err
	}
	return emit(o, files, out)
}

func readConfig(o options) (appgen.Config, error) {
	path := o.configPath
	if path == "" {
		path = filepath.Join(o.root, "appgen.json")
	}
	raw, err := os.ReadFile(path) // #nosec G304 G703 -- the CLI's own argument; no trust boundary
	if errors.Is(err, os.ErrNotExist) {
		return appgen.DefaultConfig(), nil
	}
	if err != nil {
		return appgen.DefaultConfig(), err
	}
	cfg, err := appgen.ParseConfig(raw)
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// emit writes the consumer's files, or with -check compares its Generated ones: a Seam is the
// app's own once written, so it is neither compared nor required.
func emit(o options, files []plan.File, out io.Writer) error {
	if o.check {
		return checkDrift(o.root, files, out)
	}
	for _, f := range files {
		if err := writeOne(o.root, f); err != nil {
			return err
		}
	}
	return nil
}

// writeOne writes a Generated file always, and a Seam only when it is absent.
func writeOne(root string, f plan.File) error {
	dest := filepath.Join(root, filepath.FromSlash(f.Path))
	if plan.RoleName(f.Role) == "seam" {
		owned, err := exists(dest)
		if err != nil || owned {
			return err // the app's own now
		}
	}
	return writeGenerated(dest, f.Content)
}

// exists reports whether a file is on disk.
func exists(p string) (bool, error) {
	_, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// checkDrift reports every generated file that is missing or differs from what is on disk.
func checkDrift(root string, files []plan.File, out io.Writer) error {
	drifted := false
	for _, f := range files {
		if plan.RoleName(f.Role) != "generated" {
			continue
		}
		have, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path))) // #nosec G304 G703 -- derived from the repo's own project.json
		if err != nil || !bytes.Equal(have, f.Content) {
			drifted = true
			_, _ = fmt.Fprintf(out, "drift: %s\n", f.Path)
		}
	}
	if drifted {
		return errDrift
	}
	return nil
}

func writeGenerated(dest string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil { // #nosec G301 G703 -- derived from the repo's own project.json
		return err
	}
	return os.WriteFile(dest, content, 0o600) // #nosec G306 G703 -- derived from the repo's own project.json
}
