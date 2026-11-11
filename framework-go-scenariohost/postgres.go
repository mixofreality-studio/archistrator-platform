package scenariohost

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// pgTimeout bounds one administrative statement against the host's Postgres.
const pgTimeout = 30 * time.Second

// PostgresDatabase creates a database of its own on the host's Postgres server, "devstack_<n>",
// and returns the host's URL with that database; the database is dropped (with any connection
// still open to it) when t ends. A launched app's migrations and data land there, so two apps
// launched into one host see none of each other's rows. The test is skipped where Postgres is
// not booted (-short), as Input is.
func (h *Host) PostgresDatabase(t *testing.T) string { return h.postgresDatabase(t) }

// postgresDatabase is PostgresDatabase over the part of a test a launch uses (launchT).
func (h *Host) postgresDatabase(t launchT) string {
	t.Helper()
	if h.PostgresURL == "" {
		t.Skip("scenariohost: postgres is not booted under -short")
	}
	h.launchMu.Lock()
	name := fmt.Sprintf("devstack_%d", len(h.databases)+1)
	h.databases = append(h.databases, name)
	h.launchMu.Unlock()

	if err := h.adminExec(fmt.Sprintf("CREATE DATABASE %s TEMPLATE template0", pgx.Identifier{name}.Sanitize())); err != nil {
		t.Fatalf("scenariohost: create database %q: %v", name, err)
	}
	t.Cleanup(func() {
		if err := h.adminExec(fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", pgx.Identifier{name}.Sanitize())); err != nil {
			t.Errorf("scenariohost: drop database %q: %v", name, err)
		}
	})
	u, err := url.Parse(h.PostgresURL)
	if err != nil {
		t.Fatalf("scenariohost: parse the postgres URL %q: %v", h.PostgresURL, err)
	}
	u.Path = "/" + name
	return u.String()
}

// LaunchDatabases is the names of the databases PostgresDatabase has created on this host, in
// creation order, dropped or not.
func (h *Host) LaunchDatabases() []string {
	h.launchMu.Lock()
	defer h.launchMu.Unlock()
	return append([]string(nil), h.databases...)
}

// PostgresDSN is the Postgres URL the components a scenario builds in-process connect to: the
// database of the app the test launched (LaunchDevstack) once it has, the host's own URL before.
// A machine client's scenario drives the launched app over the wire and arranges and probes other
// components in-process; both must see one set of rows, so they share the launch's database. With
// several launches the latest one is the answer. "" under -short, as PostgresURL.
func (h *Host) PostgresDSN() string {
	h.launchMu.Lock()
	defer h.launchMu.Unlock()
	if h.appDatabase != "" {
		return h.appDatabase
	}
	return h.PostgresURL
}

// followLaunch makes dsn the database in-process components connect to (PostgresDSN) until t ends.
// The host is shared by every test of the package, so the previous answer comes back with the
// launch's database dropped: a later test never reads one that is gone.
func (h *Host) followLaunch(t launchT, dsn string) {
	h.launchMu.Lock()
	prev := h.appDatabase
	h.appDatabase = dsn
	h.launchMu.Unlock()
	t.Cleanup(func() {
		h.launchMu.Lock()
		h.appDatabase = prev
		h.launchMu.Unlock()
	})
}

// adminExec runs one statement against the host's server (its maintenance database).
func (h *Host) adminExec(stmt string) error {
	ctx, cancel := context.WithTimeout(context.Background(), pgTimeout)
	defer cancel()
	conn, err := pgx.Connect(ctx, h.PostgresURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, stmt)
	return err
}
