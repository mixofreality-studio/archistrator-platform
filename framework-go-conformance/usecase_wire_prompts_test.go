package conformance

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

// RUN-024 spent seven putDraftModel calls on a skill that taught the core use cases by name
// (`ref` on nodes, "IDENTITY BY NAME (no ids)") while the schema keys use cases, actors and nodes by
// `id`. The skill's example JSON is the part an agent copies, so it is held to the schema's own
// types: every block must decode into methodcheck's mirror with unknown fields refused. A field the
// skill invents, or a field the schema renamed, fails here instead of in a paid run.

const coreUseCasesSkill = ".claude/skills/the-method-core-use-cases/SKILL.md"

var jsonFence = regexp.MustCompile("(?s)```json\n(.*?)```")

func skillJSONBlocks(t *testing.T) []string {
	t.Helper()
	files, err := methodassets.ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	body, ok := files[coreUseCasesSkill]
	if !ok {
		t.Fatalf("%s is missing from the embedded .claude tree", coreUseCasesSkill)
	}
	var blocks []string
	for _, m := range jsonFence.FindAllSubmatch(body, -1) {
		blocks = append(blocks, strings.TrimSpace(string(m[1])))
	}
	return blocks
}

func strictDecode(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// validateDiagramIDs holds what a strict decode cannot: each node has a unique `id`, and every edge
// names nodes by those ids.
func validateDiagramIDs(t *testing.T, where string, d methodcheck.ActivityDiagram) {
	t.Helper()
	ids := map[string]bool{}
	for _, n := range d.Nodes {
		if n.ID == "" {
			t.Errorf("%s: a node has no `id`", where)
		}
		if ids[n.ID] {
			t.Errorf("%s: node id %q is repeated", where, n.ID)
		}
		ids[n.ID] = true
		if n.Kind == "" {
			t.Errorf("%s: node %q has no `kind`", where, n.ID)
		}
	}
	for _, e := range d.Edges {
		if !ids[e.From] || !ids[e.To] {
			t.Errorf("%s: edge %q -> %q names a node id that is not in the diagram", where, e.From, e.To)
		}
	}
}

func TestCoreUseCasesSkill_ExampleJSONDecodesStrictlyIntoTheSchemaTypes(t *testing.T) {
	var full, diagrams int
	for i, block := range skillJSONBlocks(t) {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal([]byte(block), &probe); err != nil {
			t.Fatalf("json block %d is not a JSON object: %v", i+1, err)
		}
		switch {
		case probe["decisions"] != nil:
			full++
			checkCoreUseCasesExample(t, i+1, block)
		case probe["nodes"] != nil:
			diagrams++
			var d methodcheck.ActivityDiagram
			if err := strictDecode([]byte(block), &d); err != nil {
				t.Errorf("json block %d does not decode into methodcheck.ActivityDiagram: %v", i+1, err)
				continue
			}
			validateDiagramIDs(t, "json block "+strconv.Itoa(i+1), d)
		default:
			t.Errorf("json block %d is neither a CoreUseCases model (decisions) nor an activity diagram (nodes)", i+1)
		}
	}
	if full == 0 {
		t.Error("the skill carries no worked CoreUseCases example (a JSON block with `decisions`); the whole wire shape, from decision to node, must be shown once")
	}
	if diagrams == 0 {
		t.Error("the skill carries no worked activity-diagram example")
	}
}

// checkCoreUseCasesExample decodes a whole-model example into methodcheck.CoreUseCases. The one
// schema field methodcheck's mirror omits is a decision's `essenceRationale` (no methodcheck rule
// reads it; the schema and DH-UC-ESSENCE-MISSING do), so it is checked here and lifted out before
// the strict decode instead of being waved through.
func checkCoreUseCasesExample(t *testing.T, n int, block string) {
	t.Helper()
	where := "json block " + strconv.Itoa(n)
	var generic struct {
		Decisions []map[string]json.RawMessage `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(block), &generic); err != nil {
		t.Fatalf("%s: %v", where, err)
	}
	for i := range generic.Decisions {
		d := generic.Decisions[i]
		var uc struct {
			Classification string `json:"classification"`
		}
		if err := json.Unmarshal(d["useCase"], &uc); err != nil {
			t.Fatalf("%s: decision %d useCase: %v", where, i, err)
		}
		var rationale *string
		if raw, ok := d["essenceRationale"]; ok {
			if err := json.Unmarshal(raw, &rationale); err != nil {
				t.Fatalf("%s: decision %d essenceRationale: %v", where, i, err)
			}
		}
		if uc.Classification == "core" && (rationale == nil || strings.TrimSpace(*rationale) == "") {
			t.Errorf("%s: core decision %d has no essenceRationale", where, i)
		}
		delete(d, "essenceRationale")
	}
	lifted, err := json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	var c methodcheck.CoreUseCases
	if err := strictDecode(lifted, &c); err != nil {
		t.Fatalf("%s does not decode into methodcheck.CoreUseCases: %v", where, err)
	}
	if len(c.Decisions) == 0 {
		t.Fatalf("%s has no decisions", where)
	}
	ids := map[string]bool{}
	for _, d := range c.Decisions {
		uc := d.UseCase
		if uc.ID == "" || ids[uc.ID] {
			t.Errorf("%s: use case %q needs a unique non-empty `id` (got %q)", where, uc.Name, uc.ID)
		}
		ids[uc.ID] = true
		actors := map[string]bool{}
		for _, a := range uc.Actors {
			if a.ID == "" || a.Role == "" {
				t.Errorf("%s: use case %q has an actor without `id` or `role`: %+v", where, uc.ID, a)
			}
			actors[a.ID] = true
		}
		if uc.Activity == nil {
			t.Errorf("%s: use case %q has no activity", where, uc.ID)
			continue
		}
		validateDiagramIDs(t, where+" use case "+uc.ID, *uc.Activity)
		for _, nd := range uc.Activity.Nodes {
			if nd.LinkedActorID != "" && !actors[nd.LinkedActorID] {
				t.Errorf("%s: node %q links actor %q, which is not one of use case %q's declared actor ids", where, nd.ID, nd.LinkedActorID, uc.ID)
			}
		}
	}
	for _, d := range c.Decisions {
		if v := d.UseCase.VariationOf; v != nil && !ids[*v] {
			t.Errorf("%s: use case %q is a variationOf %q, which is not the `id` of a use case in the example", where, d.UseCase.ID, *v)
		}
	}
}
