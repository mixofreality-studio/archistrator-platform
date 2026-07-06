package methodcheck

import (
	"fmt"
	"sort"
	"strings"
)

// rules.go ports the Phase-1 non-architecture predicate suites (volatilities,
// core use cases, operational concepts, standard check) + the verb orchestration
// + finalize/sort, FAITHFULLY from the aiarch artifactValidationEngine. Rule IDs,
// severities, and messages are byte-identical to the originals (they are the
// contract the CI check + diagnostics reference). The predicates run over the
// structural structs in project.go instead of the server's typed models, but the
// logic is line-for-line equivalent.

// ---- rule IDs (Phase-1 non-architecture verbs) ----

const (
	ruleVolTrace RuleID = "VOL-TRACE"
	ruleVolGloss RuleID = "VOL-GLOSS"
	ruleVolAxis  RuleID = "VOL-AXIS"
	ruleVolNOB   RuleID = "VOL-NOB"

	ruleCucCard      RuleID = "CUC-CARD"
	ruleUcActDiagram RuleID = "UC-ACTDIAG"
	ruleCucNameUniq  RuleID = "CUC-NAME-UNIQUE"
	ruleCucActorUniq RuleID = "CUC-ACTOR-UNIQUE"
	ruleUcNodeIDUniq RuleID = "UC-NODE-UNIQUE"

	ruleOpcObjRef RuleID = "OPC-OBJREF"

	ruleStdWaive RuleID = "STD-WAIVE"
)

// ---- verb orchestration ----

// ContractMisuseError is returned when a verb is asked to validate a model whose
// prerequisite committed inputs are absent (a wiring bug in the original Engine,
// surfaced here as the same coherence fault the CLI treats as a merge-blocking
// violation). It mirrors the aiarch fweng.ContractMisuse cases verbatim.
type ContractMisuseError struct{ Msg string }

func (e *ContractMisuseError) Error() string { return e.Msg }

// validateVolatilities ports ArtifactValidationEngine.ValidateVolatilities.
func validateVolatilities(v Volatilities, g Glossary, sr ScrubbedRequirements) (ValidationResult, error) {
	if len(v.Items) > 0 && len(g.Items) == 0 && len(sr.Items) == 0 {
		return ValidationResult{}, &ContractMisuseError{Msg: "ValidateVolatilities: volatilities present but both Glossary and ScrubbedRequirements are empty (Manager failed to read prerequisite models)"}
	}
	var findings []Finding
	findings = append(findings, volTrace(v, sr)...)
	findings = append(findings, volGloss(v, g)...)
	findings = append(findings, volAxis(v)...)
	findings = append(findings, volAxisExplicit(v)...)
	findings = append(findings, volNatureOfBusiness(v)...)
	return finalize(findings), nil
}

// validateGlossary runs the GLOSS-FOURQ twin over the committed Glossary.
func validateGlossary(g Glossary) (ValidationResult, error) {
	return finalize(glossFourQ(g)), nil
}

// validateScrubbedRequirements runs the SR-ID-UNIQUE twin over the committed
// ScrubbedRequirements.
func validateScrubbedRequirements(sr ScrubbedRequirements) (ValidationResult, error) {
	return finalize(srIDUnique(sr)), nil
}

// validateCoreUseCases ports ArtifactValidationEngine.ValidateCoreUseCases.
func validateCoreUseCases(c CoreUseCases) (ValidationResult, error) {
	var findings []Finding
	findings = append(findings, cucCardinality(c)...)
	findings = append(findings, useCaseNameUnique(c)...)
	findings = append(findings, actorNamesUnique(c)...)
	findings = append(findings, activityNodeIDsUnique(c)...)
	findings = append(findings, ucActivityDiagram(c)...)
	findings = append(findings, ucActPresent(c)...)
	findings = append(findings, ucGuardLabel(c)...)
	findings = append(findings, variationRef(c)...)
	return finalize(findings), nil
}

// validateArchitecture ports ArtifactValidationEngine.ValidateArchitecture.
func validateArchitecture(s System, c CoreUseCases) (ValidationResult, error) {
	var findings []Finding
	findings = append(findings, componentNameUnique(s)...)
	findings = append(findings, systemLayerDegenerate(s)...)
	findings = append(findings, sysLegality(s)...)
	findings = append(findings, sysCardinality(s)...)
	findings = append(findings, archChainCoverage(s, c)...)
	findings = append(findings, usecaseDynamicCoverage(s, c)...)
	findings = append(findings, raOrphan(s)...)
	findings = append(findings, encapsulates(s)...)
	findings = append(findings, relDup(s)...)
	findings = append(findings, dvChainConnected(s)...)
	findings = append(findings, dynamicViewConsistency(s)...)
	return finalize(findings), nil
}

// validateOperationalConcepts ports ArtifactValidationEngine.ValidateOperationalConcepts.
func validateOperationalConcepts(o OperationalConcepts, m MissionStatement, s System) (ValidationResult, error) {
	if len(o.Decisions) > 0 && len(m.Objectives) == 0 {
		return ValidationResult{}, &ContractMisuseError{Msg: "ValidateOperationalConcepts: operational decisions present but MissionStatement has no objectives (Manager failed to read the mission)"}
	}
	var findings []Finding
	findings = append(findings, opcObjRef(o, m)...)
	findings = append(findings, opcTopicCoverage(o)...)
	findings = append(findings, deploymentConsistency(o, s)...)
	return finalize(findings), nil
}

// validateStandardCheck ports ArtifactValidationEngine.ValidateStandardCheck and adds
// the STD-STATUS-EXPLICIT + STD-FAIL-OPEN twins.
func validateStandardCheck(sc StandardCheck) (ValidationResult, error) {
	var findings []Finding
	findings = append(findings, stdWaive(sc)...)
	findings = append(findings, stdStatusExplicit(sc)...)
	findings = append(findings, stdFailOpen(sc)...)
	return finalize(findings), nil
}

// finalize sorts findings deterministically and computes the verdict. Ported verbatim.
func finalize(findings []Finding) ValidationResult {
	sortFindings(findings)
	verdict := VerdictPass
	for _, f := range findings {
		if f.Severity == SeverityError {
			verdict = VerdictFail
			break
		}
	}
	return ValidationResult{Verdict: verdict, Findings: findings}
}

// sortFindings orders findings by (Location.Ordinal, RuleID). Ported verbatim.
func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		oi, oj := ordinalOf(findings[i]), ordinalOf(findings[j])
		if oi != oj {
			return oi < oj
		}
		return findings[i].RuleID < findings[j].RuleID
	})
}

func ordinalOf(f Finding) int {
	if f.Location == nil {
		return 0
	}
	return f.Location.Ordinal
}

// ---- heuristic tokenizers (ported verbatim) ----

func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
}

func significantTerms(s string) []string {
	var out []string
	for _, t := range tokenize(s) {
		if len(t) >= 4 {
			out = append(out, t)
		}
	}
	return out
}

// ---- ValidateVolatilities predicates ----

func volTrace(v Volatilities, sr ScrubbedRequirements) []Finding {
	reqTerms := make(map[string]bool)
	for _, r := range sr.Items {
		for _, t := range significantTerms(r.ID + " " + r.Statement) {
			reqTerms[t] = true
		}
	}
	var out []Finding
	for i, vol := range v.Items {
		if !isVolatilityTraced(vol, reqTerms) {
			section := fmt.Sprintf("volatility %d (%s)", i+1, vol.Name)
			out = append(out, Finding{
				RuleID:   ruleVolTrace,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s does not trace to any scrubbed requirement (no shared vocabulary); every volatility must justify itself against a requirement", section),
				Location: loc(i+1, section),
			})
		}
	}
	return out
}

func isVolatilityTraced(vol Volatility, reqTerms map[string]bool) bool {
	for _, t := range significantTerms(vol.Name + " " + vol.Rationale) {
		if reqTerms[t] {
			return true
		}
	}
	return false
}

func volGloss(v Volatilities, g Glossary) []Finding {
	glossTerms := make(map[string]bool)
	for _, item := range g.Items {
		for _, t := range significantTerms(item.Term) {
			glossTerms[t] = true
		}
	}
	var out []Finding
	for i, vol := range v.Items {
		if !isVolatilityInGlossary(vol, glossTerms) {
			section := fmt.Sprintf("volatility %d (%s)", i+1, vol.Name)
			out = append(out, Finding{
				RuleID:   ruleVolGloss,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s names a concept that does not resolve in the Glossary; define its terms in the glossary first", section),
				Location: loc(i+1, section),
			})
		}
	}
	return out
}

func isVolatilityInGlossary(vol Volatility, glossTerms map[string]bool) bool {
	nameTerms := significantTerms(vol.Name)
	if len(nameTerms) == 0 {
		return true // no significant terms → nothing to look up; don't penalize
	}
	for _, t := range nameTerms {
		if glossTerms[t] {
			return true
		}
	}
	return false
}

func volAxis(v Volatilities) []Finding {
	if len(v.Items) < 2 {
		return nil
	}
	axes := make(map[string]bool)
	for _, vol := range v.Items {
		axes[vol.Axis] = true
	}
	if len(axes) < 2 {
		return []Finding{{
			RuleID:   ruleVolAxis,
			Severity: SeverityError,
			Message:  "all volatilities sit on a single axis; a two-axis volatility analysis must span both the same-customer-over-time and all-customers-at-one-time axes",
			Location: loc(0, "volatility axes"),
		}}
	}
	return nil
}

func volNatureOfBusiness(v Volatilities) []Finding {
	speculativeMarkers := map[string]bool{
		"maybe": true, "might": true, "could": true, "possibly": true,
		"someday": true, "future": true, "speculative": true, "perhaps": true,
	}
	var out []Finding
	for i, vol := range v.Items {
		for _, t := range tokenize(vol.Rationale) {
			if speculativeMarkers[t] {
				section := fmt.Sprintf("volatility %d (%s)", i+1, vol.Name)
				out = append(out, Finding{
					RuleID:   ruleVolNOB,
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("%s reads as speculative (rationale uses hedging language %q); confirm this is a real volatility and not nature-of-business", section, t),
					Location: loc(i+1, section),
				})
				break
			}
		}
	}
	return out
}

// ---- ValidateCoreUseCases predicates ----

func cucCardinality(c CoreUseCases) []Finding {
	core := 0
	for _, d := range c.Decisions {
		if d.UseCase.Classification == classCore {
			core++
		}
	}
	const minCore, maxCore = 2, 6
	if core < minCore || core > maxCore {
		return []Finding{{
			RuleID:   ruleCucCard,
			Severity: SeverityError,
			Message:  fmt.Sprintf("found %d core use cases; The Method requires %d–%d core use cases", core, minCore, maxCore),
			Location: loc(0, "core use cases cardinality"),
		}}
	}
	return nil
}

func useCaseNameUnique(c CoreUseCases) []Finding {
	var out []Finding
	seen := make(map[string]bool, len(c.Decisions))
	for i, d := range c.Decisions {
		uc := d.UseCase
		section := fmt.Sprintf("use case %d (%s)", i+1, uc.Name)
		key := Slug(uc.Name)
		if key == "" {
			out = append(out, Finding{
				RuleID:   ruleCucNameUniq,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: use-case name is empty or yields no identity slug; every use case needs a unique human-readable name", section),
				Location: loc(i+1, section),
			})
			continue
		}
		if seen[key] {
			out = append(out, Finding{
				RuleID:   ruleCucNameUniq,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s: use-case name %q is not unique (its identity slug %q collides with an earlier use case)", section, uc.Name, key),
				Location: loc(i+1, section),
			})
		}
		seen[key] = true
	}
	return out
}

func actorNamesUnique(c CoreUseCases) []Finding {
	var out []Finding
	for i, d := range c.Decisions {
		uc := d.UseCase
		seen := make(map[string]bool, len(uc.Actors))
		for j, a := range uc.Actors {
			section := fmt.Sprintf("use case %d (%s) actor %d", i+1, uc.Name, j+1)
			key := Slug(a.Role)
			if key == "" {
				out = append(out, Finding{
					RuleID:   ruleCucActorUniq,
					Severity: SeverityError,
					Message:  fmt.Sprintf("%s: actor role is empty or yields no identity slug", section),
					Location: loc(i+1, section),
				})
				continue
			}
			if seen[key] {
				out = append(out, Finding{
					RuleID:   ruleCucActorUniq,
					Severity: SeverityError,
					Message:  fmt.Sprintf("%s: actor role %q is duplicated within the use case", section, a.Role),
					Location: loc(i+1, section),
				})
			}
			seen[key] = true
		}
	}
	return out
}

func activityNodeIDsUnique(c CoreUseCases) []Finding {
	var out []Finding
	for i, d := range c.Decisions {
		uc := d.UseCase
		if uc.Activity == nil {
			continue
		}
		seen := make(map[string]bool, len(uc.Activity.Nodes))
		section := fmt.Sprintf("core use case %d (%s)", i+1, uc.Name)
		for _, n := range uc.Activity.Nodes {
			out = append(out, checkNodeUniqueness(n, seen, section, i+1)...)
			if n.ID != "" {
				seen[n.ID] = true
			}
		}
	}
	return out
}

func checkNodeUniqueness(n ActivityNode, seen map[string]bool, section string, ucOrdinal int) []Finding {
	if n.ID == "" {
		return []Finding{{
			RuleID:   ruleUcNodeIDUniq,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: an activity node has an empty id; every node needs a unique identity", section),
			Location: loc(ucOrdinal, section),
		}}
	}
	if seen[n.ID] {
		return []Finding{{
			RuleID:   ruleUcNodeIDUniq,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s: activity node id %q is not unique within the diagram", section, n.ID),
			Location: loc(ucOrdinal, section),
		}}
	}
	return nil
}

func ucActivityDiagram(c CoreUseCases) []Finding {
	var out []Finding
	for i, d := range c.Decisions {
		uc := d.UseCase
		if uc.Activity == nil {
			continue
		}
		section := fmt.Sprintf("core use case %d (%s)", i+1, uc.Name)
		add := func(msg string) {
			out = append(out, Finding{
				RuleID:   ruleUcActDiagram,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s %s", section, msg),
				Location: loc(i+1, section),
			})
		}
		kindByID, inCount, outCount, guardedOut, unguardedOut := checkActivityEdges(uc.Activity, add)
		checkActivityNodes(sortedKeys(kindByID), kindByID, inCount, outCount, guardedOut, unguardedOut, add)
	}
	return out
}

func checkActivityEdges(activity *ActivityDiagram, add func(string)) (kindByID map[string]string, inCount, outCount, guardedOut, unguardedOut map[string]int) {
	kindByID = make(map[string]string, len(activity.Nodes))
	for _, n := range activity.Nodes {
		kindByID[n.ID] = n.Kind
	}
	inCount = make(map[string]int)
	outCount = make(map[string]int)
	guardedOut = make(map[string]int)
	unguardedOut = make(map[string]int)
	edges := append([]ActivityEdge(nil), activity.Edges...)
	sort.Slice(edges, func(a, b int) bool {
		if edges[a].From != edges[b].From {
			return edges[a].From < edges[b].From
		}
		return edges[a].To < edges[b].To
	})
	for _, e := range edges {
		from := e.From
		inCount[e.To]++
		outCount[from]++
		if e.Kind == edgeGuardedFlow {
			guardedOut[from]++
			if kindByID[from] != nodeDecision {
				add(fmt.Sprintf("has a guarded edge from non-decision node %s; only a decision node may carry guarded outgoing edges", from))
			}
		} else {
			unguardedOut[from]++
		}
	}
	return
}

func checkActivityNodes(ids []string, kindByID map[string]string, inCount, outCount, guardedOut, unguardedOut map[string]int, add func(string)) {
	for _, id := range ids {
		switch kindByID[id] {
		case nodeDecision:
			checkDecisionNode(id, guardedOut, unguardedOut, add)
		case nodeFork:
			checkForkNode(id, outCount, guardedOut, add)
		case nodeMerge:
			checkMergeNode(id, inCount, add)
		case nodeJoin:
			checkJoinNode(id, inCount, add)
		}
	}
}

func checkDecisionNode(id string, guardedOut, unguardedOut map[string]int, add func(string)) {
	if guardedOut[id] < 2 || unguardedOut[id] > 0 {
		add(fmt.Sprintf("decision node %s must have >=2 guarded outgoing edges (a choice) and no unguarded outgoing edge; its branches reconverge at a merge", id))
	}
}

func checkForkNode(id string, outCount, guardedOut map[string]int, add func(string)) {
	if outCount[id] < 2 || guardedOut[id] > 0 {
		add(fmt.Sprintf("fork node %s must have >=2 unguarded (controlFlow) outgoing edges — concurrency, not a guarded choice", id))
	}
}

func checkMergeNode(id string, inCount map[string]int, add func(string)) {
	if inCount[id] < 2 {
		add(fmt.Sprintf("merge node %s must have >=2 incoming edges (it rejoins a decision's alternative branches)", id))
	}
}

func checkJoinNode(id string, inCount map[string]int, add func(string)) {
	if inCount[id] < 2 {
		add(fmt.Sprintf("join node %s must have >=2 incoming edges (it synchronizes a fork's concurrent paths)", id))
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---- ValidateOperationalConcepts predicate ----

func opcObjRef(o OperationalConcepts, m MissionStatement) []Finding {
	objNumbers := make(map[int]bool, len(m.Objectives))
	for _, obj := range m.Objectives {
		objNumbers[obj.Number] = true
	}
	var out []Finding
	for i, d := range o.Decisions {
		if !objNumbers[d.JustifyingObjective] {
			section := fmt.Sprintf("operational decision %d (%s)", i+1, d.Topic)
			out = append(out, Finding{
				RuleID:   ruleOpcObjRef,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s justifies against objective %d, which does not exist in the mission statement", section, d.JustifyingObjective),
				Location: loc(i+1, section),
			})
		}
	}
	return out
}

// ---- ValidateStandardCheck predicate ----

func stdWaive(sc StandardCheck) []Finding {
	var out []Finding
	for i, item := range sc.Items {
		if item.Status == checkWaived && strings.TrimSpace(item.Justification) == "" {
			section := fmt.Sprintf("checklist item %d (%s %s)", i+1, item.Section, item.Guideline)
			out = append(out, Finding{
				RuleID:   ruleStdWaive,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s is waived without a justification; every waiver must carry a written justification", section),
				Location: loc(i+1, section),
			})
		}
	}
	return out
}
