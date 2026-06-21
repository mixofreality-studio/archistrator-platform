// Package llm is the sanctioned LLM-worker infrastructure toolkit for systems
// built with aiarch. It carries generic, reusable low-level clients for the
// supported LLM Worker Providers — with faults already mapped onto the framework
// error model — so each app's WorkerAccess builds its prompt assembly, usage
// shaping and idempotency on top without re-implementing the transport:
//
//   - *Client (llm.go): the Anthropic-hosted Ollama-compatible HTTP transport —
//     blocking generate + model pull — used for local/test Worker Providers.
//   - *AnthropicClient (anthropic.go): the Anthropic Messages-API transport
//     (official anthropic-sdk-go), the PRODUCTION Worker Provider.
//
// The companion testinfra subpackage spins a throwaway Ollama testcontainer (with
// a pulled model) for integration and system tests — Ollama is the test-only
// provider; production uses Anthropic.
//
// An LLM Worker Provider (Anthropic in production, Ollama for tests) is one of the
// FIXED infrastructure options an aiarch-built app may use (see the Worker /
// CustomerAppInfrastructure volatilities in the Method design and the dependency
// allowlist enforced by framework-go/arch — the anthropic-sdk-go dependency lives
// here, inside the sanctioned infrastructure module, never in app code).
//
// Infrastructure-opacity is the caller's job: the model name, prompt assembly,
// temperature/seed and usage metering belong to the consuming WorkerAccess Store
// and never cross its contract surface. This client only moves bytes to and from
// the provider.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// pullTimeout bounds a model pull. The first pull of a model is a slow,
// multi-hundred-MB network download on a cold cache.
const pullTimeout = 15 * time.Minute

// GenerateOptions are the provider-neutral knobs Generate forwards. Determinism-
// leaning defaults (temperature 0, a fixed seed) are the caller's choice, not
// this client's.
type GenerateOptions struct {
	Temperature float64 `json:"temperature"`
	Seed        int     `json:"seed"`
	NumPredict  int     `json:"num_predict"`
}

// GenerateRequest is one blocking generation call. Stream is forwarded as given;
// a WorkerAccess Store collapsing provider streaming to a single collected
// result sets it false.
//
// Format, when set to "json", instructs the Ollama provider to constrain its
// output to valid JSON. Set by workerAccess.Generate (the generic typed surface)
// so that responses can be reliably unmarshalled. Leave empty for the
// unstructured (plain-text) Dispatch path.
type GenerateRequest struct {
	Model   string          `json:"model"`
	Prompt  string          `json:"prompt"`
	Stream  bool            `json:"stream"`
	Format  string          `json:"format,omitempty"`
	Options GenerateOptions `json:"options"`
}

// GenerateResponse is the collected provider result plus its raw consumption
// counters. The counters are infrastructure-opaque; a Store derives its own
// normalized usage scalar and forwards the raw fields as an opaque billing blob.
type GenerateResponse struct {
	Response           string `json:"response"`
	Done               bool   `json:"done"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	EvalCount          int    `json:"eval_count"`
	TotalDuration      int64  `json:"total_duration"`
	LoadDuration       int64  `json:"load_duration"`
	PromptEvalDuration int64  `json:"prompt_eval_duration"`
	EvalDuration       int64  `json:"eval_duration"`
}

// Client is a low-level Ollama HTTP client. It is safe for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a client against an Ollama HTTP endpoint. timeout bounds each
// generate call (a Worker run is slow; the caller's durable-execution Activity
// owns the real deadline). A trailing slash on baseURL is trimmed.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
	}
}

// Generate performs the blocking generate-and-collect call and maps transport/
// HTTP faults onto the framework error model. A successful HTTP response is
// collected into a single GenerateResponse.
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return GenerateResponse{}, fwra.Wrap(fwra.ContractMisuse, err, "llm.Generate: marshal request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return GenerateResponse{}, fwra.Wrap(fwra.Infrastructure, err, "llm.Generate: build request")
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		// Network blip / unreachable endpoint / timeout → retryable Transient.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return GenerateResponse{}, fwra.Wrap(fwra.Transient, err, "llm.Generate: timed out / cancelled")
		}
		return GenerateResponse{}, fwra.Wrap(fwra.Transient, err, "llm.Generate: provider unreachable")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return GenerateResponse{}, mapHTTPError(resp)
	}

	var out GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return GenerateResponse{}, fwra.Wrap(fwra.Infrastructure, err, "llm.Generate: decode response")
	}
	return out, nil
}

// Pull asks the Ollama daemon to pull the named model and blocks until the pull
// stream completes. /api/pull streams newline-delimited JSON status objects; the
// stream ends when the model is fully downloaded and ready. A long internal
// timeout accommodates the multi-hundred-MB download on a cold cache.
func (c *Client) Pull(ctx context.Context, model string) error {
	pullCtx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()

	body, err := json.Marshal(map[string]any{"model": model, "stream": true})
	if err != nil {
		return fmt.Errorf("llm.Pull: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(pullCtx, http.MethodPost, c.baseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("llm.Pull: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("llm.Pull: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("llm.Pull: returned %d: %s", resp.StatusCode, string(out))
	}

	// Drain the streamed status objects to completion, watching for an explicit
	// error line.
	dec := json.NewDecoder(resp.Body)
	for {
		var status struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if decErr := dec.Decode(&status); decErr != nil {
			if decErr == io.EOF {
				break
			}
			return fmt.Errorf("llm.Pull: decode stream: %w", decErr)
		}
		if status.Error != "" {
			return fmt.Errorf("llm.Pull: %s", status.Error)
		}
	}
	return nil
}

// mapHTTPError maps a non-200 provider response onto the framework error model.
// The body is read for the detail string but not surfaced raw to callers.
func mapHTTPError(resp *http.Response) error {
	detail := fmt.Sprintf("provider returned HTTP %d", resp.StatusCode)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fwra.New(fwra.RateLimited, detail) // 429 → RateLimited (retryable, back off).
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fwra.New(fwra.Auth, detail) // auth → Auth (terminal).
	case resp.StatusCode == http.StatusPaymentRequired:
		return fwra.New(fwra.QuotaExhausted, detail) // quota/credit → QuotaExhausted (terminal).
	case resp.StatusCode == http.StatusNotFound:
		return fwra.New(fwra.NotFound, detail) // unknown model / endpoint missing.
	case resp.StatusCode >= 500:
		return fwra.New(fwra.Transient, detail) // 5xx → Transient (retryable).
	default:
		// Other 4xx: a provider fault not classifiable above.
		return fwra.New(fwra.Infrastructure, detail)
	}
}
