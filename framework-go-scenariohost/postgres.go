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
func (h *Host) PostgresDatabase(t *testing.T) string {
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
