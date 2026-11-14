package appgen

import (
	"slices"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// The generators build every surface end to end: web, rest and mcpTools, and mcpApp since its
// resource and bridge generation landed.
func TestSupportedSurfaces_IncludesMcpApp(t *testing.T) {
	want := []methodcheck.Surface{methodcheck.SurfaceWeb, methodcheck.SurfaceREST, methodcheck.SurfaceMCPTools, methodcheck.SurfaceMCPApp}
	if got := SupportedSurfaces(); !slices.Equal(got, want) {
		t.Fatalf("SupportedSurfaces() = %v, want %v", got, want)
	}
}
