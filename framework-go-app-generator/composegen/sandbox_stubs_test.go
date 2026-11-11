package composegen_test

// addTemporalStub writes the go.temporal.io/sdk stub module (client, worker,
// workflow, interceptor, log, contrib/opentelemetry) — one module, six packages.
func addTemporalStub(files map[string]string) {
	files["_stubs/temporal/go.mod"] = "module go.temporal.io/sdk\n\ngo 1.25\n"
	files["_stubs/temporal/interceptor/interceptor.go"] = `package interceptor

type ClientInterceptor interface{}
`
	files["_stubs/temporal/workflow/workflow.go"] = `package workflow

import (
	"time"

	"go.temporal.io/sdk/temporal"
)

type ContextPropagator interface{}

// ActivityOptions is the part of the SDK's options the framework's manager package builds.
type ActivityOptions struct {
	StartToCloseTimeout time.Duration
	RetryPolicy         *temporal.RetryPolicy
}
`
	// The framework's manager package (the call Context every generated Null wrapper takes) builds
	// these; the sandbox only has to link them.
	files["_stubs/temporal/temporal/temporal.go"] = `package temporal

type RetryPolicy struct {
	NonRetryableErrorTypes []string
	MaximumAttempts        int32
}

type ApplicationError struct{ msg string }

func (e *ApplicationError) Error() string { return e.msg }

func NewApplicationErrorWithCause(msg, errType string, cause error, details ...any) error {
	return &ApplicationError{msg: msg}
}

func NewNonRetryableApplicationError(msg, errType string, cause error, details ...any) error {
	return &ApplicationError{msg: msg}
}
`
	files["_stubs/temporal/log/log.go"] = `package log

import "log/slog"

type Logger interface {
	Debug(msg string, kv ...any)
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

// NewStructuredLogger is what the seeded TemporalLogger hook calls.
func NewStructuredLogger(l *slog.Logger) Logger { return nil }
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
	files["_stubs/pgxpool/go.mod"] = "module github.com/jackc/pgx/v5/pgxpool\n\ngo 1.25\n"
	files["_stubs/pgxpool/pgxpool.go"] = `package pgxpool

// Pool stands in for the real pgxpool.Pool — just enough surface
// (Close, referenced from main.gen.go's "defer pool.Close()") for the sandbox
// to compile the profile-gated "var pool *pgxpool.Pool" predecl.
type Pool struct{}

func (*Pool) Close() {}
`
	files["_stubs/postgres/go.mod"] = "module github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres\n\ngo 1.25\n"
	files["_stubs/postgres/postgres.go"] = `package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool returns *pgxpool.Pool directly, matching the REAL
// framework-go-infrastructure-postgres.NewPool signature — main.gen.go's
// profile-gated form spells out "var pool *pgxpool.Pool" explicitly, so the
// stub must return the same concrete type, not a local wrapper.
func NewPool(ctx context.Context, url string) (*pgxpool.Pool, error) { return &pgxpool.Pool{}, nil }
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
	// The security stub decides through the PDP it is built with, as the real Utility does, so
	// the generated TestPolicyAdmitsEveryGeneratedRequest really runs in the sandbox.
	files["_stubs/framework-go/utilities/security/security.go"] = `package security

import "context"

type PrincipalKind string

const (
	PrincipalUser        PrincipalKind = "user"
	PrincipalApplication PrincipalKind = "application"
)

type Principal struct {
	Kind    PrincipalKind
	Subject string
	Roles   []string
	Claims  map[string]any
}

type Action struct{ Verb string }

type ResourceRef struct{ Kind, ID string }

type Decision struct{ Permit bool }

type PolicyDecisionPoint interface {
	Decide(ctx context.Context, principal Principal, action Action, resource ResourceRef) (bool, error)
}

type Validator interface{}

type Security interface {
	Authorize(ctx context.Context, principal Principal, action Action, resource ResourceRef) (Decision, error)
}

type service struct{ pdp PolicyDecisionPoint }

type Option func(*service)

func WithPolicyDecisionPoint(p PolicyDecisionPoint) Option { return func(s *service) { s.pdp = p } }

func New(opts ...Option) Security {
	s := &service{}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *service) Authorize(ctx context.Context, p Principal, a Action, r ResourceRef) (Decision, error) {
	if s.pdp == nil {
		return Decision{}, nil
	}
	permit, err := s.pdp.Decide(ctx, p, a, r)
	return Decision{Permit: permit && err == nil}, err
}
`
	// The call Context of a Manager interface method, which the generated Null wrappers repeat, and the
	// error kinds the generated fault wrapper answers with.
	files["_stubs/framework-go/manager/manager.go"] = `package manager

import "context"

type Context struct{ context.Context }

type Kind int

const (
	Unknown Kind = iota
	ContractMisuse
	NotFound
	Unauthorized
	FailedPrecondition
	Infrastructure
)

type Error struct {
	Kind   Kind
	Detail string
}

func New(kind Kind, detail string) *Error { return &Error{Kind: kind, Detail: detail} }

func (e *Error) Error() string { return e.Detail }
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
