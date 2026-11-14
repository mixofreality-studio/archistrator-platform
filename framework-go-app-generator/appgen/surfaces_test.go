package appgen

import (
	"slices"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// The generators build web, rest and mcpTools end to end. mcpApp joins this list in the task that
// emits its resource and bridge, together with its conformance fixture.
func TestSupportedSurfaces_ExcludesMcpAppUntilGenerated(t *testing.T) {
	want := []methodcheck.Surface{methodcheck.SurfaceWeb, methodcheck.SurfaceREST, methodcheck.SurfaceMCPTools}
	if got := SupportedSurfaces(); !slices.Equal(got, want) {
		t.Fatalf("SupportedSurfaces() = %v, want %v", got, want)
	}
}
