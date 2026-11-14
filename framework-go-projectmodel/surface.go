package projectmodel

import "fmt"

// Surface is how a Client reaches its users (FPC W6). The wire form is a string enum: a Client has
// one surface and no variant carries a payload.
type Surface string

// The closed set of Surfaces.
const (
	SurfaceWeb      Surface = "web"
	SurfaceREST     Surface = "rest"
	SurfaceMCPTools Surface = "mcpTools"
	SurfaceMCPApp   Surface = "mcpApp"
)

// Surfaces is the closed set, in declaration order.
func Surfaces() []Surface {
	return []Surface{SurfaceWeb, SurfaceREST, SurfaceMCPTools, SurfaceMCPApp}
}

// SurfaceKind is the sealed sum over the Surface set. Code that branches on a surface parses it
// with ParseSurface and switches over the variants with no default; gochecksumtype keeps the
// switch exhaustive.
//
//sumtype:decl
type SurfaceKind interface {
	isSurfaceKind()
	// Surface is the wire value of the variant.
	Surface() Surface
}

// WebSurface is a browser page on the API's origin; hand-built UI.
type WebSurface struct{}

// RESTSurface is the shared generated REST transport.
type RESTSurface struct{}

// MCPToolsSurface is generated MCP tool tables.
type MCPToolsSurface struct{}

// MCPAppSurface is a hand-built UI an MCP host embeds; its tool tables and ui:// resource are
// generated.
type MCPAppSurface struct{}

func (WebSurface) isSurfaceKind()      {}
func (RESTSurface) isSurfaceKind()     {}
func (MCPToolsSurface) isSurfaceKind() {}
func (MCPAppSurface) isSurfaceKind()   {}

// Surface returns the wire value of the variant.
func (WebSurface) Surface() Surface      { return SurfaceWeb }
func (RESTSurface) Surface() Surface     { return SurfaceREST }
func (MCPToolsSurface) Surface() Surface { return SurfaceMCPTools }
func (MCPAppSurface) Surface() Surface   { return SurfaceMCPApp }

// ParseSurface maps a wire value to its variant. An unknown value, or the empty string, is an
// error naming the four values.
func ParseSurface(s string) (SurfaceKind, error) {
	switch Surface(s) {
	case SurfaceWeb:
		return WebSurface{}, nil
	case SurfaceREST:
		return RESTSurface{}, nil
	case SurfaceMCPTools:
		return MCPToolsSurface{}, nil
	case SurfaceMCPApp:
		return MCPAppSurface{}, nil
	}
	return nil, fmt.Errorf("unknown surface %q (want one of web, rest, mcpTools, mcpApp)", s)
}
