package methodcheck

import (
	"fmt"
	"sort"
)

// rules_deployment_edges.go owns the EDGE family of the deployment suite —
// DEP-KEY-UNIQUE, DEP-EDGE-REF, DEP-EDGE-ISOLATED, DEP-FRONTEND-PRESENT and
// DEP-EDGE-GATEWAY.
//
// The suite that preceded these rules checked that the deployment topology was
// internally consistent: containers referenced, members exclusive, profiles
// covered. All of it was satisfiable by a document that drew nothing but boxes,
// and that is what drafting agents produced. These rules add the other half —
// that the elements are CONNECTED, that a human or agent can be seen to reach
// the system at all, and that where an edge gateway fronts the system it does so
// through the platform's standard authenticated front door.
//
// Every rule judges the FULL edge set (authored ∪ derived, see
// deployment_derive.go). Judging only authored edges would force every
// server-to-database relationship already stated in the System model to be
// re-authored in the deployment slot purely to satisfy DEP-EDGE-ISOLATED.

// checkDeploymentEdges runs the edge family over every non-dev-boot environment.
//
// The local DEV-BOOT profile is skipped for the same reason every other
// structural check skips it (see isDevBootProfile): it is a development-boot
// convenience that may legitimately be sparse or empty, and is never a required,
// operated profile. Authoring its edges remains worthwhile for the rendered
// picture; requiring them would change a deliberate carve-out as a side effect.
func checkDeploymentEdges(topo DeploymentTopology, s System, expected map[string]bool) []Finding {
	containersByKey := containersByKeyIndex(topo.Containers)
	var out []Finding
	for i, env := range topo.Environments {
		if isDevBootProfile(env.Profile, expected) {
			continue
		}
		out = append(out, checkEnvironmentEdges(env, i+1, topo.Containers, containersByKey, s)...)
	}
	return out
}

// checkEnvironmentEdges runs the whole edge family over ONE environment.
func checkEnvironmentEdges(
	env DeploymentEnvironment,
	ordinal int,
	containers []DeployContainer,
	containersByKey map[string]DeployContainer,
	s System,
) []Finding {
	section := fmt.Sprintf("deployment environment %q", profileName(env.Profile))
	elements := EnvironmentElements(env, containersByKey)

	out := checkKeysUnique(elements, section, ordinal)

	// A document whose elements have no keys at all cannot be edge-checked
	// coherently: every reference would dangle and every element would read as
	// isolated, burying the real finding (the missing keys) under noise. The
	// key findings above already name the problem.
	if hasUnkeyedElement(elements) {
		return append(out, Finding{
			RuleID:   ruleDepKeyUnique,
			Severity: SeverityError,
			Message: fmt.Sprintf("%s: an element has no key; every deployment element needs a key unique within its environment so relationships can address it",
				section),
			Location: loc(ordinal, section),
		})
	}

	edges := AllRelationships(env, containers, s)
	out = append(out, checkEdgeRefs(edges, elements, section, ordinal)...)
	out = append(out, checkEdgesConnected(edges, elements, section, ordinal)...)
	out = append(out, checkFrontendPresent(elements, section, ordinal)...)
	out = append(out, checkGatewayChain(edges, elements, section, ordinal)...)
	return out
}

// hasUnkeyedElement reports whether any element lacks a key.
func hasUnkeyedElement(elements []DeploymentElement) bool {
	for _, e := range elements {
		if e.Key == "" {
			return true
		}
	}
	return false
}

// checkKeysUnique emits DEP-KEY-UNIQUE for every key claimed by more than one
// element of the same environment. A duplicated key makes every relationship
// touching it ambiguous.
func checkKeysUnique(elements []DeploymentElement, section string, ordinal int) []Finding {
	count := make(map[string]int, len(elements))
	for _, e := range elements {
		if e.Key != "" {
			count[e.Key]++
		}
	}
	var dupes []string
	for key, n := range count {
		if n > 1 {
			dupes = append(dupes, key)
		}
	}
	if len(dupes) == 0 {
		return nil
	}
	sort.Strings(dupes)
	return []Finding{{
		RuleID:   ruleDepKeyUnique,
		Severity: SeverityError,
		Message: fmt.Sprintf("%s: element keys are claimed by more than one element (%v); a key must identify exactly one element within its environment",
			section, dupes),
		Location: loc(ordinal, section),
	}}
}

// checkEdgeRefs emits DEP-EDGE-REF for every relationship endpoint that names no
// declared element of this environment — an edge drawn to nothing.
func checkEdgeRefs(edges []DeploymentRelationship, elements []DeploymentElement, section string, ordinal int) []Finding {
	known := make(map[string]bool, len(elements))
	for _, e := range elements {
		known[e.Key] = true
	}
	var dangling []string
	seen := make(map[string]bool)
	for _, edge := range edges {
		for _, end := range []string{edge.From, edge.To} {
			if known[end] || seen[end] {
				continue
			}
			seen[end] = true
			dangling = append(dangling, end)
		}
	}
	if len(dangling) == 0 {
		return nil
	}
	sort.Strings(dangling)
	return []Finding{{
		RuleID:   ruleDepEdgeRef,
		Severity: SeverityError,
		Message: fmt.Sprintf("%s: relationship endpoints reference no declared element (%v); every endpoint must name an element key in the same environment",
			section, dangling),
		Location: loc(ordinal, section),
	}}
}

// checkEdgesConnected emits DEP-EDGE-ISOLATED for every container instance,
// infrastructure node, external software system or person that no relationship
// touches — the "boxes with no lines" defect, named per element.
//
// Deployment NODES are exempt: a node is a place (a cluster, a namespace, a
// laptop), and in the C4 idiom it carries edges only incidentally. What must be
// connected is what RUNS.
func checkEdgesConnected(edges []DeploymentRelationship, elements []DeploymentElement, section string, ordinal int) []Finding {
	touched := make(map[string]bool, len(edges)*2)
	for _, e := range edges {
		touched[e.From] = true
		touched[e.To] = true
	}
	var isolated []string
	for _, e := range elements {
		if e.Kind == ElementNode || touched[e.Key] {
			continue
		}
		isolated = append(isolated, fmt.Sprintf("%s (%s)", e.Key, e.Kind))
	}
	if len(isolated) == 0 {
		return nil
	}
	sort.Strings(isolated)
	return []Finding{{
		RuleID:   ruleDepEdgeIsolated,
		Severity: SeverityError,
		Message: fmt.Sprintf("%s: these elements carry no relationship (%v); every container instance, infrastructure node, external system and person must have ≥1 edge — an element nothing reaches and that reaches nothing is not deployed, it is decoration",
			section, isolated),
		Location: loc(ordinal, section),
	}}
}

// checkFrontendPresent emits DEP-FRONTEND-PRESENT when an environment instances
// no frontend surface. Every system built on the platform is reached by someone
// — through a single-page application, a mobile app, a CLI, or an agent harness
// speaking to its tool surface. A deployment view showing only back-end services
// has omitted how the system is used.
func checkFrontendPresent(elements []DeploymentElement, section string, ordinal int) []Finding {
	for _, e := range elements {
		if e.IsFrontend() {
			return nil
		}
	}
	return []Finding{{
		RuleID:   ruleDepFrontendPresent,
		Severity: SeverityError,
		Message: fmt.Sprintf("%s: no frontend surface is deployed; every environment must instance at least one container whose surface is %q, %q or %q, or an element with the %q role",
			section, surfaceSPA, surfaceMobile, surfaceCLI, roleAgentHarness),
		Location: loc(ordinal, section),
	}}
}

// checkGatewayChain emits DEP-EDGE-GATEWAY when an environment fronts the system
// with an edge gateway but does not show the platform's standard front door
// through it: a frontend surface reaches the gateway, the gateway authenticates
// against an identity provider, and the gateway forwards to an application
// container.
//
// The rule is gated on the PRESENCE of a gateway element rather than applied
// unconditionally. A local dev-boot topology binds a loopback port with neither
// gateway nor identity provider, and demanding the chain there would require the
// model to state something untrue. Where a gateway does exist, the whole chain
// is required — a gateway that forwards without authenticating, or that nothing
// reaches, is not the platform's front door.
func checkGatewayChain(edges []DeploymentRelationship, elements []DeploymentElement, section string, ordinal int) []Finding {
	members := classifyFrontDoor(elements)
	if len(members[frontDoorGateway]) == 0 {
		return nil // no edge gateway in this environment — nothing to require
	}
	missing := missingFrontDoorLinks(edges, members)
	if len(missing) == 0 {
		return nil
	}
	return []Finding{{
		RuleID:   ruleDepEdgeGateway,
		Severity: SeverityError,
		Message: fmt.Sprintf("%s: an edge gateway is deployed but the standard web-app front door is incomplete (missing: %v); a request reaches the gateway, is authenticated against the identity provider, and is forwarded to the application — start from the webapp deployment prototype rather than re-deriving it",
			section, missing),
		Location: loc(ordinal, section),
	}}
}

// The front door's positions: what an element can BE in the chain. Each is also
// the wording a DEP-EDGE-GATEWAY finding uses for it.
const (
	frontDoorFrontend = "frontend surface"
	frontDoorGateway  = "gateway"
	frontDoorIdP      = "identity provider"
	frontDoorApp      = "application container"
)

// frontDoorLink is one hop of the front door, from one position to another.
type frontDoorLink struct{ from, to string }

func (l frontDoorLink) String() string { return l.from + " → " + l.to }

// frontDoorChain is the standard front door, in the order a finding names its
// missing hops: a frontend surface reaches the gateway, the gateway
// authenticates against the identity provider, and the gateway forwards to the
// application container.
var frontDoorChain = []frontDoorLink{
	{from: frontDoorFrontend, to: frontDoorGateway},
	{from: frontDoorGateway, to: frontDoorIdP},
	{from: frontDoorGateway, to: frontDoorApp},
}

// frontDoorPositions lists the chain positions an element occupies. Role and
// surface are independent facts, so the checks are independent too.
func frontDoorPositions(e DeploymentElement) []string {
	var ps []string
	switch e.Role {
	case roleGateway:
		ps = append(ps, frontDoorGateway)
	case roleIdentityProvider:
		ps = append(ps, frontDoorIdP)
	}
	if e.IsFrontend() {
		ps = append(ps, frontDoorFrontend)
	}
	if e.Kind == ElementContainer && e.Surface == surfaceService {
		ps = append(ps, frontDoorApp)
	}
	return ps
}

// frontDoorMembers indexes element keys by the chain position they occupy.
type frontDoorMembers map[string]map[string]bool

func classifyFrontDoor(elements []DeploymentElement) frontDoorMembers {
	m := frontDoorMembers{}
	for _, e := range elements {
		for _, p := range frontDoorPositions(e) {
			if m[p] == nil {
				m[p] = map[string]bool{}
			}
			m[p][e.Key] = true
		}
	}
	return m
}

// linkOf returns the index of the FIRST chain hop an edge realizes. An edge
// credits at most one hop. That only matters when one key names elements in two
// positions, a document DEP-KEY-UNIQUE has already failed; the chain then judges
// it by the earlier hop.
func (m frontDoorMembers) linkOf(edge DeploymentRelationship) (int, bool) {
	for i, l := range frontDoorChain {
		if m[l.from][edge.From] && m[l.to][edge.To] {
			return i, true
		}
	}
	return 0, false
}

// missingFrontDoorLinks names, in chain order, the hops no edge realizes.
func missingFrontDoorLinks(edges []DeploymentRelationship, m frontDoorMembers) []string {
	realized := make([]bool, len(frontDoorChain))
	for _, edge := range edges {
		if i, ok := m.linkOf(edge); ok {
			realized[i] = true
		}
	}
	var missing []string
	for i, l := range frontDoorChain {
		if !realized[i] {
			missing = append(missing, l.String())
		}
	}
	return missing
}
