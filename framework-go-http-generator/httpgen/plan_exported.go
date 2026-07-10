package httpgen

import (
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// PlanParam is one operation parameter as PlanOps exposes it: enough for a
// client-side mirror (transportgen) to stringify a path/query value or shape a
// body-wrapper field without re-deriving httpgen's scalar classification.
type PlanParam struct {
	// Name is the param name exactly as declared on the contract operation
	// (e.g. "projectID"). It is also the JSON field name for a body param —
	// the emitted request wrapper's json tag is the param name verbatim.
	Name string
	// Pointer reports whether the manager signature takes this param by
	// pointer (an optional value).
	Pointer bool
	// ScalarKind is one of "string" / "integer" / "number" / "boolean" /
	// "uuid" for a path or query param — the wire-stringify family httpgen
	// itself parses/formats. It is "" for a body param, whose shape may be
	// arbitrary JSON (object, array, or scalar).
	ScalarKind string
	// RefName is the $defs type name the param's schema resolves to (e.g.
	// "ProjectID"), or "" when the schema is an inline scalar (a bare uuid or
	// primitive with no named-type wrapper).
	RefName string
}

// OpPlan is the exported, stable view of one operation's resolved HTTP
// binding — the same binding Generate uses to emit the handler for that
// operation, translated into a form a client-side mirror (transportgen) can
// consume without linking against httpgen's internal types.
type OpPlan struct {
	// Name is the operation name (PascalCase), e.g. "GetProject".
	Name string
	// Verb is the HTTP method: "GET" or "POST".
	Verb string
	// PathTemplate is the exact mounted route, e.g.
	// "/api/v1/project/get-project/{projectID}".
	PathTemplate string
	// PathParams are the op's ID path params, in URL segment order (the order
	// they appear appended to PathTemplate).
	PathParams []PlanParam
	// QueryParams are the op's GET query-string params, in declared order.
	QueryParams []PlanParam
	// BodyParams are the op's POST JSON body params (request wrapper fields),
	// in declared order.
	BodyParams []PlanParam
	// HasResult reports whether the op returns a result (bare-JSON 200 body)
	// or is void (204 No Content).
	HasResult bool
}

// PlanOps resolves the HTTP binding for every operation in doc — the single
// source of route/verb/param truth Generate itself binds against. It is the
// exported surface client-side mirrors (framework-go-app-generator's
// transportgen, systemtests SDKs, ...) consume instead of re-deriving the
// archistrator client convention independently.
//
// PlanOps is a pure adapter over the package-internal planOps: it performs no
// additional derivation, so it can never diverge from what Generate binds.
func PlanOps(doc *projectmodel.Doc) ([]OpPlan, error) {
	plans := planOps(doc)
	out := make([]OpPlan, 0, len(plans))
	for _, p := range plans {
		out = append(out, exportOpPlan(doc, p))
	}
	return out, nil
}

// exportOpPlan translates one internal opPlan into its exported OpPlan.
func exportOpPlan(doc *projectmodel.Doc, p opPlan) OpPlan {
	return OpPlan{
		Name:         p.op.Name,
		Verb:         p.method,
		PathTemplate: p.path,
		PathParams:   exportParamPlans(doc, p.pathParams),
		QueryParams:  exportParamPlans(doc, p.queryParams),
		BodyParams:   exportParamPlans(doc, p.bodyParams),
		HasResult:    p.op.Result != nil,
	}
}

// exportParamPlans translates internal paramPlans into exported PlanParams,
// resolving each param's scalar kind and $def ref name the same way the
// handler emitter does.
func exportParamPlans(doc *projectmodel.Doc, params []paramPlan) []PlanParam {
	if len(params) == 0 {
		return nil
	}
	out := make([]PlanParam, 0, len(params))
	for _, pp := range params {
		kind, _ := doc.ScalarKind(pp.param.Schema)
		out = append(out, PlanParam{
			Name:       pp.param.Name,
			Pointer:    pp.param.Pointer,
			ScalarKind: kind,
			RefName:    pp.param.Schema.RefName(),
		})
	}
	return out
}
