// Package modsandbox writes the go.mod and go.sum of a throwaway module that compiles generated code against
// the local checkout of the platform modules, offline (GOPROXY=off). Tests that type-check emitted code
// share it so none of them hand-writes a go.mod that only builds on a warm module cache.
//
// The throwaway go.mod must be tidy the way a platform module's own go.mod is. Otherwise the go tool cannot
// resolve the build from go.mod alone, loads the complete module graph, and that walk reaches go.mod files of
// old transitive versions (errcheck v1.5.0 behind gogo/protobuf, go-spew v1.1.1 behind containerd/platforms,
// go-ansiterm behind testcontainers) that no platform module's own build ever fetches. GOPROXY=off then fails
// on a cold module cache (CI) while a developer's warm cache hides it. Tidy here means:
//
//   - a go directive at least as new as every dependency's (go 1.25 is older than go 1.25.0);
//   - every module the build touches, platform siblings included, listed at the version the platform
//     modules' own requirements select. The siblings are replaced by the local checkout, so their version
//     only has to agree with what the graph selects.
package modsandbox

import (
	"errors"
	"fmt"
	"go/version"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// PlatformBase is the module-path prefix of every platform module.
const PlatformBase = "github.com/mixofreality-studio/archistrator-platform/"

// ScenariohostModule is the platform directory (and module suffix) of the scenario host.
const ScenariohostModule = "framework-go-scenariohost"

// Env is the environment the go tool runs in over a sandbox: no workspace, go.mod may be completed, no network.
func Env() []string {
	return append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")
}

// Closure returns mod and every platform sibling it transitively requires (read from each go.mod in the local
// checkout), so a build against the real host needs neither the module cache nor the network.
func Closure(platform, mod string) ([]string, error) {
	seen := map[string]bool{}
	queue := []string{mod}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		f, err := parse(platform, cur)
		if err != nil {
			return nil, err
		}
		for _, r := range f.Require {
			if dep, ok := strings.CutPrefix(r.Mod.Path, PlatformBase); ok {
				queue = append(queue, dep)
			}
		}
	}
	return slices.Sorted(maps.Keys(seen)), nil
}

// RealHost returns the go.mod and go.sum of a sandbox module named module that builds against the real
// scenario host and the whole local closure beneath it. extra names further external modules (path to
// version) the sandbox's own files import.
func RealHost(platform, module string, extra map[string]string) (goMod string, goSum []byte, err error) {
	closure, err := Closure(platform, ScenariohostModule)
	if err != nil {
		return "", nil, err
	}
	replaces := map[string]string{PlatformBase + "framework-go": filepath.Join(platform, "framework-go")}
	sumFrom := []string{"framework-go-app-generator", "framework-go-mcp-generator"}
	for _, dep := range closure {
		replaces[PlatformBase+dep] = filepath.Join(platform, dep)
		sumFrom = append(sumFrom, dep)
	}
	return Render(platform, module, replaces, sumFrom, extra)
}

// Render returns the go.mod and go.sum of a sandbox module named module. replaces maps a module path to the
// directory that replaces it; platform modules listed there are required too. sumFrom are the platform
// modules whose requirements and go.sum feed the sandbox. extra adds external modules (path to version).
func Render(platform, module string, replaces map[string]string, sumFrom []string, extra map[string]string) (goMod string, goSum []byte, err error) {
	goVersion := "1.25.0"
	best := map[string]string{}
	for mod := range replaces {
		best[mod] = "v0.0.0"
	}
	maps.Copy(best, extra)
	for _, mod := range sumFrom {
		f, err := parse(platform, mod)
		if err != nil {
			return "", nil, err
		}
		if f.Go != nil && version.Compare("go"+f.Go.Version, "go"+goVersion) > 0 {
			goVersion = f.Go.Version
		}
		raise(best, f.Require, replaces)
		sums, err := readSum(platform, mod)
		if err != nil {
			return "", nil, err
		}
		goSum = append(goSum, sums...)
	}
	return render(module, goVersion, best, replaces), goSum, nil
}

func render(module, goVersion string, requires, replaces map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\ngo %s\n\nrequire (\n", module, goVersion)
	for _, path := range slices.Sorted(maps.Keys(requires)) {
		fmt.Fprintf(&b, "\t%s %s\n", path, requires[path])
	}
	b.WriteString(")\n\n")
	for _, path := range slices.Sorted(maps.Keys(replaces)) {
		fmt.Fprintf(&b, "replace %s => %s\n", path, replaces[path])
	}
	return b.String()
}

// raise lifts best to the highest version of each requirement, skipping platform modules that are not replaced.
func raise(best map[string]string, reqs []*modfile.Require, replaces map[string]string) {
	for _, r := range reqs {
		if _, replaced := replaces[r.Mod.Path]; strings.HasPrefix(r.Mod.Path, PlatformBase) && !replaced {
			continue
		}
		if cur, ok := best[r.Mod.Path]; !ok || semver.Compare(r.Mod.Version, cur) > 0 {
			best[r.Mod.Path] = r.Mod.Version
		}
	}
}

// readSum returns a platform module's go.sum; a module with no dependencies of its own has none.
func readSum(platform, mod string) ([]byte, error) {
	raw, err := fs.ReadFile(os.DirFS(platform), mod+"/go.sum")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return raw, err
}

func parse(platform, mod string) (*modfile.File, error) {
	raw, err := fs.ReadFile(os.DirFS(platform), mod+"/go.mod")
	if err != nil {
		return nil, err
	}
	return modfile.ParseLax(filepath.Join(platform, mod, "go.mod"), raw, nil)
}
