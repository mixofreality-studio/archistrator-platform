// Package testinfra provides a throwaway Ollama testcontainer bootstrap (with a
// pulled model) for the integration tests of any aiarch-built system. It is
// TEST-ONLY: nothing here is imported by production code, and StartOllama is
// designed to be skipped under testing.Short() so `go test -short ./...` stays
// fast and container-free.
package testinfra

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	fwllm "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-llm"
)

// ollamaImage pins the Ollama image used for workerAccess integration tests. It
// matches the external LLM Worker Provider infrastructure aiarch's workerProvider
// Resource fronts in production. Pinning a tag keeps the test deterministic.
const ollamaImage = "ollama/ollama:0.5.7"

// ollamaModel is the SMALL model pulled into the throwaway container. Worker
// tests assert structural/behavioural properties of the run, never exact
// generated text, so a tiny model is sufficient and keeps the pull + inference
// fast.
//
// WARNING: the FIRST run pulls this model into the container — a network
// download of a few hundred MB that can take a minute or more. Each fresh
// container pulls the model into its own volume, so the cost is paid once per
// test process.
const ollamaModel = "qwen2.5:0.5b"

// ollamaModelEnv lets a caller override the pulled model without code changes
// (e.g. a Manager integration suite needs a model capable enough to reliably
// emit a structured artifact a validation Engine accepts; the tiny default is
// fine for model-agnostic structural tests).
const ollamaModelEnv = "AIARCH_OLLAMA_MODEL"

// clientTimeout bounds generate calls made through the returned handle's client.
// A generous timeout; an individual test's deadline is the real bound.
const clientTimeout = 5 * time.Minute

// Ollama is the ready-to-use handle StartOllama hands back: the externally
// reachable base URL of the running Ollama HTTP API and the name of the model
// that has been pulled and is ready to serve. The caller never manages the
// container lifecycle.
type Ollama struct {
	// BaseURL is the externally-reachable HTTP root of the Ollama API, e.g.
	// http://127.0.0.1:53212.
	BaseURL string
	// Model is the logical model name that has been pulled and is ready.
	Model string
}

// StartOllama spins a throwaway Ollama container, waits for its HTTP API to be
// live, pulls a small model so the first dispatch does not pay the pull cost
// mid-test, and returns a ready Ollama handle. Cleanup (container termination)
// is registered on t via t.Cleanup, so callers never manage the lifecycle.
//
// The helper is skipped under `testing.Short()` so the container is never spun
// in the fast path.
//
// WARNING: the model pull (see ollamaModel) is a slow, multi-hundred-MB network
// download on first run. The pull is performed here, ONCE, after the API is live
// and before the handle is returned, so individual dispatch tests see a warm
// model and a predictable latency.
func StartOllama(t *testing.T) Ollama {
	t.Helper()
	model := ollamaModel
	if env := os.Getenv(ollamaModelEnv); env != "" {
		model = env
	}
	return StartOllamaWithModel(t, model)
}

// StartOllamaWithModel is StartOllama with an explicit model, for callers that
// need a specific model rather than the tiny default. Same lifecycle/skip
// semantics as StartOllama.
func StartOllamaWithModel(t *testing.T, model string) Ollama {
	t.Helper()

	if testing.Short() {
		t.Skip("testinfra.StartOllama: skipped under -short (requires Docker + model pull)")
	}

	ctx := context.Background()

	container, err := testcontainers.Run(ctx, ollamaImage,
		testcontainers.WithExposedPorts("11434/tcp"),
		testcontainers.WithWaitStrategy(
			// Ollama answers /api/version once the HTTP server is up; the model is
			// pulled separately below.
			wait.ForHTTP("/api/version").
				WithPort("11434/tcp").
				WithStartupTimeout(120*time.Second).
				WithStatusCodeMatcher(func(status int) bool { return status == 200 }),
		),
	)
	if err != nil {
		t.Fatalf("testinfra.StartOllama: start container: %v", err)
	}
	t.Cleanup(func() {
		if termErr := testcontainers.TerminateContainer(container); termErr != nil {
			t.Logf("testinfra.StartOllama: terminate container: %v", termErr)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("testinfra.StartOllama: host: %v", err)
	}
	port, err := container.MappedPort(ctx, "11434/tcp")
	if err != nil {
		t.Fatalf("testinfra.StartOllama: mapped port: %v", err)
	}
	baseURL := "http://" + host + ":" + port.Port()

	t.Logf("testinfra.StartOllama: pulling model %q — first run is a slow multi-hundred-MB download", model)
	pullStart := time.Now()
	if err := fwllm.NewClient(baseURL, clientTimeout).Pull(ctx, model); err != nil {
		t.Fatalf("testinfra.StartOllama: pull model %q: %v", model, err)
	}
	t.Logf("testinfra.StartOllama: model %q ready in %s", model, time.Since(pullStart).Round(time.Second))

	return Ollama{BaseURL: baseURL, Model: model}
}
