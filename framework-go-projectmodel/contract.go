package projectmodel

import (
	"encoding/json"
	"fmt"
	"go/token"
	"maps"
	"slices"
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
	// Setting names the deployment setting that supplies this plain dependency's value: cfg.<Field>
	// in the production composition, the setting's default in a scenario. It must be of a type
	// compatible with GoType (duration <-> time.Duration, string, bool, int).
	Setting string `json:"setting,omitempty"`
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

// The fields each contract object may carry. ParseContract ignores anything else so an old reader
// parses a newer document; ValidateContract refuses it, because a field no schema defines is a
// misspelling (goPackge) or a guess that no generator will ever read.
var (
	contractFields  = fieldSet("component", "layer", "goPackage", "infra", "stub", "deps", "title", "$id", "$schema", "$defs", "interface", "notes", "railAuthority")
	interfaceFields = fieldSet("name", "layer", "operations")
	operationFields = fieldSet("name", "params", "result", "error", "readOnly", "ui")
	paramFields     = fieldSet("name", "pointer", "schema")
	depFields       = fieldSet("name", "component", "goType", "goImport", "setting")
)

func fieldSet(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

// ValidateContract reports every loadability problem in one serviceContracts entry at once: an
// undecodable document, fields the schema does not define, an unknown layer, zero operations, an
// operation a Go-emitting contract cannot export, and a built Manager/Engine/ResourceAccess
// contract with no goPackage. A contract that emits no Go (a Client or Utility with no goPackage,
// or a stub) may name its operations as it likes: they are not Go identifiers.
func ValidateContract(key string, raw json.RawMessage) Problems {
	base := "serviceContracts." + key
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return Problems{problem(base, CodeDecode, "contract %s: %v", key, err)}
	}
	probs := unknownFields(base, top, contractFields)
	var meta struct {
		Layer     string            `json:"layer"`
		GoPackage string            `json:"goPackage"`
		Stub      bool              `json:"stub"`
		Deps      []json.RawMessage `json:"deps"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return append(probs, problem(base, CodeDecode, "contract %s: metadata: %v", key, err))
	}
	if !knownLayers[meta.Layer] {
		probs = append(probs, problem(base+".layer", CodeLayerUnknown, "contract %s: unknown layer %q", key, meta.Layer))
	}
	if !meta.Stub && meta.GoPackage == "" && isJoinableLayer(meta.Layer) {
		probs = append(probs, problem(base+".goPackage", CodeGoPackageMissing, "contract %s (%s) is built and has no goPackage, so no generator has a package to emit into", key, meta.Layer))
	}
	for i, d := range meta.Deps {
		probs = append(probs, objectFields(fmt.Sprintf("%s.deps[%d]", base, i), d, depFields)...)
	}
	emitsGo := !meta.Stub && (meta.GoPackage != "" || isJoinableLayer(meta.Layer))
	return append(probs, interfaceProblems(base+".interface", key, top["interface"], emitsGo)...)
}

// interfaceProblems checks a contract's interface descriptor: its fields, that it names itself
// and has at least one operation, and each operation.
func interfaceProblems(path, key string, raw json.RawMessage, emitsGo bool) Problems {
	if len(raw) == 0 || string(raw) == "null" {
		return Problems{problem(path, CodeDecode, "contract %s: document has no interface descriptor", key)}
	}
	var iface map[string]json.RawMessage
	if err := json.Unmarshal(raw, &iface); err != nil {
		return Problems{problem(path, CodeDecode, "contract %s: interface: %v", key, err)}
	}
	probs := unknownFields(path, iface, interfaceFields)
	var name string
	_ = json.Unmarshal(iface["name"], &name)
	if name == "" {
		probs = append(probs, problem(path+".name", CodeDecode, "contract %s: document has no interface descriptor", key))
	}
	var ops []json.RawMessage
	if rawOps := iface["operations"]; len(rawOps) != 0 && string(rawOps) != "null" {
		if err := json.Unmarshal(rawOps, &ops); err != nil {
			return append(probs, problem(path+".operations", CodeDecode, "contract %s: operations: %v", key, err))
		}
	}
	if len(ops) == 0 {
		return append(probs, problem(path+".operations", CodeOpsEmpty, "contract %s has no operations: a contract with nothing to call generates nothing", key))
	}
	for i, op := range ops {
		probs = append(probs, operationProblems(fmt.Sprintf("%s.operations[%d]", path, i), op, emitsGo)...)
	}
	return probs
}

// operationProblems checks one operation: its fields, its params' fields, and (when the contract
// emits Go) that its name is an exported Go identifier.
func operationProblems(path string, raw json.RawMessage, emitsGo bool) Problems {
	var op map[string]json.RawMessage
	if err := json.Unmarshal(raw, &op); err != nil {
		return Problems{problem(path, CodeDecode, "operation: %v", err)}
	}
	probs := unknownFields(path, op, operationFields)
	var name string
	_ = json.Unmarshal(op["name"], &name)
	if emitsGo && !token.IsExported(name) {
		probs = append(probs, problem(path+".name", CodeOpUnexported, "operation %q is not an exported Go name (it must start with an upper-case letter): the generated interface method would be invisible to its callers", name))
	}
	var params []json.RawMessage
	if err := json.Unmarshal(op["params"], &params); err == nil {
		for i, p := range params {
			probs = append(probs, objectFields(fmt.Sprintf("%s.params[%d]", path, i), p, paramFields)...)
		}
	}
	return probs
}

// objectFields reports the fields of one JSON object that are not in allowed.
func objectFields(path string, raw json.RawMessage, allowed map[string]bool) Problems {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil // a wrongly typed element is the typed decode's to refuse, not a field problem
	}
	return unknownFields(path, obj, allowed)
}

// unknownFields reports each key of obj that is not in allowed, in key order.
func unknownFields(path string, obj map[string]json.RawMessage, allowed map[string]bool) Problems {
	var probs Problems
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		if !allowed[k] {
			probs = append(probs, problem(path+"."+k, CodeFieldUnknown, "unknown field %q", k))
		}
	}
	return probs
}
