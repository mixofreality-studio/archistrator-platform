package llm

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// makeAPIError builds an *anthropic.Error for a given HTTP status and provider
// error body. It populates the SDK error the same way the transport does — via
// UnmarshalJSON — so Type() and RawJSON() are set without dialling the API. The
// status is overlaid afterwards (the transport sets it from the HTTP response).
func makeAPIError(t *testing.T, status int, body string) *anthropic.Error {
	t.Helper()
	var e anthropic.Error
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("unmarshal provider error body: %v", err)
	}
	e.StatusCode = status
	return &e
}

// errBody renders the standard Anthropic error envelope {"type":"error",
// "error":{"type":..,"message":..}} a provider 4xx/5xx carries.
func errBody(typ, msg string) string {
	b, _ := json.Marshal(map[string]any{
		"type":  "error",
		"error": map[string]string{"type": typ, "message": msg},
	})
	return string(b)
}

// creditMsg is the real production billing message Anthropic returns as a 400
// invalid_request_error when the account is out of credits (the prod incident).
const creditMsg = "Your credit balance is too low to access the Anthropic API. " +
	"Please go to Plans & Billing to upgrade or purchase credits."

// TestMapAnthropicError_StatusMapping is the table covering the fault → Kind
// classification, asserting BOTH the Kind AND its Retryable flag. The mapping is
// PURELY status-code-driven (no message/body string-matching): a status carries
// a fixed terminal/retryable classification regardless of the provider message.
//
// The load-bearing case is the 400 (prod incident 2026-06-01): the out-of-credits
// fault arrives as a 400 invalid_request_error, and a 400 means "an issue with
// the format or content of your request" — a PERMANENT client error. So EVERY
// non-listed 4xx (incl. 400) maps to the terminal, non-retryable ContractMisuse:
// re-issuing the identical request can't help. This fails the generate Activity
// fast instead of retrying 3× and silently wedging the draft.
func TestMapAnthropicError_StatusMapping(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name          string
		status        int
		typ           string
		msg           string
		wantKind      fwra.Kind
		wantRetryable bool
	}{
		{
			name:          "400 credit balance too low → ContractMisuse (terminal)",
			status:        400,
			typ:           "invalid_request_error",
			msg:           creditMsg,
			wantKind:      fwra.ContractMisuse,
			wantRetryable: false,
		},
		{
			name:          "400 malformed request → ContractMisuse (terminal)",
			status:        400,
			typ:           "invalid_request_error",
			msg:           "max_tokens: must be greater than 0",
			wantKind:      fwra.ContractMisuse,
			wantRetryable: false,
		},
		{
			name:          "422 unprocessable (other 4xx) → ContractMisuse (terminal)",
			status:        422,
			typ:           "invalid_request_error",
			msg:           "unprocessable entity",
			wantKind:      fwra.ContractMisuse,
			wantRetryable: false,
		},
		{
			name:          "402 → QuotaExhausted (terminal)",
			status:        402,
			typ:           "invalid_request_error",
			msg:           "payment required",
			wantKind:      fwra.QuotaExhausted,
			wantRetryable: false,
		},
		{
			name:          "401 → Auth (terminal)",
			status:        401,
			typ:           "authentication_error",
			msg:           "invalid x-api-key",
			wantKind:      fwra.Auth,
			wantRetryable: false,
		},
		{
			name:          "403 → Auth (terminal)",
			status:        403,
			typ:           "permission_error",
			msg:           "not allowed",
			wantKind:      fwra.Auth,
			wantRetryable: false,
		},
		{
			name:          "429 → RateLimited (retryable)",
			status:        429,
			typ:           "rate_limit_error",
			msg:           "slow down",
			wantKind:      fwra.RateLimited,
			wantRetryable: true,
		},
		{
			name:          "404 → NotFound (terminal)",
			status:        404,
			typ:           "not_found_error",
			msg:           "model not found",
			wantKind:      fwra.NotFound,
			wantRetryable: false,
		},
		{
			name:          "500 → Transient (retryable)",
			status:        500,
			typ:           "api_error",
			msg:           "internal",
			wantKind:      fwra.Transient,
			wantRetryable: true,
		},
		{
			name:          "529 overloaded → Transient (retryable)",
			status:        529,
			typ:           "overloaded_error",
			msg:           "overloaded",
			wantKind:      fwra.Transient,
			wantRetryable: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			apiErr := makeAPIError(t, c.status, errBody(c.typ, c.msg))
			got := mapAnthropicError(ctx, apiErr)

			var rae *fwra.Error
			if !errors.As(got, &rae) {
				t.Fatalf("expected *fwra.Error, got %T (%v)", got, got)
			}
			if rae.Kind != c.wantKind {
				t.Fatalf("status %d type %q: got kind %v, want %v", c.status, c.typ, rae.Kind, c.wantKind)
			}
			if rae.Retryable != c.wantRetryable {
				t.Fatalf("status %d: got Retryable=%v, want %v", c.status, rae.Retryable, c.wantRetryable)
			}
		})
	}
}

// TestMapAnthropicError_NonAPIError_Transient — a transport-level failure with no
// HTTP status (network blip / timeout) is a retryable Transient fault.
func TestMapAnthropicError_NonAPIError_Transient(t *testing.T) {
	ctx := context.Background()
	got := mapAnthropicError(ctx, errors.New("dial tcp: connection refused"))
	var rae *fwra.Error
	if !errors.As(got, &rae) || rae.Kind != fwra.Transient {
		t.Fatalf("non-API error must map to Transient, got %v", got)
	}
	if !rae.Retryable {
		t.Fatalf("transport Transient must be retryable, got Retryable=%v", rae.Retryable)
	}
}

// TestMapAnthropicError_ContextCancelled_Transient — a cancelled/timed-out
// context maps to a retryable Transient fault.
func TestMapAnthropicError_ContextCancelled_Transient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := mapAnthropicError(ctx, context.Canceled)
	var rae *fwra.Error
	if !errors.As(got, &rae) || rae.Kind != fwra.Transient {
		t.Fatalf("cancelled context must map to Transient, got %v", got)
	}
	if !rae.Retryable {
		t.Fatalf("context Transient must be retryable, got Retryable=%v", rae.Retryable)
	}
}
