package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
)

// The CLI and the manifest are one source: testgen writes exactly the files its consumer emits,
// the Playwright output (../uitests/generated of the module) included.
func TestCLIWritesItsConsumersEmit(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "server")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app/server\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join("..", "..", "testgen", "testdata", "project_tp.json")
	raw, err := os.ReadFile(project)
	if err != nil {
		t.Fatal(err)
	}
	consumer, _ := appgen.ConsumerNamed("testgen")
	want, err := consumer.Emit(appgen.Input{Project: raw, ModulePath: "example.com/app/server", ModuleDir: "server"})
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("testgen emits nothing for the fixture: the proof is vacuous")
	}
	if err := run([]string{"-project", project, "-root", root}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, f := range want {
		got, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Errorf("%s was not written: %v", f.Path, err)
			continue
		}
		if !bytes.Equal(got, f.Content) {
			t.Errorf("%s differs from the consumer's emission", f.Path)
		}
	}
}
