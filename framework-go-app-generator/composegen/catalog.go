package composegen

import "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"

// The substrate catalog (a substrate's ordered configuration inputs) is projectmodel's:
// projectmodel.Substrate.Inputs. configgen emits the fields from it and composegen threads the
// SAME cfg fields, so the two cannot drift.
//
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
//     key (github-app / keycloak / git-repo: the variant mints its own client
//     internally from these — the A1 fold that keeps sibling infra out of the
//     RA surface).
func variantArgsForSubstrate(substrate projectmodel.Substrate, infraKey string) []string {
	switch substrate {
	case projectmodel.SubstrateTemporal:
		return []string{"tc"}
	case projectmodel.SubstratePostgres:
		return []string{"ctx", "pool"}
	case projectmodel.SubstrateOtel:
		return nil
	case projectmodel.SubstrateGitHubApp, projectmodel.SubstrateKeycloak, projectmodel.SubstrateExternalHTTP, projectmodel.SubstrateGitRepo:
		var out []string
		for _, in := range substrate.Inputs() {
			out = append(out, "cfg."+infraFieldName(infraKey, in.Name))
		}
		return out
	}
	return nil
}
