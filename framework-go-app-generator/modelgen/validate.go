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

// ValidateContract refuses a contract document modelgen could not turn into a working
// component package, naming what to change. It is what recordServiceContract runs, so the
// refusal reaches the agent that can fix it rather than aborting generation later.
func ValidateContract(key string, raw []byte) error {
	var doc struct {
		contractMeta
		Interface struct {
			Name  string `json:"name"`
			Layer string `json:"layer"`
		} `json:"interface"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("contract %q: %w", key, err)
	}
	if _, ok := layerContext[doc.Interface.Layer]; !ok {
		return fmt.Errorf("contract %q: interface.layer %q is not one of %s", key, doc.Interface.Layer, strings.Join(layerNames(), ", "))
	}
	for _, name := range doc.Infra {
		if _, ok := infraBindings[name]; !ok {
			return fmt.Errorf("contract %q: infra %q is not approved; choose from %s (a Resource's name is not an infra name: a service reached over HTTP is ExternalHTTP)", key, name, strings.Join(ApprovedInfra(), ", "))
		}
	}
	if doc.Interface.Layer == "resourceaccess" && !doc.Stub && doc.GoPackage != "" && len(doc.Infra) == 0 {
		return fmt.Errorf("contract %q: a ResourceAccess must bind the infrastructure it fronts, or nothing can construct it; set infra from %s", key, strings.Join(ApprovedInfra(), ", "))
	}
	if doc.GoPackage != "" {
		dir := "internal/" + doc.Interface.Layer + "/"
		if !strings.HasPrefix(doc.GoPackage, dir) || len(doc.GoPackage) == len(dir) {
			return fmt.Errorf("contract %q: goPackage %q must be a package under %s (every app lays its components out the same way)", key, doc.GoPackage, dir)
		}
	}
	return nil
}
