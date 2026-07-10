// Package configgen emits a container's env-loaded configuration file
// (config.gen.go) from the deployment model's infrastructure declarations and
// settings. The emitted file carries one exported Config struct (a field per
// infrastructure input + a typed field per setting), a LoadConfig() that reads
// the environment and collects EVERY missing always-required variable into a
// single error, a MissingFor(profile) helper the hand composition root
// consults for per-profile requirements, and a DormantWarnings() method for
// optional-dormant infrastructure that is unconfigured.
//
// The generator deliberately does NOT resolve the active profile or enforce
// per-profile / conditional requirements at load time — profile RESOLUTION and
// the conditional requirement checks (e.g. "these creds are required only when
// dry-run is off") stay hand-written until a later step. configgen hard-
// requires ONLY the inputs that are required across ALL declared profiles;
// everything else is optional-with-accessor plus the MissingFor data the hand
// run() uses to reproduce its conditional checks.
package configgen

import (
	"fmt"
	"sort"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// Config parameterizes Generate.
type Config struct {
	// ContainerKey is the deployment container the config file is emitted for.
	// It is validated to resolve against the deployment's containers; the
	// emitted input/setting set is currently deployment-global (container
	// scoping via bindings — including per-binding Binding.Settings, which
	// collectSettings does not walk today — is a future refinement).
	ContainerKey string
	// EnvPrefix is the env-var namespace prefix (e.g. "ARCHISTRATOR"). Default
	// env-var names are "<PREFIX>_<INFRAKEY>_<INPUT>" for infra inputs and
	// "<PREFIX>_<SETTING_UPPER_SNAKE>" for settings; a per-declaration env
	// override replaces the derived name.
	EnvPrefix string
	// PackageName is the package clause of the emitted file — it is generated
	// INTO an existing package, not a new one.
	PackageName string
}

// infraField is one emitted infrastructure input field.
type infraField struct {
	GoName        string   // exported Go field name
	Env           string   // resolved env-var name
	InfraKey      string   // owning infra decl key
	RequiredAll   bool     // required across ALL declared profiles ⇒ hard-required
	RequiredClass bool     // owning decl presence == "required" (neither optional nor optional-dormant)
	Profiles      []string // profiles the owning decl is provisioned for
	OptDormant    bool     // owning decl presence == optional-dormant
	Default       string   // catalog-declared default; non-empty ⇒ never "missing" (LoadConfig falls back to it)
}

// settingField is one emitted setting field.
type settingField struct {
	GoName  string
	GoType  string // "string" | "bool" | "int" | "time.Duration"
	Env     string
	Default string // raw string default, parsed by the helper at load time
	Kind    string // "string" | "bool" | "int" | "duration"
}

// Generate emits {"config.gen.go": src} for cfg.ContainerKey against m's
// deployment model.
func Generate(m *projectmodel.Model, cfg Config) (map[string][]byte, error) {
	if cfg.PackageName == "" {
		return nil, fmt.Errorf("configgen: PackageName is required")
	}
	if cfg.EnvPrefix == "" {
		return nil, fmt.Errorf("configgen: EnvPrefix is required")
	}
	if m.Deployment == nil {
		return nil, fmt.Errorf("configgen: model has no deployment")
	}
	if cfg.ContainerKey != "" && !hasContainer(m.Deployment, cfg.ContainerKey) {
		return nil, fmt.Errorf("configgen: container %q: not found in deployment", cfg.ContainerKey)
	}

	infra, err := collectInfra(m.Deployment, cfg.EnvPrefix)
	if err != nil {
		return nil, err
	}
	settings, err := collectSettings(m.Deployment, cfg.EnvPrefix)
	if err != nil {
		return nil, err
	}

	src, err := emitFile(m.Deployment, cfg, infra, settings)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"config.gen.go": src}, nil
}

// hasContainer reports whether the deployment declares a container with key.
func hasContainer(d *projectmodel.Deployment, key string) bool {
	for _, c := range d.Containers {
		if c.Key == key {
			return true
		}
	}
	return false
}

// declaredProfiles returns the deployment's environment profiles as a set.
func declaredProfiles(d *projectmodel.Deployment) map[string]bool {
	out := make(map[string]bool, len(d.Environments))
	for _, e := range d.Environments {
		out[e.Profile] = true
	}
	return out
}

// collectInfra flattens every infra decl's catalog inputs into ordered
// infraFields (decls sorted by key, inputs in catalog order).
func collectInfra(d *projectmodel.Deployment, prefix string) ([]infraField, error) {
	allProfiles := declaredProfiles(d)
	decls := append([]projectmodel.InfraDecl(nil), d.Infrastructure...)
	sort.Slice(decls, func(i, j int) bool { return decls[i].Key < decls[j].Key })

	var out []infraField
	for _, decl := range decls {
		inputs, ok := substrateCatalog[decl.Substrate]
		if !ok {
			return nil, fmt.Errorf("configgen: infra %q: unknown substrate %q", decl.Key, decl.Substrate)
		}
		requiredClass := decl.Presence != "optional" && decl.Presence != "optional-dormant"
		for _, in := range inputs {
			env := decl.Env[in.Name]
			if env == "" {
				env = prefix + "_" + upperSnakeKey(decl.Key) + "_" + in.Name
			}
			out = append(out, infraField{
				GoName:        pascalToken(decl.Key) + pascalToken(in.Name),
				Env:           env,
				InfraKey:      decl.Key,
				RequiredAll:   requiredClass && coversAll(decl.Profiles, allProfiles) && in.Default == "",
				RequiredClass: requiredClass,
				Profiles:      decl.Profiles,
				OptDormant:    decl.Presence == "optional-dormant",
				Default:       in.Default,
			})
		}
	}
	return out, nil
}

// collectSettings maps each top-level setting to a typed settingField (in
// declaration order).
func collectSettings(d *projectmodel.Deployment, prefix string) ([]settingField, error) {
	var out []settingField
	for _, s := range d.Settings {
		goType, ok := settingGoType(s.Type)
		if !ok {
			return nil, fmt.Errorf("configgen: setting %q: unsupported type %q", s.Name, s.Type)
		}
		env := s.Env
		if env == "" {
			env = prefix + "_" + upperSnakeFromCamel(s.Name)
		}
		out = append(out, settingField{
			GoName:  upperFirst(s.Name),
			GoType:  goType,
			Env:     env,
			Default: s.Default,
			Kind:    settingKind(s.Type),
		})
	}
	return out, nil
}

// coversAll reports whether profiles lists every declared profile (⇒ the decl
// is required in every profile, hence hard-required at load time).
func coversAll(profiles []string, all map[string]bool) bool {
	have := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		have[p] = true
	}
	for p := range all {
		if !have[p] {
			return false
		}
	}
	return true
}

// settingGoType maps a setting type to its Go type (default "string" for an
// empty type). ok=false for an unrecognized type.
func settingGoType(t string) (string, bool) {
	switch t {
	case "", "string":
		return "string", true
	case "bool":
		return "bool", true
	case "int":
		return "int", true
	case "duration":
		return "time.Duration", true
	}
	return "", false
}

// settingKind normalizes an empty type to "string".
func settingKind(t string) string {
	if t == "" {
		return "string"
	}
	return t
}
