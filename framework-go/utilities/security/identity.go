package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// ServiceAudience names the downstream a service credential is for.
type ServiceAudience string

// ServiceCredential is an opaque, short-lived, attachable credential plus the
// service [Principal]. The credential value is never parsed by callers;
// the [ServiceIdentitySource] mints/caches/refreshes it internally near ExpiresAt.
type ServiceCredential struct {
	opaque    string    // attachable credential (today: a short-lived bearer); never parsed by callers
	Principal Principal // the service principal, usable in Authorize
	ExpiresAt time.Time // callers re-request near expiry
}

// AttachableValue returns the opaque credential value to attach to an outbound
// call. Callers attach it verbatim and never inspect or parse it.
func (c ServiceCredential) AttachableValue() string { return c.opaque }

// ServiceIdentitySource is the service-identity seam: it mints the platform's
// own short-lived credential for an audience. The default implementation mints an
// in-process credential; a workload-identity source (client-credentials grant,
// SPIFFE/SVID) is supplied via [WithServiceIdentitySource] without changing
// [Security.ObtainServiceIdentity]. A non-nil error means the source was
// unreachable — [Security.ObtainServiceIdentity] surfaces [ErrIdentityUnavailable].
type ServiceIdentitySource interface {
	Mint(ctx context.Context, audience ServiceAudience) (ServiceCredential, error)
}

// Clock abstracts time so credential-expiry behaviour is deterministically
// testable. Production passes nil to use the system clock.
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// inProcessIdentitySource mints a short-lived in-process credential. It is a
// real, deterministic implementation (not a stub) so callers can exercise the
// credential lifecycle; production replaces it with a workload-identity source.
type inProcessIdentitySource struct {
	clk Clock
	ttl time.Duration
}

// NewInProcessIdentitySource builds the default [ServiceIdentitySource] minting
// credentials with the given time-to-live. A nil clk uses the system clock.
func NewInProcessIdentitySource(clk Clock, ttl time.Duration) ServiceIdentitySource {
	if clk == nil {
		clk = systemClock{}
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return inProcessIdentitySource{clk: clk, ttl: ttl}
}

func (d inProcessIdentitySource) Mint(_ context.Context, audience ServiceAudience) (ServiceCredential, error) {
	if audience == "" {
		return ServiceCredential{}, errors.New("audience required")
	}
	now := d.clk.Now()
	// Opaque, short-lived credential value. Callers attach it verbatim and never
	// parse it; the internal shape is not part of any contract.
	raw := sha256.Sum256([]byte(string(audience) + "@" + now.UTC().Format(time.RFC3339Nano)))
	return ServiceCredential{
		opaque: hex.EncodeToString(raw[:]),
		Principal: Principal{
			Subject: "aiarch-service",
			Kind:    PrincipalApplication,
		},
		ExpiresAt: now.Add(d.ttl),
	}, nil
}
