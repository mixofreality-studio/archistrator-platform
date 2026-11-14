// Package appgen is the importable face of the app generator: the facts about what it can build
// that design-time gates need (cmd/appgen is package main and cannot be imported).
package appgen

import "github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"

// SupportedSurfaces is every Client surface the generators build end to end, each proven by a
// conformance fixture (framework-go-conformance TestSupportedSurfaces_EachHasAConformanceFixture).
// A surface joins this list only with its generator and its fixture. mcpApp joins it when its
// resource and bridge generation land.
func SupportedSurfaces() []methodcheck.Surface {
	return []methodcheck.Surface{methodcheck.SurfaceWeb, methodcheck.SurfaceREST, methodcheck.SurfaceMCPTools}
}
