package scenariohost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/devstack"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// App is a launched composed app (devstack.App).
type App = devstack.App

// Defaults of the app's own appgen.json, as cmd/appgen reads it.
const (
	defaultEnvPrefix = "APP"
	defaultContainer = "server"
	// projectFile is the model's path under the app's module root.
	projectFile = ".aiarch/state/project.json"
	// defaultPrincipal is the dev principal an app launched with no scenario principal runs as.
	defaultPrincipal = "scenario"
	// namespaceRetention is how long the dev server keeps a launch namespace's closed workflows.
	namespaceRetention = 24 * time.Hour
	// namespaceVisible bounds the wait for a registered namespace to answer.
	namespaceVisible = 30 * time.Second
)

// LaunchDevstack launches the composed app at moduleDir ("" = the host's module root) through
// its generated run contract, `go run ./cmd/devstack`, into this host's substrates: its model
// (<moduleDir>/.aiarch/state/project.json) and appgen.json name the infrastructure and the env
// prefix, the host answers each local input (Env), and the launcher provisions none of them.
// Isolation is per launch: a Postgres database (PostgresDatabase) and a Temporal namespace
// (TemporalNamespaceFor) of the launch's own, so two apps launched into one host share neither
// rows nor task queues. The app runs as principals[0] ("scenario" with none); the host's Stop is
// registered on t and a Stop that leaves anything behind fails the test.
func (h *Host) LaunchDevstack(t *testing.T, moduleDir string, principals ...Principal) *App {
	t.Helper()
	if moduleDir == "" {
		moduleDir = moduleRoot()
	}
	env := h.launchEnv(t, moduleDir, principals)
	app, err := devstack.Launch(t.Context(), moduleDir, env, nil)
	if err != nil {
		t.Fatalf("scenariohost: LaunchDevstack: %v", err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("scenariohost: devstack output for %s:\n%s", app.BaseURL, app.Log())
		}
		if err := app.Stop(); err != nil {
			t.Fatalf("scenariohost: stop devstack %s: %v", app.BaseURL, err)
		}
	})
	return app
}

// launchEnv is the environment one launch of the app at moduleDir runs with: the host's answer
// to every local input of the app's deployment (a database and a namespace of this launch's own
// in place of the host's shared ones) and the dev principal.
func (h *Host) launchEnv(t *testing.T, moduleDir string, principals []Principal) []string {
	t.Helper()
	m, err := projectmodel.LoadFile(filepath.Join(moduleDir, filepath.FromSlash(projectFile)))
	if err != nil {
		t.Fatalf("scenariohost: LaunchDevstack: load the model: %v", err)
	}
	cfg, err := readAppgen(moduleDir)
	if err != nil {
		t.Fatalf("scenariohost: LaunchDevstack: %v", err)
	}
	inputs := &launchInputs{h: h, t: t}
	env := h.env(cfg.EnvPrefix, m.EffectiveDeployment(cfg.Container), inputs.value)
	subject := defaultPrincipal
	if len(principals) > 0 {
		subject = principals[0].Subject
	}
	return append(env, configgen.DevPrincipalEnv(cfg.EnvPrefix)+"="+subject)
}

// launchInputs answers the inputs of one launch: the host's Input, except that the Postgres URL
// names a database created for the launch and the Temporal namespace is one registered for it.
// Each is made on first ask, so a deployment with several Postgres declarations shares one
// database, as the launcher's single provisioned server would.
type launchInputs struct {
	h                   *Host
	t                   *testing.T
	database, namespace string
}

func (l *launchInputs) value(decl projectmodel.InfraDecl, input string) string {
	switch {
	case decl.Substrate == projectmodel.SubstratePostgres && input == "URL":
		if l.database == "" {
			l.database = l.h.PostgresDatabase(l.t)
		}
		return l.database
	case decl.Substrate == projectmodel.SubstrateTemporal && input == "NAMESPACE":
		if l.namespace == "" {
			l.namespace = l.h.TemporalNamespaceFor(l.t)
		}
		return l.namespace
	}
	return l.h.Input(l.t, decl.Key, decl.Substrate, input)
}

// appgenConfig is the part of the app's appgen.json the launch reads.
type appgenConfig struct {
	EnvPrefix string `json:"envPrefix"`
	Container string `json:"container"`
}

// readAppgen reads <moduleDir>/appgen.json; an absent file or field takes cmd/appgen's default.
func readAppgen(moduleDir string) (appgenConfig, error) {
	cfg := appgenConfig{EnvPrefix: defaultEnvPrefix, Container: defaultContainer}
	raw, err := os.ReadFile(filepath.Join(moduleDir, "appgen.json")) // #nosec G304 -- the app under test
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	var read appgenConfig
	if err := json.Unmarshal(raw, &read); err != nil {
		return cfg, fmt.Errorf("appgen.json: %w", err)
	}
	if read.EnvPrefix != "" {
		cfg.EnvPrefix = read.EnvPrefix
	}
	if read.Container != "" {
		cfg.Container = read.Container
	}
	return cfg, nil
}

// TemporalNamespaceFor registers a Temporal namespace of its own on the host's dev server and
// returns it once the server answers for it. Two apps in one namespace would poll the same task
// queues and run each other's workflows; one namespace per launch keeps them apart. The dev
// server keeps the namespace until the process ends, and its database persists across runs, so
// the name carries the host's boot time as well as a counter.
func (h *Host) TemporalNamespaceFor(t *testing.T) string {
	t.Helper()
	if h.temporal == nil {
		t.Fatal("scenariohost: TemporalNamespaceFor: the host has no Temporal dev server")
	}
	h.launchMu.Lock()
	h.launches++
	name := fmt.Sprintf("devstack-%d-%d", h.booted.Unix(), h.launches)
	h.launchMu.Unlock()

	ctx := t.Context()
	svc := h.temporal.WorkflowService()
	if _, err := svc.RegisterNamespace(ctx, &workflowservice.RegisterNamespaceRequest{
		Namespace:                        name,
		WorkflowExecutionRetentionPeriod: durationpb.New(namespaceRetention),
	}); err != nil {
		t.Fatalf("scenariohost: register namespace %q: %v", name, err)
	}
	for deadline := time.Now().Add(namespaceVisible); ; {
		_, err := svc.DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{Namespace: name})
		if err == nil {
			return name
		}
		if time.Now().After(deadline) {
			t.Fatalf("scenariohost: namespace %q was registered but never answered: %v", name, err)
		}
		time.Sleep(settleGap)
	}
}
