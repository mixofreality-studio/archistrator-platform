package methodcheck

import (
	"strings"
	"testing"
)

// rules_deployment_test.go PORTS predicates_deployment_test.go to the C4 container
// deployment model: System components are packaged into DeployContainers (by name),
// and DeploymentNodes instance containers (by key) rather than components directly.

func deploymentBaseSystem(t *testing.T) System {
	t.Helper()
	return System{Components: []Component{
		comp(t, "AppClient", kindClient),
		comp(t, "DesignManager", kindManager),
		comp(t, "ValidatingEngine", kindEngine),
		comp(t, "StateAccess", kindResourceAccess),
		comp(t, "StateDB", kindResource),
	}}
}

// deploymentBaseContainers packages each System component into its own container,
// keyed by a slug of the component name.
func deploymentBaseContainers(s System) []DeployContainer {
	containers := make([]DeployContainer, 0, len(s.Components))
	for _, c := range s.Components {
		containers = append(containers, DeployContainer{
			Key:        containerKey(c.Name),
			Name:       c.Name,
			Components: []string{c.Name},
		})
	}
	return containers
}

func containerKey(name string) string {
	return "c-" + name
}

// resourceInfraNodes represents every System Resource as an InfrastructureNode
// named after the component, so a base deployment satisfies DEP-RESOURCE-PRESENT
// (Resources deploy as infrastructure, slug-matched to their System component).
func resourceInfraNodes(s System) []InfrastructureNode {
	var infra []InfrastructureNode
	for _, c := range s.Components {
		if c.Kind == kindResource {
			infra = append(infra, InfrastructureNode{Name: c.Name})
		}
	}
	return infra
}

func envInstancingAll(profile, title string, containers []DeployContainer, infra []InfrastructureNode) DeploymentEnvironment {
	var instances []ContainerInstance
	for _, c := range containers {
		instances = append(instances, ContainerInstance{ContainerKey: c.Key})
	}
	return DeploymentEnvironment{
		Profile: profile, Title: title,
		Nodes: []DeploymentNode{{Name: "cluster", Technology: "k8s", ContainerInstances: instances, InfrastructureNodes: infra}},
	}
}

func deploymentBaseOC(t *testing.T, s System) OperationalConcepts {
	t.Helper()
	containers := deploymentBaseContainers(s)
	infra := resourceInfraNodes(s)
	return OperationalConcepts{Deployment: DeploymentTopology{
		DeliveryStyle: styleBoth,
		Containers:    containers,
		Environments: []DeploymentEnvironment{
			envInstancingAll(profileCloud, "Cloud", containers, infra),
			envInstancingAll(profileLocal, "Local", containers, infra),
			envInstancingAll(profileTest, "Test", containers, infra),
		},
	}}
}

func TestDeploymentConsistency_ValidBaseHasNoFindings(t *testing.T) {
	s := deploymentBaseSystem(t)
	if f := deploymentConsistency(deploymentBaseOC(t, s), s); len(f) != 0 {
		t.Fatalf("a fully legal deployment must produce zero findings, got %+v", f)
	}
}

func TestDeployment_ContainerRefMustResolve(t *testing.T) {
	s := System{Components: []Component{{ID: "billing-manager", Name: "Billing Manager", Kind: kindManager}}}
	op := OperationalConcepts{Deployment: DeploymentTopology{
		DeliveryStyle: styleCloud,
		Containers:    []DeployContainer{{Key: "server", Name: "server", Components: []string{"Billing Manager"}}},
		Environments: []DeploymentEnvironment{
			{Profile: profileCloud, Title: "Cloud", Nodes: []DeploymentNode{
				{Name: "ns", ContainerInstances: []ContainerInstance{{ContainerKey: "MISSING"}}}}},
			{Profile: profileTest, Title: "Test", Nodes: []DeploymentNode{
				{Name: "p", ContainerInstances: []ContainerInstance{{ContainerKey: "server"}}}}},
		},
	}}
	got := deploymentConsistency(op, s)
	if !hasRuleFindings(got, ruleDepContainerRef) {
		t.Fatalf("expected DEP-CONTAINER-REF for missing container, got %v", got)
	}
}

func TestDeployment_MemberMustBeSystemComponent(t *testing.T) {
	s := System{Components: []Component{{ID: "billing-manager", Name: "Billing Manager", Kind: kindManager}}}
	op := OperationalConcepts{Deployment: DeploymentTopology{
		DeliveryStyle: styleCloud,
		Containers:    []DeployContainer{{Key: "server", Name: "server", Components: []string{"Ghost Manager"}}},
		Environments: []DeploymentEnvironment{
			{Profile: profileCloud, Title: "Cloud", Nodes: []DeploymentNode{{Name: "ns", ContainerInstances: []ContainerInstance{{ContainerKey: "server"}}}}},
			{Profile: profileTest, Title: "Test", Nodes: []DeploymentNode{{Name: "p", ContainerInstances: []ContainerInstance{{ContainerKey: "server"}}}}},
		},
	}}
	got := deploymentConsistency(op, s)
	if !hasRuleFindings(got, ruleDepMemberExist) {
		t.Fatalf("expected DEP-MEMBER-EXIST for unknown component, got %v", got)
	}
}

func TestDeploymentConsistency_ProfileSet_Missing(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	op.Deployment.Environments = []DeploymentEnvironment{op.Deployment.Environments[0], op.Deployment.Environments[2]}
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepProfileSet) {
		t.Fatalf("expected DEP-PROFILE-SET for missing local")
	}
}

func TestDeploymentConsistency_ProfileSet_Unexpected(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	op.Deployment.DeliveryStyle = styleCloud
	// "local" is now the recognized DEV-BOOT profile — always permitted, never
	// required, for any delivery style (see isDevBootProfile in
	// rules_deployment.go) — so it is no longer an "unexpected profile" under
	// StyleCloud. Swap it for a genuinely unknown profile to keep probing
	// DEP-PROFILE-SET's unexpected-profile detection; local-under-cloud
	// tolerance itself is covered by TestDeployment_DevBootLocal_EmptyNodesPasses.
	op.Deployment.Environments[1].Profile = "prod"
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepProfileSet) {
		t.Fatalf("expected DEP-PROFILE-SET for unexpected \"prod\" environment under StyleCloud")
	}
}

func TestDeploymentConsistency_GraphIdentity_CloudLocalDiffer(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	insts := op.Deployment.Environments[1].Nodes[0].ContainerInstances
	op.Deployment.Environments[1].Nodes[0].ContainerInstances = insts[:len(insts)-1]
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepGraphIdentity) {
		t.Fatalf("expected DEP-GRAPH-IDENTITY for cloud/local divergence")
	}
}

func TestDeploymentConsistency_GraphIdentity_TestMissingInternal(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	insts := op.Deployment.Environments[2].Nodes[0].ContainerInstances
	// Drop the FIRST instance (AppClient, a CODE component) rather than the last
	// (StateDB, a Resource) — Resources are no longer container-coverage-required,
	// so dropping one must not (and, per the sibling test below, does not) trip
	// DEP-GRAPH-IDENTITY. A missing CODE component still must.
	op.Deployment.Environments[2].Nodes[0].ContainerInstances = insts[1:]
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepGraphIdentity) {
		t.Fatalf("expected DEP-GRAPH-IDENTITY for test missing an internal (code) component")
	}
}

// TestDeploymentConsistency_TestMissingResourceOnly_NotFlagged proves the coverage
// relaxation: Resources are deployment infrastructure (a CNPG cluster in cloud, a
// docker container locally) that deploy separately from the server code, so a test
// environment missing only a Resource container must NOT trip DEP-GRAPH-IDENTITY or
// DEP-COVERAGE — only missing CODE components (Client/Manager/Engine/ResourceAccess)
// are coverage-required.
func TestDeploymentConsistency_TestMissingResourceOnly_NotFlagged(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	insts := op.Deployment.Environments[2].Nodes[0].ContainerInstances
	op.Deployment.Environments[2].Nodes[0].ContainerInstances = insts[:len(insts)-1] // drop StateDB (Resource)
	if f := deploymentConsistency(op, s); len(f) != 0 {
		t.Fatalf("a test env missing only a Resource must produce zero findings, got %+v", f)
	}
}

func TestDeploymentConsistency_Coverage_IsError(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	// Drop the FIRST instance (AppClient, a CODE component) in the cloud env only —
	// leaving local+test intact keeps the graph-identity check from also firing so
	// we observe DEP-COVERAGE in isolation. (Resources are exempt, so dropping the
	// last (StateDB) would NOT trip DEP-COVERAGE.)
	insts := op.Deployment.Environments[0].Nodes[0].ContainerInstances
	op.Deployment.Environments[0].Nodes[0].ContainerInstances = insts[1:]
	findings := deploymentConsistency(op, s)
	sev, ok := findingSeverity(findings, ruleDepCoverage)
	if !ok {
		t.Fatalf("expected DEP-COVERAGE finding, got %+v", findings)
	}
	if sev != SeverityError {
		t.Fatalf("DEP-COVERAGE must be Error (founder requirement), got %v", sev)
	}
}

func TestDeployment_ContainerUsed_DeclaredButNeverInstanced(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	// Declare an extra container that no environment instances.
	op.Deployment.Containers = append(op.Deployment.Containers, DeployContainer{
		Key: "orphan", Name: "orphan", Components: []string{"DesignManager"},
	})
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepContainerUsed) {
		t.Fatalf("expected DEP-CONTAINER-USED for a declared-but-never-instanced container")
	}
}

func TestDeployment_ContainerUsed_AllInstancedPasses(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	if hasRuleFindings(deploymentConsistency(op, s), ruleDepContainerUsed) {
		t.Fatalf("every base container is instanced; DEP-CONTAINER-USED must not fire")
	}
}

func TestDeployment_MemberExclusive_SameComponentInTwoContainers(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	// Package DesignManager into a SECOND container in addition to its own.
	dup := DeployContainer{Key: "extra", Name: "extra", Components: []string{"DesignManager"}}
	op.Deployment.Containers = append(op.Deployment.Containers, dup)
	op.Deployment.Environments[0].Nodes[0].ContainerInstances = append(
		op.Deployment.Environments[0].Nodes[0].ContainerInstances, ContainerInstance{ContainerKey: "extra"})
	op.Deployment.Environments[1].Nodes[0].ContainerInstances = append(
		op.Deployment.Environments[1].Nodes[0].ContainerInstances, ContainerInstance{ContainerKey: "extra"})
	op.Deployment.Environments[2].Nodes[0].ContainerInstances = append(
		op.Deployment.Environments[2].Nodes[0].ContainerInstances, ContainerInstance{ContainerKey: "extra"})
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepMemberExclusive) {
		t.Fatalf("expected DEP-MEMBER-EXCLUSIVE for a component packaged by two containers")
	}
}

func TestDeployment_MemberExclusive_ReplicaInstancingAllowed(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	// Instance the SAME container a second time in the cloud env (a horizontal
	// replica). Membership is unchanged, so DEP-MEMBER-EXCLUSIVE must NOT fire.
	first := op.Deployment.Environments[0].Nodes[0].ContainerInstances[0]
	op.Deployment.Environments[0].Nodes[0].ContainerInstances = append(
		op.Deployment.Environments[0].Nodes[0].ContainerInstances, first)
	if hasRuleFindings(deploymentConsistency(op, s), ruleDepMemberExclusive) {
		t.Fatalf("replica instancing of one container is allowed; DEP-MEMBER-EXCLUSIVE must not fire")
	}
}

// resourcePresentOC builds a topology whose cloud+test profiles name the StateDB
// Resource via an infrastructure node (slug-matched), so DEP-RESOURCE-PRESENT is
// satisfied for the base system.
func resourcePresentOC(t *testing.T, s System) OperationalConcepts {
	t.Helper()
	op := deploymentBaseOC(t, s)
	op.Deployment.DeliveryStyle = styleCloud // required profiles: {cloud, test}
	op.Deployment.Environments = []DeploymentEnvironment{
		op.Deployment.Environments[0], // cloud
		op.Deployment.Environments[2], // test
	}
	for i := range op.Deployment.Environments {
		op.Deployment.Environments[i].Nodes[0].InfrastructureNodes = []InfrastructureNode{{Name: "StateDB"}}
	}
	return op
}

func TestDeployment_ResourcePresent_NamedInfraPasses(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := resourcePresentOC(t, s)
	if hasRuleFindings(deploymentConsistency(op, s), ruleDepResourcePresent) {
		t.Fatalf("a Resource named by an infrastructure node in every required profile must not trip DEP-RESOURCE-PRESENT")
	}
}

func TestDeployment_ResourcePresent_MissingInfraWarns(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := resourcePresentOC(t, s)
	// Remove the infra node naming StateDB from the cloud profile.
	op.Deployment.Environments[0].Nodes[0].InfrastructureNodes = nil
	findings := deploymentConsistency(op, s)
	sev, ok := findingSeverity(findings, ruleDepResourcePresent)
	if !ok {
		t.Fatalf("expected DEP-RESOURCE-PRESENT when a Resource is un-named in a required profile, got %+v", findings)
	}
	if sev != SeverityWarning {
		t.Fatalf("DEP-RESOURCE-PRESENT must be Warning for now, got %v", sev)
	}
}

// TestDeploymentConsistency_Coverage_MissingResourceOnly_NotFlagged proves a
// Resource missing from a cloud/local deployment environment does NOT trip
// DEP-COVERAGE — Resources deploy separately as infrastructure (e.g. a CNPG
// cluster in cloud, a docker container locally), not packaged inside a server
// deployment container.
func TestDeploymentConsistency_Coverage_MissingResourceOnly_NotFlagged(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	drop := func(env *DeploymentEnvironment) {
		insts := env.Nodes[0].ContainerInstances
		env.Nodes[0].ContainerInstances = insts[:len(insts)-1] // drop StateDB (Resource)
	}
	drop(&op.Deployment.Environments[0])
	drop(&op.Deployment.Environments[1])
	drop(&op.Deployment.Environments[2])
	// Removing the Resource's container everywhere means the container is no longer
	// declared either (else DEP-CONTAINER-USED would rightly flag the orphan) — a
	// Resource simply is not packaged as a container in this topology.
	var kept []DeployContainer
	for _, c := range op.Deployment.Containers {
		if c.Key != containerKey("StateDB") {
			kept = append(kept, c)
		}
	}
	op.Deployment.Containers = kept
	if f := deploymentConsistency(op, s); len(f) != 0 {
		t.Fatalf("a Resource missing across all envs must produce zero findings, got %+v", f)
	}
}

func TestDeploymentConsistency_NodeWellformed_EmptyName(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	op.Deployment.Environments[0].Nodes[0].Name = ""
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepNodeWellformed) {
		t.Fatalf("expected DEP-NODE-WELLFORMED for empty node name")
	}
}

func TestDeploymentConsistency_NodeWellformed_EmptyEnvironment(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	op.Deployment.Environments[0].Nodes = []DeploymentNode{{Name: "cluster", Technology: "k8s"}}
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepNodeWellformed) {
		t.Fatalf("expected DEP-NODE-WELLFORMED for empty environment")
	}
}

// devBootLocalOC builds a cloud-delivery-style topology (required profiles:
// {cloud, test}) with an ADDITIONAL "local" environment representing the
// dev-boot profile — not an operated topology, so its node tree may be
// sparse/minimal (even empty).
func devBootLocalOC(t *testing.T, s System, localNodes []DeploymentNode) OperationalConcepts {
	t.Helper()
	containers := deploymentBaseContainers(s)
	infra := resourceInfraNodes(s)
	return OperationalConcepts{Deployment: DeploymentTopology{
		DeliveryStyle: styleCloud,
		Containers:    containers,
		Environments: []DeploymentEnvironment{
			envInstancingAll(profileCloud, "Cloud", containers, infra),
			envInstancingAll(profileTest, "Test", containers, infra),
			{Profile: profileLocal, Title: "Local", Nodes: localNodes},
		},
	}}
}

// TestDeployment_DevBootLocal_EmptyNodesPasses proves the local dev-boot
// profile is recognized under a cloud delivery style (not required, not
// coverage-checked) and its node tree may be entirely empty with zero
// DEP-* findings.
func TestDeployment_DevBootLocal_EmptyNodesPasses(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := devBootLocalOC(t, s, nil)
	if f := deploymentConsistency(op, s); len(f) != 0 {
		t.Fatalf("a cloud-style deployment with a minimal (empty) local dev-boot environment must produce zero findings, got %+v", f)
	}
}

// TestDeployment_DevBootLocal_BareNodePasses covers the other minimal shape —
// a single bare node with no container instances.
func TestDeployment_DevBootLocal_BareNodePasses(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := devBootLocalOC(t, s, []DeploymentNode{{Name: "dev"}})
	if f := deploymentConsistency(op, s); len(f) != 0 {
		t.Fatalf("a cloud-style deployment with a bare-node local dev-boot environment must produce zero findings, got %+v", f)
	}
}

// TestDeploymentConsistency_ProfileSet_UnknownProfileStillFatals proves the
// local dev-boot tolerance is NOT a general "any extra profile is fine"
// relaxation — a typo'd/unknown profile must still trip DEP-PROFILE-SET.
func TestDeploymentConsistency_ProfileSet_UnknownProfileStillFatals(t *testing.T) {
	s := deploymentBaseSystem(t)
	containers := deploymentBaseContainers(s)
	infra := resourceInfraNodes(s)
	op := OperationalConcepts{Deployment: DeploymentTopology{
		DeliveryStyle: styleCloud,
		Containers:    containers,
		Environments: []DeploymentEnvironment{
			envInstancingAll(profileCloud, "Cloud", containers, infra),
			envInstancingAll(profileTest, "Test", containers, infra),
			envInstancingAll("staging", "Staging", containers, infra),
		},
	}}
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepProfileSet) {
		t.Fatalf("expected DEP-PROFILE-SET for an unknown \"staging\" profile")
	}
}

// TestDeployment_DevBootLocal_ExemptFromCoverageButCloudStillFires proves the
// local dev-boot environment produces NO coverage findings even when it is
// missing components the cloud environment requires, while the cloud
// environment's OWN coverage violation still fires.
func TestDeployment_DevBootLocal_ExemptFromCoverageButCloudStillFires(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := devBootLocalOC(t, s, []DeploymentNode{{Name: "dev"}}) // local: bare node, packages nothing
	// Break cloud's coverage by dropping its first container instance (AppClient).
	insts := op.Deployment.Environments[0].Nodes[0].ContainerInstances
	op.Deployment.Environments[0].Nodes[0].ContainerInstances = insts[1:]
	findings := deploymentConsistency(op, s)
	sev, ok := findingSeverity(findings, ruleDepCoverage)
	if !ok {
		t.Fatalf("expected DEP-COVERAGE for the cloud environment's own missing coverage, got %+v", findings)
	}
	if sev != SeverityError {
		t.Fatalf("DEP-COVERAGE must be Error, got %v", sev)
	}
	for _, f := range findings {
		if f.RuleID == ruleDepCoverage || f.RuleID == ruleDepGraphIdentity || f.RuleID == ruleDepNodeWellformed {
			if strings.Contains(f.Message, `"local"`) {
				t.Fatalf("the local dev-boot environment must be exempt from coverage/identity/node-wellformed checks, got %+v", f)
			}
		}
	}
}

func TestDeploymentConsistency_FlattensNestedNodes(t *testing.T) {
	s := deploymentBaseSystem(t)
	containers := deploymentBaseContainers(s)
	half := len(containers) / 2
	nestedEnv := func(profile string) DeploymentEnvironment {
		var parentInst, childInst []ContainerInstance
		for i, c := range containers {
			ci := ContainerInstance{ContainerKey: c.Key}
			if i < half {
				parentInst = append(parentInst, ci)
			} else {
				childInst = append(childInst, ci)
			}
		}
		return DeploymentEnvironment{
			Profile: profile,
			Nodes: []DeploymentNode{{
				Name: "cluster", ContainerInstances: parentInst,
				InfrastructureNodes: resourceInfraNodes(s),
				Children:            []DeploymentNode{{Name: "namespace", ContainerInstances: childInst}},
			}},
		}
	}
	op := OperationalConcepts{Deployment: DeploymentTopology{
		DeliveryStyle: styleBoth,
		Containers:    containers,
		Environments:  []DeploymentEnvironment{nestedEnv(profileCloud), nestedEnv(profileLocal), nestedEnv(profileTest)},
	}}
	if f := deploymentConsistency(op, s); len(f) != 0 {
		t.Fatalf("nested-node instances must flatten cleanly with zero findings, got %+v", f)
	}
}
