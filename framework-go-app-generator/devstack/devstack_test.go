package devstack

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeModule writes a module whose cmd/devstack/main.go is src, the stand-in for a generated
// launcher.
func fakeModule(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":               "module example.com/fake\n\ngo 1.25\n",
		"cmd/devstack/main.go": src,
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A launcher that dies before it is ready must hand its own output to the caller: the CI log is
// otherwise just "exit status 1".
func TestLaunch_ExitBeforeReadyCarriesTheLog(t *testing.T) {
	dir := fakeModule(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "boom")
	os.Exit(3)
}
`)
	_, err := Launch(context.Background(), dir, nil, nil)
	if err == nil {
		t.Fatal("Launch succeeded against a launcher that exits 3")
	}
	for _, want := range []string{"exited before it was ready", "boom", "exit status 3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
}

const readyLauncher = `package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
)

func main() {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	go func() { _ = http.Serve(l, mux) }()
	fmt.Fprintln(os.Stderr, "preset:"+os.Getenv("FAKE_PRESET"))
	line, _ := json.Marshal(map[string]string{"devstack": "ready", "url": "http://" + l.Addr().String()})
	fmt.Println(string(line))
	select {}
}
`

func TestLaunch_ReadyLineThenReadyz(t *testing.T) {
	dir := fakeModule(t, readyLauncher)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var seen strings.Builder
	app, err := Launch(ctx, dir, []string{"FAKE_PRESET=yes"}, &seen)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !strings.HasPrefix(app.BaseURL, "http://127.0.0.1:") {
		t.Errorf("BaseURL = %q", app.BaseURL)
	}
	if app.MCPURL != app.BaseURL+"/mcp" {
		t.Errorf("MCPURL = %q, want %q", app.MCPURL, app.BaseURL+"/mcp")
	}
	if !strings.Contains(app.Log(), "preset:yes") || !strings.Contains(seen.String(), "preset:yes") {
		t.Errorf("env was not passed to the launcher: log=%q tee=%q", app.Log(), seen.String())
	}
	hostport := strings.TrimPrefix(app.BaseURL, "http://")
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := app.Stop(); err != nil {
		t.Errorf("a second Stop = %v, want nil", err)
	}
	if c, err := net.DialTimeout("tcp", hostport, time.Second); err == nil {
		_ = c.Close()
		t.Errorf("%s still accepts connections after Stop", hostport)
	}
}

func TestLastLines(t *testing.T) {
	var b strings.Builder
	for i := range 100 {
		b.WriteString(strings.Repeat("x", i%3+1) + "\n")
	}
	if n := len(strings.Split(lastLines(b.String(), 40), "\n")); n != 40 {
		t.Errorf("lines = %d, want 40", n)
	}
}

func TestDockerLeftovers(t *testing.T) {
	got := DockerLeftovers(context.Background(),
		[]string{"container:a", "volume:v1"},
		[]string{"container:a", "container:b", "volume:v1", "volume:v2"})
	if want := []string{"container:b", "volume:v2"}; !slices.Equal(got, want) {
		t.Errorf("leftovers = %v, want %v", got, want)
	}
	if left := DockerLeftovers(context.Background(), []string{"container:a"}, []string{"container:a"}); len(left) != 0 {
		t.Errorf("no change must leave nothing, got %v", left)
	}
}
