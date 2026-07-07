package projectmodel

import (
	"encoding/json"
	"strings"
)

// System is the codegen-relevant slice of the systemDesign slot model
// (slots[kind=5].model): the component inventory + the relationship edges.
// dynamicViews and all other fields are deliberately not parsed (tolerant
// subset — spec: Schema ownership & evolution).
type System struct {
	Components    []SystemComponent `json:"components"`
	Relationships []Relationship    `json:"relationships"`
}

type SystemComponent struct {
	ID    string `json:"id"`   // kebab-case ("system-design-manager")
	Name  string `json:"name"` // PascalCase ("SystemDesignManager")
	Kind  string `json:"kind"`
	Layer string `json:"layer"`
}

type Relationship struct {
	From  string `json:"from"` // kebab component id
	To    string `json:"to"`
	Mode  string `json:"mode"` // "sync" | "queued"
	Label string `json:"label"`
}

func ParseSystem(slotModel json.RawMessage) (*System, error) {
	var s System
	if err := json.Unmarshal(slotModel, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ComponentByContractKey joins a camelCase serviceContracts key to its
// systemDesign component — the Go port of the webApp's
// resolveContractComponentId: (1) Kebab(key) match on ID, (2) case-insensitive
// ID match, (3) case-insensitive Kebab(name) match. Honest miss on no match.
func (s *System) ComponentByContractKey(key string) (*SystemComponent, bool) {
	kebab := Kebab(key)
	for i := range s.Components {
		if s.Components[i].ID == kebab {
			return &s.Components[i], true
		}
	}
	for i := range s.Components {
		if strings.EqualFold(s.Components[i].ID, kebab) {
			return &s.Components[i], true
		}
	}
	for i := range s.Components {
		if strings.EqualFold(Kebab(s.Components[i].Name), kebab) {
			return &s.Components[i], true
		}
	}
	return nil, false
}
