package methodcheck

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSurfaces_DecodeMission(t *testing.T) {
	const raw = `{"id":"p","version":1,"slots":{"0":{"kind":0,"status":2,"model":{
	  "vision":"v","mission":"m",
	  "objectives":[{"number":7,"statement":"s"}],
	  "deliverySurfaces":[{"kind":"mcpApp","audience":"human","source":"plus an MCP Apps surface","objective":7}]}}}}`
	var p Project
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	m, ok, err := p.mission()
	if err != nil || !ok {
		t.Fatalf("mission: ok=%v err=%v", ok, err)
	}
	want := DeliverySurface{Kind: SurfaceMCPApp, Audience: AudienceHuman, Source: "plus an MCP Apps surface", Objective: 7}
	if len(m.DeliverySurfaces) != 1 || m.DeliverySurfaces[0] != want {
		t.Fatalf("deliverySurfaces = %+v, want [%+v]", m.DeliverySurfaces, want)
	}
}

func TestSYS_ClientSurface_Fires(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*System)
		want string // the component the single finding names
	}{
		{"a built client with no surface", func(s *System) { s.Components[0].Surface = "" }, "WebApp"},
		{"surface mcp", func(s *System) { s.Components[0].Surface = "mcp" }, "WebApp"},
		{"a manager with surface web", func(s *System) { s.Components[3].Surface = SurfaceWeb }, "ListManager"},
		{"two rest clients", func(s *System) { s.Components[2].Surface = SurfaceREST }, "McpServer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sys, _, _ := clientDesign(t)
			tc.edit(&sys)
			var got []Finding
			for _, f := range clientSurfaceStated(sys) {
				if f.RuleID == ruleSysClientSurface {
					got = append(got, f)
				}
			}
			if len(got) != 1 {
				t.Fatalf("want exactly one %s finding, got %+v", ruleSysClientSurface, got)
			}
			if got[0].Severity != SeverityError || !strings.Contains(got[0].Message, tc.want) {
				t.Errorf("got %+v, want an Error naming %s", got[0], tc.want)
			}
		})
	}
}

func TestSYS_ClientSurface_PlannedMayOmit(t *testing.T) {
	sys, _, _ := clientDesign(t)
	sys.Components[0].Surface = ""
	sys.Components[0].BuildStatus = buildStatusPlanned
	if got := clientSurfaceStated(sys); len(got) != 0 {
		t.Fatalf("a planned client may omit its surface: %+v", got)
	}
	sys.Components[0].BuildStatus = buildStatusExternal
	if got := clientSurfaceStated(sys); len(got) != 0 {
		t.Fatalf("an external client may omit its surface: %+v", got)
	}
}

// describeSurface switches over the four variants with no default: gochecksumtype keeps it
// exhaustive.
func describeSurface(k SurfaceKind) string {
	switch k.(type) {
	case WebSurface:
		return "web"
	case RESTSurface:
		return "rest"
	case MCPToolsSurface:
		return "mcpTools"
	case MCPAppSurface:
		return "mcpApp"
	}
	return ""
}

func TestSurfaceKind_SwitchIsExhaustive(t *testing.T) {
	for _, s := range Surfaces() {
		k, err := ParseSurface(string(s))
		if err != nil {
			t.Fatal(err)
		}
		if got := describeSurface(k); got != string(s) {
			t.Errorf("describe(%s) = %q", s, got)
		}
		wantAudience := AudienceMachine
		if s == SurfaceWeb || s == SurfaceMCPApp {
			wantAudience = AudienceHuman
		}
		if got := AudienceOf(k); got != wantAudience {
			t.Errorf("AudienceOf(%s) = %s, want %s", s, got, wantAudience)
		}
	}
}
