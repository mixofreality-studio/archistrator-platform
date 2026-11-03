package modelgen

import (
	"cmp"
	"encoding/json"
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"
)

// ApprovedInfra is the sorted menu of infra names a contract may bind. It is the single
// source for the record-time refusal and for what the prompts tell an agent to pick from.
func ApprovedInfra() []string {
	out := make([]string, 0, len(infraBindings))
	for name := range infraBindings {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func layerNames() []string {
	out := make([]string, 0, len(layerContext))
	for name := range layerContext {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// CanonicalLayer is the wire form of a Method layer name ("ResourceAccess" →
// "resourceaccess"), or false when the name is no layer.
func CanonicalLayer(layer string) (string, bool) {
	l := strings.ToLower(layer)
	_, ok := layerContext[l]
	return l, ok
}

// layerIfaceSuffix is the interface-name suffix a layer's port must carry, and
// layerOpsReturnError the layers whose every operation returns an error. The construction
// arch gate enforces both on the generated code, where the contract is already frozen, so
// they are refused here instead. A Utility and a Client are free. TestContractLayerRulesMatchArch
// pins these tables to arch.MethodSpec.
var (
	layerIfaceSuffix    = map[string]string{"engine": "Engine", "resourceaccess": "Access"}
	layerOpsReturnError = map[string]bool{"engine": true, "resourceaccess": true, "manager": true}
)

// ValidateContract refuses a contract document modelgen could not turn into a working
// component package, or the construction arch gate would refuse, naming what to change.
// It is what recordServiceContract runs, so the refusal reaches the agent that can fix it
// rather than aborting generation or construction later.
func ValidateContract(key string, raw []byte) error {
	var doc struct {
		contractMeta
		Defs      map[string]json.RawMessage `json:"$defs"`
		Interface struct {
			Name       string            `json:"name"`
			Layer      string            `json:"layer"`
			Operations []contractOpShape `json:"operations"`
		} `json:"interface"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("contract %q: %w", key, err)
	}
	if _, ok := layerContext[doc.Interface.Layer]; !ok {
		return fmt.Errorf("contract %q: interface.layer %q is not one of %s", key, doc.Interface.Layer, strings.Join(layerNames(), ", "))
	}
	if err := validateNames(key, doc.GoPackage, doc.Interface.Name, doc.Interface.Layer, doc.Interface.Operations, doc.Defs, raw); err != nil {
		return err
	}
	if err := validateInfra(key, doc.Infra); err != nil {
		return err
	}
	if doc.Interface.Layer == "resourceaccess" && !doc.Stub && doc.GoPackage != "" && len(doc.Infra) == 0 {
		return fmt.Errorf("contract %q: a ResourceAccess must bind the infrastructure it fronts, or nothing can construct it; set infra from %s", key, strings.Join(ApprovedInfra(), ", "))
	}
	return validateGoPackage(key, doc.GoPackage, doc.Interface.Layer)
}

// validateNames holds what the contract names to the generators and to the layer rules.
func validateNames(key, goPackage, name, layer string, ops []contractOpShape, defs map[string]json.RawMessage, raw []byte) error {
	if err := validateIdentifiers(key, name, ops); err != nil {
		return err
	}
	if err := validateDefs(key, goPackage, name, defs, raw); err != nil {
		return err
	}
	return validateLayerRules(key, name, layer, ops)
}

// contractOpShape is what the layer rules read of an operation.
type contractOpShape struct {
	Name   string `json:"name"`
	Error  bool   `json:"error"`
	Params []struct {
		Name string `json:"name"`
	} `json:"params"`
}

// reservedParamNames are the identifiers the generated code around an operation already
// uses, so a param of that name does not compile in at least one generated file. It holds
// for every layer: one rule, because a contract's author should not need to know which
// generator reads which layer. TestValidateContractMatchesWhatCompiles pins this list to
// what really fails, in both directions.
var reservedParamNames = []string{
	"rc", // the call Context of every generated method
	// the activity, invoker and fake wrappers of a dependency
	"a", "ctx", "err", "f", "fwmanager", "fwra", "i", "out", "v", "workflow",
	// the REST and MCP handlers of a Manager
	"decision", "h", "http", "ok", "principal", "r", "result", "security", "w",
}

// ReservedParamNames is the sorted list a param name may not take, for the prompts and tests.
func ReservedParamNames() []string {
	out := slices.Clone(reservedParamNames)
	slices.Sort(out)
	return out
}

// validateDefs refuses the model types modelgen could not emit, or would emit into a package
// that does not compile: it stops generation for the whole project, far from the one agent
// that can fix the contract.
func validateDefs(key, goPackage, ifaceName string, defs map[string]json.RawMessage, raw []byte) error {
	if goPackage != "" && len(defs) == 0 {
		return fmt.Errorf("contract %q: $defs is empty, and a built contract declares its model types there, because they are the component's public type surface; name at least the types its operations use", key)
	}
	if err := validateDefNames(key, ifaceName, defs); err != nil {
		return err
	}
	if err := validateRefs(key, defs, raw); err != nil {
		return err
	}
	iface, ok, err := parseInterfaceOnly(raw)
	if err != nil {
		return fmt.Errorf("contract %q: %w", key, err)
	}
	if !ok {
		return nil
	}
	return validateExportedRefs(key, iface)
}

func validateDefNames(key, ifaceName string, defs map[string]json.RawMessage) error {
	for _, name := range slices.Sorted(maps.Keys(defs)) {
		if !token.IsIdentifier(name) || name == ifaceName {
			return fmt.Errorf("contract %q: $defs name %q must be a Go identifier other than the interface's own name, because it becomes a Go type in the component's package; rename it", key, name)
		}
		if err := validateDefFields(key, name, defs[name]); err != nil {
			return err
		}
	}
	return nil
}

func validateDefFields(key, def string, raw json.RawMessage) error {
	var shape struct {
		Properties map[string]struct {
			GoName string `json:"x-go-name"`
		} `json:"properties"`
		VarNames []string `json:"x-enum-varnames"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return fmt.Errorf("contract %q: $defs %s: %w", key, def, err)
	}
	seen := make(map[string]string, len(shape.Properties))
	for _, prop := range slices.Sorted(maps.Keys(shape.Properties)) {
		field := cmp.Or(shape.Properties[prop].GoName, exportName(prop))
		if !token.IsIdentifier(field) {
			return fmt.Errorf("contract %q: $defs %s: property %q becomes the Go field %q, which is no identifier; rename it (letters, digits and underscores)", key, def, prop, field)
		}
		if other, dup := seen[field]; dup {
			return fmt.Errorf("contract %q: $defs %s: properties %q and %q become the same Go field %s; rename one", key, def, other, prop, field)
		}
		seen[field] = prop
	}
	for _, name := range shape.VarNames {
		if !token.IsIdentifier(name) {
			return fmt.Errorf("contract %q: $defs %s: x-enum-varnames %q must be a Go identifier, because it becomes a constant; rename it", key, def, name)
		}
	}
	return nil
}

// validateRefs refuses a $ref that names no $def of this contract: the generated code would
// use an undefined type.
func validateRefs(key string, defs map[string]json.RawMessage, raw []byte) error {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("contract %q: %w", key, err)
	}
	for _, ref := range refsIn(doc) {
		if _, known := defs[refName(ref)]; !known {
			return fmt.Errorf("contract %q: $ref %q names no type in this contract's $defs; declare it there or correct the reference", key, ref)
		}
	}
	return nil
}

// refsIn is every "$ref" string anywhere in a decoded JSON document.
func refsIn(node any) []string {
	var out []string
	switch n := node.(type) {
	case map[string]any:
		if ref, ok := n["$ref"].(string); ok {
			out = append(out, ref)
		}
		for _, v := range n {
			out = append(out, refsIn(v)...)
		}
	case []any:
		for _, v := range n {
			out = append(out, refsIn(v)...)
		}
	}
	return out
}

// validateIdentifiers holds every name the contract hands to the generators to Go's rules.
// Nothing downstream can repair a bad one: a lowercase operation still generates, and then
// no fake or caller can implement or reach it, while a keyword stops generation for the
// whole project. The generators must not repair it either, because the plan's TP-STEP-OP
// compares operation names exactly, so one spelling has to be recorded.
func validateIdentifiers(key, name string, ops []contractOpShape) error {
	if !token.IsIdentifier(name) || !token.IsExported(name) {
		return fmt.Errorf("contract %q: interface.name %q must be an exported Go identifier, because it names the generated interface and its constructor; rename it (a capital first letter, then letters and digits)", key, name)
	}
	seen := make(map[string]string, len(ops))
	for _, op := range ops {
		if !token.IsIdentifier(op.Name) || !token.IsExported(op.Name) {
			return fmt.Errorf("contract %q: operation %q must be an exported Go identifier, because no fake or caller outside the package can reach anything else; rename it (a capital first letter, then letters and digits). A dynamic view's label such as %q still resolves to the capitalised operation, because labels are matched ignoring case", key, op.Name, op.Name+"(...)")
		}
		fold := strings.ToLower(op.Name)
		if other, dup := seen[fold]; dup {
			return fmt.Errorf("contract %q: operations %q and %q differ at most in case, and a dynamic view's label matches ignoring case, so it could not tell them apart; rename one", key, other, op.Name)
		}
		seen[fold] = op.Name
		if err := validateParamNames(key, op); err != nil {
			return err
		}
	}
	return nil
}

func validateParamNames(key string, op contractOpShape) error {
	seen := make(map[string]string, len(op.Params))
	for _, p := range op.Params {
		switch {
		case !token.IsIdentifier(p.Name):
			return fmt.Errorf("contract %q: operation %s: param %q must be a Go identifier and not a keyword, because it is a parameter of the generated method; rename it", key, op.Name, p.Name)
		case slices.Contains(reservedParamNames, p.Name) || predeclaredInBodies(p.Name) || p.Name == "_":
			return fmt.Errorf("contract %q: operation %s: param %q collides with a name the generated code uses (%s, and Go's predeclared types and nil); rename it", key, op.Name, p.Name, strings.Join(reservedParamNames, ", "))
		}
		fold := strings.ToLower(p.Name)
		if other, dup := seen[fold]; dup {
			return fmt.Errorf("contract %q: operation %s: params %q and %q differ at most in case, and the generated request struct exports both to one field; rename one", key, op.Name, other, p.Name)
		}
		seen[fold] = p.Name
	}
	return nil
}

// predeclaredInBodies reports a universe name a generated body uses: a type such as string,
// or nil. A builtin function such as len or a constant such as true is never used there, so
// it stays a legal param name.
func predeclaredInBodies(name string) bool {
	switch types.Universe.Lookup(name).(type) {
	case *types.TypeName, *types.Nil:
		return true
	default:
		return false
	}
}

// validateLayerRules is the part of the construction arch gate a contract already decides.
func validateLayerRules(key, name, layer string, ops []contractOpShape) error {
	if want := layerIfaceSuffix[layer]; want != "" && !strings.HasSuffix(name, want) {
		return fmt.Errorf("contract %q: interface.name %q must end in %s, because a %s's port is found by that suffix; rename the interface", key, name, want, layer)
	}
	if err := validateManagerActionParam(key, layer, ops); err != nil {
		return err
	}
	if !layerOpsReturnError[layer] {
		return nil
	}
	for _, op := range ops {
		if !op.Error {
			return fmt.Errorf("contract %q: operation %s must declare error:true, because every %s operation returns an error as its last result and the construction gate refuses one that does not; a total operation keeps the declaration and simply returns nil", key, op.Name, layer)
		}
	}
	return nil
}

// validateManagerActionParam refuses a Manager operation whose one param is named action: that is
// the shape of a browser operation, and a machine client's contract carries the Manager's params,
// so its generated tests would be classified as browser tests and stop generation for the project.
func validateManagerActionParam(key, layer string, ops []contractOpShape) error {
	if layer != "manager" {
		return nil
	}
	for _, op := range ops {
		if len(op.Params) == 1 && op.Params[0].Name == "action" {
			return fmt.Errorf("contract %q: operation %s: a Manager operation whose only param is named action reads as a browser action in its machine clients' tests; rename the param", key, op.Name)
		}
	}
	return nil
}

func validateInfra(key string, infra []string) error {
	for _, name := range infra {
		if _, ok := infraBindings[name]; !ok {
			return fmt.Errorf("contract %q: infra %q is not approved; choose from %s (a Resource's name is not an infra name: a service reached over HTTP is ExternalHTTP)", key, name, strings.Join(ApprovedInfra(), ", "))
		}
	}
	return nil
}

func validateGoPackage(key, goPackage, layer string) error {
	if goPackage == "" {
		return nil
	}
	dir := "internal/" + layer + "/"
	if !strings.HasPrefix(goPackage, dir) || len(goPackage) == len(dir) {
		return fmt.Errorf("contract %q: goPackage %q must be a package under %s (every app lays its components out the same way)", key, goPackage, dir)
	}
	return nil
}
