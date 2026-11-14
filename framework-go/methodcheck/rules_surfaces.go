package methodcheck

import (
	"fmt"
	"slices"
	"strings"
)

// rules_surfaces.go: the design-time rules over the mission's delivery surfaces (FPC W6). The
// brief names the channels the system must be reachable through; the System must have a Client for
// each (SYS-SURFACE-REQUIRED), every core use case must be enterable through each of those
// channels its actor uses (SYS-SURFACE-ENTRY), and a surface the platform cannot generate yet is
// refused at design rather than discovered at construction (GEN-SURFACE-SUPPORTED).

const (
	// ruleSysSurfaceRequired: every mission delivery surface has a Client with that surface, and
	// the audience the mission states agrees with the surface.
	ruleSysSurfaceRequired RuleID = "SYS-SURFACE-REQUIRED"
	// ruleSysSurfaceEntry: a core use case's actor enters through every required surface they use,
	// as one alt group at their first step. A System.waivers record per use case waives it.
	ruleSysSurfaceEntry RuleID = "SYS-SURFACE-ENTRY"
	// ruleGenSurfaceSupported: every Client's surface is one the platform's generators build.
	ruleGenSurfaceSupported RuleID = "GEN-SURFACE-SUPPORTED"
)

// surfaceRuleIDs is this family's entry in the emitted-rule registry (emitters.go).
var surfaceRuleIDs = []RuleID{ruleSysSurfaceRequired, ruleSysSurfaceEntry, ruleGenSurfaceSupported}

// ValidateOptions are the platform facts the design rules need and the document cannot carry.
type ValidateOptions struct {
	// SupportedSurfaces is what the platform's generators can build (appgen.SupportedSurfaces()).
	// Empty fails closed: GEN-SURFACE-SUPPORTED fires on every Client that states a surface.
	SupportedSurfaces []Surface
}

// surfaceFindings runs the three surface rules over the committed slots. A rule that reads a slot
// that is not committed is skipped.
func surfaceFindings(p Project, o ValidateOptions) ([]Finding, error) {
	s, ok, err := p.system()
	if err != nil || !ok {
		return nil, err
	}
	out := surfaceSupported(s, o.SupportedSurfaces)
	m, mok, err := p.mission()
	if err != nil {
		return nil, err
	}
	if !mok {
		return out, nil
	}
	out = append(out, surfaceRequired(m, s)...)
	c, cok, err := p.coreUseCases()
	if err != nil {
		return nil, err
	}
	if cok {
		out = append(out, surfaceEntry(m, s, c)...)
	}
	return out, nil
}

func surfaceFinding(id RuleID, l *Location, format string, args ...any) Finding {
	return Finding{RuleID: id, Severity: SeverityError, Message: fmt.Sprintf(format, args...), Location: l}
}

// surfaceRequired: each delivery surface needs a Client with that surface (RUN-021: an MCP tool
// server is not an MCP Apps UI, so the kind must match exactly), and the stated audience must be
// the one the surface serves.
func surfaceRequired(m MissionStatement, s System) []Finding {
	have := map[Surface]bool{}
	for _, c := range s.Components {
		have[c.Surface] = have[c.Surface] || c.Kind == kindClient
	}
	var out []Finding
	for i, d := range m.DeliverySurfaces {
		l := loc(i+1, "mission deliverySurfaces["+string(d.Kind)+"]")
		out = append(out, audienceFindings(d, l)...)
		if !have[d.Kind] {
			out = append(out, surfaceFinding(ruleSysSurfaceRequired, l,
				"the mission commits to a %s surface (%q) but no Client has surface %s", d.Kind, d.Source, d.Kind))
		}
	}
	return out
}

func audienceFindings(d DeliverySurface, l *Location) []Finding {
	k, err := ParseSurface(string(d.Kind))
	if err != nil {
		return nil
	}
	want := AudienceOf(k)
	if d.Audience == want {
		return nil
	}
	return []Finding{surfaceFinding(ruleSysSurfaceRequired, l,
		"the mission lists %s for a %s audience, but %s serves a %s audience", d.Kind, d.Audience, d.Kind, want)}
}

// entryEnv is what SYS-SURFACE-ENTRY reads: the required surfaces and the component index.
type entryEnv struct {
	required map[Surface]bool
	idx      map[string]Component
}

// surfaceOf is the surface of a Client component; false when id is not a Client that states one.
func (e entryEnv) surfaceOf(id string) (Surface, bool) {
	c, ok := e.idx[id]
	if !ok {
		return "", false
	}
	if _, ok := c.surfaceKind(); !ok {
		return "", false
	}
	return c.Surface, true
}

// uses maps each actor (a call source that is no component) to the required surfaces it calls
// into, in any view, in declaration order of Surfaces().
func (e entryEnv) uses(s System) map[string][]Surface {
	sets := map[string]map[Surface]bool{}
	for _, call := range allCalls(s) {
		sf, ok := e.surfaceOf(call.To)
		if _, isComp := e.idx[call.From]; isComp || !ok || !e.required[sf] {
			continue
		}
		if sets[call.From] == nil {
			sets[call.From] = map[Surface]bool{}
		}
		sets[call.From][sf] = true
	}
	out := make(map[string][]Surface, len(sets))
	for actor, set := range sets {
		out[actor] = slices.DeleteFunc(Surfaces(), func(sf Surface) bool { return !set[sf] })
	}
	return out
}

// allCalls flattens every call of every step of every view.
func allCalls(s System) []TraceCall {
	var out []TraceCall
	for _, dv := range s.DynamicViews {
		for _, st := range dv.Steps {
			out = append(out, st.Calls...)
		}
	}
	return out
}

// surfaceEntry checks each core use case's actor entry (D7). An actor's entry step is the first
// step of the view that holds a call from the actor into a Client; it must hold one call per
// required surface the actor uses anywhere, and when that is more than one surface the calls share
// one non-empty alt.
func surfaceEntry(m MissionStatement, s System, c CoreUseCases) []Finding {
	e := entryEnv{required: map[Surface]bool{}, idx: componentIndex(s)}
	for _, d := range m.DeliverySurfaces {
		e.required[d.Kind] = true
	}
	uses := e.uses(s)
	ucByID := useCaseIndex(c)
	var out []Finding
	for i, dv := range s.DynamicViews {
		uc, ok := ucByID[dv.UseCaseID]
		if !ok || uc.Classification != classCore || surfaceEntryWaived(s, uc.ID) {
			continue
		}
		for _, actor := range uc.Actors {
			if need := uses[actor.ID]; len(need) > 0 {
				out = append(out, e.actorEntry(i+1, dv, uc.ID, actor.ID, need)...)
			}
		}
	}
	return out
}

// actorEntry checks one actor's entry step of one view.
func (e entryEnv) actorEntry(ordinal int, dv DynamicView, ucID, actor string, need []Surface) []Finding {
	for _, st := range dv.Steps {
		calls := e.clientCalls(st, actor)
		if len(calls) == 0 {
			continue
		}
		l := loc(ordinal, "dynamicView "+ccKeyLabel(dv)+" step "+st.ActivityNodeID)
		return e.stepFindings(l, ucID, actor, st.ActivityNodeID, need, calls)
	}
	return nil
}

// clientCalls are the step's calls from the actor into a Client.
func (e entryEnv) clientCalls(st CallStep, actor string) []TraceCall {
	var out []TraceCall
	for _, call := range st.Calls {
		if _, ok := e.surfaceOf(call.To); ok && call.From == actor {
			out = append(out, call)
		}
	}
	return out
}

func (e entryEnv) stepFindings(l *Location, ucID, actor, step string, need []Surface, calls []TraceCall) []Finding {
	var out []Finding
	var group []TraceCall
	for _, sf := range need {
		i := slices.IndexFunc(calls, func(c TraceCall) bool { got, _ := e.surfaceOf(c.To); return got == sf })
		if i < 0 {
			out = append(out, surfaceFinding(ruleSysSurfaceEntry, l,
				"use case %q: %s enters at step %q without a call into a %s Client", ucID, actor, step, sf))
			continue
		}
		group = append(group, calls[i])
	}
	if len(out) == 0 && len(group) > 1 && !oneAltGroup(group) {
		out = append(out, surfaceFinding(ruleSysSurfaceEntry, l,
			"use case %q: %s's calls into its surfaces at step %q are not one alt group", ucID, actor, step))
	}
	return out
}

func oneAltGroup(calls []TraceCall) bool {
	return calls[0].Alt != "" && !slices.ContainsFunc(calls[1:], func(c TraceCall) bool { return c.Alt != calls[0].Alt })
}

// surfaceEntryWaived: a System.waivers record for the use case, with a justification.
func surfaceEntryWaived(s System, useCaseID string) bool {
	return slices.ContainsFunc(s.Waivers, func(w CheckItem) bool {
		return w.Guideline == string(ruleSysSurfaceEntry) && w.Section == useCaseID &&
			w.Status == checkWaived && strings.TrimSpace(w.Justification) != ""
	})
}

// surfaceSupported: every Client surface is one the generators build.
func surfaceSupported(s System, supported []Surface) []Finding {
	names := make([]string, len(supported))
	for i, sf := range supported {
		names[i] = string(sf)
	}
	var out []Finding
	for i, c := range s.Components {
		if c.Kind != kindClient || c.Surface == "" || slices.Contains(supported, c.Surface) {
			continue
		}
		out = append(out, surfaceFinding(ruleGenSurfaceSupported, loc(i+1, "component "+c.ID),
			"Client %q states surface %s, which the platform cannot generate yet (supported: %s)", c.ID, c.Surface, strings.Join(names, ", ")))
	}
	return out
}
