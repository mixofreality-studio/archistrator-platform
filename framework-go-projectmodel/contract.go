package projectmodel

import (
	"encoding/json"
	"fmt"
)

// Contract is one parsed .serviceContracts entry: the self-describing metadata
// plus the embedded contract document (title/$defs/interface). Unknown fields
// are ignored and absent fields default — every old document parses under
// every newer projectmodel (spec: Schema ownership & evolution).
type Contract struct {
	Key       string // the serviceContracts map key (camelCase component key)
	Component string // self-describing component field (== Key today)
	Layer     string // "Manager" | "Engine" | "ResourceAccess" | "Client" | "Utility"
	GoPackage string // e.g. "internal/manager/billing"; empty ⇒ no Go emission target
	Infra     []string
	Stub      bool
	Deps      []Dep
	Doc       *Doc
}

// Dep is one DI constructor dependency. Exactly one of Component / GoType is
// set: a COMPONENT dep references another serviceContracts entry; a PLAIN dep
// carries a verbatim Go type (+ optional import).
type Dep struct {
	Name      string `json:"name"`
	Component string `json:"component,omitempty"`
	GoType    string `json:"goType,omitempty"`
	GoImport  string `json:"goImport,omitempty"`
}

// ParseContract parses one serviceContracts entry. The raw entry doubles as
// the contract document Parse consumes (title/$defs/interface).
func ParseContract(key string, raw json.RawMessage) (*Contract, error) {
	var meta struct {
		Component string   `json:"component"`
		Layer     string   `json:"layer"`
		GoPackage string   `json:"goPackage"`
		Infra     []string `json:"infra"`
		Stub      bool     `json:"stub"`
		Deps      []Dep    `json:"deps"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("contract %s: metadata: %w", key, err)
	}
	doc, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("contract %s: %w", key, err)
	}
	return &Contract{
		Key: key, Component: meta.Component, Layer: meta.Layer,
		GoPackage: meta.GoPackage, Infra: meta.Infra, Stub: meta.Stub,
		Deps: meta.Deps, Doc: doc,
	}, nil
}
