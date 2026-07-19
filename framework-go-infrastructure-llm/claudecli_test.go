package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// installClaudeShim writes an executable script named `claude` into a fresh
// temp dir and prepends that dir to PATH for the duration of the test, so
// exec.CommandContext("claude", ...) resolves to the shim instead of a real
// installation — the local analog of the Ollama testcontainer / VCR cassette:
// no real subscription is touched in CI.
func installClaudeShim(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shim is a POSIX shell script; claude-local is not exercised on windows in CI")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test shim, deliberately executable
		t.Fatalf("write shim: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const happyEnvelope = `{"type":"result","subtype":"success","is_error":false,` +
	`"result":"Hello from claude-local","total_cost_usd":0.0123,` +
	`"usage":{"input_tokens":42,"output_tokens":7}}`

// TestClaudeCLIGenerate_Happy — a clean success envelope on stdout, exit 0.
func TestClaudeCLIGenerate_Happy(t *testing.T) {
	installClaudeShim(t, "#!/bin/sh\ncat <<'EOF'\n"+happyEnvelope+"\nEOF\nexit 0\n")

	c := NewClaudeCLIClient(0)
	got, err := c.Generate(context.Background(), AnthropicGenerateRequest{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}
	if got.Text != "Hello from claude-local" {
		t.Fatalf("Text = %q, want %q", got.Text, "Hello from claude-local")
	}
	if got.InputTokens != 42 || got.OutputTokens != 7 {
		t.Fatalf("tokens = (%d,%d), want (42,7)", got.InputTokens, got.OutputTokens)
	}
}

// TestClaudeCLIGenerate_FencedEnvelope — the whole stdout payload is wrapped in
// a markdown code fence (a malformed-JSON-at-face-value shape) and must be
// recovered through the SAME trimJSONFences path anthropic.go's Generate
// applies to the model's raw text, before the envelope can be decoded at all.
func TestClaudeCLIGenerate_FencedEnvelope(t *testing.T) {
	fenced := "```json\n" + happyEnvelope + "\n```\n"
	installClaudeShim(t, "#!/bin/sh\ncat <<'EOF'\n"+fenced+"EOF\nexit 0\n")

	c := NewClaudeCLIClient(0)
	got, err := c.Generate(context.Background(), AnthropicGenerateRequest{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Generate: unexpected error decoding fenced envelope: %v", err)
	}
	if got.Text != "Hello from claude-local" {
		t.Fatalf("Text = %q, want %q", got.Text, "Hello from claude-local")
	}
}

// TestClaudeCLIGenerate_ResultTextFenced — a well-formed envelope whose
// `result` field itself carries a fenced JSON blob (the model emitted JSON
// content despite the fence habit) — trimJSONFences must strip it from the
// returned Text, exactly as anthropic.go's Generate does for the SDK's text
// blocks.
func TestClaudeCLIGenerate_ResultTextFenced(t *testing.T) {
	fencedResult := "```json\n{\"key\":\"value\"}\n```"
	payload, err := json.Marshal(map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"result": fencedResult,
		"usage":  map[string]int{"input_tokens": 1, "output_tokens": 1},
	})
	if err != nil {
		t.Fatalf("marshal test envelope: %v", err)
	}
	installClaudeShim(t, "#!/bin/sh\ncat <<'EOF'\n"+string(payload)+"\nEOF\nexit 0\n")

	c := NewClaudeCLIClient(0)
	got, err := c.Generate(context.Background(), AnthropicGenerateRequest{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}
	if got.Text != `{"key":"value"}` {
		t.Fatalf("Text = %q, want fence-stripped JSON", got.Text)
	}
}

// TestClaudeCLIGenerate_NonZeroExit_AuthShaped — a non-zero exit with an
// auth-shaped stderr message (no valid subscription / not logged in) must map
// to the terminal, non-retryable fwra.Auth kind — re-running the identical
// request cannot succeed until the user re-authenticates, mirroring the
// terminal-on-retry principle anthropic.go documents for its 400 credit case.
func TestClaudeCLIGenerate_NonZeroExit_AuthShaped(t *testing.T) {
	installClaudeShim(t, "#!/bin/sh\necho 'Invalid API key \xc2\xb7 Please run /login' >&2\nexit 1\n")

	c := NewClaudeCLIClient(0)
	_, err := c.Generate(context.Background(), AnthropicGenerateRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("Generate: expected an error, got nil")
	}
	var rae *fwra.Error
	if !errors.As(err, &rae) {
		t.Fatalf("expected *fwra.Error, got %T (%v)", err, err)
	}
	if rae.Kind != fwra.Auth {
		t.Fatalf("Kind = %v, want %v", rae.Kind, fwra.Auth)
	}
	if rae.Retryable {
		t.Fatalf("Auth-kind fault must be non-retryable, got Retryable=true")
	}
}

// TestClaudeCLIGenerate_TimeoutKill — a caller-supplied deadline expires while
// the subprocess is still running; the subprocess must be killed and the
// fault must map to the retryable Transient kind (Temporal retries, same as
// the cloud transport's context.DeadlineExceeded path).
func TestClaudeCLIGenerate_TimeoutKill(t *testing.T) {
	installClaudeShim(t, "#!/bin/sh\nsleep 5\n")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	c := NewClaudeCLIClient(0)
	start := time.Now()
	_, err := c.Generate(ctx, AnthropicGenerateRequest{Prompt: "hi"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Generate: expected a timeout error, got nil")
	}
	if elapsed > 4*time.Second {
		t.Fatalf("Generate did not return promptly on ctx timeout: took %v", elapsed)
	}
	var rae *fwra.Error
	if !errors.As(err, &rae) {
		t.Fatalf("expected *fwra.Error, got %T (%v)", err, err)
	}
	if rae.Kind != fwra.Transient {
		t.Fatalf("Kind = %v, want %v", rae.Kind, fwra.Transient)
	}
	if !rae.Retryable {
		t.Fatalf("Transient-kind fault must be retryable, got Retryable=false")
	}
}

// TestClaudeCLIGenerate_NonZeroExit_Unclassified_Transient — a non-zero exit
// with stderr that matches neither the auth nor quota vocabulary defaults to
// the retryable Transient kind: an unrecognized CLI failure is treated the
// same way the Ollama transport treats "provider unreachable" — retry is
// safe, defaulting to terminal would risk masking a flaky/transient CLI
// hiccup as a permanent stop.
func TestClaudeCLIGenerate_NonZeroExit_Unclassified_Transient(t *testing.T) {
	installClaudeShim(t, "#!/bin/sh\necho 'panic: unexpected internal error' >&2\nexit 1\n")

	c := NewClaudeCLIClient(0)
	_, err := c.Generate(context.Background(), AnthropicGenerateRequest{Prompt: "hi"})
	var rae *fwra.Error
	if !errors.As(err, &rae) {
		t.Fatalf("expected *fwra.Error, got %T (%v)", err, err)
	}
	if rae.Kind != fwra.Transient {
		t.Fatalf("Kind = %v, want %v", rae.Kind, fwra.Transient)
	}
	if !rae.Retryable {
		t.Fatalf("Transient-kind fault must be retryable, got Retryable=false")
	}
}

// TestClaudeCLIGenerate_QuotaShaped — a non-zero exit whose stderr names a
// usage-limit-shaped failure maps to the terminal QuotaExhausted kind.
func TestClaudeCLIGenerate_QuotaShaped(t *testing.T) {
	installClaudeShim(t, "#!/bin/sh\necho 'Claude usage limit reached. Your limit will reset at 5pm.' >&2\nexit 1\n")

	c := NewClaudeCLIClient(0)
	_, err := c.Generate(context.Background(), AnthropicGenerateRequest{Prompt: "hi"})
	var rae *fwra.Error
	if !errors.As(err, &rae) {
		t.Fatalf("expected *fwra.Error, got %T (%v)", err, err)
	}
	if rae.Kind != fwra.QuotaExhausted {
		t.Fatalf("Kind = %v, want %v", rae.Kind, fwra.QuotaExhausted)
	}
	if rae.Retryable {
		t.Fatalf("QuotaExhausted-kind fault must be non-retryable, got Retryable=true")
	}
}

// TestClaudeCLIGenerate_IsErrorEnvelope_AuthShaped — exit 0 but the JSON
// envelope itself carries is_error:true with an auth-shaped result message
// (headless claude can report failure inside a well-formed envelope rather
// than a bare non-zero exit).
func TestClaudeCLIGenerate_IsErrorEnvelope_AuthShaped(t *testing.T) {
	envelope := `{"type":"result","subtype":"error_during_execution","is_error":true,` +
		`"result":"Invalid API key. Please run /login.","usage":{"input_tokens":0,"output_tokens":0}}`
	installClaudeShim(t, "#!/bin/sh\ncat <<'EOF'\n"+envelope+"\nEOF\nexit 0\n")

	c := NewClaudeCLIClient(0)
	_, err := c.Generate(context.Background(), AnthropicGenerateRequest{Prompt: "hi"})
	var rae *fwra.Error
	if !errors.As(err, &rae) {
		t.Fatalf("expected *fwra.Error, got %T (%v)", err, err)
	}
	if rae.Kind != fwra.Auth {
		t.Fatalf("Kind = %v, want %v", rae.Kind, fwra.Auth)
	}
}

// TestClaudeCLIGenerate_ClaudeNotOnPath — no shim installed at all (PATH has
// no `claude`): starting the subprocess itself fails, which is an
// Infrastructure-kind fault (distinct from a model-call fault the process
// itself reports).
func TestClaudeCLIGenerate_ClaudeNotOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-specific PATH handling")
	}
	t.Setenv("PATH", t.TempDir()) // empty dir — guarantees no `claude` resolves

	c := NewClaudeCLIClient(0)
	_, err := c.Generate(context.Background(), AnthropicGenerateRequest{Prompt: "hi"})
	var rae *fwra.Error
	if !errors.As(err, &rae) {
		t.Fatalf("expected *fwra.Error, got %T (%v)", err, err)
	}
	if rae.Kind != fwra.Infrastructure {
		t.Fatalf("Kind = %v, want %v", rae.Kind, fwra.Infrastructure)
	}
}
