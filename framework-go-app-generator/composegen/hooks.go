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
//   - one method per func-typed plain manager dep (a resolver seam, e.g. the
//     design PR-rail repo resolvers — archistrator A2; none in the greenfield
//     fixture).
func deriveHooks(r *resolved) []hookMethod {
	var hs []hookMethod
	hs = append(hs, hookResolveProfile())
	if r.hasTemporal {
		hs = append(hs, hookTemporalLogger())
	}
	if len(r.webMgrs) > 0 {
		hs = append(hs, hookPDP(), hookTokenValidator(), hookDevConfig(), hookWrapManagers(), hookExtraMounts())
	}
	hs = append(hs, resolverHooks(r)...)
	return hs
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

// resolverHooks emits one hook per distinct func-typed plain manager dep, in
// first-seen order.
func resolverHooks(r *resolved) []hookMethod {
	var hs []hookMethod
	seen := map[string]bool{}
	for _, mc := range r.managers {
		for _, arg := range mc.ctorArgs {
			if !strings.HasPrefix(arg, "hooks.") || seen[arg] {
				continue
			}
			seen[arg] = true
			name := strings.TrimSuffix(strings.TrimPrefix(arg, "hooks."), "()")
			hs = append(hs, hookMethod{
				doc:  []string{name + " supplies a composition-root resolver the model cannot express."},
				line: name + "() any",
			})
		}
	}
	return hs
}
