package methodcheck

import (
	"fmt"
	"sort"
)

// rules_deployment.go ports the deployment-consistency suite owned by
// ValidateOperationalConcepts, FAITHFULLY from predicates_deployment.go. Rule IDs
// / severities / messages are byte-identical.

const (
	ruleDepContainerRef   RuleID = "DEP-CONTAINER-REF"
	ruleDepMemberExist    RuleID = "DEP-MEMBER-EXIST"
	ruleDepProfileSet     RuleID = "DEP-PROFILE-SET"
	ruleDepGraphIdentity  RuleID = "DEP-GRAPH-IDENTITY"
	ruleDepCoverage       RuleID = "DEP-COVERAGE"
	ruleDepNodeWellformed RuleID = "DEP-NODE-WELLFORMED"
)

type envSet struct {
	ordinal int
	set     map[string]bool
}

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

// componentNameIndex maps System component NAME → Component (deployment refs by name).
func componentNameIndex(s System) map[string]Component {
	idx := make(map[string]Component, len(s.Components))
	for _, c := range s.Components {
		idx[c.Name] = c
	}
	return idx
}

// flattenContainerKeys collects every containerInstance key in an env's node tree,
// and reports whether any deployment node has an empty Name.
func flattenContainerKeys(nodes []DeploymentNode) (keys []string, emptyNodeName bool) {
	for _, n := range nodes {
		if n.Name == "" {
			emptyNodeName = true
		}
		for _, ci := range n.ContainerInstances {
			keys = append(keys, ci.ContainerKey)
		}
		childKeys, childEmpty := flattenContainerKeys(n.Children)
		keys = append(keys, childKeys...)
		if childEmpty {
			emptyNodeName = true
		}
	}
	return keys, emptyNodeName
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
	nameIdx := componentNameIndex(s)
	var out []Finding

	// DEP-MEMBER-EXIST: every container member resolves to a System component.
	containersByKey := make(map[string]DeployContainer, len(topo.Containers))
	for _, c := range topo.Containers {
		containersByKey[c.Key] = c
		for _, member := range c.Components {
			if _, ok := nameIdx[member]; !ok {
				out = append(out, Finding{
					RuleID:   ruleDepMemberExist,
					Severity: SeverityError,
					Message:  fmt.Sprintf("container %q packages %q, which is not a System component", c.Key, member),
					Location: loc(0, "deployment topology"),
				})
			}
		}
	}

	byProfile := make(map[string]envSet)
	presentProfiles := make(map[string]bool)
	for i, env := range topo.Environments {
		ordinal := i + 1
		presentProfiles[env.Profile] = true
		covered, envFindings := checkDeploymentEnvironment(env, ordinal, containersByKey)
		out = append(out, envFindings...)
		byProfile[env.Profile] = envSet{ordinal: ordinal, set: covered}
	}
	out = append(out, checkProfileSets(presentProfiles, expectedProfiles(topo.DeliveryStyle))...)
	out = append(out, checkCrossProfileCoverage(byProfile, internalComponentNames(s))...)
	return out
}

// checkDeploymentEnvironment validates container-key refs and returns the SET of
// System component NAMES covered by the containers instanced in this env.
func checkDeploymentEnvironment(env DeploymentEnvironment, ordinal int, containersByKey map[string]DeployContainer) (map[string]bool, []Finding) {
	section := fmt.Sprintf("deployment environment %q", profileName(env.Profile))
	var out []Finding
	keys, emptyNodeName := flattenContainerKeys(env.Nodes)
	if emptyNodeName {
		out = append(out, Finding{RuleID: ruleDepNodeWellformed, Severity: SeverityError,
			Message: fmt.Sprintf("%s: a deployment node has an empty Name", section), Location: loc(ordinal, section)})
	}
	if len(keys) == 0 {
		out = append(out, Finding{RuleID: ruleDepNodeWellformed, Severity: SeverityError,
			Message: fmt.Sprintf("%s: environment instances no containers", section), Location: loc(ordinal, section)})
	}
	covered := make(map[string]bool)
	for _, key := range keys {
		c, ok := containersByKey[key]
		if !ok {
			out = append(out, Finding{RuleID: ruleDepContainerRef, Severity: SeverityError,
				Message: fmt.Sprintf("%s: containerInstance %q does not reference a declared container", section, key), Location: loc(ordinal, section)})
			continue
		}
		for _, member := range c.Components {
			covered[member] = true
		}
	}
	return covered, out
}

func checkProfileSets(presentProfiles, expected map[string]bool) []Finding {
	var out []Finding
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
	return out
}

func internalComponentNames(s System) map[string]bool {
	internal := make(map[string]bool)
	for _, c := range s.Components {
		if isRunningComponent(c.Kind) {
			internal[c.Name] = true
		}
	}
	return internal
}

func checkCrossProfileCoverage(byProfile map[string]envSet, internal map[string]bool) []Finding {
	var out []Finding
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
		out = append(out, checkTestEnvCoverage(test, internal)...)
	}
	for _, p := range []string{profileCloud, profileLocal} {
		env, ok := byProfile[p]
		if !ok {
			continue
		}
		out = append(out, checkProfileEnvCoverage(p, env, internal)...)
	}
	return out
}

func checkTestEnvCoverage(test envSet, internal map[string]bool) []Finding {
	var missing []string
	for id := range internal {
		if !test.set[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return []Finding{{
		RuleID:   ruleDepGraphIdentity,
		Severity: SeverityError,
		Message:  fmt.Sprintf("the test environment does not instance every internal component; test must be a superset of the internal set (missing=%v)", missing),
		Location: loc(test.ordinal, fmt.Sprintf("deployment environment %q", profileName(profileTest))),
	}}
}

func checkProfileEnvCoverage(p string, env envSet, internal map[string]bool) []Finding {
	var missing []string
	for id := range internal {
		if !env.set[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return []Finding{{
		RuleID:   ruleDepCoverage,
		Severity: SeverityWarning,
		Message:  fmt.Sprintf("deployment environment %q does not instance every running component (missing=%v)", profileName(p), missing),
		Location: loc(env.ordinal, fmt.Sprintf("deployment environment %q", profileName(p))),
	}}
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
