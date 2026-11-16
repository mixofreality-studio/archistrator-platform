package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/filesnap"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/plan"
)

// The CLI and the manifest are one source: every subcommand writes exactly the files, with
// exactly the bytes, its consumer in package appgen emits, and no other.
func TestCLIsWriteTheirConsumersEmit(t *testing.T) {
	for _, sub := range []string{"temporal", "config", "compose", "clients"} {
		t.Run(sub, func(t *testing.T) {
			root, base := appFixture(t, "internal/resourceaccess/todo", "internal/resourceaccess/caller", "internal/manager/list")
			doc := projectDoc
			if sub == "clients" { // the machine client the clients generator fronts the Manager with
				doc = strings.TrimSuffix(strings.TrimSpace(projectDoc), "}") + `,"slots":{"5":{"kind":5,"model":{
"components":[{"id":"api","name":"MachineApi","kind":"client","surface": "mcpTools"},{"id":"list-manager","name":"ListManager","kind":"manager","contractKey":"listManager"}],
"relationships":[{"from":"api","to":"list-manager"}]}}}}`
			}
			if err := os.WriteFile(filepath.Join(root, ".aiarch/state/project.json"), []byte(doc), 0o600); err != nil {
				t.Fatal(err)
			}
			consumer, ok := appgen.ConsumerNamed("appgen " + sub)
			if !ok {
				t.Fatalf("no consumer appgen %s", sub)
			}
			want, err := consumer.Emit(appgen.Input{Project: []byte(doc), ModulePath: "example.com/fresh", Config: appgen.DefaultConfig(), Tree: os.DirFS(root)})
			if err != nil {
				t.Fatal(err)
			}
			if len(want) == 0 {
				t.Fatalf("appgen %s emits nothing for the fixture: the proof is vacuous", sub)
			}
			before := filesnap.Take(t, root)
			if err := run(append([]string{sub}, base...), io.Discard); err != nil {
				t.Fatal(err)
			}
			var wantPaths []string
			for _, f := range want {
				wantPaths = append(wantPaths, f.Path)
			}
			slices.Sort(wantPaths)
			if got := before.ChangedSince(filesnap.Take(t, root)); !slices.Equal(got, wantPaths) {
				t.Errorf("the CLI wrote %v, its consumer emits %v", got, wantPaths)
			}
			for _, f := range want {
				got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
				if err != nil {
					t.Errorf("%s (%s) was not written: %v", f.Path, plan.RoleName(f.Role), err)
					continue
				}
				if !bytes.Equal(got, f.Content) {
					t.Errorf("%s differs from the consumer's emission", f.Path)
				}
			}
			if err := run(append([]string{sub, "-check"}, base...), io.Discard); err != nil {
				t.Errorf("-check after a write: %v", err)
			}
		})
	}
}

// -check holds only the Generated outputs: a Seam is the app's own after the first write.
func TestCheckIgnoresSeams(t *testing.T) {
	root, base := appFixture(t, "internal/resourceaccess/todo", "internal/manager/list")
	if err := run(append([]string{"compose"}, base...), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd/server/hooks.go"), []byte("package main\n// the app's\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(append([]string{"compose", "-check"}, base...), io.Discard); err != nil {
		t.Fatalf("an edited seam is not drift: %v", err)
	}
}
