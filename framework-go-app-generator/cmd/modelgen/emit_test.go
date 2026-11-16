package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/filesnap"
)

// The CLI and the manifest are one source: modelgen writes exactly the files its consumer emits.
func TestCLIWritesItsConsumersEmit(t *testing.T) {
	root, project := fixture(t)
	put(t, filepath.Join(root, "internal/resourceaccess/todo/impl.go"), "package todo\n")
	put(t, filepath.Join(root, "internal/engine/sibling/impl.go"), "package sibling\n")
	raw, err := os.ReadFile(project)
	if err != nil {
		t.Fatal(err)
	}
	consumer, _ := appgen.ConsumerNamed("modelgen")
	want, err := consumer.Emit(appgen.Input{Project: raw, ModulePath: "example.com/fresh", Tree: os.DirFS(root)})
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("modelgen emits nothing for the fixture: the proof is vacuous")
	}
	before := filesnap.Take(t, root)
	if err := run([]string{"-project", project, "-root", root}, io.Discard); err != nil {
		t.Fatal(err)
	}
	var wantPaths []string
	for _, f := range want {
		wantPaths = append(wantPaths, f.Path)
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Errorf("%s was not written: %v", f.Path, err)
			continue
		}
		if !bytes.Equal(got, f.Content) {
			t.Errorf("%s differs from the consumer's emission", f.Path)
		}
	}
	slices.Sort(wantPaths)
	if gotPaths := before.ChangedSince(filesnap.Take(t, root)); !slices.Equal(wantPaths, gotPaths) {
		t.Errorf("the CLI wrote %v, its consumer emits %v", gotPaths, wantPaths)
	}
}
