package devstack

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The two launchers below are `go run ./cmd/devstack` stand-ins that, as the generated one does,
// spawn a child in the launcher's process group. The child writes FAKE_CHILD_READY once its signal
// disposition is installed, and the launcher prints its ready line only after that, so Stop never
// races the child's start-up.

// slowLauncher: SIGTERM ends `go run` and the launcher at once, while the child takes a moment to
// wind down (the Postgres removal and the server's shutdown run there). Stop must wait for it.
const slowLauncher = `package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ready := os.Getenv("FAKE_CHILD_READY")
	if os.Getenv("FAKE_ROLE") == "child" {
		term := make(chan os.Signal, 1)
		signal.Notify(term, syscall.SIGTERM)
		_ = os.WriteFile(ready, nil, 0o600)
		<-term
		time.Sleep(1500 * time.Millisecond)
		return
	}
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), "FAKE_ROLE=child")
	if err := child.Start(); err != nil {
		os.Exit(1)
	}
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	go func() { _ = http.Serve(l, mux) }()
	line, _ := json.Marshal(map[string]string{"devstack": "ready", "url": "http://" + l.Addr().String()})
	fmt.Println(string(line))
	select {}
}
`

// strayLauncher: a child that ignores SIGTERM keeps running after the launcher exits, a real leak.
const strayLauncher = `package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ready := os.Getenv("FAKE_CHILD_READY")
	if os.Getenv("FAKE_ROLE") == "stray" {
		signal.Ignore(syscall.SIGTERM)
		_ = os.WriteFile(ready, nil, 0o600)
		time.Sleep(time.Hour)
	}
	stray := exec.Command(os.Args[0])
	stray.Env = append(os.Environ(), "FAKE_ROLE=stray")
	if err := stray.Start(); err != nil {
		os.Exit(1)
	}
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	go func() { _ = http.Serve(l, mux) }()
	line, _ := json.Marshal(map[string]string{"devstack": "ready", "url": "http://" + l.Addr().String()})
	fmt.Println(string(line))
	<-term
}
`

func launchWithChild(t *testing.T, src string) *App {
	t.Helper()
	env := []string{"FAKE_CHILD_READY=" + filepath.Join(t.TempDir(), "child-ready")}
	app, err := Launch(context.Background(), fakeModule(t, src), env, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	return app
}

// The launcher's group winds down after the process Stop waits on is gone: that is a clean stop,
// not a leak. Stop returns only once the whole group has gone.
func TestStop_WaitsForTheGroupToWindDownAfterTheLauncherExits(t *testing.T) {
	app := launchWithChild(t, slowLauncher)
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop: %v (a group still winding down after the launcher exits is not a leak)", err)
	}
	if syscall.Kill(-app.PGID, 0) == nil {
		t.Errorf("process group %d still has members after a clean Stop", app.PGID)
	}
}

// A member that outlives the grace after the launcher exited is a leak: Stop kills it and says so.
func TestStop_ReportsAndKillsAMemberThatOutlivesTheLauncher(t *testing.T) {
	old := groupGrace
	groupGrace = 2 * time.Second
	t.Cleanup(func() { groupGrace = old })
	app := launchWithChild(t, strayLauncher)
	err := app.Stop()
	if err == nil || !strings.Contains(err.Error(), "left processes in its group") {
		t.Fatalf("Stop = %v, want the leak report", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(-app.PGID, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if syscall.Kill(-app.PGID, 0) == nil {
		t.Errorf("process group %d still has members after Stop killed the leak", app.PGID)
	}
}
