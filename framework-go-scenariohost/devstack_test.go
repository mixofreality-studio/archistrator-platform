package scenariohost

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"go.temporal.io/api/workflowservice/v1"
)

// fakeApp is a module shaped like a built app as far as LaunchDevstack reads it: a project model
// with a Manager on Temporal and a ResourceAccess on Postgres, an appgen.json naming the env
// prefix, and a launcher that echoes the FAKE_* variables it was given and then reports ready.
func fakeApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":      "module example.com/fakeapp\n\ngo 1.25\n",
		"appgen.json": `{"envPrefix":"FAKE"}`,
		filepath.Join(".aiarch", "state", "project.json"): `{"serviceContracts":{
	"listManager":{"component":"listManager","layer":"Manager","goPackage":"internal/manager/list",
	  "deps":[{"name":"client","goType":"client.Client","goImport":"go.temporal.io/sdk/client"},{"name":"todos","component":"todoAccess"}],
	  "$defs":{"T":{"type":"string"}},"interface":{"name":"ListManager","layer":"manager","operations":[{"name":"Ping","params":[],"error":false}]}},
	"todoAccess":{"component":"todoAccess","layer":"ResourceAccess","goPackage":"internal/resourceaccess/todo","infra":["Postgres"],"$defs":{"T":{"type":"string"}},
	  "interface":{"name":"TodoAccess","layer":"resourceaccess","operations":[{"name":"Ping","params":[],"error":false}]}}}}`,
		filepath.Join("cmd", "devstack", "main.go"): `package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

func main() {
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "FAKE_") {
			fmt.Fprintln(os.Stderr, "env:"+kv)
		}
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	go func() { _ = http.Serve(l, mux) }()
	line, _ := json.Marshal(map[string]string{"devstack": "ready", "url": "http://" + l.Addr().String()})
	fmt.Println(string(line))
	select {}
}
`,
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// envOf is the value of name among the FAKE_* lines the fake launcher echoed.
func envOf(log, name string) string {
	for line := range strings.SplitSeq(log, "\n") {
		if v, ok := strings.CutPrefix(line, "env:"+name+"="); ok {
			return v
		}
	}
	return ""
}

func databaseOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return strings.TrimPrefix(u.Path, "/")
}

func databaseExists(t *testing.T, h *Host, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, h.PostgresURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_database WHERE datname = $1", name).Scan(&n); err != nil {
		t.Fatalf("pg_database: %v", err)
	}
	return n == 1
}

// PostgresDatabase hands out a database of its own per call and drops it when the test ends.
func TestPostgresDatabase_DistinctAndDropped(t *testing.T) {
	h := Start(t)
	if h.PostgresURL == "" {
		t.Skip("postgres is not booted under -short")
	}
	var a, b string
	t.Run("launches", func(t *testing.T) {
		a, b = h.PostgresDatabase(t), h.PostgresDatabase(t)
		da, db := databaseOf(t, a), databaseOf(t, b)
		if da == db || !strings.HasPrefix(da, "devstack_") || !strings.HasPrefix(db, "devstack_") {
			t.Fatalf("databases %q and %q, want two distinct devstack_<n>", da, db)
		}
		for _, name := range []string{da, db} {
			if !databaseExists(t, h, name) {
				t.Errorf("database %q was not created", name)
			}
		}
		if got := h.LaunchDatabases(); len(got) < 2 || got[len(got)-2] != da || got[len(got)-1] != db {
			t.Errorf("LaunchDatabases = %v, want it to end with %q, %q", got, da, db)
		}
	})
	for _, raw := range []string{a, b} {
		if databaseExists(t, h, databaseOf(t, raw)) {
			t.Errorf("database %q outlived the test that asked for it", databaseOf(t, raw))
		}
	}
}

// TemporalNamespaceFor registers a namespace of its own per call, one the dev server answers for.
func TestTemporalNamespaceFor_Registered(t *testing.T) {
	h := Start(t)
	a, b := h.TemporalNamespaceFor(t), h.TemporalNamespaceFor(t)
	if a == b || a == h.TemporalNamespace || b == h.TemporalNamespace {
		t.Fatalf("namespaces %q and %q must differ from each other and from the host's %q", a, b, h.TemporalNamespace)
	}
	for _, ns := range []string{a, b} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := h.temporal.WorkflowService().DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{Namespace: ns})
		cancel()
		if err != nil {
			t.Errorf("namespace %q is not registered: %v", ns, err)
		}
	}
}

// LaunchDevstack reads the app's model and appgen.json, and launches its run contract with the
// host's substrates: Temporal's address, a namespace and a database of this launch's own, the
// host's git repositories, and the principal the first scenario principal names.
func TestLaunchDevstack_HandsTheLauncherTheHostsSubstrates(t *testing.T) {
	h := Start(t)
	dir := fakeApp(t)
	app := h.LaunchDevstack(t, dir, Principal{Subject: "agent-1", Roles: []string{"agent"}, OnBehalfOf: "alice", Intents: []string{"read"}})
	log := app.Log()
	if got := envOf(log, "FAKE_TEMPORAL_HOSTPORT"); got != h.TemporalHostPort {
		t.Errorf("FAKE_TEMPORAL_HOSTPORT = %q, want the host's %q\n%s", got, h.TemporalHostPort, log)
	}
	ns := envOf(log, "FAKE_TEMPORAL_NAMESPACE")
	if ns == "" || ns == h.TemporalNamespace {
		t.Errorf("FAKE_TEMPORAL_NAMESPACE = %q, want a namespace of this launch's own (not %q)", ns, h.TemporalNamespace)
	}
	pg := envOf(log, "FAKE_POSTGRES_URL")
	if pg == "" || pg == h.PostgresURL || !strings.HasPrefix(databaseOf(t, pg), "devstack_") {
		t.Errorf("FAKE_POSTGRES_URL = %q, want the host's server with a devstack_<n> database", pg)
	}
	if got := envOf(log, "FAKE_DEV_PRINCIPAL"); got != "agent-1" {
		t.Errorf("FAKE_DEV_PRINCIPAL = %q, want the first principal's subject", got)
	}
	if err := app.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

// With no principal the launched app runs as the "scenario" principal.
func TestLaunchDevstack_DefaultPrincipalIsScenario(t *testing.T) {
	h := Start(t)
	app := h.LaunchDevstack(t, fakeApp(t))
	if got := envOf(app.Log(), "FAKE_DEV_PRINCIPAL"); got != "scenario" {
		t.Errorf("FAKE_DEV_PRINCIPAL = %q, want scenario", got)
	}
}
