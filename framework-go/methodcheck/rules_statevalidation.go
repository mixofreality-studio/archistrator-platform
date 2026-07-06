package methodcheck

import (
	"fmt"
	"sort"
	"strings"
)

// rules_statevalidation.go holds the AUTHORITATIVE platform twins of the app-side
// state-validation rules the architect ratified 2026-07-05 (app commit a19a25b):
// the systemdesign read-back finding generators (statevalidationfindings.go) plus
// the projectstate.RequireModelFields presence/consistency gates. The app enforces
// these two ways — presence rules hard-block on the write+read-back codec, and
// cross-artifact/semantic rules surface as display findings that must never hard-fail
// a READ of committed state — because the app's ordinal enums collapse an absent field
// onto its zero value, so presence is only observable on the raw JSON before decode.
//
// methodcheck's structural mirror (project.go) decodes every enum as its WIRE STRING,
// so an absent enum decodes to "" here and is directly observable on the typed model —
// no raw-JSON pass is needed. These twins therefore run as ordinary typed predicates,
// wired into the same verb orchestrators as the ported Method rules, and fire at BOTH
// the aiarch-state MCP putDraftModel write gate and the CI methodcheck gate. Rule IDs
// and severities match the app-side originals; see each function's doc comment for the
// one deliberate reconciliation (SYS-ENCAPSULATES client severity, documented below).

// ---- rule IDs (state-validation twins) ----

const (
	// System twins (rules over the committed System model).
	ruleSysRAOrphan     RuleID = "SYS-RA-ORPHAN"
	ruleSysEncapsulates RuleID = "SYS-ENCAPSULATES"
	ruleSysRelDup       RuleID = "SYS-REL-DUP"
	ruleDVChainConn     RuleID = "DV-CHAIN-CONNECTED"

	// CoreUseCases twins.
	ruleUCActPresent   RuleID = "UC-ACT-PRESENT"
	ruleUCGuardLabel   RuleID = "UC-GUARD-LABEL"
	ruleUCVariationRef RuleID = "UC-VARIATION-REF"

	// Single-artifact presence/coverage twins.
	ruleVolAxisExplicit   RuleID = "VOL-AXIS-EXPLICIT"
	ruleStdStatusExplicit RuleID = "STD-STATUS-EXPLICIT"
	ruleStdFailOpen       RuleID = "STD-FAIL-OPEN"
	ruleGlossFourQ        RuleID = "GLOSS-FOURQ"
	ruleSRIDUnique        RuleID = "SR-ID-UNIQUE"
	ruleOPCTopicCoverage  RuleID = "OPC-TOPIC-COVERAGE"
)

// ===================== System twins =====================

// raOrphan — SYS-RA-ORPHAN (error). Twin of systemdesign.raOrphanFindings. Every
// ResourceAccess component must have at least one outbound sync/queued relationship to
// a Resource (or to a documented external system — an edge target that is not itself a
// modeled component). A ResourceAccess that reaches no resource encapsulates nothing.
func raOrphan(s System) []Finding {
	idx := componentIndex(s)
	var out []Finding
	for i, c := range s.Components {
		if c.Kind != kindResourceAccess {
			continue
		}
		if resourceAccessReaches(c, s, idx) {
			continue
		}
		label := componentName(c, i)
		out = append(out, Finding{
			RuleID:   ruleSysRAOrphan,
			Severity: SeverityError,
			Message:  fmt.Sprintf("ResourceAccess %q has no outbound sync/queued relationship to a resource (or documented external system); every ResourceAccess must encapsulate at least one resource", label),
			Location: loc(i+1, "component "+label),
		})
	}
	return out
}

func resourceAccessReaches(c Component, s System, idx map[string]Component) bool {
	for _, r := range s.Relationships {
		if r.From != c.ID {
			continue
		}
		if r.Mode != modeSync && r.Mode != modeQueued {
			continue
		}
		to, known := idx[r.To]
		// A Resource target, or an external target (not a modeled component), satisfies.
		if !known || to.Kind == kindResource {
			return true
		}
	}
	return false
}

// encapsulates — SYS-ENCAPSULATES. Twin of systemdesign.encapsulatesFindings. Every
// component should name the volatility (or, for a resource/utility, the responsibility)
// it owns.
//
// SEVERITY RECONCILIATION (documented deviation, agreed with the app-side note): the
// app hard-blocks empty encapsulates on Manager/Engine/ResourceAccess in
// RequireModelFields (a WRITE block AND a read-back block), so committed state can never
// carry an empty-encapsulates M/E/RA — those may safely be ERROR here (methodcheck's
// ERROR fails the verdict / blocks the write, and no committed state trips it). The app
// treats CLIENT empty as an ERROR *display* finding that does NOT hard-fail a read,
// because committed state (gtdapp) legitimately carries empty-encapsulates clients (a
// transport entry point owns no volatility) and reads must never break. methodcheck's
// ERROR has no non-blocking mode — an ERROR here WOULD block a gtdapp write/CI run — so
// to preserve the SAME EFFECT (client empty never blocks committed state) the client
// case is downgraded to WARNING, alongside resource/utility (which the app already
// warns). Net: methodcheck FAILS exactly where the app hard-blocks (M/E/RA), and is
// advisory exactly where the app is advisory-on-read (client/resource/utility).
func encapsulates(s System) []Finding {
	var out []Finding
	for i, c := range s.Components {
		if strings.TrimSpace(c.Encapsulates) != "" {
			continue
		}
		sev, ok := encapsulatesSeverity(c.Kind)
		if !ok {
			continue
		}
		label := componentName(c, i)
		out = append(out, Finding{
			RuleID:   ruleSysEncapsulates,
			Severity: sev,
			Message:  fmt.Sprintf("component %q has an empty encapsulates; state the volatility (or, for a client/resource/utility, the responsibility) it owns", label),
			Location: loc(i+1, "component "+label),
		})
	}
	return out
}

// encapsulatesSeverity returns the SYS-ENCAPSULATES severity for a component kind, and
// false for an unrecognized kind (no finding). See encapsulates for the client downgrade.
func encapsulatesSeverity(kind string) (Severity, bool) {
	switch kind {
	case kindManager, kindEngine, kindResourceAccess:
		return SeverityError, true
	case kindClient, kindResource, kindUtility:
		return SeverityWarning, true
	}
	return SeverityInfo, false
}

// relDup — SYS-REL-DUP. Twin of systemdesign.relDupFindings. An EXACT duplicate
// relationship (same from, to AND mode) is an ERROR (a redundant edge). Two edges on the
// SAME (from,to) pair that differ only by label (a label-split) are a WARNING suggesting
// the labels be aggregated with " | " onto one edge.
func relDup(s System) []Finding {
	type pair struct{ from, to string }
	exact := map[string]int{}           // from|to|mode → count
	byPair := map[pair]map[string]int{} // (from,to) → distinct label → count
	var order []pair
	for _, r := range s.Relationships {
		exact[r.From+"|"+r.To+"|"+r.Mode]++
		p := pair{r.From, r.To}
		if byPair[p] == nil {
			byPair[p] = map[string]int{}
			order = append(order, p)
		}
		byPair[p][r.Label]++
	}
	var out []Finding
	for _, p := range order {
		labels := byPair[p]
		if pairEdgeCount(labels) < 2 {
			continue
		}
		section := fmt.Sprintf("relationship %s → %s", p.from, p.to)
		switch {
		case pairHasExactDup(p.from, p.to, s, exact):
			out = append(out, Finding{
				RuleID:   ruleSysRelDup,
				Severity: SeverityError,
				Message:  fmt.Sprintf("relationship %s → %s is declared more than once with the same mode; remove the exact duplicate edge", p.from, p.to),
				Location: &Location{Section: section},
			})
		case len(labels) > 1:
			out = append(out, Finding{
				RuleID:   ruleSysRelDup,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("relationship %s → %s is split across %d edges with different labels; aggregate them onto one edge with a \" | \"-joined label", p.from, p.to, len(labels)),
				Location: &Location{Section: section},
			})
		}
	}
	return out
}

func pairEdgeCount(labels map[string]int) int {
	total := 0
	for _, n := range labels {
		total += n
	}
	return total
}

func pairHasExactDup(from, to string, s System, exact map[string]int) bool {
	for _, r := range s.Relationships {
		if r.From == from && r.To == to && exact[r.From+"|"+r.To+"|"+r.Mode] > 1 {
			return true
		}
	}
	return false
}

// dvChainConnected — DV-CHAIN-CONNECTED (warning). Twin of systemdesign.dvChainFindings.
// Each dynamic view's edges should form a connected chain rooted at a Client participant:
// every participant must be reachable by following the directed edges out of some
// Client-kind participant. An unrooted or disconnected call chain is a modeling smell.
func dvChainConnected(s System) []Finding {
	idx := componentIndex(s)
	var out []Finding
	for i, dv := range s.DynamicViews {
		if len(dv.Participants) <= 1 {
			continue
		}
		out = append(out, dvChainFindingsFor(dv, idx, i)...)
	}
	return out
}

func dvChainFindingsFor(dv DynamicView, idx map[string]Component, i int) []Finding {
	label := viewLabel(dv)
	roots := clientRoots(dv, idx)
	if len(roots) == 0 {
		return []Finding{{
			RuleID:   ruleDVChainConn,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("dynamic view %q has no Client participant to root its call chain; a use-case call chain should originate at a Client", label),
			Location: loc(i+1, "dynamic view "+label),
		}}
	}
	unreached := unreachedParticipants(dv, roots)
	if len(unreached) == 0 {
		return nil
	}
	sort.Strings(unreached)
	return []Finding{{
		RuleID:   ruleDVChainConn,
		Severity: SeverityWarning,
		Message:  fmt.Sprintf("dynamic view %q is not a connected chain from its Client root(s): %s unreachable via its edges", label, strings.Join(unreached, ", ")),
		Location: loc(i+1, "dynamic view "+label),
	}}
}

func clientRoots(dv DynamicView, idx map[string]Component) []string {
	var roots []string
	for _, pid := range dv.Participants {
		if idx[pid].Kind == kindClient {
			roots = append(roots, pid)
		}
	}
	return roots
}

func unreachedParticipants(dv DynamicView, roots []string) []string {
	adj := map[string][]string{}
	for _, e := range dv.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	seen := map[string]bool{}
	stack := append([]string{}, roots...)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n] {
			continue
		}
		seen[n] = true
		stack = append(stack, adj[n]...)
	}
	var out []string
	for _, pid := range dv.Participants {
		if !seen[pid] {
			out = append(out, pid)
		}
	}
	return out
}

// ===================== CoreUseCases twins =====================

// ucActPresent — UC-ACT-PRESENT (error). Twin of the PROMOTED app-side rule (a19a25b
// promoted the advisory USECASE-ACTIVITY-MISSING finding to a write-path block). Every
// use case — core AND nonCore variation — must carry a non-null activity diagram with at
// least one start node and at least one action step.
func ucActPresent(c CoreUseCases) []Finding {
	var out []Finding
	for i, d := range c.Decisions {
		uc := d.UseCase
		section := fmt.Sprintf("use case %d (%s)", i+1, uc.Name)
		if uc.Activity == nil {
			out = append(out, Finding{
				RuleID:   ruleUCActPresent,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s is missing its required activity diagram; every use case must carry a non-empty activity diagram with a start node and at least one action step", section),
				Location: loc(i+1, section),
			})
			continue
		}
		if !activityHasStartAndAction(uc.Activity) {
			out = append(out, Finding{
				RuleID:   ruleUCActPresent,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s activity diagram is structurally empty; it must contain at least one start node and at least one action step", section),
				Location: loc(i+1, section),
			})
		}
	}
	return out
}

func activityHasStartAndAction(a *ActivityDiagram) bool {
	hasStart, hasAction := false, false
	for _, n := range a.Nodes {
		switch n.Kind {
		case nodeStart:
			hasStart = true
		case nodeAction:
			hasAction = true
		}
	}
	return hasStart && hasAction
}

// ucGuardLabel — UC-GUARD-LABEL (error). Twin of the app-side write-path block: a
// guardedFlow edge (the outgoing edge of a decision) must carry non-empty guard text — an
// unlabeled guard makes the branch condition unreadable. Plain controlFlow edges carry no
// guard and are unaffected.
func ucGuardLabel(c CoreUseCases) []Finding {
	var out []Finding
	for i, d := range c.Decisions {
		uc := d.UseCase
		if uc.Activity == nil {
			continue
		}
		section := fmt.Sprintf("use case %d (%s)", i+1, uc.Name)
		for _, e := range uc.Activity.Edges {
			if e.Kind == edgeGuardedFlow && strings.TrimSpace(e.Guard) == "" {
				out = append(out, Finding{
					RuleID:   ruleUCGuardLabel,
					Severity: SeverityError,
					Message:  fmt.Sprintf("%s has a guardedFlow edge %s→%s with empty guard text; a guarded branch must carry a non-empty guard label", section, e.From, e.To),
					Location: loc(i+1, section),
				})
			}
		}
	}
	return out
}

// variationRef — UC-VARIATION-REF (error). Twin of systemdesign.variationRefFindings.
// variationOf, when set, must resolve to an existing use-case id whose target is CORE. A
// nonCore use case must carry a variationOf AND a non-empty rejectionReason. A core use
// case must NOT carry a variationOf (it is the base, not a permutation).
func variationRef(c CoreUseCases) []Finding {
	coreIDs := coreUseCaseIDs(c)
	var out []Finding
	for i, d := range c.Decisions {
		out = append(out, variationRefForDecision(d, i, coreIDs)...)
	}
	return out
}

func coreUseCaseIDs(c CoreUseCases) map[string]bool {
	ids := map[string]bool{}
	for _, d := range c.Decisions {
		if d.UseCase.Classification == classCore {
			ids[d.UseCase.ID] = true
		}
	}
	return ids
}

func variationRefForDecision(d UseCaseDecision, i int, coreIDs map[string]bool) []Finding {
	uc := d.UseCase
	label := uc.Name
	if label == "" {
		label = fmt.Sprintf("use case %d", i+1)
	}
	location := loc(i+1, "use case "+label)
	if uc.Classification == classCore {
		if uc.VariationOf != nil && strings.TrimSpace(*uc.VariationOf) != "" {
			return []Finding{{
				RuleID:   ruleUCVariationRef,
				Severity: SeverityError,
				Message:  fmt.Sprintf("core use case %q declares a variationOf (%q); a core use case is a base, not a variation — clear variationOf or reclassify it nonCore", label, *uc.VariationOf),
				Location: location,
			}}
		}
		return nil
	}
	return nonCoreVariationFindings(uc, label, location, d.RejectionReason, coreIDs)
}

func nonCoreVariationFindings(uc UseCase, label string, location *Location, rejectionReason string, coreIDs map[string]bool) []Finding {
	var out []Finding
	switch {
	case uc.VariationOf == nil || strings.TrimSpace(*uc.VariationOf) == "":
		out = append(out, Finding{
			RuleID:   ruleUCVariationRef,
			Severity: SeverityError,
			Message:  fmt.Sprintf("nonCore use case %q has no variationOf; a nonCore use case must link to the core use case it permutes", label),
			Location: location,
		})
	case !coreIDs[*uc.VariationOf]:
		out = append(out, Finding{
			RuleID:   ruleUCVariationRef,
			Severity: SeverityError,
			Message:  fmt.Sprintf("nonCore use case %q has variationOf %q, which does not resolve to an existing CORE use case", label, *uc.VariationOf),
			Location: location,
		})
	}
	if strings.TrimSpace(rejectionReason) == "" {
		out = append(out, Finding{
			RuleID:   ruleUCVariationRef,
			Severity: SeverityError,
			Message:  fmt.Sprintf("nonCore use case %q has an empty rejectionReason; state why it is not core", label),
			Location: location,
		})
	}
	return out
}

// ===================== single-artifact twins =====================

// volAxisExplicit — VOL-AXIS-EXPLICIT (error). Twin of the app-side RequireModelFields
// axis-presence block: every volatility must explicitly declare its axis. An absent axis
// decodes to "" on the structural model (the app's ordinal enum would silently default it
// to AxisSameCustomerOverTime). Distinct from VOL-AXIS, which checks that the SET of
// volatilities spans BOTH axes.
func volAxisExplicit(v Volatilities) []Finding {
	var out []Finding
	for i, vol := range v.Items {
		section := fmt.Sprintf("volatility %d (%s)", i+1, vol.Name)
		switch vol.Axis {
		case "":
			out = append(out, Finding{
				RuleID:   ruleVolAxisExplicit,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s has no axis; every volatility must explicitly declare its axis (sameCustomerOverTime|allCustomersAtOneTime)", section),
				Location: loc(i+1, section),
			})
		case axisSameCustomerOverTime, axisAllCustomersAtOneTime:
		default:
			out = append(out, Finding{
				RuleID:   ruleVolAxisExplicit,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s has an unrecognized axis %q; use one of sameCustomerOverTime|allCustomersAtOneTime", section, vol.Axis),
				Location: loc(i+1, section),
			})
		}
	}
	return out
}

// stdStatusExplicit — STD-STATUS-EXPLICIT (error). Twin of the app-side
// RequireModelFields status-presence block: every standard-check item must explicitly
// emit its status. An absent status decodes to "" here (the app's ordinal enum would
// silently default it to CheckPass — a failing/waived guideline masquerading as passed).
func stdStatusExplicit(sc StandardCheck) []Finding {
	var out []Finding
	for i, item := range sc.Items {
		section := fmt.Sprintf("checklist item %d (%s %s)", i+1, item.Section, item.Guideline)
		switch item.Status {
		case "":
			out = append(out, Finding{
				RuleID:   ruleStdStatusExplicit,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s has no status; every standard-check item must explicitly emit its status (pass|waived|fail)", section),
				Location: loc(i+1, section),
			})
		case checkPass, checkWaived, checkFail:
		default:
			out = append(out, Finding{
				RuleID:   ruleStdStatusExplicit,
				Severity: SeverityError,
				Message:  fmt.Sprintf("%s has an unrecognized status %q; use one of pass|waived|fail", section, item.Status),
				Location: loc(i+1, section),
			})
		}
	}
	return out
}

// stdFailOpen — STD-FAIL-OPEN (error). Twin of the app-side AdvancePhase gate: a
// committed standard check must not carry a failing item. Where the app refuses to SEAL
// the phase over an open failure (ignoring acknowledgeStale), methodcheck fails the
// verdict whenever a committed StandardCheck carries a fail item — the design cannot be
// sealed over an open failure; fix the guideline or waive it with justification.
func stdFailOpen(sc StandardCheck) []Finding {
	var out []Finding
	for i, item := range sc.Items {
		if item.Status != checkFail {
			continue
		}
		section := fmt.Sprintf("checklist item %d (%s %s)", i+1, item.Section, item.Guideline)
		out = append(out, Finding{
			RuleID:   ruleStdFailOpen,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s is marked fail; a committed standard check must not carry a failing item — the design phase cannot be sealed over an open failure (fix the guideline or waive it with a justification)", section),
			Location: loc(i+1, section),
		})
	}
	return out
}

// glossFourQCategories is the closed Four-Questions category set (ch. 4).
var glossFourQCategories = map[string]bool{"Who": true, "What": true, "How": true, "Where": true}

// glossFourQ — GLOSS-FOURQ. Twin of systemdesign.glossaryFourQFindings. ERROR: a term
// whose category is not one of the four canonical values. WARNING: no term covers one of
// Who / What / How / Where (the Four Questions each want at least one term).
func glossFourQ(g Glossary) []Finding {
	var out []Finding
	counts := map[string]int{}
	for i, it := range g.Items {
		cat := strings.TrimSpace(it.Category)
		if !glossFourQCategories[cat] {
			out = append(out, Finding{
				RuleID:   ruleGlossFourQ,
				Severity: SeverityError,
				Message:  fmt.Sprintf("glossary term %q has non-canonical category %q; use one of Who|What|How|Where", it.Term, it.Category),
				Location: loc(i+1, "glossary term "+it.Term),
			})
			continue
		}
		counts[cat]++
	}
	for _, cat := range []string{"Who", "What", "How", "Where"} {
		if counts[cat] == 0 {
			out = append(out, Finding{
				RuleID:   ruleGlossFourQ,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("no glossary term covers the %q question; the Four Questions each want at least one term", cat),
				Location: loc(0, "glossary"),
			})
		}
	}
	return out
}

// srIDUnique — SR-ID-UNIQUE (error). Twin of systemdesign.scrubbedIDFindings. Every
// scrubbed requirement must carry a non-empty, unique id and a non-empty statement.
func srIDUnique(sr ScrubbedRequirements) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for i, it := range sr.Items {
		out = append(out, srIDUniqueForItem(it, i, seen)...)
	}
	return out
}

func srIDUniqueForItem(it Requirement, i int, seen map[string]bool) []Finding {
	var out []Finding
	section := fmt.Sprintf("requirement %d", i+1)
	id := strings.TrimSpace(it.ID)
	switch {
	case id == "":
		out = append(out, Finding{
			RuleID:   ruleSRIDUnique,
			Severity: SeverityError,
			Message:  fmt.Sprintf("scrubbed requirement %d has an empty id; every requirement needs a stable non-empty id", i+1),
			Location: loc(i+1, section),
		})
	case seen[id]:
		out = append(out, Finding{
			RuleID:   ruleSRIDUnique,
			Severity: SeverityError,
			Message:  fmt.Sprintf("scrubbed requirement id %q is duplicated; requirement ids must be unique", id),
			Location: loc(i+1, section),
		})
	default:
		seen[id] = true
	}
	if strings.TrimSpace(it.Statement) == "" {
		out = append(out, Finding{
			RuleID:   ruleSRIDUnique,
			Severity: SeverityError,
			Message:  fmt.Sprintf("scrubbed requirement %q has an empty statement", it.ID),
			Location: loc(i+1, section),
		})
	}
	return out
}

// opcCanonicalTopics maps a canonical ch.5 operational-concept topic to the substrings
// that evidence it appears among decisions[].topic.
var opcCanonicalTopics = []struct {
	name  string
	needs []string
}{
	{"topology", []string{"topology"}},
	{"sync/queued", []string{"sync", "queued"}},
	{"layering style", []string{"layering"}},
	{"state handling", []string{"state"}},
}

// opcTopicCoverage — OPC-TOPIC-COVERAGE (info). Twin of systemdesign.opcTopicFindings.
// Nudge when a canonical ch.5 topic (topology, sync/queued, layering style, state
// handling) is absent from decisions[].topic.
func opcTopicCoverage(o OperationalConcepts) []Finding {
	var lowered []string
	for _, d := range o.Decisions {
		lowered = append(lowered, strings.ToLower(d.Topic))
	}
	joined := strings.Join(lowered, " | ")
	var out []Finding
	for _, t := range opcCanonicalTopics {
		if topicCovered(joined, t.needs) {
			continue
		}
		out = append(out, Finding{
			RuleID:   ruleOPCTopicCoverage,
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("no operational-concept decision addresses %q; ch.5 expects topology, sync/queued, layering style, and state handling to be decided", t.name),
			Location: loc(0, "operational concepts"),
		})
	}
	return out
}

func topicCovered(joined string, needs []string) bool {
	for _, n := range needs {
		if strings.Contains(joined, n) {
			return true
		}
	}
	return false
}

// ===================== shared helpers =====================

// componentName picks a human-readable label for a component in finding messages,
// preferring the name, then the id, then a positional fallback.
func componentName(c Component, i int) string {
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	if strings.TrimSpace(c.ID) != "" {
		return c.ID
	}
	return fmt.Sprintf("component %d", i+1)
}

// viewLabel picks a human-readable name for a dynamic view in finding messages: its
// title, else its key, else its use-case id.
func viewLabel(dv DynamicView) string {
	switch {
	case strings.TrimSpace(dv.Title) != "":
		return dv.Title
	case strings.TrimSpace(dv.Key) != "":
		return dv.Key
	default:
		return dv.UseCaseID
	}
}
