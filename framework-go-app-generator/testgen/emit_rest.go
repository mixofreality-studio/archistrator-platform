package testgen

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// emit_rest.go is the REST machine client's dialect. Like an MCP client, every op is one generated
// surface entry, spelled ManagerBase+Op, and its scenario test drives that entry over the wire
// against the launched app (scenariohost.LaunchDevstack) as the plan's principals: here by HTTP
// requests (scenariohost.RESTClient), there by MCP tool calls (scenariohost.MCPSession). The two are
// separate tests of separate clients. The scenario bodies are the same code as the MCP dialect's
// (emit_mcp.go): only the session a principal's steps call through differs.

// restRoute is one generated route of a Manager, with the Manager operation it serves: the route's
// verb and path, and the "<Iface>.<Op>" name the composed server's null-operation middleware matches.
type restRoute struct{ Verb, Path, Manager, Op string }

// managerRoutes is every Manager operation's route, by its client op name. It reads the same route
// planner the handlers are generated from, so a test cannot name a route the handler does not mount.
func managerRoutes(projectJSON []byte) (map[string]restRoute, error) {
	routes := map[string]restRoute{}
	err := eachManagerPlan(projectJSON, func(_ string, parsed *projectmodel.Doc, plans []httpgen.OpPlan) {
		for _, p := range plans {
			routes[parsed.ManagerBase()+p.Name] = restRoute{Verb: p.Verb, Path: p.PathTemplate, Manager: parsed.Interface.Name, Op: p.Name}
		}
	})
	return routes, err
}

// componentRoutes is every Manager operation's route, by the contract's key and the operation's
// name: the route a generated browser test calls to arrange state or probe an effect. It reads
// the same route planner as managerRoutes.
func componentRoutes(projectJSON []byte) (map[string]map[string]restRoute, error) {
	routes := map[string]map[string]restRoute{}
	err := eachManagerPlan(projectJSON, func(key string, parsed *projectmodel.Doc, plans []httpgen.OpPlan) {
		routes[key] = map[string]restRoute{}
		for _, p := range plans {
			routes[key][p.Name] = restRoute{Verb: p.Verb, Path: p.PathTemplate, Manager: parsed.Interface.Name, Op: p.Name}
		}
	})
	return routes, err
}

// eachManagerPlan plans the routes of every Manager contract of the document, in key order.
func eachManagerPlan(projectJSON []byte, visit func(key string, parsed *projectmodel.Doc, plans []httpgen.OpPlan)) error {
	var doc struct {
		ServiceContracts map[string]json.RawMessage `json:"serviceContracts"`
	}
	if err := json.Unmarshal(projectJSON, &doc); err != nil {
		return fmt.Errorf("testgen: %w", err)
	}
	for _, key := range sortedKeys(doc.ServiceContracts) {
		parsed, plans, err := planManager(doc.ServiceContracts[key])
		if err != nil {
			return fmt.Errorf("testgen: contract %s: %w", key, err)
		}
		if parsed != nil {
			visit(key, parsed, plans)
		}
	}
	return nil
}

// planManager plans one contract's routes when it is a Manager's; a contract of any other
// layer plans nothing (a nil document).
func planManager(raw json.RawMessage) (*projectmodel.Doc, []httpgen.OpPlan, error) {
	var meta struct {
		Layer     string `json:"layer"`
		Interface struct {
			Layer string `json:"layer"`
		} `json:"interface"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(meta.Layer, "manager") && !strings.EqualFold(meta.Interface.Layer, "manager") {
		return nil, nil, nil
	}
	parsed, err := projectmodel.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	plans, err := httpgen.PlanOps(parsed)
	if err != nil {
		return nil, nil, err
	}
	return parsed, plans, nil
}

// transportHelpers is the generated code a machine client file carries for its transport: the route
// table of a REST client, nothing for an MCP one, whose tools are named by the contract's operations
// alone. The session itself is the scenario host's (scenariohost.MCPSession, RESTClient).
func transportHelpers(plan componentPlan, rest bool, routes map[string]restRoute) (string, error) {
	if !rest {
		return "", nil
	}
	return restRouteTable(plan, routes)
}

// errorHasCodeHelper is the transport's code match.
func errorHasCodeHelper(rest bool) string {
	if rest {
		return restErrorHasCode
	}
	return toolsErrorHasCode
}

func isRESTPlan(plan componentPlan) (bool, error) {
	d, err := clientDialect(plan.Key, plan.Contract)
	return d == dialectREST, err
}

// restRouteTable is the generated map from tool name to route, over the plan's contract: the table
// scenariohost.RESTClient spells each call's arguments onto.
func restRouteTable(plan componentPlan, routes map[string]restRoute) (string, error) {
	lines := make([]string, 0, len(plan.Contract.Interface.Operations))
	for _, op := range plan.Contract.Interface.Operations {
		r, ok := routes[op.Name]
		if !ok {
			return "", fmt.Errorf("testgen: %s operation %s is no Manager's generated route", plan.Key, op.Name)
		}
		lines = append(lines, fmt.Sprintf("\t%q: {Verb: %q, Path: %q},\n", toolName(op), r.Verb, r.Path))
	}
	sort.Strings(lines)
	lines = slices.Compact(lines)
	return "// clientRoutes is the generated route of each operation.\nvar clientRoutes = map[string]scenariohost.Route{\n" + strings.Join(lines, "") + "}\n\n", nil
}

// restErrorHasCode is the REST dialect's code match. A response carries the wire code of the
// Manager's error kind (not_found for NotFound), and a plan names the refusal as the contract's
// error model does, the same on both transports. The table mirrors httpgen's codeForKind; a
// scenario in archistrator's fresh-project tests holds the two together.
const restErrorHasCode = `// restWireCodes is the code a generated handler answers each Manager error kind with.
var restWireCodes = map[string]string{
	"ContractMisuse":     "contract_misuse",
	"NotFound":           "not_found",
	"Unauthorized":       "forbidden",
	"FailedPrecondition": "failed_precondition",
	"Infrastructure":     "infrastructure",
}

// errorHasCode matches the code, or the wire code of that error kind, in the error text.
func errorHasCode(err error, code string) bool {
	text := err.Error()
	wire, known := restWireCodes[code]
	return strings.Contains(text, code) || (known && strings.Contains(text, wire))
}

`

// componentsWithoutLocalSubject is the components the generated test subject does not build: those
// whose deployment binding is not required and names no local-profile arm (composegen's localArm
// returns no arm for them, and scenariocompose emits no constructor). A scenario for one has no
// subject to call, so testgen names it and emits none rather than emit a call to a missing constructor.
// bindingLacksLocalArm is true for an optional, per-profile binding with no local-profile arm.
func bindingLacksLocalArm(b projectmodel.Binding) bool {
	if len(b.PerProfile) == 0 || strings.EqualFold(b.Presence, "required") {
		return false
	}
	_, local := b.PerProfile[projectmodel.LocalProfile]
	return !local
}

func componentsWithoutLocalSubject(projectJSON []byte) (map[string]bool, error) {
	// A lenient read, not projectmodel.Load: testgen tolerates plan-shaped conditions that Load
	// refuses, and the binding is all it needs.
	var doc struct {
		Slots map[string]struct {
			Model struct {
				Deployment *projectmodel.Deployment `json:"deployment"`
			} `json:"model"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(projectJSON, &doc); err != nil {
		return nil, fmt.Errorf("testgen: deployment: %w", err)
	}
	out := map[string]bool{}
	for _, slot := range doc.Slots {
		d := slot.Model.Deployment
		if d == nil {
			continue
		}
		for _, b := range d.Bindings {
			if bindingLacksLocalArm(b) {
				out[b.Component] = true
			}
		}
	}
	return out, nil
}
