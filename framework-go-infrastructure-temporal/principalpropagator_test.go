package temporal_test

import (
	"context"
	"testing"

	temporalprop "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-temporal"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

// fakeHeader is an in-memory workflow.HeaderWriter + workflow.HeaderReader for
// exercising the propagator's context.Context path without a Temporal server.
type fakeHeader struct{ m map[string]*commonpb.Payload }

func newFakeHeader() *fakeHeader { return &fakeHeader{m: map[string]*commonpb.Payload{}} }

func (h *fakeHeader) Set(k string, p *commonpb.Payload) { h.m[k] = p }

func (h *fakeHeader) Get(k string) (*commonpb.Payload, bool) {
	p, ok := h.m[k]
	return p, ok
}

func (h *fakeHeader) ForEachKey(fn func(string, *commonpb.Payload) error) error {
	for k, v := range h.m {
		if err := fn(k, v); err != nil {
			return err
		}
	}
	return nil
}

func TestInjectExtractRoundTrip(t *testing.T) {
	prop := temporalprop.NewPrincipalPropagator()
	want := security.SecurityPrincipal{
		Kind:          security.PrincipalUser,
		Subject:       "user-123",
		Username:      "amira",
		Email:         "amira@example.com",
		Issuer:        "https://keycloak.example.com/realms/aiarch",
		Roles:         []string{"drive-phase"},
		Organizations: []security.Organization{{ID: "org-1", Name: "Acme"}},
	}

	hdr := newFakeHeader()
	if err := prop.Inject(security.WithPrincipal(context.Background(), want), hdr); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if _, ok := hdr.Get("aiarch-security-principal"); !ok {
		t.Fatal("principal not written to header")
	}

	ctx, err := prop.Extract(context.Background(), hdr)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got, ok := security.PrincipalFrom(ctx)
	if !ok {
		t.Fatal("principal not extracted into context")
	}
	if got.Subject != want.Subject || got.Kind != want.Kind || got.Issuer != want.Issuer ||
		!got.HasRole("drive-phase") || !got.IsMemberOf("org-1") {
		t.Fatalf("round-trip mismatch: got %+v", got)
	}
}

func TestInjectNoPrincipalWritesNothing(t *testing.T) {
	prop := temporalprop.NewPrincipalPropagator()
	hdr := newFakeHeader()
	if err := prop.Inject(context.Background(), hdr); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if _, ok := hdr.Get("aiarch-security-principal"); ok {
		t.Fatal("header written when no principal present")
	}
}

func TestExtractEmptyHeaderLeavesContextClean(t *testing.T) {
	prop := temporalprop.NewPrincipalPropagator()
	ctx, err := prop.Extract(context.Background(), newFakeHeader())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if _, ok := security.PrincipalFrom(ctx); ok {
		t.Fatal("principal present from empty header")
	}
}

func TestExtractGarbagePayloadDegradesGracefully(t *testing.T) {
	prop := temporalprop.NewPrincipalPropagator()
	// A payload that decodes to a string, which cannot unmarshal into the
	// principal struct — simulates a schema-incompatible / pre-propagator header.
	bad, err := converter.GetDefaultDataConverter().ToPayload("not-a-principal")
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	hdr := newFakeHeader()
	hdr.Set("aiarch-security-principal", bad)

	ctx, err := prop.Extract(context.Background(), hdr)
	if err != nil {
		t.Fatalf("Extract must not error on bad payload: %v", err)
	}
	if _, ok := security.PrincipalFrom(ctx); ok {
		t.Fatal("garbage payload should not yield a principal")
	}
}
