package methodcheck

import "testing"

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

func envInstancingAll(profile, title string, containers []DeployContainer) DeploymentEnvironment {
	var instances []ContainerInstance
	for _, c := range containers {
		instances = append(instances, ContainerInstance{ContainerKey: c.Key})
	}
	return DeploymentEnvironment{
		Profile: profile, Title: title,
		Nodes: []DeploymentNode{{Name: "cluster", Technology: "k8s", ContainerInstances: instances}},
	}
}

func deploymentBaseOC(t *testing.T, s System) OperationalConcepts {
	t.Helper()
	containers := deploymentBaseContainers(s)
	return OperationalConcepts{Deployment: DeploymentTopology{
		DeliveryStyle: styleBoth,
		Containers:    containers,
		Environments: []DeploymentEnvironment{
			envInstancingAll(profileCloud, "Cloud", containers),
			envInstancingAll(profileLocal, "Local", containers),
			envInstancingAll(profileTest, "Test", containers),
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
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepProfileSet) {
		t.Fatalf("expected DEP-PROFILE-SET for unexpected local under StyleCloud")
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
	op.Deployment.Environments[2].Nodes[0].ContainerInstances = insts[:len(insts)-1]
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepGraphIdentity) {
		t.Fatalf("expected DEP-GRAPH-IDENTITY for test missing an internal component")
	}
}

func TestDeploymentConsistency_Coverage_IsWarning(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	drop := func(env *DeploymentEnvironment) {
		insts := env.Nodes[0].ContainerInstances
		env.Nodes[0].ContainerInstances = insts[:len(insts)-1]
	}
	drop(&op.Deployment.Environments[0])
	drop(&op.Deployment.Environments[1])
	drop(&op.Deployment.Environments[2])
	findings := deploymentConsistency(op, s)
	sev, ok := findingSeverity(findings, ruleDepCoverage)
	if !ok {
		t.Fatalf("expected DEP-COVERAGE finding, got %+v", findings)
	}
	if sev != SeverityWarning {
		t.Fatalf("DEP-COVERAGE must be Warning, got %v", sev)
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
				Children: []DeploymentNode{{Name: "namespace", ContainerInstances: childInst}},
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
