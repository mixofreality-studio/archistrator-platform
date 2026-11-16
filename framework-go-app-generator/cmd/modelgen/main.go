// Command modelgen emits each component's contract.gen.go and fake/fake.gen.go from the
// service contracts in project.json (package modelgen). Run it from the app's Go module
// with `go tool modelgen`:
//
//	go tool modelgen -project .aiarch/state/project.json -root . -contract todoAccess
//	go tool modelgen -project .aiarch/state/project.json -root . -check
//
// Flags:
//
//	-project   the project.json to read
//	-root      the Go module the goPackage directories resolve against (default .)
//	-module    the Go module path of -root (default: the module line of <root>/go.mod)
//	-contract  a contract key to generate, repeatable: a construction task names its own
//	-check     report drift and exit 1 instead of writing
//
// With -contract, exactly those contracts' packages are generated, created if absent. A
// sibling's contract is left alone: its delegating constructor needs a builder that does
// not exist until the sibling is built, and would break the build it landed in. Without
// -contract, every contract whose package holds code (or is flagged a stub) is generated.
//
// -check holds the same set: a contracted package whose contract.gen.go is missing or stale
// is drift, and so is a -contract whose package has no generated file at all, so a component
// cannot skip generation to stay out of the surface gate.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/cli"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// errDrift is the -check verdict when anything drifted.
var errDrift = errors.New("modelgen: generated models are out of date; run `make gen-models`")

// repeated is a repeatable string flag.
type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ",") }
func (r *repeated) Set(v string) error { *r = append(*r, v); return nil }

type options struct {
	project, root, module string
	contracts             repeated
	check                 bool
}

func run(args []string, errOut io.Writer) error {
	var o options
	fl := flag.NewFlagSet("modelgen", flag.ContinueOnError)
	fl.SetOutput(errOut)
	fl.StringVar(&o.project, "project", ".aiarch/state/project.json", "the project.json to read")
	fl.StringVar(&o.root, "root", ".", "the Go module the goPackage directories resolve against")
	fl.StringVar(&o.module, "module", "", "the Go module path of -root (default: read from its go.mod)")
	fl.Var(&o.contracts, "contract", "a contract key to generate (repeatable)")
	fl.BoolVar(&o.check, "check", false, "report drift and exit 1 instead of writing")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if o.module == "" {
		module, err := arch.ModulePathOf(o.root)
		if err != nil {
			return err
		}
		o.module = module
	}
	raw, err := cli.ReadAdmitted(o.project) // the one loader: every problem, before any emission
	if err != nil {
		return err
	}
	consumer, _ := appgen.ConsumerNamed("modelgen")
	emitted, err := consumer.Emit(appgen.Input{Project: raw, ModulePath: o.module, Tree: os.DirFS(o.root), Only: o.contracts})
	if err != nil {
		return err
	}
	if len(emitted) == 0 {
		return nil // nothing built yet: nothing to generate and nothing to drift from
	}
	files := make(map[string][]byte, len(emitted))
	for _, f := range emitted {
		files[f.Path] = f.Content
	}
	if o.check {
		return checkFiles(o.root, files, errOut)
	}
	return writeFiles(o.root, files)
}

func sortedPaths(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for p := range files {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func writeFiles(root string, files map[string][]byte) error {
	for _, p := range sortedPaths(files) {
		dest := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil { // #nosec G301 G703 -- derived from the repo's own project.json
			return err
		}
		if err := os.WriteFile(dest, files[p], 0o600); err != nil { // #nosec G306 G703 -- derived from the repo's own project.json
			return err
		}
	}
	return nil
}

func checkFiles(root string, files map[string][]byte, out io.Writer) error {
	drifted := false
	for _, p := range sortedPaths(files) {
		have, err := os.ReadFile(filepath.Join(root, p)) // #nosec G304 G703 -- derived from the repo's own project.json
		switch {
		case errors.Is(err, os.ErrNotExist):
			drifted = true
			_, _ = fmt.Fprintf(out, "missing: %s\n", p)
		case err != nil:
			return err
		case !bytes.Equal(have, files[p]):
			drifted = true
			_, _ = fmt.Fprintf(out, "stale: %s\n", p)
		}
	}
	if drifted {
		return errDrift
	}
	return nil
}
