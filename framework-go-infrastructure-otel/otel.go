// Package otel is the OpenTelemetry infrastructure satellite: the concrete,
// OTLP-backend-coupled implementation of the framework-go telemetry facade's
// [telemetry.Provider]. It constructs the OTLP/gRPC exporters and the SDK
// tracer/meter/logger providers wired to the collector, and hands them back behind
// the facade's opaque interface.
//
// It lives in its OWN module (a satellite, like framework-go-infrastructure-temporal
// for the Temporal SDK) — NOT in framework-go/utilities — because the OTLP exporter
// is infra-technology-coupled to a specific backend/wire protocol, the same
// category as the postgres/temporal/gitea/github/keycloak/llm satellites. The
// utility names no backend; this satellite owns ALL the backend vocabulary (OTLP,
// gRPC, endpoint, batch/retry, the SDK→collector coupling). A future swap of
// OTLP→stdout/another backend is a change to THIS package alone.
//
// Configuration is the OpenTelemetry STANDARD environment, read by the SDK itself —
// there are no service-specific keys:
//
//	OTEL_EXPORTER_OTLP_ENDPOINT    e.g. otel-collector-traces-collector.observability.svc.cluster.local:4317
//	OTEL_EXPORTER_OTLP_PROTOCOL    grpc (this satellite uses the gRPC exporters)
//	OTEL_EXPORTER_OTLP_INSECURE    true for the plaintext in-cluster collector
//	OTEL_SERVICE_NAME              e.g. archistrator-server
//	OTEL_RESOURCE_ATTRIBUTES       e.g. service.version=...,deployment.environment.name=production
//
// Graceful resolution lives HERE (the layer that knows the backend config): when
// OTEL_EXPORTER_OTLP_ENDPOINT is unset, [NewProvider] returns
// [telemetry.NoopProvider] so the composition root's Install wires inert globals
// and the service runs unchanged with telemetry off.
package otel

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/telemetry"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Options configure [NewProvider]. All fields are optional — the zero value yields
// a provider driven entirely by the standard OTEL_* environment.
type Options struct {
	// ServiceName seeds service.name when OTEL_SERVICE_NAME is not set in the
	// environment. The environment always wins; this is only a fallback so a
	// service is never nameless.
	ServiceName string

	// ServiceVersion seeds service.version when it is not already present in the
	// environment. Optional; omitted when empty (a service with no build-version
	// constant passes "" and it is simply not reported).
	ServiceVersion string
}

// NewProvider builds the OTLP/gRPC-backed [telemetry.Provider]: a tracer, meter,
// and logger provider exporting over OTLP/gRPC to the collector named in
// OTEL_EXPORTER_OTLP_ENDPOINT. When that endpoint is unset it returns
// [telemetry.NoopProvider] (telemetry off). The returned Provider's Shutdown
// flushes and stops every exporter/provider.
//
// The endpoint, protocol, and insecure flag are read from the standard OTEL_* env
// by the SDK exporters themselves — nothing is hardcoded here.
func NewProvider(ctx context.Context, opts Options) (telemetry.Provider, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return telemetry.NoopProvider(), nil
	}

	res, err := buildResource(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("otel satellite: build resource: %w", err)
	}

	traceExp, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otel satellite: otlp trace exporter: %w", err)
	}
	metricExp, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otel satellite: otlp metric exporter: %w", err)
	}
	logExp, err := otlploggrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otel satellite: otlp log exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)

	return otlpProvider{tp: tp, mp: mp, lp: lp}, nil
}

// otlpProvider adapts the concrete SDK providers to the facade's opaque
// [telemetry.Provider]. The SDK provider types satisfy the OTel API provider
// interfaces the facade exposes, so no per-handle wrapping is needed.
type otlpProvider struct {
	tp *sdktrace.TracerProvider
	mp *sdkmetric.MeterProvider
	lp *sdklog.LoggerProvider
}

func (p otlpProvider) TracerProvider() trace.TracerProvider { return p.tp }
func (p otlpProvider) MeterProvider() metric.MeterProvider  { return p.mp }
func (p otlpProvider) LoggerProvider() log.LoggerProvider   { return p.lp }

// Shutdown flushes and stops every provider, aggregating errors so one failure does
// not mask the others on the shutdown path.
func (p otlpProvider) Shutdown(ctx context.Context) error {
	return errors.Join(
		p.tp.Shutdown(ctx),
		p.mp.Shutdown(ctx),
		p.lp.Shutdown(ctx),
	)
}
