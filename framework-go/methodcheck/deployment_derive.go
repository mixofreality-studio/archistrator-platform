package methodcheck

import (
	"sort"
)

// deployment_derive.go owns the deployment view's ELEMENT INDEX and its DERIVED
// relationships — the half of a Structurizr-style deployment view that nobody
// authors.
//
// A deployment view carries two kinds of edge. The application edges (the SPA
// calling the server, the server reading its database) are already stated in
// the committed System model as component relationships; re-authoring them in
// the deployment slot would be duplication that drifts. So they are DERIVED
// here: each System relationship is mapped through container membership onto
// the deployment elements that actually host its endpoints. The remaining edges
// — browser to gateway, gateway to identity provider — have endpoints that are
// not System components at all, and those stay authored.
//
// This is the ONE implementation of that mapping. The gate consumes it (an edge
// rule that ignored derived edges would demand every server-to-database edge be
// hand-written purely to satisfy it) and so does the server's compute-at-read
// enrichment, so the picture the founder sees and the picture the gate judges
// cannot disagree.

// Element kinds — what a deployment element IS. These are the addressable
// endpoints of a deployment relationship.
const (
	ElementNode      = "node"
	ElementContainer = "containerInstance"
	ElementInfra     = "infrastructureNode"
	ElementExternal  = "softwareSystemInstance"
	ElementPerson    = "person"
)

// DeploymentElement is one addressable element of a deployment environment,
// flattened out of the node tree. Key is the identity relationships reference.
type DeploymentElement struct {
	Key  string
	Kind string
	Name string
	// ContainerKey is the declared container an ElementContainer instances;
	// empty for every other kind.
	ContainerKey string
	// Surface is the resolved DeployContainer.Surface for an ElementContainer;
	// empty for every other kind.
	Surface string
	// Role is the declared role of an ElementInfra / ElementExternal; empty for
	// every other kind.
	Role string
}

// isFrontendSurface reports whether a container surface is a human- or
// agent-facing entry point — the set DEP-FRONTEND-PRESENT requires. A deployment
// view that shows only back-end services has omitted how anyone actually reaches
// the system, which is the omission this predicate names.
func isFrontendSurface(surface string) bool {
	switch surface {
	case surfaceSPA, surfaceMobile, surfaceCLI, surfaceAgentHarness:
		return true
	default:
		return false
	}
}

// IsFrontend reports whether an element is a frontend surface — a container
// whose surface says so, or any element declaring the agentHarness role (an
// agent harness is frequently third-party software, so it appears as a
// softwareSystemInstance rather than a container we package).
func (e DeploymentElement) IsFrontend() bool {
	return isFrontendSurface(e.Surface) || e.Role == roleAgentHarness
}

// containerSurface resolves a container key to its declared surface, defaulting
// to `service` so documents authored before the field existed behave as
// back-end containers rather than as untyped ones.
func containerSurface(containersByKey map[string]DeployContainer, key string) string {
	c, ok := containersByKey[key]
	if !ok || c.Surface == "" {
		return surfaceService
	}
	return c.Surface
}

// elementRole defaults a declared role to `other`, so pre-existing documents
// parse unchanged and only an EXPLICIT role can satisfy a role-gated rule.
func elementRole(role string) string {
	if role == "" {
		return roleOther
	}
	return role
}

// EnvironmentElements flattens an environment into its addressable elements, in
// a stable order: persons first, then the node tree depth-first with each node
// followed by the container instances, infrastructure and external systems it
// hosts.
func EnvironmentElements(env DeploymentEnvironment, containersByKey map[string]DeployContainer) []DeploymentElement {
	out := make([]DeploymentElement, 0, len(env.Persons))
	for _, p := range env.Persons {
		out = append(out, DeploymentElement{Key: p.Key, Kind: ElementPerson, Name: p.Name})
	}
	var walk func(nodes []DeploymentNode)
	walk = func(nodes []DeploymentNode) {
		for _, n := range nodes {
			out = append(out, DeploymentElement{Key: n.Key, Kind: ElementNode, Name: n.Name})
			for _, ci := range n.ContainerInstances {
				out = append(out, DeploymentElement{
					Key:          ci.Key,
					Kind:         ElementContainer,
					Name:         containerDisplayName(containersByKey, ci.ContainerKey),
					ContainerKey: ci.ContainerKey,
					Surface:      containerSurface(containersByKey, ci.ContainerKey),
				})
			}
			for _, in := range n.InfrastructureNodes {
				out = append(out, DeploymentElement{
					Key: in.Key, Kind: ElementInfra, Name: in.Name, Role: elementRole(in.Role),
				})
			}
			for _, ss := range n.SoftwareSystemInstances {
				out = append(out, DeploymentElement{
					Key: ss.Key, Kind: ElementExternal, Name: ss.Name, Role: elementRole(ss.Role),
				})
			}
			walk(n.Children)
		}
	}
	walk(env.Nodes)
	return out
}

// containerDisplayName resolves a container key to its declared name, falling
// back to the key so an unresolvable instance still reads as something (the
// dangling reference itself is DEP-CONTAINER-REF's finding, not this function's).
func containerDisplayName(containersByKey map[string]DeployContainer, key string) string {
	if c, ok := containersByKey[key]; ok && c.Name != "" {
		return c.Name
	}
	return key
}

// containersByKeyIndex indexes declared containers by key.
func containersByKeyIndex(containers []DeployContainer) map[string]DeployContainer {
	idx := make(map[string]DeployContainer, len(containers))
	for _, c := range containers {
		idx[c.Key] = c
	}
	return idx
}

// DeriveDeploymentRelationships maps the committed System relationships onto the
// deployment elements of ONE environment.
//
// The mapping resolves each endpoint component to the element that hosts it:
//
//   - a component resolves to every instance of the container that packages it,
//     AND to every infrastructure node or external software system whose NAME
//     matches it — the slug match DEP-RESOURCE-PRESENT already established,
//     reused rather than replaced by a second convention for the same join. Both
//     lookups apply because both can be true at once: a Resource is ordinarily
//     deployed as infrastructure, but an embedded one (a dev-server compiled into
//     the binary, an on-disk store the process owns) is genuinely deployed inside
//     the container as well, and an edge to it is real in both places;
//   - a Utility resolves to nothing. Utilities are ambient and carry no lines in
//     the architecture views either; drawing them here would produce a hairball
//     that says nothing about deployment.
//
// Edges whose endpoints land on the SAME element are dropped — in a system whose
// Managers, Engines and ResourceAccess all ship in one container, that is most
// of them, and what survives is exactly the picture a deployment view is for:
// what crosses a process or machine boundary.
func DeriveDeploymentRelationships(env DeploymentEnvironment, containers []DeployContainer, s System) []DeploymentRelationship {
	containersByKey := containersByKeyIndex(containers)
	elements := EnvironmentElements(env, containersByKey)

	componentByID := make(map[string]Component, len(s.Components))
	for _, c := range s.Components {
		componentByID[c.ID] = c
	}

	// component NAME → the container key packaging it.
	containerByComponent := make(map[string]string)
	for _, c := range containers {
		for _, member := range c.Components {
			containerByComponent[member] = c.Key
		}
	}

	// container key → its instance element keys in this environment.
	instancesByContainer := make(map[string][]string)
	// resource name slug → the infra / external element keys naming it.
	elementsByResourceSlug := make(map[string][]string)
	for _, e := range elements {
		switch e.Kind {
		case ElementContainer:
			instancesByContainer[e.ContainerKey] = append(instancesByContainer[e.ContainerKey], e.Key)
		case ElementInfra, ElementExternal:
			slug := Slug(e.Name)
			elementsByResourceSlug[slug] = append(elementsByResourceSlug[slug], e.Key)
		}
	}

	resolve := func(componentID string) []string {
		c, ok := componentByID[componentID]
		if !ok || c.Kind == kindUtility {
			return nil
		}
		var keys []string
		if containerKey, packaged := containerByComponent[c.Name]; packaged {
			keys = append(keys, instancesByContainer[containerKey]...)
		}
		return append(keys, elementsByResourceSlug[Slug(c.Name)]...)
	}

	type edgeKey struct{ from, to, label string }
	seen := make(map[edgeKey]bool)
	var out []DeploymentRelationship
	for _, rel := range s.Relationships {
		fromKeys := resolve(rel.From)
		toKeys := resolve(rel.To)
		for _, from := range fromKeys {
			for _, to := range toKeys {
				if from == to {
					continue // same element — not a deployment-visible edge
				}
				k := edgeKey{from, to, rel.Label}
				if seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, DeploymentRelationship{
					From: from, To: to, Label: rel.Label, Mode: rel.Mode,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// AllRelationships is the full edge set of an environment as a view must render
// it and as an edge rule must judge it: what the architect authored, plus what
// derivation produced from the System model.
func AllRelationships(env DeploymentEnvironment, containers []DeployContainer, s System) []DeploymentRelationship {
	out := make([]DeploymentRelationship, 0, len(env.Relationships))
	out = append(out, env.Relationships...)
	return append(out, DeriveDeploymentRelationships(env, containers, s)...)
}
