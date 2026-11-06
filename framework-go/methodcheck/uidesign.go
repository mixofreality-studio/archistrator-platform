package methodcheck

import (
	"slices"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// UIDesignRecord is a surface's typed UI design (`.phaseArtifacts.uiDesign[<surface>]`):
// the screens it has and, per screen, the elements a user sees and operates, each with
// the role and accessible name a user would find it by. It is the one place a control's
// role and name are written down; UI test steps target elements by id, and the generated
// test derives its locator (role, name, containing element) from here. Content is
// narrative nothing reads (GC-3).
type UIDesignRecord struct {
	Surface string     `json:"surface"`
	Screens []UIScreen `json:"screens"`
	Content string     `json:"content"`
}

// UIScreen is one route of the surface and the elements on it.
type UIScreen struct {
	ID       string      `json:"id"`
	Route    string      `json:"route"`
	Elements []UIElement `json:"elements"`
}

// UIElement is one thing on a screen. Role and Name locate it the way a user does; Within
// names the element that contains it (empty: the screen); Repeats marks an element that
// appears once per item, so a target may count or index it.
type UIElement struct {
	ID      string `json:"id"`
	Role    UIRole `json:"role"`
	Name    string `json:"name"`
	Within  string `json:"within,omitempty"`
	Repeats bool   `json:"repeats"`
}

// Element finds the element with the id anywhere in the design, with its screen.
func (d UIDesignRecord) Element(id string) (UIElement, UIScreen, bool) {
	for _, s := range d.Screens {
		for _, e := range s.Elements {
			if e.ID == id {
				return e, s, true
			}
		}
	}
	return UIElement{}, UIScreen{}, false
}

// HasRoute reports whether some screen of the design is at route.
func (d UIDesignRecord) HasRoute(route string) bool {
	return slices.ContainsFunc(d.Screens, func(s UIScreen) bool { return s.Route == route })
}

// findDesign resolves a surface to its recorded UI design: the uiDesign key or the
// record's own surface, under scenario.Normalize equality.
func findDesign(p Project, surface string) (UIDesignRecord, bool) {
	if p.PhaseArtifacts == nil || surface == "" {
		return UIDesignRecord{}, false
	}
	want := scenario.Normalize(surface)
	for key, d := range p.PhaseArtifacts.UIDesign {
		if scenario.Normalize(key) == want || scenario.Normalize(d.Surface) == want {
			return d, true
		}
	}
	return UIDesignRecord{}, false
}

// nameOptional reports the roles that are located by structure, not by an accessible name.
func nameOptional(r UIRole) bool {
	switch r {
	case UIRoleList, UIRoleMain, UIRoleNavigation, UIRoleForm:
		return true
	case UIRoleButton, UIRoleCheckbox, UIRoleTextbox, UIRoleListitem, UIRoleLink, UIRoleHeading,
		UIRoleStatus, UIRoleDialog, UIRoleOption, UIRoleCombobox, UIRoleSwitch, UIRoleTab,
		UIRoleTabpanel, UIRoleTable, UIRoleRow, UIRoleCell, UIRoleImg:
		return false
	}
	return false
}
