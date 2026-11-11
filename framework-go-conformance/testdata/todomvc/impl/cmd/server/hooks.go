package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	web "example.com/conformance/todomvc/internal/client/web"
	"example.com/conformance/todomvc/internal/resourceaccess/todolist"
	security "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	tlog "go.temporal.io/sdk/log"
)

// main loads the configuration and runs the generated composition root.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := LoadConfig()
	if err != nil {
		logger.Error("server exited with error", "err", err)
		os.Exit(1)
	}
	if err := RunGenerated(cfg, appHooks{}, logger); err != nil {
		logger.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

// appHooks answers what the deployment model cannot express.
type appHooks struct{}

func (appHooks) ResolveProfile(cfg *Config) string {
	return "local"
}

func (appHooks) TemporalLogger(logger *slog.Logger) tlog.Logger {
	return tlog.NewStructuredLogger(logger)
}

func (appHooks) TokenValidator(ctx context.Context, cfg *Config) (security.Validator, error) {
	return nil, nil
}

func (h appHooks) DevConfig(cfg *Config) web.DevConfig {
	subject, err := cfg.LocalDevPrincipal(h.ResolveProfile(cfg))
	if err != nil || subject == "" {
		return web.DevConfig{}
	}
	return web.DevConfig{Enabled: true, Principal: security.Principal{Subject: subject}}
}

func (appHooks) WrapManagers(managers WebManagers) WebManagers {
	return managers
}

func (appHooks) ExtraMounts(root *http.ServeMux, cfg *Config, dev web.DevConfig, validator security.Validator, sec security.Security, managers WebManagers) {
	// No composition-root routes yet.
}

// MCPMount is a seam the platform added after the harvest (the generated /mcp mount); the
// harvest's composition root had no MCP mount, so the app answers it with the authenticated
// handler unchanged.
func (appHooks) MCPMount(server *mcp.Server, dev web.DevConfig, authed http.Handler) http.Handler {
	return authed
}

func (appHooks) FinalizeTodoListAccess(cfg *Config, v todolist.TodoListAccess) todolist.TodoListAccess {
	return v
}
