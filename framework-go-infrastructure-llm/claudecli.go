package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// claudeCLIWaitDelay bounds how long Generate waits for stdout/stderr pipes to
// close AFTER the subprocess has already been killed by a ctx cancellation.
// Without this, os/exec's documented gotcha bites: if claude (or, in tests,
// the shim) has spawned its own child that inherited the pipe's write end,
// killing the direct child alone does not close the pipe, and cmd.Wait()
// blocks until that grandchild exits on its own — silently defeating the
// caller's deadline. See https://pkg.go.dev/os/exec#Cmd.WaitDelay.
const claudeCLIWaitDelay = 2 * time.Second

// ClaudeCLIClient is the claude-local Worker Provider: it shells out to the
// locally installed `claude` CLI in headless print mode
// (`claude -p --output-format json`, prompt on stdin) so LLM work rides the
// user's OWN Claude Pro/Max subscription (headless mode resolves OAuth from
// the CLI's own on-disk credentials — the ambient mechanism a `claude login`
// / interactive Claude Code session already established) instead of an
// ANTHROPIC_API_KEY. It is the sibling of AnthropicClient (anthropic.go, the
// CLOUD provider) in this module and exposes the IDENTICAL
// Generate(ctx, AnthropicGenerateRequest) (AnthropicGenerateResponse, error)
// method signature — the SAME request/response structs, reused verbatim, not
// re-declared — so a caller can select between the two providers behind one
// interface without touching prompt-assembly or response-shaping code. Like
// AnthropicClient, this transport only moves bytes to and from the provider
// and maps faults onto the framework error model; model choice, prompt
// assembly and usage shaping stay the caller's job.
//
// Ambient auth, no forwarded key: this client never reads ANTHROPIC_API_KEY
// itself and explicitly strips it from the subprocess environment (see
// claudeCLIEnv) — a stray exported key in the parent process cannot silently
// hijack a local subscription run and get billed to the wrong account.
//
// # Why there is no GenerateWithTools / GenerateToolTurn here
//
// The brief for this provider asked for a tool-calling transport mirroring
// AnthropicClient.GenerateWithTools (anthropic_tools.go) / Client.Chat
// (ollama_tools.go) via `claude --mcp-config` pointing at an ephemeral stdio
// MCP server. Reading BOTH existing tool-calling transports end-to-end first
// (their shared contract, restated here) shows that shape does not fit:
//
//   - The existing contract is "one blocking turn, caller drives the loop
//     externally": GenerateWithTools/Chat take the FULL running conversation
//     (including any results the caller already decided for prior turns) and
//     return exactly ONE assistant turn — text plus at most a batch of
//     tool_use blocks plus a stop reason. Control returns to the CALLER after
//     that single turn. The caller (a WorkerAccess) then executes each
//     tool_use itself, applies ITS OWN validation/self-correction policy to
//     decide the tool_result content (ok, or an actionable error asking the
//     model to retry), appends that result, and calls the transport again for
//     the NEXT turn. The provider owns zero loop state between calls.
//
//   - Headless `claude` with `--mcp-config` cannot reproduce that shape.
//     `--mcp-config` attaches a REAL MCP server that headless claude calls
//     autonomously, inside its OWN internal agentic loop, and that loop runs
//     to COMPLETION inside the single `claude -p` subprocess invocation —
//     there is no point at which control returns to an external Go caller
//     after just ONE tool_use so the caller can inject its own validation
//     decision before the model's next step. The MCP tool handler executes
//     synchronously as part of the SAME headless run; by the time our Go
//     process would see anything, the model has already reasoned past
//     whatever the handler returned. The "record invocations and feed them
//     back" framing in the original brief describes an MCP server that
//     answers each call algorithmically in isolation — but the caller-owned
//     turn-by-turn validation loop (e.g. the historical coreUseCases
//     submit_use_case/finish self-correcting draft loop referenced in
//     project memory) needs EXTERNAL, request-specific validation logic
//     injected between turns, which an MCP tool handler embedded in the
//     subprocess cannot receive from the calling WorkerAccess without
//     collapsing "one turn per Generate call" into "the whole multi-turn loop
//     runs inside one subprocess invocation" — a fundamentally different
//     shape from GenerateWithTools's contract, not a drop-in implementation
//     of it.
//
//   - Separately: as of this provider's construction, NOTHING in the
//     archistrator app calls GenerateWithTools/Chat-shaped API on ANY
//     provider. The coreUseCases tool-loop drafting path this brief's Step 4
//     cites was dropped from the app during the 2026-06 agentic pivot (design
//     dispatches a GitHub-Actions job that writes .aiarch/ JSON directly
//     instead of a synchronous in-process tool-calling loop); the historical
//     GenerateToolTurn caller-loop this brief describes no longer exists as a
//     live consumer to mirror.
//
// Per the plan's own Sequencing note ("If Task 3 Step 4 (tool-turn) stalls,
// ship 2–5 with typed-generate-only drafts and surface the limitation"), this
// provider ships Generate-only. If a future task needs claude-local inside a
// genuine multi-turn tool-calling loop, the honest design is a NEW method
// shaped around what headless claude can actually do — e.g. a single
// `RunToolLoop` call that hands claude-local the tools AND a Go callback it
// invokes synchronously from inside the SAME subprocess's ephemeral MCP
// server (the "whole loop in one invocation" shape above) — not a
// GenerateWithTools mirror. That is a founder-level API design decision, not
// implemented here.
type ClaudeCLIClient struct {
	maxTokens int // currently informational only: claude -p has no per-call max-tokens flag; kept for symmetry with AnthropicClient and future use (e.g. surfaced via a system-prompt instruction).
}

// NewClaudeCLIClient builds a claude-local provider. defaultMaxTokens mirrors
// NewAnthropicClient's parameter for symmetry between the two constructors;
// headless `claude -p` has no equivalent per-call flag today, so the value is
// currently unused by Generate.
func NewClaudeCLIClient(defaultMaxTokens int) *ClaudeCLIClient {
	return &ClaudeCLIClient{maxTokens: defaultMaxTokens}
}

// claudeCLIResultEnvelope is the `claude -p --output-format json` result
// shape: a single JSON object printed to stdout on completion (successful or
// not) — Result carries the final text answer (or, when IsError, the
// human-readable failure), Usage carries the token counters the AnthropicSDK
// response also exposes.
type claudeCLIResultEnvelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Generate performs one blocking headless-claude call and maps process/parse
// faults onto the framework error model. Faults are classified WITHOUT an
// HTTP status code to key off (unlike AnthropicClient.Generate) — see
// mapClaudeCLIProcessError / classifyClaudeCLIFailureText for the
// exit-code/stderr/is_error-envelope-driven scheme this provider uses
// instead, and the documented caveat that a CLI's stderr wording is a less
// stable signal than an HTTP status.
func (c *ClaudeCLIClient) Generate(ctx context.Context, req AnthropicGenerateRequest) (AnthropicGenerateResponse, error) {
	args := []string{"-p", "--output-format", "json"}
	if strings.TrimSpace(req.System) != "" {
		args = append(args, "--append-system-prompt", req.System)
	}

	cmd := exec.CommandContext(ctx, "claude", args...) //nolint:gosec // fixed trusted binary name + internal-only args, mirrors the aiarch-state-mcp cmd/ precedent
	cmd.WaitDelay = claudeCLIWaitDelay
	cmd.Stdin = strings.NewReader(req.Prompt)
	cmd.Env = claudeCLIEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return AnthropicGenerateResponse{}, mapClaudeCLIProcessError(ctx, err, stderr.String())
	}

	// The envelope itself may arrive fenced (e.g. the CLI/model wraps its JSON
	// output in a markdown code block) — recover it through the SAME
	// trimJSONFences path anthropic.go's Generate applies to raw model text
	// before treating stdout as malformed.
	cleaned := trimJSONFences(stdout.String())
	var env claudeCLIResultEnvelope
	if err := json.Unmarshal([]byte(cleaned), &env); err != nil {
		return AnthropicGenerateResponse{}, fwra.Wrap(fwra.Infrastructure, err, "claudecli.Generate: decode result envelope")
	}
	if env.IsError {
		return AnthropicGenerateResponse{}, classifyClaudeCLIFailureText(env.Result)
	}

	return AnthropicGenerateResponse{
		Text:         trimJSONFences(env.Result),
		InputTokens:  env.Usage.InputTokens,
		OutputTokens: env.Usage.OutputTokens,
	}, nil
}

// claudeCLIEnv returns the subprocess environment: the parent's environment
// verbatim EXCEPT any ANTHROPIC_API_KEY entry, which is stripped so a stray
// exported key on the archistrator server process can never override the
// user's own ambient subscription OAuth for a claude-local run.
func claudeCLIEnv() []string {
	parent := os.Environ()
	out := make([]string, 0, len(parent))
	for _, kv := range parent {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// mapClaudeCLIProcessError maps a cmd.Run() failure (the subprocess exited
// non-zero, or never started at all) onto the framework error model.
func mapClaudeCLIProcessError(ctx context.Context, err error, stderr string) error {
	// A caller-supplied deadline/cancellation firing while the subprocess was
	// still running is ALWAYS retryable, regardless of what (if anything) the
	// killed process wrote to stderr — mirrors the cloud transport's
	// context.Canceled/DeadlineExceeded → Transient handling.
	if ctx.Err() != nil {
		return fwra.Wrap(fwra.Transient, err, "claudecli.Generate: timed out / cancelled")
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		// The subprocess never ran at all (binary missing from PATH, not
		// executable, etc.) — an infrastructure-level fault distinct from a
		// model-call fault the process itself reported.
		return fwra.Wrap(fwra.Infrastructure, err, "claudecli.Generate: failed to start claude subprocess")
	}

	return classifyClaudeCLIFailureText(stderr)
}

// classifyClaudeCLIFailureText classifies a claude-local failure by matching
// KNOWN, DOCUMENTED Claude Code CLI failure phrasings — the closest analog
// available to anthropic.go's HTTP-status-driven classification, but
// necessarily weaker: a subprocess exit/stderr carries no status-code-shaped
// signal, so unlike mapAnthropicError this function DOES read message text.
// This is a known fragility (CLI wording can change across claude releases)
// flagged for review — see the package doc comment's Step-1 analysis. Any
// non-zero exit / is_error envelope that matches NEITHER vocabulary defaults
// to the retryable Transient kind (mirrors the Ollama transport's
// "provider unreachable" default): treating an unrecognized failure as
// terminal risks silently masking a flaky/transient CLI hiccup as a
// permanent stop, which is the worse failure mode of the two.
func classifyClaudeCLIFailureText(text string) error {
	lower := strings.ToLower(text)
	switch {
	case containsAny(lower, "invalid api key", "please run /login", "not authenticated", "authentication_error", "please run `claude login`", "please run `/login`"):
		return fwra.New(fwra.Auth, text)
	case containsAny(lower, "usage limit reached", "credit balance is too low", "quota", "payment required", "billing"):
		return fwra.New(fwra.QuotaExhausted, text)
	default:
		return fwra.New(fwra.Transient, text)
	}
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// preflightClaudeCLI runs `claude --version` and returns a friendly,
// actionable error naming the install command when the binary is absent or
// broken. Exported for the app composition root's boot-time preflight
// (hooks.go: local profile + no ANTHROPIC_API_KEY MUST have a working
// `claude` on PATH before the server accepts traffic).
func PreflightClaudeCLI() error {
	cmd := exec.Command("claude", "--version") //nolint:gosec // fixed trusted binary, no args from external input
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"claude-local preflight failed: `claude --version` did not run cleanly (%w); "+
				"install the Claude Code CLI (npm install -g @anthropic-ai/claude-code) and run `claude login`, "+
				"or set ANTHROPIC_API_KEY to use the cloud provider instead; output: %s",
			err, strings.TrimSpace(string(out)))
	}
	return nil
}
