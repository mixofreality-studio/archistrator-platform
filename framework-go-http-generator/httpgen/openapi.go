package httpgen

import (
	"encoding/json"
	"fmt"
	"strings"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"gopkg.in/yaml.v3"
)

// genOpenAPI builds an OpenAPI 3.1 document for the contract. paths come from the
// routing convention; components/schemas are the contract $defs passed through as
// OAS 3.1 component schemas (OAS 3.1 schemas ARE JSON Schema, so this is largely
// verbatim — only the internal $ref base "#/$defs/" is rewritten to
// "#/components/schemas/"; x-* extensions, incl. x-go-* and x-enum-varnames,
// pass through unchanged as permitted specification extensions).
func genOpenAPI(doc *projectmodel.Doc, plans []opPlan, opts Options) ([]byte, error) {
	schemas := map[string]any{}
	for name, raw := range doc.Defs {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("openapi: decode $def %s: %w", name, err)
		}
		schemas[name] = rewriteRefs(v)
	}
	schemas["ErrorResponse"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"error": map[string]any{"type": "string"},
			"code":  map[string]any{"type": "string"},
		},
		"required":             []any{"error", "code"},
		"additionalProperties": false,
	}

	paths := map[string]any{}
	for _, p := range plans {
		opObj := operationObject(p)
		method := strings.ToLower(p.method)
		if existing, ok := paths[p.path].(map[string]any); ok {
			existing[method] = opObj
		} else {
			paths[p.path] = map[string]any{method: opObj}
		}
	}

	root := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   opts.Title,
			"version": opts.Version,
		},
		"security": []any{map[string]any{"bearerAuth": []any{}}},
		"paths":    paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"},
			},
			"schemas": schemas,
		},
	}

	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("openapi: marshal yaml: %w", err)
	}
	_ = enc.Close()
	return []byte(b.String()), nil
}

// operationObject builds the OAS Operation object (parameters, requestBody,
// responses) for one planned op.
func operationObject(p opPlan) map[string]any {
	obj := map[string]any{
		"operationId": p.op.Name,
		"security":    []any{map[string]any{"bearerAuth": []any{}}},
	}

	var params []any
	for _, pp := range p.pathParams {
		params = append(params, map[string]any{
			"name":     pp.param.Name,
			"in":       "path",
			"required": true,
			"schema":   nodeToOAS(pp.param.Schema),
		})
	}
	for _, qp := range p.queryParams {
		params = append(params, map[string]any{
			"name":     qp.param.Name,
			"in":       "query",
			"required": !qp.param.Pointer,
			"schema":   nodeToOAS(qp.param.Schema),
		})
	}
	if len(params) > 0 {
		obj["parameters"] = params
	}

	if len(p.bodyParams) > 0 {
		obj["requestBody"] = requestBodyObject(p)
	}

	obj["responses"] = responsesObject(p)
	return obj
}

// requestBodyObject builds the OAS requestBody object for a POST op: a wrapper
// object schema with one property per body param, required unless the param is a
// pointer (optional).
func requestBodyObject(p opPlan) map[string]any {
	props := map[string]any{}
	var required []any
	for _, bp := range p.bodyParams {
		props[bp.param.Name] = nodeToOAS(bp.param.Schema)
		if !bp.param.Pointer {
			required = append(required, bp.param.Name)
		}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{"schema": schema},
		},
	}
}

func responsesObject(p opPlan) map[string]any {
	resp := map[string]any{}
	if p.op.Result != nil {
		resp["200"] = map[string]any{
			"description": "success",
			"content": map[string]any{
				"application/json": map[string]any{"schema": nodeToOAS(p.op.Result)},
			},
		}
	} else {
		resp["204"] = map[string]any{"description": "no content"}
	}
	// Error-kind -> status set, matching the handler's manager.Kind mapping plus
	// the transport 401.
	for code, desc := range map[string]string{
		"400": "contract misuse",
		"401": "unauthenticated",
		"403": "forbidden",
		"404": "not found",
		"409": "failed precondition",
		"500": "internal error",
		"503": "infrastructure unavailable",
	} {
		resp[code] = errorResponseRef(desc)
	}
	return resp
}

func errorResponseRef(desc string) map[string]any {
	return map[string]any{
		"description": desc,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
			},
		},
	}
}

// nodeToOAS renders a param/result schema node as an OAS schema object.
func nodeToOAS(n *projectmodel.SchemaNode) map[string]any {
	if n == nil {
		return map[string]any{}
	}
	if name := n.RefName(); name != "" {
		return map[string]any{"$ref": "#/components/schemas/" + name}
	}
	out := map[string]any{}
	if t := n.Type.Primary(); t != "" {
		out["type"] = t
	}
	if n.Format != "" {
		out["format"] = n.Format
	}
	if n.XGoType != "" {
		out["x-go-type"] = n.XGoType
	}
	if n.XGoImport != "" {
		out["x-go-import"] = n.XGoImport
	}
	if n.Items != nil {
		out["items"] = nodeToOAS(n.Items)
	}
	return out
}

// rewriteRefs recursively rewrites "#/$defs/X" $ref strings to the OAS component
// path "#/components/schemas/X" across a decoded JSON value.
func rewriteRefs(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return rewriteRefMap(t)
	case []any:
		for i, val := range t {
			t[i] = rewriteRefs(val)
		}
		return t
	default:
		return v
	}
}

// rewriteRefMap rewrites $ref strings in an object node and recurses into the
// rest of its values.
func rewriteRefMap(m map[string]any) map[string]any {
	for k, val := range m {
		if k == "$ref" {
			if s, ok := val.(string); ok {
				m[k] = strings.Replace(s, "#/$defs/", "#/components/schemas/", 1)
				continue
			}
		}
		m[k] = rewriteRefs(val)
	}
	return m
}
