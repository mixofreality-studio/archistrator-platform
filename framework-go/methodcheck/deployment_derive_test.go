package methodcheck

import (
	"reflect"
	"testing"
)

// deployment_derive_test.go pins the element index and the derivation at unit
// level, one small hand-built fixture per behaviour. A change to either one
// changes what the deployment view draws and what the edge rules judge, so
// each behaviour gets its own case.

// TestEnvironmentElements_OrderAndResolution pins the flattening order (persons,
// then each node followed by what it hosts, in container → infrastructure →
// external order, then its children depth-first, then its next sibling) and the
// per-kind resolution of name, surface and role.
func TestEnvironmentElements_OrderAndResolution(t *testing.T) {
	containers := containersByKeyIndex([]DeployContainer{
		{Key: "spa", Name: "Web app", Surface: surfaceSPA},
		{Key: "api"}, // no name → the key; no surface → service
	})
	env := DeploymentEnvironment{
		Persons: []DeploymentPerson{{Key: "p1", Name: "User"}, {Key: "p2", Name: "Admin"}},
		Nodes: []DeploymentNode{
			{
				Key: "n1", Name: "Device",
				Children: []DeploymentNode{{
					Key: "n1a", Name: "Browser",
					ContainerInstances: []ContainerInstance{{Key: "ci-spa", ContainerKey: "spa"}},
				}},
				SoftwareSystemInstances: []SoftwareSystemInstance{{Key: "ss", Name: "Harness", Role: roleAgentHarness}},
				InfrastructureNodes:     []InfrastructureNode{{Key: "in", Name: "Cache"}},
				ContainerInstances:      []ContainerInstance{{Key: "ci-ghost", ContainerKey: "ghost"}},
			},
			{Key: "n2", Name: "Cluster", ContainerInstances: []ContainerInstance{{Key: "ci-api", ContainerKey: "api"}}},
		},
	}
	want := []DeploymentElement{
		{Key: "p1", Kind: ElementPerson, Name: "User"},
		{Key: "p2", Kind: ElementPerson, Name: "Admin"},
		{Key: "n1", Kind: ElementNode, Name: "Device"},
		{Key: "ci-ghost", Kind: ElementContainer, Name: "ghost", ContainerKey: "ghost", Surface: surfaceService},
		{Key: "in", Kind: ElementInfra, Name: "Cache", Role: roleOther},
		{Key: "ss", Kind: ElementExternal, Name: "Harness", Role: roleAgentHarness},
		{Key: "n1a", Kind: ElementNode, Name: "Browser"},
		{Key: "ci-spa", Kind: ElementContainer, Name: "Web app", ContainerKey: "spa", Surface: surfaceSPA},
		{Key: "n2", Kind: ElementNode, Name: "Cluster"},
		{Key: "ci-api", Kind: ElementContainer, Name: "api", ContainerKey: "api", Surface: surfaceService},
	}
	if got := EnvironmentElements(env, containers); !reflect.DeepEqual(got, want) {
		t.Fatalf("EnvironmentElements:\n got %+v\nwant %+v", got, want)
	}
}

// deriveSystem is the System the derivation cases share. IDs differ from names
// on purpose: relationships address components by ID, while containers and
// infrastructure join on NAME.
func deriveSystem() System {
	return System{Components: []Component{
		{ID: "c-client", Name: "WebClient", Kind: kindClient},
		{ID: "c-mgr", Name: "OrderManager", Kind: kindManager},
		{ID: "c-ra", Name: "OrderAccess", Kind: kindResourceAccess},
		{ID: "c-db", Name: "OrderDB", Kind: kindResource},
		{ID: "c-gh", Name: "GitHub", Kind: kindResource},
		{ID: "c-log", Name: "Logging", Kind: kindUtility},
	}}
}

// deriveContainers packages the client alone and the manager, RA and the
// Logging utility together.
func deriveContainers() []DeployContainer {
	return []DeployContainer{
		{Key: "spa", Components: []string{"WebClient"}, Surface: surfaceSPA},
		{Key: "api", Components: []string{"OrderManager", "OrderAccess", "Logging"}},
	}
}

// deriveEnv instances each container once, and hosts the database and a log
// collector (named like the Logging utility) as infrastructure and GitHub as an
// external system.
func deriveEnv() DeploymentEnvironment {
	return DeploymentEnvironment{Nodes: []DeploymentNode{{
		Key: "n", Name: "cluster",
		ContainerInstances: []ContainerInstance{
			{Key: "spa-1", ContainerKey: "spa"},
			{Key: "api-1", ContainerKey: "api"},
		},
		InfrastructureNodes: []InfrastructureNode{
			{Key: "db", Name: "OrderDB"},
			{Key: "log", Name: "Logging"},
		},
		SoftwareSystemInstances: []SoftwareSystemInstance{{Key: "gh", Name: "GitHub"}},
	}}}
}

func TestDeriveDeploymentRelationships_Cases(t *testing.T) {
	rel := func(from, to, label, mode string) Relationship {
		return Relationship{From: from, To: to, Label: label, Mode: mode}
	}
	edge := func(from, to, label, mode string) DeploymentRelationship {
		return DeploymentRelationship{From: from, To: to, Label: label, Mode: mode}
	}
	cases := []struct {
		name string
		rels []Relationship
		want []DeploymentRelationship
	}{
		{
			name: "a relationship lands on the instances hosting its endpoints",
			rels: []Relationship{rel("c-client", "c-mgr", "calls", modeSync)},
			want: []DeploymentRelationship{edge("spa-1", "api-1", "calls", modeSync)},
		},
		{
			name: "a relationship inside one element is not deployment-visible",
			rels: []Relationship{rel("c-mgr", "c-ra", "uses", modeSync)},
			want: nil,
		},
		{
			name: "a resource resolves by name to infrastructure",
			rels: []Relationship{rel("c-ra", "c-db", "reads", modeSync)},
			want: []DeploymentRelationship{edge("api-1", "db", "reads", modeSync)},
		},
		{
			name: "a resource resolves by name to an external system",
			rels: []Relationship{rel("c-ra", "c-gh", "pushes", "async")},
			want: []DeploymentRelationship{edge("api-1", "gh", "pushes", "async")},
		},
		{
			name: "a utility draws nothing, even where its name matches infrastructure",
			rels: []Relationship{rel("c-mgr", "c-log", "logs via", modeSync)},
			want: nil,
		},
		{
			name: "an undeclared component draws nothing",
			rels: []Relationship{rel("c-mgr", "c-nowhere", "calls", modeSync)},
			want: nil,
		},
		{
			name: "one line per (from, to, label); the first relationship's mode wins",
			rels: []Relationship{
				rel("c-mgr", "c-db", "reads", modeSync),
				rel("c-ra", "c-db", "reads", "async"),
			},
			want: []DeploymentRelationship{edge("api-1", "db", "reads", modeSync)},
		},
		{
			name: "a different label is a different line",
			rels: []Relationship{
				rel("c-mgr", "c-db", "reads", modeSync),
				rel("c-ra", "c-db", "writes", modeSync),
			},
			want: []DeploymentRelationship{
				edge("api-1", "db", "reads", modeSync),
				edge("api-1", "db", "writes", modeSync),
			},
		},
		{
			name: "sorted by from, then to, then label",
			rels: []Relationship{
				rel("c-ra", "c-gh", "pushes", modeSync),
				rel("c-client", "c-mgr", "calls", modeSync),
				rel("c-ra", "c-db", "writes", modeSync),
				rel("c-ra", "c-db", "reads", modeSync),
			},
			want: []DeploymentRelationship{
				edge("api-1", "db", "reads", modeSync),
				edge("api-1", "db", "writes", modeSync),
				edge("api-1", "gh", "pushes", modeSync),
				edge("spa-1", "api-1", "calls", modeSync),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := deriveSystem()
			s.Relationships = tc.rels
			got := DeriveDeploymentRelationships(deriveEnv(), deriveContainers(), s)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// TestEndpointResolver_Resolve pins how one component maps onto elements,
// including the cases the full derivation hides behind self-edge removal.
func TestEndpointResolver_Resolve(t *testing.T) {
	s := deriveSystem()
	containers := []DeployContainer{
		{Key: "spa", Components: []string{"WebClient"}, Surface: surfaceSPA},
		// OrderDB is ALSO packaged here: an embedded store the process owns.
		{Key: "api", Components: []string{"OrderManager", "OrderDB", "Logging"}},
		// When two containers claim one component, the later claim wins.
		{Key: "worker", Components: []string{"OrderAccess"}},
		{Key: "late", Components: []string{"OrderAccess"}},
	}
	env := DeploymentEnvironment{Nodes: []DeploymentNode{{
		Key: "n", Name: "cluster",
		ContainerInstances: []ContainerInstance{
			{Key: "spa-1", ContainerKey: "spa"},
			{Key: "api-1", ContainerKey: "api"},
			{Key: "api-2", ContainerKey: "api"},
			{Key: "worker-1", ContainerKey: "worker"},
			{Key: "late-1", ContainerKey: "late"},
		},
		InfrastructureNodes:     []InfrastructureNode{{Key: "db", Name: "OrderDB"}, {Key: "log", Name: "Logging"}},
		SoftwareSystemInstances: []SoftwareSystemInstance{{Key: "gh", Name: "GitHub"}},
		// A NODE named like a component is a place, never an edge endpoint.
		Children: []DeploymentNode{{Key: "n-client", Name: "WebClient"}},
	}}}
	r := newEndpointResolver(EnvironmentElements(env, containersByKeyIndex(containers)), containers, s.Components)

	cases := []struct {
		id   string
		want []string
	}{
		{"c-client", []string{"spa-1"}},
		{"c-mgr", []string{"api-1", "api-2"}},
		{"c-db", []string{"api-1", "api-2", "db"}}, // packaged AND named: instances first, then infra
		{"c-ra", []string{"late-1"}},
		{"c-gh", []string{"gh"}},
		{"c-log", nil},
		{"c-nowhere", nil},
	}
	for _, tc := range cases {
		if got := r.resolve(tc.id); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("resolve(%s) = %v, want %v", tc.id, got, tc.want)
		}
	}
}
