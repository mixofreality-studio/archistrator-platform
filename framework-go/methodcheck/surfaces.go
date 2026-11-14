package methodcheck

import (
	"fmt"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// Delivery surfaces (FPC W6). framework-go-projectmodel carries the same closed set for the
// generators; framework-go keeps its own structural mirror so methodcheck stays dependency-free.

// Surface is how a Client reaches its users. The wire form is a string enum.
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
// with ParseSurface and switches over the variants with no default.
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

// Audience is who uses a delivery surface.
type Audience string

// The two audiences.
const (
	AudienceHuman   Audience = "human"
	AudienceMachine Audience = "machine"
)

// AudienceOf is the audience a surface serves: web and mcpApp serve people, rest and mcpTools
// serve machines.
func AudienceOf(k SurfaceKind) Audience {
	switch k.(type) {
	case WebSurface, MCPAppSurface:
		return AudienceHuman
	case RESTSurface, MCPToolsSurface:
		return AudienceMachine
	}
	return AudienceMachine
}

// DeliverySurface is one channel the brief commits the system to (MissionStatement.DeliverySurfaces).
type DeliverySurface struct {
	Kind      Surface  `json:"kind"`
	Audience  Audience `json:"audience"`
	Source    string   `json:"source"`    // the brief's words, quoted
	Objective int      `json:"objective"` // an Objective.Number of the same mission
}

// surfaceKind parses the component's surface; false when it is not a Client, states none, or
// states one outside the closed set.
func (c Component) surfaceKind() (SurfaceKind, bool) {
	if c.Kind != kindClient {
		return nil, false
	}
	k, err := ParseSurface(string(c.Surface))
	if err != nil {
		return nil, false
	}
	return k, true
}

// ClientSurface resolves a serviceContracts key to the Client component it belongs to and the
// surface that component states. A facet (a contract whose `component` names another component)
// resolves to its parent. ok is false when the key names no Client of the committed System, or
// the Client states no surface of the closed set.
func (p Project) ClientSurface(contractKey string) (componentID string, s Surface, ok bool) {
	sys, committed, err := p.system()
	if err != nil || !committed {
		return "", "", false
	}
	target := contractKey
	if sc, has := p.ServiceContracts[contractKey]; has && sc.Component != "" {
		target = sc.Component
	}
	want := scenario.Normalize(target)
	for _, c := range sys.Components {
		if _, isClient := c.surfaceKind(); !isClient {
			continue
		}
		if scenario.Normalize(c.ID) == want || scenario.Normalize(c.Name) == want {
			return c.ID, c.Surface, true
		}
	}
	return "", "", false
}
