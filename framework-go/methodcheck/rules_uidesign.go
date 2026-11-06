package methodcheck

import (
	"sort"
	"strings"
)

// rules_uidesign.go is the UID-* family over a surface's typed UI design and the
// TP-UI-* rules that resolve a browser step's targets through it (FPC plan 2, Task 2.5).
// Every finding is an Error (tpFinding); the app downgrades a rule to a Warning only
// under a dated ruleWaiver.

// Rule ids of the UID and TP-UI families.
const (
	// ruleUIDRole: an element's role is in the closed role set.
	ruleUIDRole RuleID = "UID-ROLE"
	// ruleUIDName: an element is located by an accessible name, except the structural roles.
	ruleUIDName RuleID = "UID-NAME"
	// ruleUIDIDUnique: an element id names exactly one element of the design.
	ruleUIDIDUnique RuleID = "UID-ID-UNIQUE"
	// ruleUIDWithin: an element's container is an element of the same screen.
	ruleUIDWithin RuleID = "UID-WITHIN"
	// ruleUIDRoute: a navigate or url names a screen route of the design.
	ruleUIDRoute RuleID = "UID-ROUTE"
	// ruleTPUITarget: a step's targets resolve in the design, agree with the action's role,
	// and a count is of a repeating element.
	ruleTPUITarget RuleID = "TP-UI-TARGET"
	// ruleTPUIAction: a browser step carries exactly one action input.
	ruleTPUIAction RuleID = "TP-UI-ACTION"
)

// uiDesignRuleIDs is this family's entry in the emitted-rule registry (emitters.go).
var uiDesignRuleIDs = []RuleID{
	ruleUIDRole, ruleUIDName, ruleUIDIDUnique, ruleUIDWithin, ruleUIDRoute,
	ruleTPUITarget, ruleTPUIAction,
}

// uiDesignFindings runs the UID rules over one recorded design (key is its uiDesign key).
func uiDesignFindings(key string, d UIDesignRecord) []Finding {
	l := loc(0, "phaseArtifacts.uiDesign."+key)
	var out []Finding
	seen := map[string]bool{}
	for _, s := range d.Screens {
		onScreen := map[string]bool{}
		for _, e := range s.Elements {
			onScreen[e.ID] = true
		}
		for _, e := range s.Elements {
			out = append(out, uiElementFindings(key, s, e, onScreen, seen, l)...)
		}
	}
	return out
}

// uiElementFindings is the per-element UID rules; seen carries the ids met so far.
func uiElementFindings(key string, s UIScreen, e UIElement, onScreen, seen map[string]bool, l *Location) []Finding {
	var out []Finding
	if !isKnownRole(e.Role) {
		out = append(out, tpFinding(ruleUIDRole, l, "%s screen %s element %q: role %q is not one of %s", key, s.ID, e.ID, e.Role, roleList()))
	} else if e.Name == "" && !nameOptional(e.Role) {
		out = append(out, tpFinding(ruleUIDName, l, "%s screen %s element %q: a %s is located by its accessible name; give it one", key, s.ID, e.ID, e.Role))
	}
	if seen[e.ID] {
		out = append(out, tpFinding(ruleUIDIDUnique, l, "%s screen %s: element id %q is used more than once in the design", key, s.ID, e.ID))
	}
	seen[e.ID] = true
	if e.Within != "" && (e.Within == e.ID || !onScreen[e.Within]) {
		out = append(out, tpFinding(ruleUIDWithin, l, "%s screen %s element %q: within %q is not another element of this screen", key, s.ID, e.ID, e.Within))
	}
	return out
}

func isKnownRole(r UIRole) bool {
	for _, k := range UIRoles() {
		if k == r {
			return true
		}
	}
	return false
}

func roleList() string {
	roles := UIRoles()
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = string(r)
	}
	return strings.Join(names, ", ")
}

// uiDesignRecordFindings runs the UID rules over every recorded design, in key order.
func uiDesignRecordFindings(p Project) []Finding {
	if p.PhaseArtifacts == nil {
		return nil
	}
	keys := make([]string, 0, len(p.PhaseArtifacts.UIDesign))
	for k := range p.PhaseArtifacts.UIDesign {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Finding
	for _, k := range keys {
		out = append(out, uiDesignFindings(k, p.PhaseArtifacts.UIDesign[k])...)
	}
	return out
}

// uiStepFindings resolves a browser step's targets through the surface's design:
// TP-UI-TARGET (each target is an element of the design; the action's role agreement; a
// count is of a repeating element), UID-ROUTE (navigate and url name a screen route).
// The step's inputs and observations are already decoded; one that did not decode is
// TP-ARG-TYPE's / TP-EXPECT's and is skipped here.
func (sc tpScope) uiStepFindings(actions []UIAction, obs []UIObservable, section string) []Finding {
	if len(actions) == 0 && len(obs) == 0 {
		return nil
	}
	design, ok := sc.design()
	if !ok {
		return []Finding{tpFinding(ruleTPUITarget, sc.l, "%s: no uiDesign is recorded for surface %q, so no target resolves: record the surface's UI design (screens and elements) first", section, sc.comp)}
	}
	var out []Finding
	for _, a := range actions {
		out = append(out, actionTargetFindings(design, a, section, sc.l)...)
	}
	for _, o := range obs {
		out = append(out, observableTargetFindings(design, o, section, sc.l)...)
	}
	return out
}

// design resolves the scope's surface (the plan key, then the contract's component).
func (sc tpScope) design() (UIDesignRecord, bool) {
	if d, ok := findDesign(sc.p, sc.comp); ok {
		return d, true
	}
	return findDesign(sc.p, sc.contract.Component)
}

func actionTargetFindings(d UIDesignRecord, a UIAction, section string, l *Location) []Finding {
	if nav, ok := a.(UINavigate); ok {
		if !d.HasRoute(nav.Route) {
			return []Finding{tpFinding(ruleUIDRoute, l, "%s: navigate to %q, which is not a screen route of the design (%s)", section, nav.Route, d.routeNames())}
		}
		return nil
	}
	t, ok := ActionTarget(a)
	if !ok {
		return nil
	}
	el, _, found := d.Element(t.Element)
	if !found {
		return []Finding{tpFinding(ruleTPUITarget, l, "%s: %s targets element %q, which the design does not have", section, a.Kind(), t.Element)}
	}
	if !hasRole(ActionRoles(a.Kind()), el.Role) {
		return []Finding{tpFinding(ruleTPUITarget, l, "%s: %s cannot act on element %q, whose role is %s (it needs %s)", section, a.Kind(), t.Element, el.Role, rolesText(ActionRoles(a.Kind())))}
	}
	return nil
}

func observableTargetFindings(d UIDesignRecord, o UIObservable, section string, l *Location) []Finding {
	if u, ok := o.(UIURL); ok {
		if !d.HasRoute(u.Route) {
			return []Finding{tpFinding(ruleUIDRoute, l, "%s: the url observation names %q, which is not a screen route of the design (%s)", section, u.Route, d.routeNames())}
		}
		return nil
	}
	t, ok := ObservableTarget(o)
	if !ok {
		return nil
	}
	el, _, found := d.Element(t.Element)
	if !found {
		return []Finding{tpFinding(ruleTPUITarget, l, "%s: %s observes element %q, which the design does not have", section, o.Kind(), t.Element)}
	}
	if _, isCount := o.(UICount); isCount && !el.Repeats {
		return []Finding{tpFinding(ruleTPUITarget, l, "%s: count observes element %q, which does not repeat; mark it repeats in the design or observe it another way", section, t.Element)}
	}
	return nil
}

func rolesText(rs []UIRole) string {
	names := make([]string, len(rs))
	for i, r := range rs {
		names[i] = string(r)
	}
	return strings.Join(names, " or ")
}

// routeNames lists a design's screen routes for a diagnostic.
func (d UIDesignRecord) routeNames() string {
	var rs []string
	for _, s := range d.Screens {
		rs = append(rs, s.Route)
	}
	return strings.Join(rs, ", ")
}
