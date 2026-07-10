package composegen

import "github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/deploynaming"

// substrateCatalog maps a declared infrastructure substrate to its ordered
// configuration INPUTS — the MECHANISM half of the deployment split. Shared
// verbatim with configgen via internal/deploynaming (composegen threads the
// SAME cfg fields the config file exposes for those inputs — see that
// package's doc comment for the full rationale; a drift here can no longer
// silently break the cfg.<Field> references).
var substrateCatalog = deploynaming.SubstrateCatalog

// The substrates that produce a SINGLE root-level satellite the composition
// root constructs once and shares (rather than config values a variant consumes
// directly) are otel/temporal/postgres — their construction is emitted in the
// fixed walk order (telemetry → temporal → postgres) before any binding (see
// resolved.resolveInfra + the write* functions).

// variantArgsForSubstrate returns the arg EXPRESSIONS a variant consuming an
// infra key of the given substrate contributes to its New<Variant><Interface>
// call, in the step-8 A1 positional convention (infra values first).
//
//   - temporal  → the shared dialed client `tc`.
//   - postgres  → the request context + the shared pool `ctx, pool`.
//   - otel      → nothing (telemetry is a global install, never a variant dep).
//   - otherwise → the cfg string fields for every catalog input of that infra
//     key (github-app / keycloak: the variant mints its own client internally
//     from these — the A1 fold that keeps sibling infra out of the RA surface).
func variantArgsForSubstrate(substrate, infraKey string) []string {
	switch substrate {
	case "temporal":
		return []string{"tc"}
	case "postgres":
		return []string{"ctx", "pool"}
	case "otel":
		return nil
	default:
		var out []string
		for _, in := range substrateCatalog[substrate] {
			out = append(out, "cfg."+infraFieldName(infraKey, in))
		}
		return out
	}
}
