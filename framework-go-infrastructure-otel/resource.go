package otel

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// buildResource merges the SDK's default + environment resource attributes
// (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES, folded in by resource.Default())
// with the opts fallbacks. resource.Merge keeps the second argument's values on
// conflict, so the environment-derived attributes (resource.Default) win over the
// opts fallback — the deployment's env is authoritative.
func buildResource(_ context.Context, opts Options) (*resource.Resource, error) {
	fallback := resource.NewSchemaless(fallbackResourceAttrs(opts)...)
	return resource.Merge(fallback, resource.Default())
}

// fallbackResourceAttrs builds the resource attributes used ONLY as a fallback
// under the environment-derived ones. service.name is always seeded from opts so a
// resource is never nameless; service.version is seeded only when provided.
func fallbackResourceAttrs(opts Options) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 2)
	if opts.ServiceName != "" {
		attrs = append(attrs, semconv.ServiceName(opts.ServiceName))
	}
	if opts.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(opts.ServiceVersion))
	}
	return attrs
}
