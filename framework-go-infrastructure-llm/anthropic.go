package llm

import (
	"context"
	"errors"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// AnthropicGenerateRequest is one blocking Messages-API generation call. Model is
// the concrete Claude model id the caller's WorkerAccess resolved from its logical
// WorkerClass; System is an optional provider-mechanical instruction (e.g. "emit
// JSON only"); Prompt is the fully-assembled caller prompt forwarded verbatim.
type AnthropicGenerateRequest struct {
	Model     string
	System    string
	Prompt    string
	MaxTokens int
}

// AnthropicGenerateResponse is the collected text plus the raw provider token
// counters. The counters are infrastructure-opaque; a consuming WorkerAccess Store
// derives its own normalized usage scalar and forwards the raw fields as an opaque
// billing blob — exactly as it does for the Ollama counters.
type AnthropicGenerateResponse struct {
	Text         string
	InputTokens  int
	OutputTokens int
}

// AnthropicClient is a low-level Anthropic Messages-API client wrapping the
// official anthropic-sdk-go. It is the Anthropic sibling of the Ollama *Client in
// this module: it only moves bytes to and from the provider and maps faults onto
// the framework error model. Model choice, prompt assembly, idempotency and usage
// shaping belong to the consuming WorkerAccess Store and never cross its surface.
//
// Safe for concurrent use (the underlying SDK client is).
type AnthropicClient struct {
	client    anthropic.Client
	maxTokens int
}

// NewAnthropicClient builds a client against the Anthropic API with an explicit
// API key. baseURL is optional — empty uses the SDK default endpoint; set it only
// to target a proxy or compatible gateway. defaultMaxTokens bounds the response
// length when a request does not set its own.
func NewAnthropicClient(apiKey, baseURL string, defaultMaxTokens int) *AnthropicClient {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if strings.TrimSpace(baseURL) != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if defaultMaxTokens <= 0 {
		defaultMaxTokens = 16000
	}
	return &AnthropicClient{
		client:    anthropic.NewClient(opts...),
		maxTokens: defaultMaxTokens,
	}
}

// Generate performs the blocking Messages-API call and collects the assistant
// text. Faults are mapped onto the framework error model by HTTP STATUS CODE alone
// (mirroring the Ollama transport): 429 → RateLimited, 401/403 → Auth, 402 →
// QuotaExhausted, 404 → NotFound, 5xx → Transient, every OTHER 4xx (incl. 400) →
// ContractMisuse (terminal); a transport-level failure with no HTTP status
// (network blip, timeout, cancellation) → Transient.
func (c *AnthropicClient) Generate(ctx context.Context, req AnthropicGenerateRequest) (AnthropicGenerateResponse, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = c.maxTokens
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(maxTokens),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.Prompt)),
		},
	}
	if strings.TrimSpace(req.System) != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	msg, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return AnthropicGenerateResponse{}, mapAnthropicError(ctx, err)
	}

	// Collect the text content blocks (skip any thinking / tool_use blocks) so the
	// result is the model's textual answer regardless of block ordering.
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}

	return AnthropicGenerateResponse{
		Text:         trimJSONFences(b.String()),
		InputTokens:  int(msg.Usage.InputTokens),
		OutputTokens: int(msg.Usage.OutputTokens),
	}, nil
}

// trimJSONFences strips a leading/trailing Markdown code fence the model may emit
// around a JSON value despite a JSON-only instruction, so the bytes unmarshal
// cleanly downstream. A response with no fence is returned unchanged.
func trimJSONFences(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return s
	}
	t = strings.TrimPrefix(t, "```")
	// Drop an optional language tag (e.g. "json") on the opening fence line.
	if nl := strings.IndexByte(t, '\n'); nl >= 0 {
		if firstLine := strings.TrimSpace(t[:nl]); !strings.ContainsAny(firstLine, "{[\"") {
			t = t[nl+1:]
		}
	}
	t = strings.TrimSuffix(strings.TrimSpace(t), "```")
	return strings.TrimSpace(t)
}

// mapAnthropicError maps an SDK error onto the framework error model PURELY by
// HTTP status code — it never inspects the provider message/body to classify. An
// *anthropic.Error carries the HTTP status; a non-API error (network / context) is
// a retryable Transient fault.
//
// Status → Kind (retryable in parentheses):
//
//	429        → RateLimited    (retryable)
//	401 / 403  → Auth           (terminal)
//	402        → QuotaExhausted (terminal)
//	404        → NotFound       (terminal)
//	5xx        → Transient      (retryable)
//	other 4xx  → ContractMisuse (terminal)   ← includes 400
//	no status  → Transient      (retryable)
//
// 400 nuance (prod incident 2026-06-01): an out-of-credits account does NOT get a
// 402 — Anthropic returns the credit fault as a 400 invalid_request_error. Per the
// Anthropic docs a 400 invalid_request_error means "there was an issue with the
// format or content of your request": a PERMANENT client error. We therefore map
// EVERY non-listed 4xx (incl. 400) to the terminal ContractMisuse — re-issuing the
// identical request cannot succeed, so it must NOT be retryable. We deliberately do
// NOT string-match the body (e.g. for "credit balance"): a billing 400 and a
// malformed-request 400 are both terminal-on-retry, so a single status-driven rule
// is correct and robust to wording changes. The human-facing remediation (top up
// credits vs fix the request) is surfaced by the caller from the detail, not by a
// classification fork here.
func mapAnthropicError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fwra.Wrap(fwra.Transient, err, "anthropic.Generate: timed out / cancelled")
	}
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return fwra.Wrap(fwra.Transient, err, "anthropic.Generate: provider unreachable")
	}
	// RawJSON() (the provider error envelope) is the safe detail source: it is set
	// by the SDK's UnmarshalJSON and, unlike Error(), never dereferences the
	// possibly-nil Request/Response. It is carried as opaque DETAIL only — never
	// parsed to drive the classification.
	detail := apiErr.RawJSON()
	switch {
	case apiErr.StatusCode == 429:
		return fwra.Wrap(fwra.RateLimited, err, detail)
	case apiErr.StatusCode == 401 || apiErr.StatusCode == 403:
		return fwra.Wrap(fwra.Auth, err, detail)
	case apiErr.StatusCode == 402:
		return fwra.Wrap(fwra.QuotaExhausted, err, detail)
	case apiErr.StatusCode == 404:
		return fwra.Wrap(fwra.NotFound, err, detail)
	case apiErr.StatusCode >= 500:
		return fwra.Wrap(fwra.Transient, err, detail)
	default:
		// Every other 4xx (incl. 400 invalid_request_error) is a permanent client
		// error: the request itself is unacceptable, so retrying the identical
		// request cannot help. Terminal, non-retryable.
		return fwra.Wrap(fwra.ContractMisuse, err, detail)
	}
}
