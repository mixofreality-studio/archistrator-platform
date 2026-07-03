package methodcheck

import (
	"fmt"
	"sort"
)

// rules_deployment.go ports the deployment-consistency suite owned by
// ValidateOperationalConcepts, FAITHFULLY from predicates_deployment.go. Rule IDs
// / severities / messages are byte-identical.

const (
	ruleDepContainerRef    RuleID = "DEP-CONTAINER-REF"
	ruleDepMemberExist     RuleID = "DEP-MEMBER-EXIST"
	ruleDepProfileSet      RuleID = "DEP-PROFILE-SET"
	ruleDepGraphIdentity   RuleID = "DEP-GRAPH-IDENTITY"
	ruleDepCoverage        RuleID = "DEP-COVERAGE"
	ruleDepNodeWellformed  RuleID = "DEP-NODE-WELLFORMED"
	ruleDepContainerUsed   RuleID = "DEP-CONTAINER-USED"
	ruleDepMemberExclusive RuleID = "DEP-MEMBER-EXCLUSIVE"
	ruleDepResourcePresent RuleID = "DEP-RESOURCE-PRESENT"
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
// and reports whether any deployment node has an empty Name. Note: a container
// legitimately instanced in more than one node/env represents horizontal
// replicas and is intentionally NOT flagged — this was dropped on purpose
// from the old per-component model (see checkDeploymentEnvironment below).
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

// isCoreComponentKind reports whether a component kind is a first-class,
// code-implemented runtime component — Client, Manager, Engine, ResourceAccess.
// Resources (physical stores / external systems) and Utilities (ambient
// cross-cutting infrastructure) are the two exempt kinds. This is the SINGLE
// Resources/Utilities-exemption predicate reused across the deployment,
// dynamic-view, and conformance suites so the exemption rationale lives in one
// place and cannot drift between them.
func isCoreComponentKind(k string) bool {
	switch k {
	case kindClient, kindManager, kindEngine, kindResourceAccess:
		return true
	default:
		return false
	}
}

// isContainerComponent reports whether a component must be packaged inside a
// deployment container (the server-side code components). It is exactly the
// core-component set: Resources are deployment infrastructure and Utilities are
// ambient — both are excluded from container coverage.
func isContainerComponent(k string) bool { return isCoreComponentKind(k) }

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

	containersByKey, memberFindings := checkContainerMembership(topo.Containers, nameIdx)
	out = append(out, memberFindings...)

	byProfile, presentProfiles, instancedKeys, envFindings := checkEnvironments(topo.Environments, containersByKey)
	out = append(out, envFindings...)

	out = append(out, checkContainersUsed(topo.Containers, instancedKeys)...)
	out = append(out, checkProfileSets(presentProfiles, expectedProfiles(topo.DeliveryStyle))...)
	out = append(out, checkCrossProfileCoverage(byProfile, internalComponentNames(s))...)
	out = append(out, checkResourcesPresent(topo, s)...)
	return out
}

// checkContainerMembership indexes containers by key and emits DEP-MEMBER-EXIST
// (a member that is not a System component) + DEP-MEMBER-EXCLUSIVE (a component
// packaged by two distinct containers).
func checkContainerMembership(containers []DeployContainer, nameIdx map[string]Component) (map[string]DeployContainer, []Finding) {
	containersByKey := make(map[string]DeployContainer, len(containers))
	memberOwner := make(map[string]string, len(nameIdx)) // component name → owning container key
	var out []Finding
	for _, c := range containers {
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
			owner, dup := memberOwner[member]
			switch {
			case dup && owner != c.Key:
				out = append(out, Finding{
					RuleID:   ruleDepMemberExclusive,
					Severity: SeverityError,
					Message:  fmt.Sprintf("component %q is packaged by two containers (%q and %q); a component must belong to exactly one container (horizontal replicas are expressed by instancing ONE container in multiple nodes, not by duplicating membership)", member, owner, c.Key),
					Location: loc(0, "deployment topology"),
				})
			case !dup:
				memberOwner[member] = c.Key
			}
		}
	}
	return containersByKey, out
}

// checkEnvironments walks every environment, collecting the per-profile covered
// component sets, the present profiles, and the union of instanced container keys,
// plus each environment's own findings.
func checkEnvironments(environments []DeploymentEnvironment, containersByKey map[string]DeployContainer) (map[string]envSet, map[string]bool, map[string]bool, []Finding) {
	byProfile := make(map[string]envSet)
	presentProfiles := make(map[string]bool)
	instancedKeys := make(map[string]bool)
	var out []Finding
	for i, env := range environments {
		ordinal := i + 1
		presentProfiles[env.Profile] = true
		keys, _ := flattenContainerKeys(env.Nodes)
		for _, k := range keys {
			instancedKeys[k] = true
		}
		covered, envFindings := checkDeploymentEnvironment(env, ordinal, containersByKey)
		out = append(out, envFindings...)
		byProfile[env.Profile] = envSet{ordinal: ordinal, set: covered}
	}
	return byProfile, presentProfiles, instancedKeys, out
}

// checkContainersUsed emits DEP-CONTAINER-USED for every declared container that
// no environment instances — a container defined but never deployed is dead
// topology (the reverse of DEP-CONTAINER-REF, which flags an instance of a
// container that was never declared).
func checkContainersUsed(containers []DeployContainer, instancedKeys map[string]bool) []Finding {
	var out []Finding
	for _, c := range containers {
		if !instancedKeys[c.Key] {
			out = append(out, Finding{
				RuleID:   ruleDepContainerUsed,
				Severity: SeverityError,
				Message:  fmt.Sprintf("container %q is declared but instanced in no environment; every declared container must be deployed in ≥1 environment", c.Key),
				Location: loc(0, "deployment topology"),
			})
		}
	}
	return out
}

// checkResourcesPresent emits DEP-RESOURCE-PRESENT (Warning) for every Resource
// component that no InfrastructureNode or SoftwareSystemInstance names (slug-
// matched, like DEP-MEMBER-EXIST) in a required deployment profile. Resources
// deploy as infrastructure (a Postgres cluster, an external SaaS), NOT inside a
// container, so their presence is asserted against the infra/software-system
// nodes of each required, present profile rather than against container members.
func checkResourcesPresent(topo DeploymentTopology, s System) []Finding {
	var resources []Component
	for _, c := range s.Components {
		if c.Kind == kindResource {
			resources = append(resources, c)
		}
	}
	if len(resources) == 0 {
		return nil
	}
	expected := expectedProfiles(topo.DeliveryStyle)
	envByProfile := make(map[string]DeploymentEnvironment, len(topo.Environments))
	ordByProfile := make(map[string]int, len(topo.Environments))
	for i, env := range topo.Environments {
		envByProfile[env.Profile] = env
		ordByProfile[env.Profile] = i + 1
	}
	var out []Finding
	for _, p := range sortedComponentIDs(expected) {
		env, present := envByProfile[p]
		if !present {
			continue // DEP-PROFILE-SET already flags a missing required profile
		}
		out = append(out, resourcesMissingFromProfile(p, env, resources, ordByProfile[p])...)
	}
	return out
}

// resourcesMissingFromProfile emits a DEP-RESOURCE-PRESENT warning for every
// Resource whose slug no infrastructure/software-system node in this profile names.
func resourcesMissingFromProfile(profile string, env DeploymentEnvironment, resources []Component, ordinal int) []Finding {
	infraSlugs := infraSlugSet(env.Nodes)
	section := fmt.Sprintf("deployment environment %q", profileName(profile))
	var out []Finding
	for _, r := range resources {
		if !infraSlugs[Slug(r.Name)] {
			out = append(out, Finding{
				RuleID:   ruleDepResourcePresent,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("%s: Resource %q is not named by any infrastructure node or external software system in this profile; a Resource must appear as deployment infrastructure in every required profile", section, r.Name),
				Location: loc(ordinal, section),
			})
		}
	}
	return out
}

// infraSlugSet collects the Slugs of every InfrastructureNode and
// SoftwareSystemInstance name across a node tree.
func infraSlugSet(nodes []DeploymentNode) map[string]bool {
	set := make(map[string]bool)
	var walk func(ns []DeploymentNode)
	walk = func(ns []DeploymentNode) {
		for _, n := range ns {
			for _, in := range n.InfrastructureNodes {
				set[Slug(in.Name)] = true
			}
			for _, ss := range n.SoftwareSystemInstances {
				set[Slug(ss.Name)] = true
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	return set
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
		if isContainerComponent(c.Kind) {
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
		Severity: SeverityError,
		Message:  fmt.Sprintf("deployment environment %q does not instance every container-eligible component; every Client/Manager/Engine/ResourceAccess component must be packaged in a container per required profile (missing=%v)", profileName(p), missing),
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
