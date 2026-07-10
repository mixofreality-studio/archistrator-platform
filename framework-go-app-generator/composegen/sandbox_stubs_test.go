package composegen_test

// addTemporalStub writes the go.temporal.io/sdk stub module (client, worker,
// workflow, interceptor, log, contrib/opentelemetry) — one module, six packages.
func addTemporalStub(files map[string]string) {
	files["_stubs/temporal/go.mod"] = "module go.temporal.io/sdk\n\ngo 1.25\n"
	files["_stubs/temporal/interceptor/interceptor.go"] = `package interceptor

type ClientInterceptor interface{}
`
	files["_stubs/temporal/workflow/workflow.go"] = `package workflow

type ContextPropagator interface{}
`
	files["_stubs/temporal/log/log.go"] = `package log

type Logger interface {
	Debug(msg string, kv ...any)
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}
`
	files["_stubs/temporal/client/client.go"] = `package client

import (
	"context"

	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/workflow"
)

type Options struct {
	HostPort           string
	Namespace          string
	Logger             log.Logger
	ContextPropagators []workflow.ContextPropagator
	Interceptors       []interceptor.ClientInterceptor
	MetricsHandler     any
}

type Client interface{ Close() }

type stub struct{}

func (stub) Close() {}

func DialContext(ctx context.Context, opts Options) (Client, error) { return stub{}, nil }
`
	files["_stubs/temporal/worker/worker.go"] = `package worker

import "go.temporal.io/sdk/client"

type Options struct{}

type Worker interface {
	Start() error
	Stop()
}

type stub struct{}

func (stub) Start() error { return nil }
func (stub) Stop()        {}

func New(c client.Client, taskQueue string, opts Options) Worker { return stub{} }
`
	files["_stubs/temporal/contrib/opentelemetry/otel.go"] = `package opentelemetry

import "go.temporal.io/sdk/interceptor"

type TracerOptions struct{}

type MetricsHandlerOptions struct{}

func NewTracingInterceptor(TracerOptions) (interceptor.ClientInterceptor, error) { return nil, nil }

func NewMetricsHandler(MetricsHandlerOptions) any { return nil }
`
}

// addFrameworkStubs writes the otelhttp, framework-go (security/telemetry), otel
// infra, postgres infra, and temporal infra stub modules.
func addFrameworkStubs(files map[string]string) {
	files["_stubs/otelhttp/go.mod"] = "module go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp\n\ngo 1.25\n"
	files["_stubs/otelhttp/otelhttp.go"] = `package otelhttp

import "net/http"

func NewHandler(h http.Handler, operation string, opts ...any) http.Handler { return h }
`
	files["_stubs/otel/go.mod"] = "module github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-otel\n\ngo 1.25\n"
	files["_stubs/otel/otel.go"] = `package otel

import "context"

type Options struct{ ServiceName string }

type Provider struct{}

func NewProvider(ctx context.Context, opts Options) (Provider, error) { return Provider{}, nil }
`
	files["_stubs/postgres/go.mod"] = "module github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres\n\ngo 1.25\n"
	files["_stubs/postgres/postgres.go"] = `package postgres

import "context"

type Pool struct{}

func (*Pool) Close() {}

func NewPool(ctx context.Context, url string) (*Pool, error) { return &Pool{}, nil }
`
	files["_stubs/temporalinfra/go.mod"] = "module github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-temporal\n\ngo 1.25\n"
	files["_stubs/temporalinfra/temporal.go"] = `package temporal

type propagator struct{}

func NewPrincipalPropagator() any { return propagator{} }
`
	addFrameworkGoStub(files)
}

func addFrameworkGoStub(files map[string]string) {
	files["_stubs/framework-go/go.mod"] = "module github.com/mixofreality-studio/archistrator-platform/framework-go\n\ngo 1.25\n"
	files["_stubs/framework-go/utilities/security/security.go"] = `package security

type PolicyDecisionPoint interface{}

type Validator interface{}

type Security interface{}

type Option func()

func WithPolicyDecisionPoint(p PolicyDecisionPoint) Option { return func() {} }

func New(opts ...Option) Security { return nil }
`
	files["_stubs/framework-go/utilities/telemetry/telemetry.go"] = `package telemetry

import (
	"context"
	"log/slog"
)

type Options struct {
	ServiceName string
	Logger      *slog.Logger
}

type Shutdown func(context.Context) error

func Install(provider any, opts Options) Shutdown {
	return func(context.Context) error { return nil }
}
`
}
