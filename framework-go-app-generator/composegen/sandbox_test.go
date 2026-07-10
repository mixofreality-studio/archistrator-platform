package composegen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
)

const sandboxModule = "example.com/greenfield/server"

// TestCompileSandbox writes the emitted main.gen.go (+ the configgen config.gen.go
// + a tiny hand main.go implementing Hooks) into a throwaway module wired to
// hand-written STUB packages for every component + framework + SDK import, and
// proves the whole thing builds and vets under GOWORK=off. This is the emitter's
// expressibility proof: the generated composition root, the derived Hooks
// interface, the variant/DI/satellite call shapes, and the WebManagers bundle all
// typecheck against the step-8 A1 convention.
func TestCompileSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile sandbox in -short")
	}
	m := loadGaps(t)
	main, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatalf("composegen: %v", err)
	}
	cfgFile, err := configgen.Generate(m, configgen.Config{ContainerKey: "order-app", EnvPrefix: "ORDERAPP", PackageName: "main"})
	if err != nil {
		t.Fatalf("configgen: %v", err)
	}

	dir := t.TempDir()
	files := sandboxFiles()
	files["main.gen.go"] = string(main["main.gen.go"])
	files["config.gen.go"] = string(cfgFile["config.gen.go"])
	writeTree(t, dir, files)

	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
		if o, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v in sandbox failed: %v\n%s", args, err, o)
		}
	}
}

// writeTree writes every path→content under root, creating parent dirs.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// sandboxFiles returns the fixed stub tree (go.mod files + stub packages + the
// hand main.go). main.gen.go / config.gen.go are added by the caller.
func sandboxFiles() map[string]string {
	files := map[string]string{}
	addRootModule(files)
	addHandMain(files)
	addInternalStubs(files)
	addTemporalStub(files)
	addFrameworkStubs(files)
	return files
}

func addRootModule(files map[string]string) {
	files["go.mod"] = `module ` + sandboxModule + `

go 1.25

require (
	github.com/mixofreality-studio/archistrator-platform/framework-go v0.0.0
	github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-otel v0.0.0
	github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres v0.0.0
	github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-temporal v0.0.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.0.0
	go.temporal.io/sdk v0.0.0
)

replace github.com/mixofreality-studio/archistrator-platform/framework-go => ./_stubs/framework-go
replace github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-otel => ./_stubs/otel
replace github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres => ./_stubs/postgres
replace github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-temporal => ./_stubs/temporalinfra
replace go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp => ./_stubs/otelhttp
replace go.temporal.io/sdk => ./_stubs/temporal
`
}

func addHandMain(files map[string]string) {
	files["main.go"] = `package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	web "` + sandboxModule + `/internal/client/web"
	managerbilling "` + sandboxModule + `/internal/manager/billing"
	"` + sandboxModule + `/internal/resourceaccess/ledgerstate"
	"` + sandboxModule + `/internal/resourceaccess/orderstate"
	"` + sandboxModule + `/internal/resourceaccess/repolookup"
	security "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	tlog "go.temporal.io/sdk/log"
)

type appHooks struct{}

func (appHooks) ResolveProfile(cfg *Config) string                   { return "local" }
func (appHooks) TemporalLogger(logger *slog.Logger) tlog.Logger      { return nil }
func (appHooks) PolicyDecisionPoint() security.PolicyDecisionPoint    { return nil }
func (appHooks) DevConfig(cfg *Config) web.DevConfig                  { return web.DevConfig{} }
func (appHooks) WrapManagers(m WebManagers) WebManagers               { return m }
func (appHooks) TokenValidator(ctx context.Context, cfg *Config) (security.Validator, error) {
	return nil, nil
}
func (appHooks) ExtraMounts(root *http.ServeMux, cfg *Config, dev web.DevConfig, validator security.Validator, m WebManagers) {
}
func (appHooks) OrderManagerRepo() func(orderID string) (repolookup.RepoRef, bool) {
	return func(orderID string) (repolookup.RepoRef, bool) { return repolookup.RepoRef{}, false }
}
func (appHooks) OrderManagerRepoBase() string { return "" }
func (appHooks) BillingManagerRepo() func(id managerbilling.AccountID) (repolookup.RepoRef, bool) {
	return func(id managerbilling.AccountID) (repolookup.RepoRef, bool) { return repolookup.RepoRef{}, false }
}

// FinalizeOrderStateAccess (B3): identity — no composition-root swap/wrap
// needed for the sandbox proof.
func (appHooks) FinalizeOrderStateAccess(cfg *Config, v orderstate.OrderStateAccess) orderstate.OrderStateAccess {
	return v
}

// FinalizeLedgerStateAccess (B3/A2): identity — ledgerStateAccess is a
// REQUIRED, arm-less stub binding (G4); the emitter now emits Finalize<X> for
// every constructed binding, required ones included, so the sandbox needs
// this identity stub too even though no swap/wrap is needed here.
func (appHooks) FinalizeLedgerStateAccess(cfg *Config, v ledgerstate.LedgerStateAccess) ledgerstate.LedgerStateAccess {
	return v
}

// RegisterOrderManagerWorker (G6b): orderStateAccess is optional-dormant in
// this fixture but always present (both profiles carry an arm), so the
// sandbox always registers the Worker.
func (appHooks) RegisterOrderManagerWorker(cfg *Config) bool { return true }

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := LoadConfig()
	if err != nil {
		logger.Error("config", "err", err)
		os.Exit(1)
	}
	if err := RunGenerated(cfg, appHooks{}, logger); err != nil {
		os.Exit(1)
	}
}
`
}

// addInternalStubs writes the module-internal component stub packages (RA,
// engine, managers, web transports).
func addInternalStubs(files map[string]string) {
	files["internal/resourceaccess/orderstate/orderstate.go"] = `package orderstate

import (
	"context"

	postgres "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres"
)

type OrderStateAccess interface{}

type stub struct{}

func NewPostgresOrderStateAccess(ctx context.Context, pool *postgres.Pool) (OrderStateAccess, error) {
	return stub{}, nil
}

func NewMemoryOrderStateAccess() OrderStateAccess { return stub{} }
`
	files["internal/engine/pricing/pricing.go"] = `package pricing

type PricingEngine interface{}

type impl struct{}

func NewPricingEngine() PricingEngine { return impl{} }
`
	files["internal/manager/order/order.go"] = `package order

import (
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"` + sandboxModule + `/internal/engine/pricing"
	"` + sandboxModule + `/internal/resourceaccess/orderstate"
	"` + sandboxModule + `/internal/resourceaccess/repolookup"
)

type OrderManager interface{}

type impl struct{}

const TaskQueue = "order"

func NewOrderManager(tc client.Client, orderState orderstate.OrderStateAccess, pr pricing.PricingEngine, repo func(orderID string) (repolookup.RepoRef, bool), repoBase string) OrderManager {
	return impl{}
}

func RegisterManagerWorker(w worker.Worker, m OrderManager) {}
`
	files["internal/resourceaccess/repolookup/repolookup.go"] = `package repolookup

// RepoRef is a placeholder for a resolved source-control repo reference — the
// stub stands in for the real framework-go-app-generator/internal/... RA
// import a func-typed plain manager dep threads through a typed Hooks method
// (composegen fix 1: the hook's dep.GoImport is added to the emitted imports).
type RepoRef struct {
	Owner string
	Name  string
}
`
	files["internal/manager/fulfillment/fulfillment.go"] = `package fulfillment

import (
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

type FulfillmentManager interface{}

type impl struct{}

const TaskQueue = "fulfillment"

func NewFulfillmentManager(tc client.Client) FulfillmentManager { return impl{} }

func RegisterManagerWorker(w worker.Worker, m FulfillmentManager) {}
`
	// G2 nil-goPackage: reportingEngine carries goPackage=null and is SKIPPED by
	// the emitter — it needs no stub. G4 zero-arm required stub RA:
	files["internal/resourceaccess/ledgerstate/ledgerstate.go"] = `package ledgerstate

// LedgerStateAccess is a modelgen no-arg stub RA (contract stub=true, an arm-less
// required binding): the emitter builds it as ledgerstate.NewLedgerStateAccess().
type LedgerStateAccess interface{}

type stub struct{}

func NewLedgerStateAccess() LedgerStateAccess { return stub{} }
`
	// G1 alias collision: internal/engine/billing + internal/manager/billing both
	// end in "billing" — the emitter aliases them enginebilling / managerbilling.
	files["internal/engine/billing/billing.go"] = `package billing

type BillingEngine interface{}

type impl struct{}

func NewBillingEngine() BillingEngine { return impl{} }
`
	// G5 cross-manager same-name func dep: billingManager also declares a "repo"
	// dep, whose bare (unqualified) exported type AccountID the emitter qualifies
	// with THIS package's alias (managerbilling.AccountID), and whose hook is
	// named per-manager (BillingManagerRepo) so it never collides with the order
	// manager's Repo.
	files["internal/manager/billing/billing.go"] = `package billing

import (
	enginebilling "` + sandboxModule + `/internal/engine/billing"
	"` + sandboxModule + `/internal/resourceaccess/ledgerstate"
	"` + sandboxModule + `/internal/resourceaccess/repolookup"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

type AccountID string

type BillingManager interface{}

type impl struct{}

const TaskQueue = "billing"

func NewBillingManager(tc client.Client, billing enginebilling.BillingEngine, ledgerState ledgerstate.LedgerStateAccess, repo func(id AccountID) (repolookup.RepoRef, bool)) BillingManager {
	return impl{}
}

func RegisterManagerWorker(w worker.Worker, m BillingManager) {}
`
	addWebStubs(files)
}

func addWebStubs(files map[string]string) {
	files["internal/client/web/web.go"] = `package web

import (
	"net/http"

	security "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

type DevConfig struct{}

type Registrar interface {
	Register(mux *http.ServeMux)
}

func NewServer(dev DevConfig, validator security.Validator, registrars ...Registrar) http.Handler {
	mux := http.NewServeMux()
	for _, r := range registrars {
		r.Register(mux)
	}
	return mux
}
`
	files["internal/client/web/order/order.go"] = webHandlerStub("order", "OrderManager")
	files["internal/client/web/fulfillment/fulfillment.go"] = webHandlerStub("fulfillment", "FulfillmentManager")
}

// webHandlerStub renders a generated-style web Handler stub for a manager.
func webHandlerStub(pkg, iface string) string {
	return `package ` + pkg + `

import (
	"net/http"

	mgr "` + sandboxModule + `/internal/manager/` + pkg + `"
	security "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

type Handler struct {
	Manager  mgr.` + iface + `
	Security security.Security
}

func (h *Handler) Register(mux *http.ServeMux) {}
`
}
