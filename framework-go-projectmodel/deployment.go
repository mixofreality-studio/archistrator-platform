// deployment.go
package projectmodel

import (
	"encoding/json"
	"fmt"
)

// Deployment is the codegen-relevant slice of the operationalConcepts slot
// (slots[kind=6].model.deployment): the container/environment topology that
// exists today plus the appgen additions — infrastructure declarations,
// component→infra bindings, and scalar settings.
//
// EVERY appgen section (Infrastructure, Bindings, Settings) is optional. A
// document that carries only the legacy deliveryStyle/containers/environments
// (every existing document — gtdapp, greenfield, archistrator today) parses
// into a Deployment whose appgen slices are empty and validates clean
// (tolerant subset — spec: Schema ownership & evolution).
type Deployment struct {
	DeliveryStyle  string              `json:"deliveryStyle"`
	Containers     []DeployContainer   `json:"containers"`
	Environments   []DeployEnvironment `json:"environments"`
	Infrastructure []InfraDecl         `json:"infrastructure"`
	Bindings       []Binding           `json:"bindings"`
	Settings       []Setting           `json:"settings"`
}

// DeployContainer is one deployable container. Only the fields the appgen
// pipeline needs are parsed (key/name/components); the Structurizr render
// fields (technology/description) are ignored.
type DeployContainer struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Components []string `json:"components"`
}

// DeployEnvironment is one deployment environment. Its Profile is the axis the
// infrastructure/binding declarations key off of.
type DeployEnvironment struct {
	Profile string `json:"profile"`
	Title   string `json:"title"`
}

// InfraDecl is one declared infrastructure dependency: a logical Key, the
// catalog Substrate that supplies its input mechanism (temporal, postgres,
// github-app, keycloak, otel), the Profiles it is provisioned for, its
// Presence class, and per-input env-var name overrides (input name → ENV_VAR).
type InfraDecl struct {
	Key       string            `json:"key"`
	Substrate string            `json:"substrate"`
	Profiles  []string          `json:"profiles"`
	Presence  string            `json:"presence"`
	Env       map[string]string `json:"env"`
}

// Binding is one component's per-profile wiring to declared infrastructure. It
// records which infra each profile's Variant consumes, the binding Presence
// (mirroring the composition root's nil-tolerance), the contract keys it
// Provides (the multi-contract substrate case), and any binding-scoped
// Settings.
type Binding struct {
	Component  string                    `json:"component"`
	PerProfile map[string]BindingVariant `json:"perProfile"`
	Presence   string                    `json:"presence"`
	Provides   []string                  `json:"provides"`
	Settings   []Setting                 `json:"settings"`
}

// BindingVariant is one profile's binding choice: a named Variant plus the
// infra keys it consumes.
type BindingVariant struct {
	Variant string   `json:"variant"`
	Infra   []string `json:"infra"`
}

// Setting is one scalar configuration knob. Type is one of string/bool/int/
// duration; Default is the string-encoded fallback; Env is the explicit env
// var name (the configgen emitter derives one when empty).
type Setting struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Env         string `json:"env"`
	Description string `json:"description"`
}

// knownPresence enumerates the legal Presence values for infra declarations
// and bindings.
var knownPresence = map[string]bool{
	"required":         true,
	"optional":         true,
	"optional-dormant": true,
}

// knownSettingTypes enumerates the legal Setting.Type values.
var knownSettingTypes = map[string]bool{
	"string":   true,
	"bool":     true,
	"int":      true,
	"duration": true,
}

// opsConceptsModel is the tolerant subset of the operationalConcepts slot
// model Load needs: the nested deployment object.
type opsConceptsModel struct {
	Deployment json.RawMessage `json:"deployment"`
}

// findDeployment locates the operationalConcepts slot (kind==6) among the
// parsed slots and parses its model.deployment. Returns nil, nil when no such
// slot is present or the slot carries no deployment object (tolerant).
func findDeployment(slots map[string]struct {
	Kind  int             `json:"kind"`
	Model json.RawMessage `json:"model"`
}) (*Deployment, error) {
	for _, slot := range slots {
		if slot.Kind != 6 || len(slot.Model) == 0 {
			continue
		}
		var ops opsConceptsModel
		if err := json.Unmarshal(slot.Model, &ops); err != nil {
			return nil, fmt.Errorf("projectmodel: operationalConcepts slot: %w", err)
		}
		if len(ops.Deployment) == 0 {
			return nil, nil
		}
		var d Deployment
		if err := json.Unmarshal(ops.Deployment, &d); err != nil {
			return nil, fmt.Errorf("projectmodel: deployment: %w", err)
		}
		return &d, nil
	}
	return nil, nil
}

// validateDeployment cross-checks the appgen sections against the environment
// profiles and the parsed contracts. It is a no-op when no deployment slot was
// present, and validates only the sections that are present (all optional).
func (m *Model) validateDeployment() error {
	if m.Deployment == nil {
		return nil
	}
	profiles := m.deploymentProfiles()
	infraKeys := m.deploymentInfraKeys()

	if err := m.validateInfrastructure(profiles); err != nil {
		return err
	}
	if err := m.validateBindings(profiles, infraKeys); err != nil {
		return err
	}
	return validateSettings("settings", m.Deployment.Settings)
}

// deploymentProfiles returns the set of profiles declared by the deployment's
// environments.
func (m *Model) deploymentProfiles() map[string]bool {
	out := make(map[string]bool, len(m.Deployment.Environments))
	for _, e := range m.Deployment.Environments {
		out[e.Profile] = true
	}
	return out
}

// deploymentInfraKeys returns the set of infrastructure keys the deployment
// declares (the targets binding variants resolve against).
func (m *Model) deploymentInfraKeys() map[string]bool {
	out := make(map[string]bool, len(m.Deployment.Infrastructure))
	for _, i := range m.Deployment.Infrastructure {
		out[i.Key] = true
	}
	return out
}

// validateInfrastructure checks each infra decl's presence enum and that every
// profile it lists is a declared environment profile.
func (m *Model) validateInfrastructure(profiles map[string]bool) error {
	for _, i := range m.Deployment.Infrastructure {
		if i.Presence != "" && !knownPresence[i.Presence] {
			return fmt.Errorf("projectmodel: deployment infra %q: unknown presence %q", i.Key, i.Presence)
		}
		for _, p := range i.Profiles {
			if !profiles[p] {
				return fmt.Errorf("projectmodel: deployment infra %q: profile %q not in environments", i.Key, p)
			}
		}
	}
	return nil
}

// validateBindings checks every binding in turn.
func (m *Model) validateBindings(profiles, infraKeys map[string]bool) error {
	for _, b := range m.Deployment.Bindings {
		if err := m.validateBinding(b, profiles, infraKeys); err != nil {
			return err
		}
	}
	return nil
}

// validateBinding checks one binding: component + provides resolve to
// contracts, presence enum, perProfile keys are declared profiles, each
// variant's infra keys resolve, and binding-scoped settings type enum.
func (m *Model) validateBinding(b Binding, profiles, infraKeys map[string]bool) error {
	if _, ok := m.Contracts[b.Component]; !ok {
		return fmt.Errorf("projectmodel: deployment binding: unknown component %q", b.Component)
	}
	if b.Presence != "" && !knownPresence[b.Presence] {
		return fmt.Errorf("projectmodel: deployment binding %q: unknown presence %q", b.Component, b.Presence)
	}
	for _, pv := range b.Provides {
		if _, ok := m.Contracts[pv]; !ok {
			return fmt.Errorf("projectmodel: deployment binding %q: provides unknown component %q", b.Component, pv)
		}
	}
	if err := validateBindingProfiles(b, profiles, infraKeys); err != nil {
		return err
	}
	return validateSettings("binding "+b.Component, b.Settings)
}

// validateBindingProfiles checks that every perProfile key is a declared
// environment profile and that each variant's infra keys resolve.
func validateBindingProfiles(b Binding, profiles, infraKeys map[string]bool) error {
	for profile, variant := range b.PerProfile {
		if !profiles[profile] {
			return fmt.Errorf("projectmodel: deployment binding %q: profile %q not in environments", b.Component, profile)
		}
		for _, ik := range variant.Infra {
			if !infraKeys[ik] {
				return fmt.Errorf("projectmodel: deployment binding %q (profile %q): unknown infra key %q", b.Component, profile, ik)
			}
		}
	}
	return nil
}

// validateSettings checks that every setting's Type is a known setting type.
// scope names the owning section for error messages.
func validateSettings(scope string, settings []Setting) error {
	for _, s := range settings {
		if s.Type != "" && !knownSettingTypes[s.Type] {
			return fmt.Errorf("projectmodel: deployment %s setting %q: unknown type %q", scope, s.Name, s.Type)
		}
	}
	return nil
}
