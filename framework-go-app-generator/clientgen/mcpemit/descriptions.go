package mcpemit

import (
	_ "embed" // the walker's fixed source
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// fieldDescriptions is a contract's own property documentation: $def name ->
// wire key -> description, for every object $def that documents at least one of
// its properties.
//
// WHY IT EXISTS: an MCP tool's output schema is INFERRED (jsonschema.For) from the
// Go types modelgen emits, and modelgen carries no property `description` into
// those types — it emits a json tag and nothing else. So a contract that states
// "this field is meaningless unless that one is true" reached the HTTP/TS surface
// and never an agent reading the tool: the agent saw the field's shape and not
// the sentence that says when to ignore it. The table below closes that, generated
// from the same contract document everything else here is.
type fieldDescriptions map[string]map[string]string

// parseFieldDescriptions collects every non-empty `description` a contract $def
// carries on one of its properties. A def bound to a type modelgen does not emit
// as mgr.<Def> (x-go-type) or emitted as a sealed sum (x-go-sumtype) is skipped:
// there is no struct for the table to key on.
func parseFieldDescriptions(entry json.RawMessage) (fieldDescriptions, error) {
	var top struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(entry, &top); err != nil {
		return nil, fmt.Errorf("mcpemit: parse $defs: %w", err)
	}
	out := fieldDescriptions{}
	for name, raw := range top.Defs {
		if docs := defFieldDescriptions(raw); len(docs) > 0 {
			out[name] = docs
		}
	}
	return out, nil
}

// defFieldDescriptions is one $def's documented properties: none for a def bound to an
// existing Go type, or one that is not an object with object-shaped properties.
func defFieldDescriptions(raw json.RawMessage) map[string]string {
	var def struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
		GoType    string          `json:"x-go-type"`
		GoSumtype json.RawMessage `json:"x-go-sumtype"`
	}
	if err := json.Unmarshal(raw, &def); err != nil || def.GoType != "" || len(def.GoSumtype) > 0 {
		return nil
	}
	out := map[string]string{}
	for prop, p := range def.Properties {
		if d := strings.TrimSpace(p.Description); d != "" {
			out[prop] = d
		}
	}
	return out
}

func sortedDescriptionKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// writeFieldDescriptions emits the description table and the walker that stamps
// it onto an inferred output schema. It is emitted ONLY when the contract
// documents a property, so a contract that documents none generates exactly what
// it generated before.
func writeFieldDescriptions(b *strings.Builder, descs fieldDescriptions) {
	b.WriteString("// contractFieldDescriptions is the contract's own property documentation,\n")
	b.WriteString("// keyed by the Go type modelgen emits for each documenting $def. modelgen\n")
	b.WriteString("// carries no description into those types, so the inferred output schema\n")
	b.WriteString("// would otherwise show an agent a field's shape but never the contract's\n")
	b.WriteString("// statement of when that field means nothing.\n")
	b.WriteString("var contractFieldDescriptions = map[reflect.Type]map[string]string{\n")
	for _, def := range sortedDescriptionKeys(descs) {
		fmt.Fprintf(b, "\treflect.TypeFor[%s.%s](): {\n", managerAlias, def)
		for _, prop := range sortedDescriptionKeys(descs[def]) {
			fmt.Fprintf(b, "\t\t%q: %q,\n", prop, descs[def][prop])
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n\n")

	b.WriteString(describeWalkerSrc)
}

// describeWalkerSrc is the walker that stamps the descriptions onto an inferred schema.
//
//go:embed describe_walker.go.txt
var describeWalkerSrc string
