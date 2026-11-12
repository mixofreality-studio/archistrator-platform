package conformance

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/devstack"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// Booted is a running composed app.
type Booted struct {
	BaseURL string
	// Dir is the built app's module directory; Probe derives the tools /mcp must list from the
	// model it holds.
	Dir string
	// Stop terminates the app and the substrates its launcher provisioned, and waits
	// for them to be gone.
	Stop func() error
}

// Boot runs the app's own run contract, `go run ./cmd/devstack`, in dir and waits
// for it to print its ready line and for GET /readyz to answer 200 (devstack.Launch,
// the launcher the scenario host wraps). The launcher provisions its own substrates
// and removes the Postgres container with its data volume on SIGTERM.
func Boot(ctx context.Context, dir string) (Booted, error) {
	app, err := devstack.Launch(ctx, dir, nil, nil)
	if err != nil {
		return Booted{}, err
	}
	return Booted{BaseURL: app.BaseURL, Dir: dir, Stop: app.Stop}, nil
}

// DockerInventory lists the containers and volumes docker holds now, as
// "container:<name>" and "volume:<name>".
func DockerInventory(ctx context.Context) ([]string, error) { return devstack.DockerInventory(ctx) }

// DockerLeftovers is the names in after that were not in before.
func DockerLeftovers(ctx context.Context, before, after []string) []string {
	return devstack.DockerLeftovers(ctx, before, after)
}

// Probe holds the booted app to what a client sees (devstack.Smoke, the check every integration
// runs): /readyz, the SPA at / once the web project is seated, and a working MCP door at /mcp that
// lists every tool the model generates.
func Probe(ctx context.Context, b Booted) error {
	m, err := projectmodel.LoadFile(filepath.Join(b.Dir, statePath))
	if err != nil {
		return fmt.Errorf("probe: load the built app's model: %w", err)
	}
	app := &devstack.App{BaseURL: b.BaseURL, MCPURL: b.BaseURL + "/mcp", Stop: b.Stop}
	return devstack.Smoke(ctx, app, devstack.SmokeSpecFor(m, b.Dir, nil))
}
