package methodcheck

import (
	"fmt"
	"sort"
)

// rules_deployment.go ports the deployment-consistency suite owned by
// ValidateOperationalConcepts, FAITHFULLY from predicates_deployment.go. Rule IDs
// / severities / messages are byte-identical.

const (
	ruleDepInstanceExist  RuleID = "DEP-INSTANCE-EXIST"
	ruleDepProfileSet     RuleID = "DEP-PROFILE-SET"
	ruleDepGraphIdentity  RuleID = "DEP-GRAPH-IDENTITY"
	ruleDepCoverage       RuleID = "DEP-COVERAGE"
	ruleDepNodeWellformed RuleID = "DEP-NODE-WELLFORMED"
)

func profileName(p string) string {
	switch p {
	case profileCloud:
		return "cloud"
	case profileLocal:
		return "local"
	case profileTest:
		return "test"
	default:
		return fmt.Sprintf("profile(%q)", p)
	}
}

// expectedProfiles returns the SET of profiles required for a delivery style:
// cloud→{cloud,test}, local→{local,test}, both→{cloud,local,test}. Ported verbatim.
func expectedProfiles(style string) map[string]bool {
	set := map[string]bool{profileTest: true}
	switch style {
	case styleCloud:
		set[profileCloud] = true
	case styleLocal:
		set[profileLocal] = true
	case styleBoth:
		set[profileCloud] = true
		set[profileLocal] = true
	}
	return set
}

func flattenInstances(nodes []DeploymentNode) (instances []ContainerInstance, emptyNodeName bool) {
	for _, n := range nodes {
		if n.Name == "" {
			emptyNodeName = true
		}
		instances = append(instances, n.Instances...)
		childInst, childEmpty := flattenInstances(n.Children)
		instances = append(instances, childInst...)
		if childEmpty {
			emptyNodeName = true
		}
	}
	return instances, emptyNodeName
}

// isRunningComponent reports whether a component kind runs as a deployed container
// (everything except Utility). Ported verbatim.
func isRunningComponent(k string) bool {
	switch k {
	case kindClient, kindManager, kindEngine, kindResourceAccess, kindResource:
		return true
	default:
		return false
	}
}

func sortedComponentIDs(set map[string]bool) []string {
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func deploymentConsistency(op OperationalConcepts, s System) []Finding {
	topo := op.Deployment

	if len(topo.Environments) == 0 {
		return nil
	}

	idx := componentIndex(s)

	var out []Finding

	type envSet struct {
		ordinal int
		set     map[string]bool
	}
	byProfile := make(map[string]envSet)
	presentProfiles := make(map[string]bool)

	for i, env := range topo.Environments {
		ordinal := i + 1
		section := fmt.Sprintf("deployment environment %q", profileName(env.Profile))
		presentProfiles[env.Profile] = true

		instances, emptyNodeName := flattenInstances(env.Nodes)

		if emptyNodeName {
			out = append(out, Finding{
				RuleID:   ruleDepNodeWellformed,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: a deployment node has an empty Name", section),
				Location: loc(ordinal, section),
			})
		}

		if len(instances) == 0 {
			out = append(out, Finding{
				RuleID:   ruleDepNodeWellformed,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: environment instances no components", section),
				Location: loc(ordinal, section),
			})
		}

		set := make(map[string]bool, len(instances))
		for _, inst := range instances {
			if _, ok := idx[inst.ComponentID]; !ok {
				out = append(out, Finding{
					RuleID:   ruleDepInstanceExist,
					Severity: SeverityError,
					Message:  fmt.Sprintf("%s: instance %s does not reference a System Component", section, inst.ComponentID),
					Location: loc(ordinal, section),
				})
			}
			if set[inst.ComponentID] {
				out = append(out, Finding{
					RuleID:   ruleDepNodeWellformed,
					Severity: SeverityError,
					Message:  fmt.Sprintf("%s: component %s is instanced more than once within the environment", section, inst.ComponentID),
					Location: loc(ordinal, section),
				})
			}
			set[inst.ComponentID] = true
		}

		byProfile[env.Profile] = envSet{ordinal: ordinal, set: set}
	}

	expected := expectedProfiles(topo.DeliveryStyle)
	for p := range expected {
		if !presentProfiles[p] {
			out = append(out, Finding{
				RuleID:   ruleDepProfileSet,
				Severity: SeverityError,
				Message:  fmt.Sprintf("delivery style requires a %q deployment environment but it is missing", profileName(p)),
				Location: loc(0, "deployment topology"),
			})
		}
	}
	for p := range presentProfiles {
		if !expected[p] {
			out = append(out, Finding{
				RuleID:   ruleDepProfileSet,
				Severity: SeverityError,
				Message:  fmt.Sprintf("deployment has an unexpected %q environment for the chosen delivery style", profileName(p)),
				Location: loc(0, "deployment topology"),
			})
		}
	}

	internal := make(map[string]bool)
	for _, c := range s.Components {
		if isRunningComponent(c.Kind) {
			internal[c.ID] = true
		}
	}

	cloud, hasCloud := byProfile[profileCloud]
	local, hasLocal := byProfile[profileLocal]
	test, hasTest := byProfile[profileTest]

	if hasCloud && hasLocal && !sameSet(cloud.set, local.set) {
		out = append(out, Finding{
			RuleID:   ruleDepGraphIdentity,
			Severity: SeverityError,
			Message:  fmt.Sprintf("the deployed component set differs between cloud and local environments; the component graph must be identical across profiles (cloud=%v local=%v)", sortedComponentIDs(cloud.set), sortedComponentIDs(local.set)),
			Location: loc(0, "deployment topology"),
		})
	}

	if hasTest {
		var missing []string
		for id := range internal {
			if !test.set[id] {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			out = append(out, Finding{
				RuleID:   ruleDepGraphIdentity,
				Severity: SeverityError,
				Message:  fmt.Sprintf("the test environment does not instance every internal component; test must be a superset of the internal set (missing=%v)", missing),
				Location: loc(test.ordinal, fmt.Sprintf("deployment environment %q", profileName(profileTest))),
			})
		}
	}

	for _, p := range []string{profileCloud, profileLocal} {
		env, ok := byProfile[p]
		if !ok {
			continue
		}
		var missing []string
		for id := range internal {
			if !env.set[id] {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			out = append(out, Finding{
				RuleID:   ruleDepCoverage,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("deployment environment %q does not instance every running component (missing=%v)", profileName(p), missing),
				Location: loc(env.ordinal, fmt.Sprintf("deployment environment %q", profileName(p))),
			})
		}
	}

	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if !b[id] {
			return false
		}
	}
	return true
}
