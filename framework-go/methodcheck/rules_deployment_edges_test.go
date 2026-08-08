package methodcheck

import (
	"strings"
	"testing"
)

// rules_deployment_edges_test.go covers the EDGE family. Each test starts from
// the shared legal base (deploymentBaseOC), breaks exactly one thing, and asserts
// the one rule that names the breakage — the same per-rule negative-fixture style
// the rest of the deployment suite uses.

// ---- derivation ----

// TestDerive_MapsSystemRelationshipsOntoElements proves the load-bearing claim:
// the application edges come from the committed System model, so nobody authors
// them and they cannot drift from the architecture.
func TestDerive_MapsSystemRelationshipsOntoElements(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	env := op.Deployment.Environments[0]

	derived := DeriveDeploymentRelationships(env, op.Deployment.Containers, s)
	if len(derived) == 0 {
		t.Fatal("a System with relationships must derive deployment edges; got none")
	}
	if !hasEdge(derived, "cloud-c-AppClient", "cloud-c-DesignManager") {
		t.Fatalf("the client→manager System relationship must derive onto their container instances, got %+v", derived)
	}
	for _, e := range derived {
		if e.From == e.To {
			t.Fatalf("derivation must drop self-edges, got %+v", e)
		}
	}
}

// TestDerive_UtilitiesCarryNoEdges holds the line the architecture views already
// hold: utilities are ambient, and drawing them produces a hairball that says
// nothing about deployment.
func TestDerive_UtilitiesCarryNoEdges(t *testing.T) {
	s := deploymentBaseSystem(t)
	s.Components = append(s.Components, comp(t, "Logging", kindUtility))
	s.Relationships = append(s.Relationships,
		Relationship{From: Slug("DesignManager"), To: Slug("Logging"), Mode: modeSync, Label: "logs via"})

	containers := deploymentBaseContainers(s)
	env := envInstancingAll(profileCloud, "Cloud", containers, resourceInfraNodes(s))
	for _, e := range DeriveDeploymentRelationships(env, containers, s) {
		if strings.Contains(e.To, "Logging") || strings.Contains(e.From, "Logging") {
			t.Fatalf("a utility must derive no deployment edge, got %+v", e)
		}
	}
}

// TestDerive_SelfContainedRelationshipsCollapse proves the deployment view shows
// what crosses a boundary rather than restating the whole architecture: when two
// components ship in ONE container, the edge between them is not deployment-visible.
func TestDerive_SelfContainedRelationshipsCollapse(t *testing.T) {
	s := deploymentBaseSystem(t)
	one := []DeployContainer{{
		Key: "monolith", Name: "monolith", Surface: surfaceService,
		Components: []string{"AppClient", "DesignManager", "ValidatingEngine", "StateAccess"},
	}}
	env := DeploymentEnvironment{Profile: profileCloud, Nodes: []DeploymentNode{{
		Key: "n", Name: "cluster",
		ContainerInstances:  []ContainerInstance{{Key: "ci", ContainerKey: "monolith"}},
		InfrastructureNodes: []InfrastructureNode{{Key: "db", Name: "StateDB"}},
	}}}

	derived := DeriveDeploymentRelationships(env, one, s)
	if len(derived) != 1 {
		t.Fatalf("only the container→resource edge crosses a boundary, got %+v", derived)
	}
	if derived[0].From != "ci" || derived[0].To != "db" {
		t.Fatalf("expected ci→db, got %+v", derived[0])
	}
}

// ---- DEP-KEY-UNIQUE ----

func TestDeploymentEdges_KeyMustBePresent(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	op.Deployment.Environments[0].Nodes[0].ContainerInstances[0].Key = ""
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepKeyUnique) {
		t.Fatal("an element with no key must trip DEP-KEY-UNIQUE")
	}
}

func TestDeploymentEdges_KeyMustBeUnique(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	insts := op.Deployment.Environments[0].Nodes[0].ContainerInstances
	insts[1].Key = insts[0].Key
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepKeyUnique) {
		t.Fatal("a key claimed by two elements must trip DEP-KEY-UNIQUE")
	}
}

// TestDeploymentEdges_SameKeyInDifferentEnvsIsFine guards the scope of the
// uniqueness rule: keys identify elements WITHIN an environment, and the same
// container instanced in cloud and test is two different elements.
func TestDeploymentEdges_SameKeyInDifferentEnvsIsFine(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	if hasRuleFindings(deploymentConsistency(op, s), ruleDepKeyUnique) {
		t.Fatal("keys are unique per environment, not globally; the base must not trip DEP-KEY-UNIQUE")
	}
}

// ---- DEP-EDGE-REF ----

func TestDeploymentEdges_EndpointMustResolve(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	op.Deployment.Environments[0].Relationships = []DeploymentRelationship{{
		From: "cloud-c-AppClient", To: "no-such-element", Label: "calls",
	}}
	findings := deploymentConsistency(op, s)
	if !hasRuleFindings(findings, ruleDepEdgeRef) {
		t.Fatalf("an edge to an undeclared element must trip DEP-EDGE-REF, got %+v", findings)
	}
}

// ---- DEP-EDGE-ISOLATED ----

// TestDeploymentEdges_IsolatedElementFlagged is the rule that answers the actual
// complaint: a box nothing reaches and that reaches nothing.
func TestDeploymentEdges_IsolatedElementFlagged(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	op.Deployment.Environments[0].Nodes[0].InfrastructureNodes = append(
		op.Deployment.Environments[0].Nodes[0].InfrastructureNodes,
		InfrastructureNode{Key: "cloud-infra-orphan", Name: "Orphaned cache", Technology: "Redis"},
	)
	findings := deploymentConsistency(op, s)
	if !hasRuleFindings(findings, ruleDepEdgeIsolated) {
		t.Fatalf("an unconnected infrastructure node must trip DEP-EDGE-ISOLATED, got %+v", findings)
	}
}

func TestDeploymentEdges_IsolatedPersonFlagged(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	op.Deployment.Environments[0].Persons = []DeploymentPerson{{Key: "p", Name: "Customer"}}
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepEdgeIsolated) {
		t.Fatal("a person connected to nothing must trip DEP-EDGE-ISOLATED")
	}
}

// TestDeploymentEdges_AuthoredEdgeSatisfiesIsolation proves the rule judges the
// FULL edge set: an element derivation cannot reach is rescued by an authored edge.
func TestDeploymentEdges_AuthoredEdgeSatisfiesIsolation(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	env := &op.Deployment.Environments[0]
	env.Persons = []DeploymentPerson{{Key: "p", Name: "Customer"}}
	env.Relationships = []DeploymentRelationship{{
		From: "p", To: "cloud-c-AppClient", Label: "Uses", Mode: modeSync,
	}}
	if hasRuleFindings(deploymentConsistency(op, s), ruleDepEdgeIsolated) {
		t.Fatal("an authored edge must satisfy DEP-EDGE-ISOLATED")
	}
}

// TestDeploymentEdges_NodesExemptFromIsolation guards the carve-out: a node is a
// PLACE, and places carry edges only incidentally in the C4 idiom.
func TestDeploymentEdges_NodesExemptFromIsolation(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	for _, f := range deploymentConsistency(op, s) {
		if f.RuleID == ruleDepEdgeIsolated && strings.Contains(f.Message, "cluster") {
			t.Fatalf("deployment nodes must be exempt from DEP-EDGE-ISOLATED, got %+v", f)
		}
	}
}

// ---- DEP-FRONTEND-PRESENT ----

func TestDeploymentEdges_FrontendRequired(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	for i := range op.Deployment.Containers {
		op.Deployment.Containers[i].Surface = surfaceService
	}
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepFrontendPresent) {
		t.Fatal("an environment with only back-end services must trip DEP-FRONTEND-PRESENT")
	}
}

// TestDeploymentEdges_AgentHarnessIsAFrontend proves the second supported
// surface: a system reached only through its generated tool surface has a
// frontend, even with no browser anywhere.
func TestDeploymentEdges_AgentHarnessIsAFrontend(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	for i := range op.Deployment.Containers {
		op.Deployment.Containers[i].Surface = surfaceService
	}
	for i := range op.Deployment.Environments {
		env := &op.Deployment.Environments[i]
		p := env.Profile
		env.Nodes[0].SoftwareSystemInstances = []SoftwareSystemInstance{{
			Key: p + "-harness", Name: "Agent harness", Technology: "MCP client", Role: roleAgentHarness,
		}}
		env.Relationships = []DeploymentRelationship{{
			From: p + "-harness", To: p + "-c-DesignManager", Label: "Makes tool calls to", Mode: modeSync,
		}}
	}
	if f := deploymentConsistency(op, s); hasRuleFindings(f, ruleDepFrontendPresent) {
		t.Fatalf("an agent harness must satisfy DEP-FRONTEND-PRESENT, got %+v", f)
	}
}

// ---- DEP-EDGE-GATEWAY ----

// gatewayEnv adds an edge gateway and identity provider to a profile's cluster
// node, with the chain edges the caller asks for. It is the fixture the gateway
// tests vary one link at a time.
func gatewayEnv(env *DeploymentEnvironment, toGateway, toIdP, toApp bool) {
	p := env.Profile
	env.Nodes[0].InfrastructureNodes = append(env.Nodes[0].InfrastructureNodes,
		InfrastructureNode{Key: p + "-gw", Name: "Edge gateway", Technology: "Envoy", Role: roleGateway},
		InfrastructureNode{Key: p + "-idp", Name: "Identity provider", Technology: "Keycloak", Role: roleIdentityProvider},
	)
	var rels []DeploymentRelationship
	if toGateway {
		rels = append(rels, DeploymentRelationship{
			From: p + "-c-AppClient", To: p + "-gw", Label: "Makes API calls to", Technology: "JSON/HTTPS", Mode: modeSync})
	}
	if toIdP {
		rels = append(rels, DeploymentRelationship{
			From: p + "-gw", To: p + "-idp", Label: "Authenticates against", Technology: "OIDC", Mode: modeSync})
	}
	if toApp {
		rels = append(rels, DeploymentRelationship{
			From: p + "-gw", To: p + "-c-DesignManager", Label: "Forwards to", Technology: "HTTP", Mode: modeSync})
	}
	env.Relationships = rels
}

func TestDeploymentEdges_GatewayChainComplete(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	for i := range op.Deployment.Environments {
		gatewayEnv(&op.Deployment.Environments[i], true, true, true)
	}
	if f := deploymentConsistency(op, s); hasRuleFindings(f, ruleDepEdgeGateway) {
		t.Fatalf("a complete front door must not trip DEP-EDGE-GATEWAY, got %+v", f)
	}
}

func TestDeploymentEdges_GatewayWithoutIdentityProviderFlagged(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	for i := range op.Deployment.Environments {
		gatewayEnv(&op.Deployment.Environments[i], true, false, true)
	}
	findings := deploymentConsistency(op, s)
	if !hasRuleFindings(findings, ruleDepEdgeGateway) {
		t.Fatalf("a gateway that authenticates nothing must trip DEP-EDGE-GATEWAY, got %+v", findings)
	}
}

func TestDeploymentEdges_GatewayUnreachedByFrontendFlagged(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	for i := range op.Deployment.Environments {
		gatewayEnv(&op.Deployment.Environments[i], false, true, true)
	}
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepEdgeGateway) {
		t.Fatal("a gateway no frontend reaches must trip DEP-EDGE-GATEWAY")
	}
}

// TestDeploymentEdges_NoGatewayNoChainRequired is the founder's profile gating:
// a topology that binds a loopback port has no gateway and no identity provider,
// and requiring the chain there would make the model state something untrue.
func TestDeploymentEdges_NoGatewayNoChainRequired(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	if hasRuleFindings(deploymentConsistency(op, s), ruleDepEdgeGateway) {
		t.Fatal("an environment with no gateway must not be asked for the gateway chain")
	}
}

// ---- the shipped prototype ----

// TestWebAppBaseline_SatisfiesItsOwnRules is the anti-drift test. The prototype
// the drafting agent starts from and the gate that judges the result are two
// consumers of one definition; if the baseline could not pass the rules, the
// template would be teaching agents to fail.
func TestWebAppBaseline_SatisfiesItsOwnRules(t *testing.T) {
	env := WebAppBaseline()
	containers := WebAppBaselineContainers()
	elements := EnvironmentElements(env, containersByKeyIndex(containers))

	findings := checkEnvironmentEdges(env, 1, containers, containersByKeyIndex(containers), System{})
	for _, f := range findings {
		if f.RuleID == ruleDepEdgeGateway || f.RuleID == ruleDepFrontendPresent ||
			f.RuleID == ruleDepEdgeRef || f.RuleID == ruleDepKeyUnique {
			t.Fatalf("the shipped prototype must satisfy the rules it teaches, got %+v", f)
		}
	}

	var frontends, gateways, idps int
	for _, e := range elements {
		if e.IsFrontend() {
			frontends++
		}
		switch e.Role {
		case roleGateway:
			gateways++
		case roleIdentityProvider:
			idps++
		}
	}
	if frontends < 2 {
		t.Fatalf("the prototype must carry both supported frontend surfaces, got %d", frontends)
	}
	if gateways != 1 || idps != 1 {
		t.Fatalf("the prototype must carry exactly one gateway and one identity provider, got %d/%d", gateways, idps)
	}
}

// hasEdge reports whether the set carries a from→to edge.
func hasEdge(edges []DeploymentRelationship, from, to string) bool {
	for _, e := range edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}
