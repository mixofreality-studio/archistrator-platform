//go:build conformance

package conformance

import (
	"testing"

	scenariohost "github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
)

// TestMain tears the scenario host's shared stack (its Temporal dev server and Postgres
// container) down when the package's tests end: the devstack launch tests start it, and a dev
// server left running holds the test binary's output pipe open. The package records no scenario
// verdicts; the component name only satisfies Main's contract.
func TestMain(m *testing.M) { scenariohost.Main(m, "conformance") }
