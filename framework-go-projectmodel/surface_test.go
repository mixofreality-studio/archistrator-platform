package projectmodel

import (
	"strings"
	"testing"
)

func TestSurface_ParseIsClosed(t *testing.T) {
	want := []Surface{SurfaceWeb, SurfaceREST, SurfaceMCPTools, SurfaceMCPApp}
	got := Surfaces()
	if len(got) != len(want) {
		t.Fatalf("Surfaces() = %v, want %v", got, want)
	}
	for i, s := range want {
		if got[i] != s {
			t.Errorf("Surfaces()[%d] = %q, want %q", i, got[i], s)
		}
		k, err := ParseSurface(string(s))
		if err != nil {
			t.Fatalf("ParseSurface(%q): %v", s, err)
		}
		if k.Surface() != s {
			t.Errorf("ParseSurface(%q).Surface() = %q", s, k.Surface())
		}
	}
	for _, bad := range []string{"", "mcp", "Web", "rest "} {
		_, err := ParseSurface(bad)
		if err == nil {
			t.Errorf("ParseSurface(%q) must fail", bad)
			continue
		}
		for _, name := range []string{"web", "rest", "mcpTools", "mcpApp"} {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("ParseSurface(%q) error %q does not name %s", bad, err, name)
			}
		}
	}
}

func TestSystemComponent_SurfacePredicates(t *testing.T) {
	cases := []struct {
		name                string
		c                   SystemComponent
		browser, web, tools bool
	}{
		{"web", SystemComponent{Kind: "client", Surface: SurfaceWeb}, true, true, false},
		{"rest", SystemComponent{Kind: "client", Surface: SurfaceREST}, false, false, false},
		{"mcpTools", SystemComponent{Kind: "client", Surface: SurfaceMCPTools}, false, false, true},
		{"mcpApp", SystemComponent{Kind: "client", Surface: SurfaceMCPApp}, true, false, true},
		{"unstated", SystemComponent{Kind: "client"}, false, false, false},
		{"manager with surface web", SystemComponent{Kind: "manager", Surface: SurfaceWeb}, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.IsBrowserSurface(); got != tc.browser {
				t.Errorf("IsBrowserSurface = %v, want %v", got, tc.browser)
			}
			if got := tc.c.IsWebSurface(); got != tc.web {
				t.Errorf("IsWebSurface = %v, want %v", got, tc.web)
			}
			if got := tc.c.ServesMCPTools(); got != tc.tools {
				t.Errorf("ServesMCPTools = %v, want %v", got, tc.tools)
			}
		})
	}
}

func TestBuiltBrowserSurfacesKeepsMcpAppAndWebSurfacesDoNot(t *testing.T) {
	s := &System{Components: []SystemComponent{
		{ID: "web", Kind: "client", Surface: SurfaceWeb},
		{ID: "app", Kind: "client", Surface: SurfaceMCPApp},
		{ID: "later", Kind: "client", Surface: SurfaceWeb, BuildStatus: "planned"},
	}}
	ids := func(cs []SystemComponent) string {
		var out []string
		for _, c := range cs {
			out = append(out, c.ID)
		}
		return strings.Join(out, ",")
	}
	if got := ids(s.BuiltBrowserSurfaces()); got != "web,app" {
		t.Errorf("BuiltBrowserSurfaces = %s, want web,app", got)
	}
	if got := ids(s.BuiltWebSurfaces()); got != "web" {
		t.Errorf("BuiltWebSurfaces = %s, want web", got)
	}
	if !s.HasWebSurface() {
		t.Error("HasWebSurface must be true")
	}
	onlyApp := &System{Components: []SystemComponent{{ID: "app", Kind: "client", Surface: SurfaceMCPApp}}}
	if onlyApp.HasWebSurface() || !onlyApp.HasBrowserSurface() {
		t.Error("an mcpApp alone is a browser surface but not a web surface")
	}
}
