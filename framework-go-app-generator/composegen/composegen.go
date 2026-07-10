// Package composegen emits a container's generated composition root
// (main.gen.go) from the deployment model + service contracts: the ordered
// boot walk today's hand cmd/server/main.go run() performs — signal context →
// telemetry install → Temporal client dial → Postgres pool → per-binding
// ResourceAccess construction (profile switch over the deployment bindings) →
// Engines → the security Utility → Managers via their generated DI constructors
// (deps threaded from the constructed locals + the temporal client + config
// settings + a typed Hooks seam) → one Temporal Worker per Manager → the
// generated web Handlers + NewServer + composition-root ExtraMounts → HTTP
// serve + graceful shutdown.
//
// The emitter contains NO policy. Everything it emits is a call to one of:
//
//	(1) a catalog SATELLITE constructor (otel/postgres/temporal — the fixed
//	    infrastructure seams),
//	(2) a package VARIANT constructor named by a binding
//	    (New<Variant><Interface>, positional args = the infra values the
//	    variant consumes then its binding settings — the step-8 A1 convention),
//	(3) a generated DI constructor (New<Interface> for managers/engines/RAs),
//	(4) a typed Hooks interface method — the genuinely-compositional policy the
//	    model cannot express (profile resolution, the Temporal logger seam, the
//	    authorization PDP, the access-token validator, the auth dev config, the
//	    manager logging wrap, and composition-root-only route mounts).
//
// The emitted Hooks set is DERIVED from the model: a container with no
// web-exposed Manager emits no PDP/validator/mounts hooks; a container with no
// temporal infrastructure emits no TemporalLogger hook. This is the P1 emitter;
// the archistrator-fidelity proof is its A2 adoption, not these goldens.
package composegen

import (
	"fmt"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// Config parameterizes Generate.
type Config struct {
	// ContainerKey is the deployment container the composition root is emitted
	// for. It is validated to resolve against the deployment's containers; the
	// emitted wiring is deployment-global today (container scoping mirrors
	// configgen — a future refinement), and doubles as the OTel service name.
	ContainerKey string
	// ModulePath is the target server module's import path root, e.g.
	// "github.com/mixofreality-studio/archistrator/server". Every component
	// package import is ModulePath + "/" + goPackage.
	ModulePath string
	// PackageName is the package clause of the emitted file — main.gen.go is
	// generated INTO the existing composition-root package (alongside the
	// configgen config.gen.go whose Config type it threads), not a new one.
	PackageName string
	// EnvPrefix is the env-var namespace prefix (carried for symmetry with the
	// other emitters; main.gen.go references the config by Go field name, not by
	// env var, so it is currently unused by the walk).
	EnvPrefix string
	// VariantHookArgs marks binding variants whose constructor arguments the
	// deployment model CANNOT supply — composition-root ports (e.g. the
	// projectstate GitHub catalog/minter) or a typed value the substrate catalog
	// can't produce (e.g. the artifact GitHub int64 installationID). Keyed by
	// "<component>/<variant>", the value is the ORDERED Go types the emitted
	// per-variant Hooks method (<Comp><Variant>Args(cfg *Config)) returns and the
	// variant constructor consumes verbatim (Go's f(g()) multi-value spread). The
	// emitter stays policy-free: it emits a typed hook, the driver supplies the
	// types here, and the hand hooks.go supplies the values. When a variant is
	// listed, its substrate-arg/setting threading is REPLACED by the single hook
	// call (the hook impl reads cfg itself).
	VariantHookArgs map[string][]HookArgType
	// WebExposedManagers, when non-nil, REPLACES the System-relationship-derived
	// web-exposed manager set (isWebExposed) with exactly this component-key
	// set — e.g. archistrator's billingManager carries a web-client relationship
	// in the committed System model, but cmd/clientgen deliberately generates no
	// internal/client/web/billing package for it (money-move ops are not
	// web-wired), so the relationship alone must not force a <mgr>web.Handler
	// import/mount that would not compile. A manager NOT in this set is never
	// web-exposed, regardless of its System relationships. When nil (the
	// default), the current relationship derivation stands unchanged. Keys are
	// component keys (e.g. "systemDesignManager"), matching VariantHookArgs's
	// binding-key convention. The emitted file's header comment records that the
	// web-exposed set came from this override.
	WebExposedManagers []string
}

// HookArgType is one return value of a per-variant args Hooks method: a verbatim
// Go type string plus the import path (if any) the emitted file must add.
type HookArgType struct {
	GoType   string
	GoImport string
}

// genHeader is the generated-code marker every emitted file starts with (the
// golangci-lint generated-file exclusion + humans recognize it). Must be first.
const genHeader = "// Code generated by composegen. DO NOT EDIT.\n"

// Generate emits {"main.gen.go": src} for cfg.ContainerKey against m's
// deployment model + service contracts.
func Generate(m *projectmodel.Model, cfg Config) (map[string][]byte, error) {
	if err := validateConfig(m, cfg); err != nil {
		return nil, err
	}
	r, err := resolve(m, cfg)
	if err != nil {
		return nil, err
	}
	src, err := emitMain(r)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"main.gen.go": src}, nil
}

// validateConfig checks the required Config fields + that the deployment exists
// and carries the named container.
func validateConfig(m *projectmodel.Model, cfg Config) error {
	if cfg.PackageName == "" {
		return fmt.Errorf("composegen: PackageName is required")
	}
	if cfg.ModulePath == "" {
		return fmt.Errorf("composegen: ModulePath is required")
	}
	if m.Deployment == nil {
		return fmt.Errorf("composegen: model has no deployment")
	}
	if cfg.ContainerKey != "" && !hasContainer(m.Deployment, cfg.ContainerKey) {
		return fmt.Errorf("composegen: container %q: not found in deployment", cfg.ContainerKey)
	}
	return nil
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
