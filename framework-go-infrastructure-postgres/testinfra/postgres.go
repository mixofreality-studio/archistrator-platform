// Package testinfra provides a throwaway Postgres testcontainer bootstrap for
// the integration tests of any aiarch-built system. It is TEST-ONLY: nothing
// here is imported by production code, and StartPostgres is designed to be
// skipped under testing.Short() so `go test -short ./...` stays fast and
// container-free.
package testinfra

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	fwpg "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres"
)

// postgresImage pins the Postgres image used for integration tests. It matches
// the CNPG-class infrastructure aiarch's projectStateLog Resource targets in
// production. Pinning a tag keeps the test deterministic.
const postgresImage = "postgres:16-alpine"

// StartPostgres spins a throwaway Postgres container and returns a connected
// pgx pool against an empty database. Cleanup (pool close + container
// termination) is registered on t via t.Cleanup, so callers never manage the
// container lifecycle themselves.
//
// The test is skipped under `testing.Short()` so the container is never spun in
// the fast path. Callers that need schema applied should run their own
// migration against the returned pool; StartPostgres deliberately hands back a
// bare, empty database so each component owns its own DDL.
func StartPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("testinfra.StartPostgres: skipped under -short (requires Docker)")
	}

	ctx := context.Background()

	container, err := postgres.Run(ctx, postgresImage,
		postgres.WithDatabase("aiarch_test"),
		postgres.WithUsername("aiarch"),
		postgres.WithPassword("aiarch"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("testinfra.StartPostgres: start container: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort: the test is already over, so a termination error is
		// logged, not fatal.
		if termErr := testcontainers.TerminateContainer(container); termErr != nil {
			t.Logf("testinfra.StartPostgres: terminate container: %v", termErr)
		}
	})

	// sslmode=disable: the throwaway container has no TLS; production CNPG TLS
	// is wiring, never threaded through this test helper.
	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("testinfra.StartPostgres: connection string: %v", err)
	}

	pool, err := fwpg.NewPool(ctx, connStr)
	if err != nil {
		t.Fatalf("testinfra.StartPostgres: open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}
