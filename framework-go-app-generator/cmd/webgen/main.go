// Command webgen generates a webApp's transport seam and preview-mode files from
// the server's OpenAPI document and MCP tool tables (package webgen). Run it from
// the webApp directory, or pass -C:
//
//	webgen init -app <name> [-surface web-client]   write webgen.json if absent
//	webgen generate                                 rewrite the generated files
//	webgen check                                    exit 1 when a generated file drifted
//	webgen scaffold                                 write the scaffold files that are absent
//	webgen bootstrap -surface <id> -kind web|mcpApp write a fresh web project and surface; keeps what exists
//	webgen skeleton -route /x <opId>...             print a minimal fixture for a screen state
//
// Run it from a Go module that requires framework-go-app-generator (the app's
// server module, whose appgen already does): `go run <pkg>@<version>` is refused
// for this module, because its go.mod carries replace directives for its
// sibling platform modules. The webApp's package.json then carries the preview
// script contract:
//
//	"gen:ops":           "cd ../server && go run github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/cmd/webgen generate -C ../webApp",
//	"build:preview":     "vite build -c vite.preview.config.ts && archistrator-preview-check-bundle dist-preview --preview",
//	"check:prod-bundle": "archistrator-preview-check-bundle dist"
//
// with check:prod-bundle run at the end of the production build.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/plan"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/webgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type command func(fs *flag.FlagSet, args []string, out io.Writer) error

var commands = map[string]command{
	"init":      cmdInit,
	"generate":  cmdGenerate,
	"check":     cmdCheck,
	"scaffold":  cmdScaffold,
	"bootstrap": cmdBootstrap,
	"skeleton":  cmdSkeleton,
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: webgen init|generate|check|scaffold|bootstrap|skeleton [-C dir] [flags]")
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("webgen: unknown command %q", args[0])
	}
	fs := flag.NewFlagSet("webgen "+args[0], flag.ContinueOnError)
	fs.String("C", ".", "the webApp directory (holding webgen.json)")
	return cmd(fs, args[1:], out)
}

func cmdInit(fs *flag.FlagSet, args []string, out io.Writer) error {
	app := fs.String("app", "", "the app name")
	surface := fs.String("surface", "web-client", "the slot-5 UI surface (client id)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := dirOf(fs)
	content, err := webgen.DefaultConfig(*app, *surface)
	if err != nil {
		return err
	}
	wrote, err := webgen.WriteScaffold(dir, []webgen.File{{Path: webgen.ConfigFile, Content: content}})
	return report(out, "wrote", wrote, err)
}

func cmdGenerate(fs *flag.FlagSet, args []string, out io.Writer) error {
	dir, files, err := generated(fs, args)
	if err != nil {
		return err
	}
	if err := webgen.Write(dir, files); err != nil {
		return err
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return report(out, "generated", paths, nil)
}

func cmdCheck(fs *flag.FlagSet, args []string, out io.Writer) error {
	dir, files, err := generated(fs, args)
	if err != nil {
		return err
	}
	stale, err := webgen.Drift(dir, files)
	if err != nil {
		return err
	}
	if len(stale) > 0 {
		return fmt.Errorf("webgen: stale generated files (run webgen generate): %v", stale)
	}
	_, err = fmt.Fprintln(out, "webgen: generated files are current")
	return err
}

func cmdScaffold(fs *flag.FlagSet, args []string, out io.Writer) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := dirOf(fs)
	c, err := webgen.LoadConfig(filepath.Join(dir, webgen.ConfigFile))
	if err != nil {
		return err
	}
	files, err := webgen.Scaffold(c)
	if err != nil {
		return err
	}
	wrote, err := webgen.WriteScaffold(dir, files)
	return report(out, "scaffolded", wrote, err)
}

func cmdBootstrap(fs *flag.FlagSet, args []string, out io.Writer) error {
	surface := fs.String("surface", "", "the browser surface's id")
	kind := fs.String("kind", "", "the surface's kind: web or mcpApp (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	k, err := projectmodel.ParseSurface(*kind)
	if err != nil {
		return fmt.Errorf("webgen bootstrap: -kind: %w", err)
	}
	module, err := arch.ModulePathOf(".") // go tool runs in the app's module
	if err != nil {
		return err
	}
	files, err := appgen.BootstrapSurface(webgen.AppSlug(module), *surface, k.Surface())
	if err != nil {
		return err
	}
	wrote, err := writeBootstrap(dirOf(fs), files)
	return report(out, "bootstrapped", wrote, err)
}

// writeBootstrap writes a surface's bootstrap under dir: a Seam only when absent (it is the app's
// afterwards), a Generated file whenever it differs from what is there. It returns what it wrote.
func writeBootstrap(dir string, files []appgen.WebFile) ([]string, error) {
	var wrote []string
	for _, f := range files {
		dest := filepath.Join(dir, f.Path)
		have, err := os.ReadFile(dest) // #nosec G304 G703 -- a seat the generator itself owns
		exists := err == nil
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return wrote, err
		}
		if exists && (plan.RoleName(f.Role) == "seam" || bytes.Equal(have, f.Content)) {
			continue
		}
		if err := webgen.Write(dir, []webgen.File{f.File}); err != nil {
			return wrote, err
		}
		wrote = append(wrote, f.Path)
	}
	return wrote, nil
}

func cmdSkeleton(fs *flag.FlagSet, args []string, out io.Writer) error {
	route := fs.String("route", "", "the screen state's concrete route")
	if err := fs.Parse(args); err != nil {
		return err
	}
	in, err := inputs(dirOf(fs))
	if err != nil {
		return err
	}
	bindings, err := in.Bindings()
	if err != nil {
		return err
	}
	skel, err := webgen.Skeleton(webgen.FixtureSchema(in, bindings), *route, fs.Args())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, webgen.Stringify(skel))
	return err
}

func generated(fs *flag.FlagSet, args []string) (string, []webgen.File, error) {
	if err := fs.Parse(args); err != nil {
		return "", nil, err
	}
	dir := dirOf(fs)
	in, err := inputs(dir)
	if err != nil {
		return "", nil, err
	}
	files, err := webgen.Generate(in)
	return dir, files, err
}

func inputs(dir string) (webgen.Inputs, error) {
	c, err := webgen.LoadConfig(filepath.Join(dir, webgen.ConfigFile))
	if err != nil {
		return webgen.Inputs{}, err
	}
	return webgen.LoadInputs(dir, c)
}

// report prints one "<verb> <path>" line per path, then returns err.
func report(out io.Writer, verb string, paths []string, err error) error {
	for _, p := range paths {
		if _, werr := fmt.Fprintln(out, verb, p); werr != nil {
			return werr
		}
	}
	return err
}

// dirOf is the -C flag's value, read after the command parsed its flags.
func dirOf(fs *flag.FlagSet) string { return fs.Lookup("C").Value.String() }
