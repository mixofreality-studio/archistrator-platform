//go:build conformance

package conformance

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/plan"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

// toolWritten are the files the chain's non-generator steps rewrite (go mod tidy and the module
// edit the scaffold applies, npm install, and `npm run gen:api`, which is openapi-typescript's
// output and deliberately not a webgen consumer's: webgen/doc.go): they differ from the scaffold's
// seed without being the output of any generator, so they are outside the manifests' claim.
var toolWritten = []string{"go.mod", "go.sum", webDir + "/package-lock.json", webDir + "/src/contracts/schema.ts"}

// toolDirs are directory names whose whole subtree is build output, never generator output.
var toolDirs = []string{"node_modules", "dist", "test-results"}

// changedPaths is every file of the scaffold at dir whose bytes the chain's generator steps wrote:
// the paths absent from seed or holding other bytes than seed's, less the tool-written files and
// the build-output directories. The impl overlay is not applied by RunChain unless asked, and this
// test does not ask.
func changedPaths(t *testing.T, dir string, seed map[string][]byte) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if slices.Contains(toolDirs, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if slices.Contains(toolWritten, rel) {
			return nil
		}
		body, err := os.ReadFile(p) // #nosec G304 -- inside the scaffold the chain just wrote
		if err != nil {
			return err
		}
		if was, ok := seed[rel]; ok && bytes.Equal(was, body) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(out)
	return out
}

// plannedPaths is the union of every consumer's Plan over the fixture (Input{Tree: nil}): path to
// the role Plan gives it and the consumers that plan it.
func plannedPaths(t *testing.T, raw []byte, modulePath string) map[string]plannedPath {
	t.Helper()
	in := appgen.Input{Project: raw, ModulePath: modulePath, ModuleDir: "", Config: appgen.DefaultConfig()}
	out := map[string]plannedPath{}
	for _, c := range appgen.AllConsumers() {
		outs, err := c.Plan(in)
		if err != nil {
			t.Fatalf("%s Plan: %v", c.Name, err)
		}
		for _, o := range outs {
			pp := out[o.Path]
			pp.role = o.Role
			pp.consumers = append(pp.consumers, c.Name)
			out[o.Path] = pp
		}
	}
	return out
}

type plannedPath struct {
	role      plan.OutputRole
	consumers []string
}

// manifestFindings compares what the chain wrote with what the manifests plan. unplanned is every
// written path no Plan names; missing is every planned Generated path the chain left absent.
func manifestFindings(written []string, planned map[string]plannedPath, exists func(string) bool) (unplanned, missing []string) {
	for _, p := range written {
		if _, ok := planned[p]; !ok {
			unplanned = append(unplanned, p)
		}
	}
	for p, pp := range planned {
		if !plan.IsSeam(pp.role) && !exists(p) {
			missing = append(missing, p+" ("+strings.Join(pp.consumers, ", ")+")")
		}
	}
	sort.Strings(missing)
	return unplanned, missing
}

// TestConformance_Manifests runs the chain over todomvc (RunChain into t.TempDir()) and walks the scaffold:
// every file whose bytes the chain's generator steps wrote (the set of paths that differ from the scaffold's
// own seed, excluding the impl overlay, node_modules, dist and test-results) is a planned path of some
// appgen.AllConsumers() Plan over the fixture (Input{Tree: nil}), with the role Plan gives it; and every
// planned Generated path exists in the scaffold after the chain.
func TestConformance_Manifests(t *testing.T) {
	const fixture = "todomvc"
	raw, err := Fixture(fixture)
	if err != nil {
		t.Fatal(err)
	}
	modulePath := "example.com/conformance/" + fixture
	seed, err := methodassets.ScaffoldFiles(methodassets.ScaffoldData{
		ModulePath: modulePath, AppSlug: "aiarch-app",
		ProjectID: fixture, Owner: "conformance", Name: fixture,
		StateMcpModulePath:    "example.com/conformance/aiarch-state-mcp",
		StateMcpModuleVersion: "v0.0.0-conformance",
	})
	if err != nil {
		t.Fatal(err)
	}
	seed[statePath] = raw

	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	if _, err := RunChain(ctx, fixture, dir); err != nil {
		t.Fatalf("RunChain: %v", err)
	}

	written := changedPaths(t, dir, seed)
	planned := plannedPaths(t, raw, modulePath)
	exists := func(p string) bool {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p)))
		return err == nil
	}
	unplanned, missing := manifestFindings(written, planned, exists)

	if len(written) == 0 || len(planned) == 0 {
		t.Fatalf("the proof is vacuous: the chain wrote %d paths, the manifests plan %d", len(written), len(planned))
	}
	for _, p := range unplanned {
		t.Errorf("the chain wrote %s, which no consumer's Plan names: edit scope and drift cannot see it", p)
	}
	for _, p := range missing {
		t.Errorf("planned Generated path %s was not written by the chain", p)
	}
}

// The comparison reports a written path no Plan names and a planned Generated path nobody wrote,
// and lets a Seam stay absent: the proof above is not satisfied by an empty comparison.
func TestManifestFindings_NamesWhatIsUnplannedAndWhatIsMissing(t *testing.T) {
	planned := map[string]plannedPath{
		"a/kept.go":  {role: plan.Generated{}, consumers: []string{"modelgen"}},
		"a/gone.go":  {role: plan.Generated{}, consumers: []string{"nullgen", "testgen"}},
		"a/seat.go":  {role: plan.Seam{}, consumers: []string{"appgen compose"}},
		"a/seen.go":  {role: plan.Seam{}, consumers: []string{"webgen bootstrap"}},
		"a/other.go": {role: plan.Generated{}, consumers: []string{"testgen"}},
	}
	onDisk := map[string]bool{"a/kept.go": true, "a/seen.go": true, "a/other.go": true}
	unplanned, missing := manifestFindings([]string{"a/kept.go", "a/stray.ts", "a/seen.go"}, planned, func(p string) bool { return onDisk[p] })
	if !slices.Equal(unplanned, []string{"a/stray.ts"}) {
		t.Errorf("unplanned = %v, want [a/stray.ts]", unplanned)
	}
	if !slices.Equal(missing, []string{"a/gone.go (nullgen, testgen)"}) {
		t.Errorf("missing = %v, want [a/gone.go (nullgen, testgen)]", missing)
	}
}
