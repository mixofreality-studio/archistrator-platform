package methodcheck

import "testing"

// rules_deployment_test.go PORTS predicates_deployment_test.go to structural structs.

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

func envInstancingAll(profile, title string, s System) DeploymentEnvironment {
	var instances []ContainerInstance
	for _, c := range s.Components {
		instances = append(instances, ContainerInstance{ComponentID: c.ID})
	}
	return DeploymentEnvironment{
		Profile: profile, Title: title,
		Nodes: []DeploymentNode{{Name: "cluster", Technology: "k8s", Instances: instances}},
	}
}

func deploymentBaseOC(t *testing.T, s System) OperationalConcepts {
	t.Helper()
	return OperationalConcepts{Deployment: DeploymentTopology{
		DeliveryStyle: styleBoth,
		Environments: []DeploymentEnvironment{
			envInstancingAll(profileCloud, "Cloud", s),
			envInstancingAll(profileLocal, "Local", s),
			envInstancingAll(profileTest, "Test", s),
		},
	}}
}

func TestDeploymentConsistency_ValidBaseHasNoFindings(t *testing.T) {
	s := deploymentBaseSystem(t)
	if f := deploymentConsistency(deploymentBaseOC(t, s), s); len(f) != 0 {
		t.Fatalf("a fully legal deployment must produce zero findings, got %+v", f)
	}
}

func TestDeploymentConsistency_InstanceExist(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	op.Deployment.Environments[0].Nodes[0].Instances = append(
		op.Deployment.Environments[0].Nodes[0].Instances, ContainerInstance{ComponentID: nid()})
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepInstanceExist) {
		t.Fatalf("expected DEP-INSTANCE-EXIST")
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
	insts := op.Deployment.Environments[1].Nodes[0].Instances
	op.Deployment.Environments[1].Nodes[0].Instances = insts[:len(insts)-1]
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepGraphIdentity) {
		t.Fatalf("expected DEP-GRAPH-IDENTITY for cloud/local divergence")
	}
}

func TestDeploymentConsistency_GraphIdentity_TestMissingInternal(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	insts := op.Deployment.Environments[2].Nodes[0].Instances
	op.Deployment.Environments[2].Nodes[0].Instances = insts[:len(insts)-1]
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepGraphIdentity) {
		t.Fatalf("expected DEP-GRAPH-IDENTITY for test missing an internal component")
	}
}

func TestDeploymentConsistency_Coverage_IsWarning(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	drop := func(env *DeploymentEnvironment) {
		insts := env.Nodes[0].Instances
		env.Nodes[0].Instances = insts[:len(insts)-1]
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

func TestDeploymentConsistency_NodeWellformed_DuplicateInstance(t *testing.T) {
	s := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, s)
	dupID := s.Components[0].ID
	op.Deployment.Environments[0].Nodes[0].Instances = append(
		op.Deployment.Environments[0].Nodes[0].Instances, ContainerInstance{ComponentID: dupID})
	if !hasRuleFindings(deploymentConsistency(op, s), ruleDepNodeWellformed) {
		t.Fatalf("expected DEP-NODE-WELLFORMED for duplicate instance")
	}
}

func TestDeploymentConsistency_FlattensNestedNodes(t *testing.T) {
	s := deploymentBaseSystem(t)
	nestedEnv := func(profile string) DeploymentEnvironment {
		half := len(s.Components) / 2
		var parentInst, childInst []ContainerInstance
		for i, c := range s.Components {
			ci := ContainerInstance{ComponentID: c.ID}
			if i < half {
				parentInst = append(parentInst, ci)
			} else {
				childInst = append(childInst, ci)
			}
		}
		return DeploymentEnvironment{
			Profile: profile,
			Nodes: []DeploymentNode{{
				Name: "cluster", Instances: parentInst,
				Children: []DeploymentNode{{Name: "namespace", Instances: childInst}},
			}},
		}
	}
	op := OperationalConcepts{Deployment: DeploymentTopology{
		DeliveryStyle: styleBoth,
		Environments:  []DeploymentEnvironment{nestedEnv(profileCloud), nestedEnv(profileLocal), nestedEnv(profileTest)},
	}}
	if f := deploymentConsistency(op, s); len(f) != 0 {
		t.Fatalf("nested-node instances must flatten cleanly with zero findings, got %+v", f)
	}
}
