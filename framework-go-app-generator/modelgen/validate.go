package modelgen

import (
	"encoding/json"
	"fmt"
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
	if err := validateLayerRules(key, doc.Interface.Name, doc.Interface.Layer, doc.Interface.Operations); err != nil {
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

// contractOpShape is what the layer rules read of an operation.
type contractOpShape struct {
	Name  string `json:"name"`
	Error bool   `json:"error"`
}

// validateLayerRules is the part of the construction arch gate a contract already decides.
func validateLayerRules(key, name, layer string, ops []contractOpShape) error {
	if want := layerIfaceSuffix[layer]; want != "" && !strings.HasSuffix(name, want) {
		return fmt.Errorf("contract %q: interface.name %q must end in %s, because a %s's port is found by that suffix; rename the interface", key, name, want, layer)
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
