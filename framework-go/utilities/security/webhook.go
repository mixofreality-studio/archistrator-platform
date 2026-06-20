package security

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// WebhookChannel is a logical channel id (e.g. "merchant-gateway") that selects
// the signing secret internally. The signature scheme is hidden behind it.
type WebhookChannel string

// SignatureMaterial is the presented signature material for a webhook, opaque to
// the surface — the verification scheme lives inside the [WebhookVerifier].
type SignatureMaterial struct {
	values map[string]string // presented signature headers, opaque
}

// NewSignatureMaterial builds [SignatureMaterial] from the presented signature
// headers (copied so the caller cannot mutate it afterward).
func NewSignatureMaterial(values map[string]string) SignatureMaterial {
	cp := make(map[string]string, len(values))
	for k, v := range values {
		cp[k] = v
	}
	return SignatureMaterial{values: cp}
}

func (s SignatureMaterial) present(header string) (string, bool) {
	v, ok := s.values[header]
	return v, ok
}

// WebhookVerifier is the signature seam: it verifies a presented signature over
// the EXACT raw body for a channel. The default implementation computes an HMAC
// over the body using the channel's shared secret and compares in constant time;
// a different scheme (Ed25519, RSA-PSS) or secret source is supplied via
// [WithWebhookVerifier] without changing [Security.VerifyWebhookSignature].
//
// It returns nil iff the signature is valid; [ErrSignatureMismatch] when the
// secret was available but the signature did not match; any other error when the
// secret could not be fetched.
type WebhookVerifier interface {
	Verify(ctx context.Context, channel WebhookChannel, rawBody []byte, presented SignatureMaterial) error
}

// ErrSignatureMismatch is the sentinel a [WebhookVerifier] returns when the
// secret was available but the presented signature did not match.
// [Security.VerifyWebhookSignature] maps it to [ErrSignatureInvalid] (terminal);
// any other verifier error maps to [ErrSigningKeyUnavailable] (transient).
var ErrSignatureMismatch = errors.New("signature mismatch")

// errSecretUnavailable means the channel's signing secret could not be fetched.
var errSecretUnavailable = errors.New("signing secret unavailable")

// signatureHeader is the presented-signature header key the default verifier
// reads. Unexported: the surface names no header and no scheme.
const signatureHeader = "signature"

// hmacWebhookVerifier computes HMAC-SHA256 over the raw body using the channel's
// shared secret and compares it to the presented signature in constant time.
type hmacWebhookVerifier struct {
	secrets map[WebhookChannel][]byte
}

// NewHMACWebhookVerifier builds the default HMAC-SHA256 [WebhookVerifier] over a
// fixed channel→secret map (loaded from env/secret-store by the caller). The
// secret bytes are copied and never cross the surface. A channel with no secret
// yields [ErrSigningKeyUnavailable] at verify time.
func NewHMACWebhookVerifier(secrets map[WebhookChannel][]byte) WebhookVerifier {
	cp := make(map[WebhookChannel][]byte, len(secrets))
	for k, v := range secrets {
		b := make([]byte, len(v))
		copy(b, v)
		cp[k] = b
	}
	return hmacWebhookVerifier{secrets: cp}
}

func (v hmacWebhookVerifier) Verify(_ context.Context, channel WebhookChannel, rawBody []byte, presented SignatureMaterial) error {
	secret, ok := v.secrets[channel]
	if !ok || len(secret) == 0 {
		return errSecretUnavailable // → ErrSigningKeyUnavailable
	}
	presentedHex, ok := presented.present(signatureHeader)
	if !ok || presentedHex == "" {
		return ErrSignatureMismatch // no signature presented → terminal reject
	}
	presentedBytes, decErr := hex.DecodeString(presentedHex)
	if decErr != nil {
		return ErrSignatureMismatch // unparsable signature → terminal reject
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(rawBody)
	expected := mac.Sum(nil)
	if !hmac.Equal(expected, presentedBytes) { // constant-time compare
		return ErrSignatureMismatch
	}
	return nil
}
