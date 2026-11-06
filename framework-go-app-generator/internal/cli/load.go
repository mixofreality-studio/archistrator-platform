// Package cli is what the generator commands share. Its one job today is the loader gate:
// projectmodel.Load is the only judge of whether a project.json is loadable, so every
// command that reads one admits it here before it emits anything.
package cli

import (
	"os"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// Admit reports whether raw is a loadable project.json. The error is a [projectmodel.Problems]
// listing EVERY problem, one per line, so a command that returns it prints them all.
func Admit(raw []byte) error {
	_, err := projectmodel.Load(raw)
	return err
}

// Load reads the project.json at path and loads it. The path is the command's own -project
// argument, not request input.
func Load(path string) (*projectmodel.Model, error) {
	raw, err := os.ReadFile(path) // #nosec G304 G703 -- the CLI's own argument; no trust boundary
	if err != nil {
		return nil, err
	}
	return projectmodel.Load(raw)
}
