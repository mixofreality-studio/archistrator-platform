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
	// ContractKey is the OPTIONAL explicit join back to a serviceContracts key
	// (camelCase, e.g. "systemDesignManager"). When present it is the preferred,
	// exact match in ComponentByContractKey; when absent (documents that predate
	// the field — spec: Schema ownership & evolution, tolerant evolution) the
	// case-convention heuristic is used instead.
	ContractKey string `json:"contractKey,omitempty"`
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
// resolveContractComponentId. Preferred-exact-match with heuristic fallback
// (spec: Schema ownership & evolution, tolerant evolution):
//
//	(0) exact match on an explicit ContractKey — the join the document owns;
//	(1) Kebab(key) match on ID,
//	(2) case-insensitive ID match,
//	(3) case-insensitive Kebab(name) match.
//
// Documents that predate the ContractKey field carry no (0) candidate and fall
// straight through to the heuristic, so behaviour is unchanged for them. Honest
// miss on no match.
func (s *System) ComponentByContractKey(key string) (*SystemComponent, bool) {
	// (0) Explicit contractKey — preferred over every case-convention heuristic.
	for i := range s.Components {
		if s.Components[i].ContractKey != "" && s.Components[i].ContractKey == key {
			return &s.Components[i], true
		}
	}
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
