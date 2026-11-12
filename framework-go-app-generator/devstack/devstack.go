// Package devstack launches an app's generated local run contract, `go run ./cmd/devstack`
// (rungen), as a child process and holds it to its ready line and GET /readyz. It is
// deliberately free of the testing package: a test substrate (framework-go-scenariohost) wraps
// it for tests, and production job binaries (the integration smoke, the web scenario run) call
// it directly, which the test-only import gate forbids them to do through scenariohost.
package devstack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// ReadyTimeout bounds the launcher up to its ready line: it may provision Postgres and
	// Temporal and build the SPA and the server first.
	ReadyTimeout = 10 * time.Minute
	// StopGrace is how long Stop waits after SIGTERM for the launcher and its cleanups before it
	// kills the process group.
	StopGrace = 90 * time.Second
	// ReadyzTimeout bounds the wait for GET /readyz once the launcher printed its ready line.
	ReadyzTimeout = 120 * time.Second

	// logTailLines is how much of the launcher's output a failure carries and App.Log returns.
	logTailLines = 40
	// logTailBytes bounds what is kept of the launcher's output.
	logTailBytes = 16 << 10
)

// groupGrace is how long Stop waits, once the process it started has exited, for the rest of the
// launcher's process group to go. A var so a test can shorten it.
var groupGrace = StopGrace

// groupPoll is how often Stop looks at whether the group is empty while it waits.
const groupPoll = 20 * time.Millisecond

// App is a running composed app.
type App struct {
	// BaseURL is "http://127.0.0.1:<port>", from the launcher's ready line.
	BaseURL string
	// MCPURL is BaseURL + "/mcp".
	MCPURL string
	// PGID is the launcher's process group: the launcher is its leader and the server it spawned
	// is a member, so no process of the group outlives a successful Stop.
	PGID int
	// Stop sends SIGTERM to the launcher's process group, waits up to StopGrace for the launcher
	// and the substrates it provisioned to go, then kills the group. It is safe to call twice and
	// returns an error when the group outlived StopGrace or was left processes after the launcher
	// exited.
	Stop func() error
	// Log is the tail (40 lines) of the launcher's combined output, for failure messages.
	Log func() string
}

// readyLine is the one JSON line the generated launcher prints on stdout.
type readyLine struct {
	Devstack string `json:"devstack"`
	URL      string `json:"url"`
}

// Launch runs the app's own run contract, `go run ./cmd/devstack`, in moduleDir and waits for
// its ready line and for GET /readyz to answer 200. The child's environment is the process's
// plus GOWORK=off plus env (NAME=VALUE; a later entry wins), so a caller that supplies a
// substrate's variables keeps the launcher from provisioning that substrate. The launcher runs in
// its own process group so Stop reaches the server it spawned. log, when non-nil, also receives
// the launcher's output as it is written.
//
// An exit before the ready line is an error carrying the launcher's output tail.
func Launch(ctx context.Context, moduleDir string, env []string, log io.Writer) (*App, error) {
	cmd := exec.Command("go", "run", "./cmd/devstack") // #nosec G204 -- the app's own run contract
	cmd.Dir = moduleDir
	cmd.Env = append(append(os.Environ(), "GOWORK=off"), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// One combined log for stdout and stderr; as the writer exec copies the streams through,
	// cmd.Wait returns only after every byte the launcher printed is in it.
	var tail tailBuffer
	var sink io.Writer = &tail
	if log != nil {
		sink = io.MultiWriter(&tail, log)
	}
	lines := make(chan string, 1)
	cmd.Stdout = &readyScanner{log: sink, found: lines}
	cmd.Stderr = sink
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start devstack: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	var once sync.Once
	var stopErr error
	stop := func() error {
		once.Do(func() { stopErr = terminate(cmd.Process.Pid, exited) })
		return stopErr
	}
	logTail := func() string { return lastLines(tail.String(), logTailLines) }
	fail := func(err error) (*App, error) {
		_ = stop()
		return nil, fmt.Errorf("%w\ndevstack output (last %d lines):\n%s", err, logTailLines, logTail())
	}

	base, err := awaitReadyLine(ctx, lines, exited)
	if err != nil {
		return fail(err)
	}
	if err := waitReadyz(ctx, base); err != nil {
		return fail(err)
	}
	return &App{BaseURL: base, MCPURL: base + "/mcp", PGID: cmd.Process.Pid, Stop: stop, Log: logTail}, nil
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
	case <-time.After(ReadyTimeout):
		return "", fmt.Errorf("devstack printed no ready line within %s", ReadyTimeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// terminate sends SIGTERM to the launcher's process group, waits for the process it started to
// exit, then for the rest of the group to go (drainGroup), and kills the group if either outlives
// its grace period.
//
// The process it started is `go run`, which a SIGTERM ends at once: the compiled launcher and the
// server it spawned (whose deferred cleanups, such as the Postgres removal, are what Stop exists to
// wait for) wind down after it, and a member that has exited stays in the group as a zombie until
// its new parent reaps it. The exit of that first process is therefore not the exit of the group.
func terminate(pgid int, exited chan error) error {
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case err := <-exited:
		exited <- err
		return drainGroup(pgid)
	case <-time.After(StopGrace):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-exited
		return fmt.Errorf("devstack did not stop within %s and was killed", StopGrace)
	}
}

// drainGroup waits up to groupGrace for the launcher's process group to empty once the process
// Stop started has exited. A group that still has members after the grace is a leak: a server that
// outlived its launcher must not outlive the test, so it is killed and reported.
func drainGroup(pgid int) error {
	deadline := time.Now().Add(groupGrace)
	for syscall.Kill(-pgid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			return fmt.Errorf("devstack left processes in its group (pgid %d) %s after it exited; they were killed", pgid, groupGrace)
		}
		time.Sleep(groupPoll)
	}
	return nil
}

func waitReadyz(ctx context.Context, base string) error {
	deadline := time.Now().Add(ReadyzTimeout)
	var last error
	for time.Now().Before(deadline) {
		code, err := statusOf(ctx, base+"/readyz")
		if err == nil && code == http.StatusOK {
			return nil
		}
		last = err
		if err == nil {
			last = fmt.Errorf("status %d", code)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("GET /readyz did not return 200 within %s: %v", ReadyzTimeout, last)
}

func statusOf(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, nil
}

// lastLines is the last n lines of s.
func lastLines(s string, n int) string {
	ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, "\n")
}

// readyScanner passes the launcher's stdout to the log and reports the URL on its ready line,
// once.
type readyScanner struct {
	log   io.Writer
	found chan<- string
	mu    sync.Mutex
	part  []byte
	done  bool
}

func (r *readyScanner) Write(p []byte) (int, error) {
	_, _ = r.log.Write(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return len(p), nil
	}
	r.part = append(r.part, p...)
	for {
		i := bytes.IndexByte(r.part, '\n')
		if i < 0 {
			break
		}
		var rl readyLine
		if json.Unmarshal(r.part[:i], &rl) == nil && rl.Devstack == "ready" && rl.URL != "" {
			r.found <- rl.URL
			r.done, r.part = true, nil
			break
		}
		r.part = r.part[i+1:]
	}
	return len(p), nil
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
	if len(t.buf) > logTailBytes {
		t.buf = t.buf[len(t.buf)-logTailBytes:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
