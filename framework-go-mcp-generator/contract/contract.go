// Package contract parses a schema-first component contract document — the
// committed contract.schema.json that the archistrator codegen pipeline emits
// for every Method component. The document is a self-contained JSON Schema doc
// whose top-level $defs carry every I/O type and whose "interface" key carries
// the RPC surface descriptor (the part JSON Schema cannot express on its own).
//
// This package is deliberately dependency-free (encoding/json only): both the
// http and the mcp generators embed an identical copy so each module builds
// standalone from its own go.mod. Keep the two copies in sync.
package contract

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Doc is a parsed contract document.
type Doc struct {
	// ID is the document $id (e.g. "archistrator://contract/project").
	ID string
	// Title is the document title (e.g. "project contract").
	Title string
	// Defs is every entry under $defs, kept as raw JSON so the OpenAPI emitter can
	// pass each I/O type through as an OAS 3.1 component schema (largely verbatim,
	// with internal $refs rewritten). Keyed by type name.
	Defs map[string]json.RawMessage
	// Interface is the RPC surface descriptor read from the "interface" key.
	Interface Interface
	// scalarStringDefs is the set of $defs that are plain string scalars (type
	// "string", no properties, no enum) — the named-scalar ID types like ProjectID.
	scalarStringDefs map[string]bool
	// scalarDefKind maps each $def that is a primitive scalar (no properties, no
	// $ref) to its JSON primary type ("string"/"integer"/"number"/"boolean").
	// Unlike scalarStringDefs it INCLUDES enums (e.g. ArtifactKind -> "integer",
	// Severity -> "string"), so the generators can parse an enum-ref param off the
	// path/query as its backing type.
	scalarDefKind map[string]string
}

// Interface mirrors the codegen Interface descriptor (server cmd/internal/codegen).
type Interface struct {
	Name       string      `json:"name"`
	Layer      string      `json:"layer"`
	Operations []Operation `json:"operations"`
}

// Operation is one method on the interface.
type Operation struct {
	Name   string      `json:"name"`
	Params []Param     `json:"params"`
	Result *SchemaNode `json:"result,omitempty"`
	Error  bool        `json:"error"`
}

// Param is one operation parameter (the layer-context first arg is already
// stripped from this list and is re-injected by the generators).
type Param struct {
	Name    string      `json:"name"`
	Pointer bool        `json:"pointer,omitempty"`
	Schema  *SchemaNode `json:"schema"`
}

// SchemaNode is the minimal slice of JSON Schema the generators need to inspect
// a param or result: a $ref into $defs, or an inline primitive / array schema. It
// also carries the foundational-type binding extensions (x-go-type / x-go-import)
// and "format" so the generators honor exactly the Go types the server's modelgen
// produces (uuid.UUID, time.Time, []byte, json.RawMessage, ...).
type SchemaNode struct {
	Ref       string      `json:"$ref"`
	Type      TypeField   `json:"type"`
	Items     *SchemaNode `json:"items"`
	Format    string      `json:"format"`
	XGoType   string      `json:"x-go-type"`
	XGoImport string      `json:"x-go-import"`
}

// TypeField decodes JSON Schema's "type", which is either a string or an array
// of strings (e.g. ["null","string"]).
type TypeField []string

// UnmarshalJSON accepts both the string and []string forms.
func (t *TypeField) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '[' {
		var arr []string
		if err := json.Unmarshal(b, &arr); err != nil {
			return err
		}
		*t = arr
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*t = []string{s}
	return nil
}

// Primary returns the first non-"null" type, or "" if none.
func (t TypeField) Primary() string {
	for _, s := range t {
		if s != "null" {
			return s
		}
	}
	return ""
}

// RefName returns the bare type name a $ref points at (the segment after the
// last "/"), or "" if the node is not a $ref.
func (n *SchemaNode) RefName() string {
	if n == nil || n.Ref == "" {
		return ""
	}
	i := strings.LastIndex(n.Ref, "/")
	return n.Ref[i+1:]
}

// Parse decodes a contract document.
func Parse(raw []byte) (*Doc, error) {
	var top struct {
		ID        string                     `json:"$id"`
		Title     string                     `json:"title"`
		Defs      map[string]json.RawMessage `json:"$defs"`
		Interface Interface                  `json:"interface"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("contract: parse: %w", err)
	}
	if top.Interface.Name == "" {
		return nil, fmt.Errorf("contract: document has no interface descriptor")
	}
	d := &Doc{
		ID:               top.ID,
		Title:            top.Title,
		Defs:             top.Defs,
		Interface:        top.Interface,
		scalarStringDefs: map[string]bool{},
		scalarDefKind:    map[string]string{},
	}
	for name, rawDef := range top.Defs {
		if isScalarString(rawDef) {
			d.scalarStringDefs[name] = true
		}
		if kind, ok := scalarDefKind(rawDef); ok {
			d.scalarDefKind[name] = kind
		}
	}
	return d, nil
}

// scalarDefKind reports the primitive backing type of a $def that is a plain
// scalar — no object properties and no $ref — including enums. Returns ok=false
// for objects, arrays, and $ref/composed defs.
func scalarDefKind(rawDef json.RawMessage) (string, bool) {
	var def struct {
		Type       TypeField       `json:"type"`
		Properties json.RawMessage `json:"properties"`
		Ref        string          `json:"$ref"`
	}
	if err := json.Unmarshal(rawDef, &def); err != nil {
		return "", false
	}
	if def.Ref != "" || len(def.Properties) > 0 {
		return "", false
	}
	switch def.Type.Primary() {
	case "string", "integer", "number", "boolean":
		return def.Type.Primary(), true
	}
	return "", false
}

// isScalarString reports whether a $def is a bare string scalar — type contains
// "string", with no object properties and no enum. These are the named ID/scalar
// types (ProjectID, OwnerScope, ActivityID, ...).
func isScalarString(rawDef json.RawMessage) bool {
	var def struct {
		Type       TypeField       `json:"type"`
		Properties json.RawMessage `json:"properties"`
		Enum       json.RawMessage `json:"enum"`
		Ref        string          `json:"$ref"`
	}
	if err := json.Unmarshal(rawDef, &def); err != nil {
		return false
	}
	if def.Ref != "" || len(def.Properties) > 0 || len(def.Enum) > 0 {
		return false
	}
	return def.Type.Primary() == "string"
}

// IsScalarStringDef reports whether the named $def is a string scalar.
func (d *Doc) IsScalarStringDef(name string) bool { return d.scalarStringDefs[name] }

// ScalarKind classifies a param/result node by how it can ride a URL path or
// query segment. It returns one of "string", "integer", "number", "boolean",
// "uuid" with ok=true when the node is a scalar value; ok=false for objects,
// arrays, and non-scalar foundational types (time.Time, []byte, json.RawMessage)
// — those can only ride a JSON body. A $ref resolves through the scalar $defs.
func (d *Doc) ScalarKind(n *SchemaNode) (string, bool) {
	if n == nil {
		return "", false
	}
	if n.XGoType == "uuid.UUID" || n.Format == "uuid" {
		return "uuid", true
	}
	if n.XGoType != "" {
		// time.Time / []byte / json.RawMessage etc. — not a path/query scalar.
		return "", false
	}
	if name := n.RefName(); name != "" {
		if kind, ok := d.scalarDefKind[name]; ok {
			return kind, true
		}
		return "", false
	}
	switch n.Type.Primary() {
	case "string", "integer", "number", "boolean":
		return n.Type.Primary(), true
	}
	return "", false
}

// GoImports returns the x-go-import paths a schema node binds (its own plus any
// nested array items'), for the emitted file's import block.
func GoImports(n *SchemaNode) []string {
	if n == nil {
		return nil
	}
	var out []string
	if n.XGoImport != "" {
		out = append(out, n.XGoImport)
	}
	out = append(out, GoImports(n.Items)...)
	return out
}

// ManagerBase is the interface name with a trailing "Manager" stripped
// (e.g. "ProjectManager" -> "Project"), the stem used for route prefixes and
// tool-name prefixes.
func (d *Doc) ManagerBase() string {
	return strings.TrimSuffix(d.Interface.Name, "Manager")
}
