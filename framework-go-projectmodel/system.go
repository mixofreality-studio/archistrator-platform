package projectmodel

import (
	"encoding/json"
	"strings"
)

// System is the codegen-relevant slice of the systemDesign slot model
// (slots[kind=5].model): the component inventory + the relationship edges.
// dynamicViews and all other fields are deliberately not parsed (tolerant
// subset — spec: Schema ownership & evolution).
type System struct {
	Components    []SystemComponent `json:"components"`
	Relationships []Relationship    `json:"relationships"`
}

// SystemComponent is one systemDesign component: a Method system building
// block with its inventory identity, layer, and optional contract join.
type SystemComponent struct {
	ID    string `json:"id"`   // kebab-case ("system-design-manager")
	Name  string `json:"name"` // PascalCase ("SystemDesignManager")
	Kind  string `json:"kind"`
	Layer string `json:"layer"`
	// ContractKey is the OPTIONAL explicit join back to a serviceContracts key
	// (camelCase, e.g. "systemDesignManager"). When present it is the preferred,
	// exact match in ComponentByContractKey; when absent (documents that predate
	// the field — spec: Schema ownership & evolution, tolerant evolution) the
	// case-convention heuristic is used instead.
	ContractKey string `json:"contractKey,omitempty"`
	// Surface is a Client's delivery surface (web, rest, mcpTools or mcpApp); empty means unstated.
	Surface Surface `json:"surface,omitempty"`
	// BuildStatus is the component's lifecycle marker ("planned", "external"; "" is built).
	BuildStatus string `json:"buildStatus,omitempty"`
}

// SurfaceKind parses the component's surface. It is false for a component that is not a Client,
// states no surface, or states one outside the closed set.
func (c SystemComponent) SurfaceKind() (SurfaceKind, bool) {
	if !strings.EqualFold(c.Kind, "client") {
		return nil, false
	}
	k, err := ParseSurface(string(c.Surface))
	if err != nil {
		return nil, false
	}
	return k, true
}

// IsBrowserSurface reports a Client whose surface is a browser surface: web or mcpApp.
func (c SystemComponent) IsBrowserSurface() bool {
	k, ok := c.SurfaceKind()
	if !ok {
		return false
	}
	switch k.(type) {
	case WebSurface, MCPAppSurface:
		return true
	case RESTSurface, MCPToolsSurface:
		return false
	}
	return false
}

// IsWebSurface reports a Client whose surface is a page served at the origin's root: web.
func (c SystemComponent) IsWebSurface() bool {
	k, ok := c.SurfaceKind()
	if !ok {
		return false
	}
	_, web := k.(WebSurface)
	return web
}

// ServesMCPTools reports a Client that gets generated MCP tool tables: mcpTools or mcpApp.
func (c SystemComponent) ServesMCPTools() bool {
	k, ok := c.SurfaceKind()
	if !ok {
		return false
	}
	switch k.(type) {
	case MCPToolsSurface, MCPAppSurface:
		return true
	case WebSurface, RESTSurface:
		return false
	}
	return false
}

// BuiltBrowserSurfaces is the System's built browser-surface Clients (web or mcpApp; not planned,
// not external), in declaration order. A nil System has none.
func (s *System) BuiltBrowserSurfaces() []SystemComponent {
	if s == nil {
		return nil
	}
	var out []SystemComponent
	for _, c := range s.Components {
		if c.IsBrowserSurface() && c.IsBuilt() {
			out = append(out, c)
		}
	}
	return out
}

// HasBrowserSurface reports whether the System has a built browser surface.
func (s *System) HasBrowserSurface() bool { return len(s.BuiltBrowserSurfaces()) > 0 }

// BuiltWebSurfaces is the System's built web Clients, in declaration order. A nil System has none.
func (s *System) BuiltWebSurfaces() []SystemComponent {
	if s == nil {
		return nil
	}
	var out []SystemComponent
	for _, c := range s.Components {
		if c.IsWebSurface() && c.IsBuilt() {
			out = append(out, c)
		}
	}
	return out
}

// HasWebSurface reports whether the System has a built web surface.
func (s *System) HasWebSurface() bool { return len(s.BuiltWebSurfaces()) > 0 }

// IsBuilt reports a component the project builds: neither planned nor external.
func (c SystemComponent) IsBuilt() bool {
	return c.BuildStatus != "planned" && c.BuildStatus != "external"
}

// KindBearsContract reports whether a component kind has a service contract at all: every kind
// but a Resource, a Client included (the surface its use-case steps and test plans bind to). This
// is the one definition of "contract-bearing" the Method's checks (methodcheck.IsContractBearing)
// hold the design to; kind is compared without spaces or case.
func KindBearsContract(kind string) bool {
	switch strings.ToLower(strings.ReplaceAll(kind, " ", "")) {
	case "client", "manager", "engine", "resourceaccess", "utility":
		return true
	}
	return false
}

// KindAuthorsContract reports whether a component kind's contract is authored by a construction
// activity of its own: a contract-bearing kind other than a Client, whose contract is derived in the
// mutation that records the Manager behind it (a machine client) or the surface's UI design (a
// browser one). The contract resolver and the app generator count the design complete by this set:
// a Client whose contract is still to be derived leaves nothing for them to wait on. It is
// KindBearsContract minus Client by construction, so the two cannot drift apart.
func KindAuthorsContract(kind string) bool {
	return KindBearsContract(kind) && strings.ToLower(strings.ReplaceAll(kind, " ", "")) != "client"
}

// OwesContract reports a component construction owes a service contract: a built component of a
// contract-bearing kind.
func (c SystemComponent) OwesContract() bool {
	return c.IsBuilt() && KindBearsContract(c.Kind)
}

// OwesAuthoredContract reports a built component whose contract a construction activity authors
// (KindAuthorsContract).
func (c SystemComponent) OwesAuthoredContract() bool {
	return c.IsBuilt() && KindAuthorsContract(c.Kind)
}

// Relationship is one systemDesign edge between two components.
type Relationship struct {
	From  string `json:"from"` // kebab component id
	To    string `json:"to"`
	Mode  string `json:"mode"` // "sync" | "queued"
	Label string `json:"label"`
}

// ParseSystem parses a systemDesign slot model into a System.
func ParseSystem(slotModel json.RawMessage) (*System, error) {
	var s System
	if err := json.Unmarshal(slotModel, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ComponentByContractKey joins a camelCase serviceContracts key to its
// systemDesign component — the Go port of the webApp's
// resolveContractComponentId. Preferred-exact-match with heuristic fallback
// (spec: Schema ownership & evolution, tolerant evolution):
//
//	(0) exact match on an explicit ContractKey — the join the document owns;
//	(1) Kebab(key) match on ID,
//	(2) case-insensitive ID match,
//	(3) case-insensitive Kebab(name) match.
//
// Documents that predate the ContractKey field carry no (0) candidate and fall
// straight through to the heuristic, so behaviour is unchanged for them. Honest
// miss on no match.
func (s *System) ComponentByContractKey(key string) (*SystemComponent, bool) {
	if c, ok := s.componentByExplicitContractKey(key); ok {
		return c, true
	}
	return s.componentByKebabHeuristic(Kebab(key))
}

// componentByExplicitContractKey implements step (0) of the join: the
// document-owned exact match, preferred over every case-convention heuristic.
func (s *System) componentByExplicitContractKey(key string) (*SystemComponent, bool) {
	for i := range s.Components {
		if s.Components[i].ContractKey != "" && s.Components[i].ContractKey == key {
			return &s.Components[i], true
		}
	}
	return nil, false
}

// componentByKebabHeuristic implements steps (1)-(3): the case-convention
// fallback used by documents that predate the ContractKey field.
func (s *System) componentByKebabHeuristic(kebab string) (*SystemComponent, bool) {
	for i := range s.Components {
		if s.Components[i].ID == kebab {
			return &s.Components[i], true
		}
	}
	for i := range s.Components {
		if strings.EqualFold(s.Components[i].ID, kebab) {
			return &s.Components[i], true
		}
	}
	for i := range s.Components {
		if strings.EqualFold(Kebab(s.Components[i].Name), kebab) {
			return &s.Components[i], true
		}
	}
	return nil, false
}
