package projectmodel

import (
	"encoding/json"
	"os"
	"testing"
)

func loadFixtureSystem(t *testing.T) *System {
	t.Helper()
	raw, err := os.ReadFile("testdata/archistrator.project.json")
	if err != nil {
		t.Fatal(err)
	}
	var top struct {
		Slots map[string]struct {
			Kind  int             `json:"kind"`
			Model json.RawMessage `json:"model"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	s, err := ParseSystem(top.Slots["5"].Model)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseSystemFixture(t *testing.T) {
	s := loadFixtureSystem(t)
	if len(s.Components) != 39 {
		t.Fatalf("components: %d", len(s.Components))
	}
	if len(s.Relationships) != 71 {
		t.Fatalf("relationships: %d", len(s.Relationships))
	}
}

func TestComponentByContractKey(t *testing.T) {
	s := loadFixtureSystem(t)
	c, ok := s.ComponentByContractKey("systemDesignManager")
	if !ok || c.ID != "system-design-manager" || c.Name != "SystemDesignManager" {
		t.Fatalf("join failed: %+v ok=%v", c, ok)
	}
	if _, ok := s.ComponentByContractKey("noSuchThing"); ok {
		t.Fatal("expected honest miss")
	}
}

func TestComponentByContractKeyKebabFallback(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		system    *System
		wantFound bool
		wantID    string
	}{
		{
			name: "step 2 case-insensitive kebab match (mixed case ID)",
			key:  "systemDesignManager",
			system: &System{
				Components: []SystemComponent{
					{ID: "System-Design-Manager", Name: "SystemDesignManager"},
				},
			},
			wantFound: true,
			wantID:    "System-Design-Manager",
		},
		{
			name: "step 2 kebab fallback with unrelated name",
			key:  "usageAccess",
			system: &System{
				Components: []SystemComponent{
					{ID: "Usage-Access", Name: "TotallyDifferent"},
				},
			},
			wantFound: true,
			wantID:    "Usage-Access",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := tt.system.ComponentByContractKey(tt.key)
			if ok != tt.wantFound {
				t.Fatalf("ComponentByContractKey(%q) found=%v, want=%v", tt.key, ok, tt.wantFound)
			}
			if ok && c.ID != tt.wantID {
				t.Fatalf("ComponentByContractKey(%q) ID=%q, want=%q", tt.key, c.ID, tt.wantID)
			}
		})
	}
}
