// Package telemetry is the framework-go telemetry Utility: the opaque facade a Go
// service's composition root uses to turn on (or leave off) export of traces,
// metrics, and logs. It is a pure Utility — it imports no other framework-go layer,
// holds no business knowledge, and names NO backend: its public surface carries no
// OTLP/exporter/collector/endpoint vocabulary, exactly as utilities/security names
// no JWT/Keycloak vocabulary.
//
// The backend coupling lives behind the [Provider] interface. A concrete,
// technology-coupled implementation — the OTLP exporters + SDK provider init wired
// to the in-cluster collector — is supplied by the framework-go-infrastructure-otel
// SATELLITE (mirroring framework-go-infrastructure-temporal). The composition root
// binds satellite→facade: it builds a Provider from the satellite and passes it to
// [Install]. A future swap of OTLP→stdout/another backend touches only the
// satellite, never this facade and never a caller.
//
// What this facade owns:
//   - [Provider]: the opaque set of telemetry handles a backend supplies (the
//     vendor-NEUTRAL OTel API provider interfaces + a Shutdown), and [NoopProvider]
//     as the always-available off switch.
//   - [Install]: installs a Provider's handles as the process-global tracer/meter/
//     logger providers + a W3C propagator, and bridges slog to the logger provider
//     so existing slog calls export. Returns one [Shutdown] that tears it all down.
//
// Graceful by default: pass [NoopProvider] (or a satellite Provider that resolved
// to off) and Install wires the API no-ops — telemetry is simply off and the
// service runs unchanged, the same "optional infra absent ⇒ degrade quietly" stance
// the rest of a composition root takes for Git/Anthropic/etc.
package telemetry

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
	lognoop "go.opentelemetry.io/otel/log/noop"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Provider is the opaque set of telemetry handles a backend supplies to [Install].
// It is expressed ONLY in the vendor-neutral OTel API provider interfaces — there
// is no exporter, OTLP, gRPC, endpoint, or collector vocabulary here, so swapping
// the backend never changes this contract. The infra satellite returns a Provider;
// [NoopProvider] returns the off switch.
type Provider interface {
	// TracerProvider, MeterProvider, LoggerProvider are the handles installed as
	// the process globals. They are the OTel API interfaces, not concrete SDK
	// types, so the facade stays backend-agnostic.
	TracerProvider() trace.TracerProvider
	MeterProvider() metric.MeterProvider
	LoggerProvider() log.LoggerProvider

	// Shutdown flushes and tears down the backing implementation. A no-op Provider
	// returns nil.
	Shutdown(ctx context.Context) error
}

// Options configure [Install]. The zero value is valid.
type Options struct {
	// ServiceName names the slog→OTel bridge's instrumentation scope. Optional.
	ServiceName string

	// Logger receives the facade's own startup/shutdown diagnostics. Defaults to
	// slog.Default() when nil.
	Logger *slog.Logger
}

// Shutdown flushes and tears down everything [Install] wired (the slog bridge has
// nothing to flush; the Provider does). Safe to call on a no-op install.
type Shutdown func(ctx context.Context) error

// Install installs the Provider's handles as the OTel process globals
// (tracer/meter/logger providers) plus a W3C trace-context + baggage propagator,
// and bridges the default slog logger to the global logger provider so the
// service's existing slog.Info/Error calls ALSO export as OTel log records (and
// carry the active trace/span ids) while still printing to stdout via the original
// handler. It returns a [Shutdown] that tears down the bridge install and the
// Provider.
//
// Passing [NoopProvider] installs the API no-ops: telemetry is off, but globals and
// the propagator are still set to valid (inert) values so instrumented call sites
// behave identically.
//
// The returned Shutdown MUST be deferred by the caller; otherwise buffered spans,
// metrics, and logs are lost on exit.
func Install(provider Provider, opts Options) Shutdown {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	otel.SetTracerProvider(provider.TracerProvider())
	otel.SetMeterProvider(provider.MeterProvider())
	logglobal.SetLoggerProvider(provider.LoggerProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Fan slog out to BOTH the original stdout handler and the OTel log bridge. The
	// bridge is vendor-neutral — it targets whatever LoggerProvider is installed
	// (the satellite's, or the no-op) — so it lives with the facade.
	bridged := slog.New(newFanoutHandler(
		slog.Default().Handler(),
		otelslog.NewHandler(opts.ServiceName, otelslog.WithLoggerProvider(provider.LoggerProvider())),
	))
	slog.SetDefault(bridged)

	logger.Info("telemetry installed", "serviceName", opts.ServiceName)

	return func(ctx context.Context) error {
		return errors.Join(provider.Shutdown(ctx))
	}
}

// NoopProvider returns the always-off Provider: the OTel API no-op tracer/meter/
// logger providers and a Shutdown that does nothing. The composition root uses it
// when telemetry is not configured (dev / systemtests), and the satellite returns
// it when its endpoint is unset — either way [Install] wires inert globals.
func NoopProvider() Provider { return noopProvider{} }

type noopProvider struct{}

func (noopProvider) TracerProvider() trace.TracerProvider { return tracenoop.NewTracerProvider() }
func (noopProvider) MeterProvider() metric.MeterProvider  { return metricnoop.NewMeterProvider() }
func (noopProvider) LoggerProvider() log.LoggerProvider   { return lognoop.NewLoggerProvider() }
func (noopProvider) Shutdown(context.Context) error       { return nil }
