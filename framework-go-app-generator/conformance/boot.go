package conformance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// readyzTimeout bounds the wait for GET /readyz once the launcher printed its
	// ready line.
	readyzTimeout = 120 * time.Second
	// launcherTimeout bounds the launcher up to its ready line: it provisions
	// Postgres and Temporal and builds the SPA and the server first.
	launcherTimeout = 10 * time.Minute
	stopGrace       = 90 * time.Second
	mcpProtocol     = "2025-06-18"
)

// Booted is a running composed app.
type Booted struct {
	BaseURL string
	// Stop terminates the app and the substrates its launcher provisioned, and waits
	// for them to be gone.
	Stop func() error
}

// readyLine is the one JSON line the generated launcher prints on stdout.
type readyLine struct {
	Devstack string `json:"devstack"`
	URL      string `json:"url"`
}

// Boot runs the app's own run contract, `go run ./cmd/devstack`, in dir and waits
// for it to print its ready line and for GET /readyz to answer 200. The launcher
// runs in its own process group so Stop reaches the server it spawned, and it
// removes the Postgres container with its data volume on SIGTERM.
func Boot(ctx context.Context, dir string) (Booted, error) {
	cmd := exec.Command("go", "run", "./cmd/devstack") // #nosec G204 -- the app's own run contract
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr tailBuffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Booted{}, err
	}
	if err := cmd.Start(); err != nil {
		return Booted{}, fmt.Errorf("start devstack: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	var once sync.Once
	var stopErr error
	stop := func() error {
		once.Do(func() { stopErr = terminate(cmd, exited) })
		return stopErr
	}
	fail := func(err error) (Booted, error) {
		_ = stop()
		return Booted{}, fmt.Errorf("%w\ndevstack stderr:\n%s", err, stderr.String())
	}

	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			var rl readyLine
			if json.Unmarshal(sc.Bytes(), &rl) == nil && rl.Devstack == "ready" && rl.URL != "" {
				lines <- rl.URL
				break
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()

	base, err := awaitReadyLine(ctx, lines, exited)
	if err != nil {
		return fail(err)
	}
	if err := waitReadyz(ctx, base); err != nil {
		return fail(err)
	}
	return Booted{BaseURL: base, Stop: stop}, nil
}

// awaitReadyLine waits for the launcher's ready line, its exit, the timeout or the context.
// An exit is put back on the channel so the later stop still sees it.
func awaitReadyLine(ctx context.Context, lines <-chan string, exited chan error) (string, error) {
	select {
	case base := <-lines:
		return base, nil
	case err := <-exited:
		exited <- err
		return "", fmt.Errorf("devstack exited before it was ready: %v", err)
	case <-time.After(launcherTimeout):
		return "", fmt.Errorf("devstack printed no ready line within %s", launcherTimeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// terminate sends SIGTERM to the launcher's process group, waits for it and its
// cleanups (the Postgres removal runs in the launcher's deferred stack), then kills
// the group if it outlives the grace period.
func terminate(cmd *exec.Cmd, exited chan error) error {
	pgid := cmd.Process.Pid
	select {
	case <-exited:
		return nil
	default:
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-exited:
		return nil
	case <-time.After(stopGrace):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-exited
		return fmt.Errorf("devstack did not stop within %s and was killed", stopGrace)
	}
}

func waitReadyz(ctx context.Context, base string) error {
	deadline := time.Now().Add(readyzTimeout)
	var last error
	for time.Now().Before(deadline) {
		status, _, err := get(ctx, base+"/readyz")
		if err == nil && status == http.StatusOK {
			return nil
		}
		last = err
		if err == nil {
			last = fmt.Errorf("status %d", status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("GET /readyz did not return 200 within %s: %v", readyzTimeout, last)
}

func get(ctx context.Context, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, body, err
}

// Probe holds the booted app to what a client sees: the SPA at / and a working
// MCP door at /mcp that lists at least one tool.
func Probe(ctx context.Context, b Booted) error {
	status, body, err := get(ctx, b.BaseURL+"/")
	if err != nil {
		return fmt.Errorf("GET /: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("GET / = %d, want 200", status)
	}
	if !strings.Contains(strings.ToLower(string(body)), "<html") {
		return fmt.Errorf("GET / body has no <html: %.200q", body)
	}
	return probeMCP(ctx, b.BaseURL+"/mcp")
}

// probeMCP drives streamable-HTTP MCP: initialize, the initialized notification,
// then tools/list.
func probeMCP(ctx context.Context, url string) error {
	init, session, err := rpc(ctx, url, "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": mcpProtocol, "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "conformance-probe", "version": "0"},
		},
	})
	if err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}
	if init.Error != nil {
		return fmt.Errorf("mcp initialize: %s", init.Error.Message)
	}
	if _, _, err := rpc(ctx, url, session, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return fmt.Errorf("mcp initialized notification: %w", err)
	}
	list, _, err := rpc(ctx, url, session, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	if err != nil {
		return fmt.Errorf("mcp tools/list: %w", err)
	}
	if list.Error != nil {
		return fmt.Errorf("mcp tools/list: %s", list.Error.Message)
	}
	var res struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(list.Result, &res); err != nil {
		return fmt.Errorf("mcp tools/list result: %w", err)
	}
	if len(res.Tools) == 0 {
		return errors.New("mcp tools/list returned no tools")
	}
	return nil
}

type rpcReply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// rpc posts one JSON-RPC message and returns the reply (zero for a notification) and
// the session id the server assigned.
func rpc(ctx context.Context, url, session string, msg map[string]any) (rpcReply, string, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return rpcReply{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return rpcReply{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
		req.Header.Set("MCP-Protocol-Version", mcpProtocol)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return rpcReply{}, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return rpcReply{}, "", err
	}
	if resp.StatusCode/100 != 2 {
		return rpcReply{}, "", fmt.Errorf("status %d: %.300q", resp.StatusCode, body)
	}
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		sid = session
	}
	if _, isCall := msg["id"]; !isCall {
		return rpcReply{}, sid, nil
	}
	reply, err := decodeReply(resp.Header.Get("Content-Type"), body)
	return reply, sid, err
}

// decodeReply reads a JSON body or the first data event of an event stream.
func decodeReply(contentType string, body []byte) (rpcReply, error) {
	var reply rpcReply
	if strings.HasPrefix(contentType, "text/event-stream") {
		for line := range strings.SplitSeq(string(body), "\n") {
			if data, ok := strings.CutPrefix(line, "data:"); ok {
				body = []byte(strings.TrimSpace(data))
				break
			}
		}
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return reply, fmt.Errorf("decode reply %.300q: %w", body, err)
	}
	return reply, nil
}

// DockerInventory lists the containers and volumes docker holds now, as
// "container:<name>" and "volume:<name>".
func DockerInventory(ctx context.Context) ([]string, error) {
	var out []string
	for _, q := range []struct {
		kind string
		argv []string
	}{
		{"container", []string{"ps", "-a", "--format", "{{.Names}}"}},
		{"volume", []string{"volume", "ls", "-q"}},
	} {
		b, err := exec.CommandContext(ctx, "docker", q.argv...).Output() // #nosec G204 -- fixed argv
		if err != nil {
			return nil, fmt.Errorf("docker %s: %w", strings.Join(q.argv, " "), err)
		}
		for _, name := range strings.Fields(string(b)) {
			out = append(out, q.kind+":"+name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// DockerLeftovers is the names in after that were not in before. The context is
// accepted for symmetry with DockerInventory; the comparison is pure.
func DockerLeftovers(_ context.Context, before, after []string) []string {
	seen := make(map[string]bool, len(before))
	for _, n := range before {
		seen[n] = true
	}
	var left []string
	for _, n := range after {
		if !seen[n] {
			left = append(left, n)
		}
	}
	return left
}

// tailBuffer keeps the end of what is written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > stderrTail {
		t.buf = t.buf[len(t.buf)-stderrTail:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
