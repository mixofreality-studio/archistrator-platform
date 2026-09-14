package methodcheck

import (
	"fmt"
	"reflect"
	"testing"
)

// rules_deployment_gateway_test.go pins DEP-EDGE-GATEWAY at unit level. It
// calls checkGatewayChain directly over hand-built elements and edges, so
// every case isolates one fact about the front door. The fixture-level tests in
// rules_deployment_edges_test.go cover the same rule through the full suite.

func TestCheckGatewayChain_Cases(t *testing.T) {
	var (
		spa     = DeploymentElement{Key: "spa", Kind: ElementContainer, Surface: surfaceSPA}
		app     = DeploymentElement{Key: "app", Kind: ElementContainer, Surface: surfaceService}
		gw      = DeploymentElement{Key: "gw", Kind: ElementInfra, Role: roleGateway}
		idp     = DeploymentElement{Key: "idp", Kind: ElementInfra, Role: roleIdentityProvider}
		harness = DeploymentElement{Key: "h", Kind: ElementExternal, Role: roleAgentHarness}
	)
	e := func(from, to string) DeploymentRelationship { return DeploymentRelationship{From: from, To: to} }
	const (
		hopGateway = "frontend surface → gateway"
		hopIdP     = "gateway → identity provider"
		hopApp     = "gateway → application container"
	)

	cases := []struct {
		name     string
		elements []DeploymentElement
		edges    []DeploymentRelationship
		missing  []string // nil: no finding
	}{
		{
			name:     "no gateway: the chain is not required",
			elements: []DeploymentElement{spa, app, idp},
			missing:  nil,
		},
		{
			name:     "the complete chain passes",
			elements: []DeploymentElement{spa, app, gw, idp},
			edges:    []DeploymentRelationship{e("spa", "gw"), e("gw", "idp"), e("gw", "app")},
			missing:  nil,
		},
		{
			name:     "nothing reaches the gateway",
			elements: []DeploymentElement{spa, app, gw, idp},
			edges:    []DeploymentRelationship{e("gw", "idp"), e("gw", "app")},
			missing:  []string{hopGateway},
		},
		{
			name:     "the gateway authenticates against nothing",
			elements: []DeploymentElement{spa, app, gw, idp},
			edges:    []DeploymentRelationship{e("spa", "gw"), e("gw", "app")},
			missing:  []string{hopIdP},
		},
		{
			name:     "the gateway forwards to nothing",
			elements: []DeploymentElement{spa, app, gw, idp},
			edges:    []DeploymentRelationship{e("spa", "gw"), e("gw", "idp")},
			missing:  []string{hopApp},
		},
		{
			name:     "every hop missing is named, in chain order",
			elements: []DeploymentElement{spa, app, gw, idp},
			missing:  []string{hopGateway, hopIdP, hopApp},
		},
		{
			name:     "edges point the other way: none of them count",
			elements: []DeploymentElement{spa, app, gw, idp},
			edges:    []DeploymentRelationship{e("gw", "spa"), e("idp", "gw"), e("app", "gw")},
			missing:  []string{hopGateway, hopIdP, hopApp},
		},
		{
			name:     "an agent harness is a frontend surface",
			elements: []DeploymentElement{harness, app, gw, idp},
			edges:    []DeploymentRelationship{e("h", "gw"), e("gw", "idp"), e("gw", "app")},
			missing:  nil,
		},
		{
			name:     "a frontend container is not the application",
			elements: []DeploymentElement{spa, gw, idp},
			edges:    []DeploymentRelationship{e("spa", "gw"), e("gw", "idp"), e("gw", "spa")},
			missing:  []string{hopApp},
		},
		{
			// Pinned as-is: a key shared by an identity provider and an
			// application container (already a DEP-KEY-UNIQUE failure) is
			// credited for the EARLIER hop only.
			name: "an edge credits only the first hop it realizes",
			elements: []DeploymentElement{spa, gw,
				{Key: "x", Kind: ElementInfra, Role: roleIdentityProvider},
				{Key: "x", Kind: ElementContainer, Surface: surfaceService},
			},
			edges:   []DeploymentRelationship{e("spa", "gw"), e("gw", "x")},
			missing: []string{hopApp},
		},
	}

	const section = "deployment environment \"cloud\""
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkGatewayChain(tc.edges, tc.elements, section, 3)
			if tc.missing == nil {
				if got != nil {
					t.Fatalf("want no finding, got %+v", got)
				}
				return
			}
			want := []Finding{{
				RuleID:   ruleDepEdgeGateway,
				Severity: SeverityError,
				Message: fmt.Sprintf("%s: an edge gateway is deployed but the standard web-app front door is incomplete (missing: %v); a request reaches the gateway, is authenticated against the identity provider, and is forwarded to the application — start from the webapp deployment prototype rather than re-deriving it",
					section, tc.missing),
				Location: loc(3, section),
			}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("\n got %+v\nwant %+v", got, want)
			}
		})
	}
}
