package composegen

import "strings"

// hookMethod is one emitted Hooks interface method: its Go signature line and a
// doc comment. The set is DERIVED from the model (see deriveHooks).
type hookMethod struct {
	doc  []string
	line string
}

// deriveHooks computes the container's Hooks method set from the resolved plan.
// Only the seams the model actually exercises are emitted:
//   - ResolveProfile: always (the fundamental legacy-toggles → profile seam the
//     binding switches key off of).
//   - TemporalLogger: when temporal infrastructure is declared (the SDK logger
//     adapter is cmd-local policy).
//   - PolicyDecisionPoint / TokenValidator / DevConfig / WrapManagers /
//     ExtraMounts: when the container has ≥1 web-exposed Manager (the auth
//     boundary + the logging wrap + composition-root-only routes).
//   - one method per distinct unmatched plain manager dep, typed at its
//     concrete dep.GoType (a resolver seam, e.g. the design PR-rail func-typed
//     repo resolvers, or a scalar/interface dep with no setting/binding link,
//     e.g. repoBase / the durableExecution client — archistrator A2).
//   - one Finalize<Component> per optional/optional-dormant binding that is
//     actually constructed (B3): the post-construction seam for composition
//     policy that swaps or wraps the constructed value (e.g. archistrator's
//     construction dry-run stub swap-in). Identity otherwise.
func deriveHooks(r *resolved) []hookMethod {
	var hs []hookMethod
	hs = append(hs, hookResolveProfile())
	if r.hasTemporal {
		hs = append(hs, hookTemporalLogger())
	}
	if len(r.webMgrs) > 0 {
		hs = append(hs, hookPDP(), hookTokenValidator(), hookDevConfig(), hookWrapManagers(), hookExtraMounts())
	}
	hs = append(hs, r.variantHooks...)    // G3: per-variant args hooks
	hs = append(hs, r.finalizeHooks...)   // B3: per-binding post-construction seams
	hs = append(hs, r.workerGateHooks...) // G6b: conditional-worker gates
	hs = append(hs, resolverHooks(r)...)  // G5: per-manager plain-dep resolvers
	return hs
}

// variantHookName is the per-variant args Hooks method name (G3):
// <Comp><Variant>Args (e.g. projectStateAccess + GitHub -> ProjectStateAccessGitHubArgs).
func variantHookName(component, variant string) string {
	return upperFirst(component) + variantToken(variant) + "Args"
}

// addVariantHook records the per-variant args Hooks method (G3) for a bound
// variant listed in Config.VariantHookArgs: <Comp><Variant>Args(cfg *Config)
// returns the ordered driver-supplied Go types the variant constructor consumes
// verbatim. The emitter stays policy-free — it emits only the typed seam; the
// hand hooks.go reads cfg and builds the composition-root ports/values.
func (r *resolved) addVariantHook(rb raBinding, variant string, specs []HookArgType) {
	types := make([]string, 0, len(specs))
	for _, s := range specs {
		types = append(types, s.GoType)
		if s.GoImport != "" {
			r.variantHookImports = append(r.variantHookImports, s.GoImport)
		}
	}
	name := variantHookName(rb.key, variant)
	r.variantHooks = append(r.variantHooks, hookMethod{
		doc: []string{
			name + " supplies the " + rb.key + " " + variant + " variant's constructor",
			"arguments the deployment model cannot express (composition-root ports /",
			"typed values). Read from cfg; the returned tuple is spread into the",
			"generated variant constructor call.",
		},
		line: name + "(cfg *Config) (" + strings.Join(types, ", ") + ")",
	})
}

// workerGateHook is the conditional-worker-registration gate (G6b) for a manager
// with ≥1 optional-dormant component dep: Register<Iface>Worker(cfg) reports
// whether to register the manager's Temporal Worker. WHICH managers get the gate
// is derived (optional-dormant dep presence); the boolean itself is irreducible
// policy — a manager may be nil-tolerant and always register (the design
// managers, whose rail just goes dormant) or gate on its external-effect deps
// being present / a dry-run stub filling them (the construction Worker, run()'s
// selectConstructionDeps) — so it is a hook.
func workerGateHook(iface string) hookMethod {
	return hookMethod{
		doc: []string{
			"Register" + iface + "Worker reports whether to register the " + iface,
			"Temporal Worker. The manager has ≥1 optional-dormant dependency, so its",
			"Worker registration is composition-root policy (return true to always",
			"register; gate on the dep presence / a dry-run stub otherwise).",
		},
		line: "Register" + iface + "Worker(cfg *Config) bool",
	}
}

// finalizeHookName is the per-binding post-construction Hooks method name
// (B3): Finalize<Component> (e.g. artifactAccess -> FinalizeArtifactAccess).
func finalizeHookName(component string) string {
	return "Finalize" + upperFirst(component)
}

// finalizeHook is the typed post-construction seam (B3) for one
// optional/optional-dormant binding: Finalize<Component>(cfg, v) is called
// immediately after the binding's construction (single arm or profile
// switch), and its return value REPLACES the constructed local. IDENTITY
// SEMANTICS: return v unchanged unless composition policy needs to swap or
// wrap it — e.g. archistrator's construction dry-run stub swap-in for
// constructionPipelineAccess/artifactAccess. This is the seam the deployment
// model cannot express: whether/when to substitute the profile-built RA is
// composition-root policy, not a binding variant.
func finalizeHook(rb raBinding) hookMethod {
	typ := rb.alias + "." + rb.iface
	name := finalizeHookName(rb.key)
	return hookMethod{
		doc: []string{
			name + " is called immediately after " + rb.key + "'s construction",
			"(presence " + strings.ToLower(rb.presence) + "). Return v unchanged unless",
			"composition policy needs to swap or wrap it (e.g. a construction",
			"dry-run stub swap-in) — the identity implementation is always correct.",
		},
		line: name + "(cfg *Config, v " + typ + ") " + typ,
	}
}

func hookResolveProfile() hookMethod {
	return hookMethod{
		doc: []string{
			"ResolveProfile maps the loaded config (legacy toggles) to the active",
			"deployment profile whose per-binding variants are constructed.",
		},
		line: "ResolveProfile(cfg *Config) string",
	}
}

func hookTemporalLogger() hookMethod {
	return hookMethod{
		doc: []string{
			"TemporalLogger adapts the composition-root slog.Logger onto the Temporal",
			"SDK logger the client + embedded workers log through.",
		},
		line: "TemporalLogger(logger *slog.Logger) tlog.Logger",
	}
}

func hookPDP() hookMethod {
	return hookMethod{
		doc: []string{
			"PolicyDecisionPoint is the authorization PDP the security Utility is built",
			"with (application policy — not model-derivable).",
		},
		line: "PolicyDecisionPoint() security.PolicyDecisionPoint",
	}
}

func hookTokenValidator() hookMethod {
	return hookMethod{
		doc: []string{
			"TokenValidator is the access-token validator for the auth boundary; nil",
			"selects the dev-principal / deny middleware path. IdP-specific.",
		},
		line: "TokenValidator(ctx context.Context, cfg *Config) (security.Validator, error)",
	}
}

func hookDevConfig() hookMethod {
	return hookMethod{
		doc: []string{
			"DevConfig is the auth dev-mode config threaded into NewServer and the auth",
			"middleware (the config_adapter seam).",
		},
		line: "DevConfig(cfg *Config) web.DevConfig",
	}
}

func hookWrapManagers() hookMethod {
	return hookMethod{
		doc: []string{
			"WrapManagers optionally decorates the web-exposed managers (e.g. the",
			"composition-root logging seam) before they are bound to the transports.",
			"Return the argument unchanged for no wrapping.",
		},
		line: "WrapManagers(managers WebManagers) WebManagers",
	}
}

func hookExtraMounts() hookMethod {
	return hookMethod{
		doc: []string{
			"ExtraMounts contributes composition-root-only routes (e.g. /api/userinfo,",
			"/mcp) onto the root mux, behind the same auth boundary. The wrapped",
			"managers are supplied for transports the generated server does not mount.",
		},
		line: "ExtraMounts(root *http.ServeMux, cfg *Config, dev web.DevConfig, validator security.Validator, managers WebManagers)",
	}
}

// resolverHooks emits one hook per distinct unmatched plain manager dep
// (r.hookDeps, populated by threadHookDep during resolveManagers), in
// first-seen order. There is no binding link for a plain dep today, so this
// covers BOTH func-typed resolvers and scalar/interface deps the model can't
// otherwise source — the hook impl returns the zero value / nil for one that
// stays unbuilt in the active profile.
func resolverHooks(r *resolved) []hookMethod {
	var hs []hookMethod
	for _, hd := range r.hookDeps {
		hs = append(hs, hookMethod{
			doc: []string{
				hd.name + " supplies a composition-root value the deployment model",
				"cannot express (a func-typed resolver, or a scalar/interface dep with no",
				"setting/binding link). Return the zero value (nil for an interface) for a",
				"dependency that stays unbuilt in the active profile.",
			},
			line: hd.name + "() " + hd.goType,
		})
	}
	return hs
}
