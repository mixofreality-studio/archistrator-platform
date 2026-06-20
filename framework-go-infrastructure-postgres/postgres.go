// Package postgres is the sanctioned Postgres infrastructure toolkit for systems
// built with aiarch. It carries the generic, reusable production helpers every
// aiarch app's Postgres-backed ResourceAccess needs — a connectivity-verified
// pool constructor and the SQLSTATE→framework error mapping — so each component
// does not re-implement them. The companion testinfra subpackage spins a
// throwaway Postgres testcontainer for integration tests.
//
// Postgres is one of the FIXED infrastructure options an aiarch-built app may
// use (see the CustomerAppInfrastructure volatility in the Method design and the
// dependency allowlist enforced by framework-go/arch). The pgx driver reachable
// through this module is part of that sanctioned surface.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// UniqueViolationCode is the SQLSTATE Postgres raises for unique_violation. A
// ResourceAccess Store classifies it as fwra.Conflict (a concurrent create lost)
// rather than a generic infrastructure fault.
const UniqueViolationCode = "23505"

// pingTimeout bounds the connectivity check NewPool performs so a dead endpoint
// fails fast instead of hanging the caller.
const pingTimeout = 10 * time.Second

// NewPool opens a pgx pool against connStr and verifies connectivity with a
// bounded ping before handing it back. A failed dial/ping closes the pool and
// returns the raw driver error (the caller, typically a Store constructor, wraps
// it into the framework error model with its own operation context).
func NewPool(ctx context.Context, connStr string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}

// MapError maps a residual Postgres/driver error to the shared framework error
// model. Conflict and ContractMisuse are decided at the call sites where the
// meaning is known (a version-guard loss, a bad argument); this helper covers
// the rest:
//
//   - no rows on a read           -> fwra.NotFound
//   - transient pgconn conditions -> fwra.Transient
//   - everything else (DB fault)  -> fwra.Infrastructure
//
// op is the caller's operation label (e.g. "projectstate.ReadProject") prefixed
// onto the wrapped error for diagnosis.
func MapError(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fwra.Wrap(fwra.NotFound, err, op+": not found")
	}
	if IsTransient(err) {
		return fwra.Wrap(fwra.Transient, err, op+": transient infrastructure error")
	}
	return fwra.Wrap(fwra.Infrastructure, err, op+": infrastructure error")
}

// IsTransient classifies a Postgres failure as a retryable transient condition
// (connection loss, admin shutdown, too-many-connections, serialization/
// deadlock) versus a hard infrastructure fault. It inspects the SQLSTATE class
// rather than error text so the classification is stable across driver versions.
func IsTransient(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "53300", // too_many_connections
			"53400", // configuration_limit_exceeded
			"57P01", // admin_shutdown
			"57P02", // crash_shutdown
			"57P03", // cannot_connect_now
			"40001", // serialization_failure
			"40P01": // deadlock_detected
			return true
		}
		// SQLSTATE class 08 == connection exception (all transient).
		if len(pgErr.Code) >= 2 && pgErr.Code[:2] == "08" {
			return true
		}
		return false
	}
	// A non-PgError (raw driver/network error before a SQLSTATE is assigned) is
	// not classified as transient: without a SQLSTATE we cannot tell a retryable
	// blip from a programmer/lifecycle fault, so it falls through to the
	// conservative fwra.Infrastructure (escalate) bucket.
	return false
}
