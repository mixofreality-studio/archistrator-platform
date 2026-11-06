// Command nullgen writes the null implementations a model with nothing built needs to compile
// and boot, for the conformance pipeline and fresh scaffolds. Run it from the app's Go module
// with `go tool nullgen`:
//
//	go tool nullgen -project .aiarch/state/project.json -root .
//
// For every non-client contract it writes <goPackage>/null.gen.go (a Null<Iface> and the
// builders the generated constructors delegate to), and cmd/server/hooks.go answering every seam
// of the generated composition root. It never replaces a hooks.go it did not write: that file is
// the app's own, and the command exits 2 without writing anything.
//
// Flags: -project (default .aiarch/state/project.json), -root (default .), -module (default the
// go.mod module line of -root), -container and -env-prefix (the defaults appgen takes).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/cli"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/nullgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

const (
	exitFailure = 1
	exitRefused = 2
)

func main() {
	err := run(os.Args[1:], os.Stderr)
	switch {
	case err == nil:
	case errors.Is(err, nullgen.ErrHandwrittenHooks):
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitRefused)
	default:
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitFailure)
	}
}

func run(args []string, errOut io.Writer) error {
	var project, root, module string
	var opts nullgen.Options
	fl := flag.NewFlagSet("nullgen", flag.ContinueOnError)
	fl.SetOutput(errOut)
	fl.StringVar(&project, "project", ".aiarch/state/project.json", "the project.json to read")
	fl.StringVar(&root, "root", ".", "the Go module to generate into")
	fl.StringVar(&module, "module", "", "the Go module path of -root (default: read from its go.mod)")
	fl.StringVar(&opts.Container, "container", "", "the deployment container to compose (default: the first)")
	fl.StringVar(&opts.EnvPrefix, "env-prefix", "", "the environment-variable prefix (default APP)")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if module == "" {
		var err error
		if module, err = arch.ModulePathOf(root); err != nil {
			return err
		}
	}
	opts.ModulePath = module
	raw, err := cli.ReadAdmitted(project) // the one loader: every problem, before any emission
	if err != nil {
		return err
	}
	out, err := nullgen.Emit(raw, opts)
	if err != nil {
		return err
	}
	return nullgen.Write(root, out)
}
