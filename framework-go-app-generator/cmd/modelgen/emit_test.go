package main

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
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
	if err := run([]string{"-project", project, "-root", root}, io.Discard); err != nil {
		t.Fatal(err)
	}
	var wantPaths, gotPaths []string
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
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".gen.go") {
			rel, _ := filepath.Rel(root, p)
			gotPaths = append(gotPaths, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(wantPaths)
	slices.Sort(gotPaths)
	if !slices.Equal(wantPaths, gotPaths) {
		t.Errorf("the CLI wrote %v, its consumer emits %v", gotPaths, wantPaths)
	}
}
