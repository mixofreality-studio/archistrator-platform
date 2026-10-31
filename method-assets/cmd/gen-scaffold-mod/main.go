// Command gen-scaffold-mod writes assets/scaffold/go.mod.tmpl and go.sum.tmpl: the tidied
// form of the module every project is born with, from the pins in scaffold.go. Run it from
// method-assets at release, once the pinned platform tags resolve:
//
//	go run ./cmd/gen-scaffold-mod
//
// It renders the real scaffold's gate test into a throwaway module, starts go.mod from the
// pins and the tool list, and lets `go mod tidy` decide everything else. Nothing here
// enumerates indirect requires, so the result cannot be subtly untidy.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

const probeModule = "example.com/aiarch-scaffold-probe"

func main() {
	if err := run("assets/scaffold"); err != nil {
		fmt.Fprintln(os.Stderr, "gen-scaffold-mod:", err)
		os.Exit(1)
	}
}

func run(dest string) error {
	files, err := methodassets.ScaffoldFiles(methodassets.ScaffoldData{ModulePath: probeModule})
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "gen-scaffold-mod")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	gate := filepath.Join(dir, methodassets.ArchGatePath)
	if err := os.MkdirAll(filepath.Dir(gate), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(gate, files[methodassets.ArchGatePath], 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(startingMod()), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod tidy: %w\n%s", err, out)
	}
	mod, err := os.ReadFile(filepath.Join(dir, "go.mod")) // #nosec G304 -- our own temp dir
	if err != nil {
		return err
	}
	if !strings.Contains(string(mod), "module "+probeModule+"\n") {
		return fmt.Errorf("tidied go.mod lost its module line:\n%s", mod)
	}
	tmpl := strings.Replace(string(mod), "module "+probeModule+"\n", "module [[.ModulePath]]\n", 1)
	if err := os.WriteFile(filepath.Join(dest, "go.mod.tmpl"), []byte(tmpl), 0o600); err != nil {
		return err
	}
	sum, err := os.ReadFile(filepath.Join(dir, "go.sum")) // #nosec G304 -- our own temp dir
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dest, "go.sum.tmpl"), sum, 0o600)
}

// startingMod is what tidy starts from: the module, the pins and the tools.
func startingMod() string {
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\ngo %s\n\nrequire (\n", probeModule, methodassets.GoVersion)
	reqs := methodassets.ScaffoldRequires()
	paths := make([]string, 0, len(reqs))
	for p := range reqs {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, p := range paths {
		fmt.Fprintf(&b, "\t%s %s\n", p, reqs[p])
	}
	b.WriteString(")\n\ntool (\n")
	for _, t := range methodassets.ScaffoldTools() {
		fmt.Fprintf(&b, "\t%s\n", t)
	}
	b.WriteString(")\n")
	return b.String()
}
