package methodcheck

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// surfaces_clean.json: a mission listing all four delivery surfaces and a System with one Client
// per surface. Two core use cases (add-a-todo, review-todos) each enter team-member through the
// web and mcpApp Clients and team-agent through the mcpTools and rest Clients, every pair an alt
// group at step s1. The second use case keeps an actor's use of a surface visible when a test
// strips it from the first (Uses(A) is read from every view).

func cleanSurfaceProject(t *testing.T) Project {
	t.Helper()
	return loadProject(t, "surfaces_clean.json")
}

func editSurfaceMission(t *testing.T, p Project, edit func(*MissionStatement)) Project {
	t.Helper()
	m, ok, err := p.mission()
	if err != nil || !ok {
		t.Fatalf("mission: %v ok=%v", err, ok)
	}
	edit(&m)
	return withSlotModel(t, p, kindMission, m)
}

func editSurfaceSystem(t *testing.T, p Project, edit func(*System)) Project {
	t.Helper()
	s, ok, err := p.system()
	if err != nil || !ok {
		t.Fatalf("system: %v ok=%v", err, ok)
	}
	edit(&s)
	return withSlotModel(t, p, kindSystem, s)
}

func withSlotModel(t *testing.T, p Project, kind int, model any) Project {
	t.Helper()
	raw, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	slots := make(map[string]Slot, len(p.Slots))
	for k, s := range p.Slots {
		if s.Kind == kind {
			s.Model = raw
		}
		slots[k] = s
	}
	p.Slots = slots
	return p
}

func withSurfaces(t *testing.T, p Project, ds ...DeliverySurface) Project {
	t.Helper()
	return editSurfaceMission(t, p, func(m *MissionStatement) { m.DeliverySurfaces = ds })
}

// withoutComponent drops a component, its relationships and its calls in every view.
func withoutComponent(t *testing.T, p Project, id string) Project {
	t.Helper()
	return editSurfaceSystem(t, p, func(s *System) {
		s.Components = slices.DeleteFunc(s.Components, func(c Component) bool { return c.ID == id })
		s.Relationships = slices.DeleteFunc(s.Relationships, func(r Relationship) bool { return r.From == id || r.To == id })
		for i := range s.DynamicViews {
			for j := range s.DynamicViews[i].Steps {
				st := &s.DynamicViews[i].Steps[j]
				st.Calls = slices.DeleteFunc(st.Calls, func(c TraceCall) bool { return c.From == id || c.To == id })
			}
		}
	})
}

// withoutCall removes the from→to call from one step of one use case's view.
func withoutCall(t *testing.T, p Project, uc, step, from, to string) Project {
	t.Helper()
	return editSurfaceSystem(t, p, func(s *System) {
		for i := range s.DynamicViews {
			if s.DynamicViews[i].UseCaseID != uc {
				continue
			}
			for j := range s.DynamicViews[i].Steps {
				st := &s.DynamicViews[i].Steps[j]
				if st.ActivityNodeID == step {
					st.Calls = slices.DeleteFunc(st.Calls, func(c TraceCall) bool { return c.From == from && c.To == to })
				}
			}
		}
	})
}

// withCall sets (or adds) the from→to call of one step of one use case's view with the given alt.
func withCall(t *testing.T, p Project, uc, step, from, to, alt string) Project {
	t.Helper()
	return editSurfaceSystem(t, p, func(s *System) {
		for i := range s.DynamicViews {
			if s.DynamicViews[i].UseCaseID != uc {
				continue
			}
			for j := range s.DynamicViews[i].Steps {
				st := &s.DynamicViews[i].Steps[j]
				if st.ActivityNodeID != step {
					continue
				}
				for k := range st.Calls {
					if st.Calls[k].From == from && st.Calls[k].To == to {
						st.Calls[k].Alt = alt
						return
					}
				}
				st.Calls = append(st.Calls, TraceCall{From: from, To: to, Mode: "sync", Label: "Act", Alt: alt})
			}
		}
	})
}

func withWaiver(t *testing.T, p Project, section, justification string) Project {
	t.Helper()
	return editSurfaceSystem(t, p, func(s *System) {
		s.Waivers = append(s.Waivers, CheckItem{Section: section, Guideline: "SYS-SURFACE-ENTRY", Status: "waived", Justification: justification})
	})
}

func surfaceFindingsOf(t *testing.T, p Project, supported []Surface, prefixes ...string) []Finding {
	t.Helper()
	fs, err := ValidateProject(p, ValidateOptions{SupportedSurfaces: supported})
	if err != nil {
		t.Fatal(err)
	}
	var out []Finding
	for _, f := range fs {
		for _, pre := range prefixes {
			if strings.HasPrefix(string(f.RuleID), pre) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

func requireOneError(t *testing.T, fs []Finding, rule RuleID, contains ...string) {
	t.Helper()
	if len(fs) != 1 {
		t.Fatalf("want exactly one finding, got %d: %+v", len(fs), fs)
	}
	f := fs[0]
	if f.RuleID != rule || f.Severity != SeverityError {
		t.Fatalf("want an Error %s, got %+v", rule, f)
	}
	for _, c := range contains {
		if !strings.Contains(f.Message, c) {
			t.Errorf("message %q lacks %q", f.Message, c)
		}
	}
}

func TestSYS_SurfaceRequired_Passes(t *testing.T) {
	p := cleanSurfaceProject(t)
	if fs := surfaceFindingsOf(t, p, Surfaces(), "SYS-SURFACE-", "GEN-"); len(fs) != 0 {
		t.Fatalf("clean fixture: %+v", fs)
	}
}

func TestSYS_SurfaceRequired_FiresOnMissingRest(t *testing.T) {
	p := withoutComponent(t, cleanSurfaceProject(t), "todo-api")
	fs := surfaceFindingsOf(t, p, Surfaces(), "SYS-SURFACE-REQUIRED")
	requireOneError(t, fs, ruleSysSurfaceRequired, "rest", "reachable as an ordinary web API")
}

// RUN-021: an MCP tool server is not an MCP Apps surface. The mcpApp entry stays unmet although
// todo-mcp-server (mcpTools) remains.
func TestSYS_SurfaceRequired_KindMustMatch(t *testing.T) {
	p := withoutComponent(t, cleanSurfaceProject(t), "todo-mcp-app")
	fs := surfaceFindingsOf(t, p, Surfaces(), "SYS-SURFACE-REQUIRED")
	requireOneError(t, fs, ruleSysSurfaceRequired, "mcpApp")

	p = withSurfaces(t, cleanSurfaceProject(t),
		DeliverySurface{Kind: SurfaceWeb, Audience: AudienceHuman, Source: "an ordinary web app", Objective: 1},
		DeliverySurface{Kind: SurfaceMCPTools, Audience: AudienceHuman, Source: "an agent can drive it", Objective: 1},
	)
	fs = surfaceFindingsOf(t, p, Surfaces(), "SYS-SURFACE-REQUIRED")
	requireOneError(t, fs, ruleSysSurfaceRequired, "human audience")
}

func TestSYS_SurfaceEntry_FiresWithoutAlt(t *testing.T) {
	p := withoutCall(t, cleanSurfaceProject(t), "add-a-todo", "s1", "team-member", "todo-mcp-app")
	fs := surfaceFindingsOf(t, p, Surfaces(), "SYS-SURFACE-ENTRY")
	requireOneError(t, fs, ruleSysSurfaceEntry, "add-a-todo", "team-member", "mcpApp")

	p = withCall(t, cleanSurfaceProject(t), "add-a-todo", "s1", "team-member", "todo-mcp-app", "")
	fs = surfaceFindingsOf(t, p, Surfaces(), "SYS-SURFACE-ENTRY")
	requireOneError(t, fs, ruleSysSurfaceEntry, "not one alt group")
}

func TestSYS_SurfaceEntry_WaivedPerUseCase(t *testing.T) {
	p := withoutCall(t, cleanSurfaceProject(t), "add-a-todo", "s1", "team-member", "todo-mcp-app")
	waived := withWaiver(t, p, "add-a-todo", "x")
	if fs := surfaceFindingsOf(t, waived, Surfaces(), "SYS-SURFACE-ENTRY"); len(fs) != 0 {
		t.Fatalf("waived: %+v", fs)
	}
	blank := withWaiver(t, p, "add-a-todo", "")
	requireOneError(t, surfaceFindingsOf(t, blank, Surfaces(), "SYS-SURFACE-ENTRY"), ruleSysSurfaceEntry, "add-a-todo")
}

func TestSYS_SurfaceEntry_OnlyRequiredSurfaces(t *testing.T) {
	p := withSurfaces(t, cleanSurfaceProject(t),
		DeliverySurface{Kind: SurfaceWeb, Audience: AudienceHuman, Source: "an ordinary web app", Objective: 1},
		DeliverySurface{Kind: SurfaceMCPApp, Audience: AudienceHuman, Source: "inside their MCP host", Objective: 1},
		DeliverySurface{Kind: SurfaceREST, Audience: AudienceMachine, Source: "reachable as an ordinary web API", Objective: 1},
	)
	p = withoutCall(t, p, "add-a-todo", "s1", "team-agent", "todo-mcp-server")
	if fs := surfaceFindingsOf(t, p, Surfaces(), "SYS-SURFACE-ENTRY"); len(fs) != 0 {
		t.Fatalf("an unlisted surface is not owed: %+v", fs)
	}
}

func TestGEN_SurfaceSupported_RefusesUnsupported(t *testing.T) {
	p := cleanSurfaceProject(t)
	fs := surfaceFindingsOf(t, p, []Surface{SurfaceWeb, SurfaceREST, SurfaceMCPTools}, "GEN-SURFACE-SUPPORTED")
	requireOneError(t, fs, ruleGenSurfaceSupported, "todo-mcp-app", "mcpApp")

	if fs := surfaceFindingsOf(t, p, Surfaces(), "GEN-SURFACE-SUPPORTED"); len(fs) != 0 {
		t.Fatalf("every surface supported: %+v", fs)
	}
	// Fail closed: no list refuses every Client that states a surface.
	if fs := surfaceFindingsOf(t, p, nil, "GEN-SURFACE-SUPPORTED"); len(fs) != 4 {
		t.Fatalf("nil list: want one per Client (4), got %d: %+v", len(fs), fs)
	}
}
