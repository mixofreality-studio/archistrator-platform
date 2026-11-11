package scenariohost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The arrange server is the host's half of the arrange endpoint a web scenario run serves (Plan 3
// Task 3.5): the three typed setup actions over HTTP, and the question the composed server's fault
// wrapper asks before each Manager operation.
func TestArrangeServer_RoundTrip(t *testing.T) {
	h := &Host{}
	srv, err := h.ServeArrange(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	post := func(kind, body string) (int, string) {
		t.Helper()
		res, err := http.Post(srv.URL+"/"+kind, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	take := func(component, op string) (int, string) {
		t.Helper()
		q := url.Values{"component": {component}, "op": {op}}
		res, err := http.Get(srv.URL + "/faults/take?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}

	t.Run("failNext is taken once", func(t *testing.T) {
		if code, text := post("failNext", `{"component":"listManager","operation":"AddTodo","errorCode":"Infrastructure"}`); code != http.StatusOK {
			t.Fatalf("failNext: %d %s", code, text)
		}
		if code, _ := take("listManager", "ListOpenList"); code != http.StatusNoContent {
			t.Errorf("a fault for AddTodo answered %d for another operation, want 204", code)
		}
		code, text := take("listManager", "AddTodo")
		var got struct {
			ErrorCode string `json:"errorCode"`
		}
		if code != http.StatusOK || json.Unmarshal([]byte(text), &got) != nil || got.ErrorCode != "Infrastructure" {
			t.Fatalf("first take: %d %q, want 200 {errorCode: Infrastructure}", code, text)
		}
		if code, _ := take("listManager", "AddTodo"); code != http.StatusNoContent {
			t.Errorf("a fault is one-shot: second take answered %d, want 204", code)
		}
	})

	t.Run("advanceClock moves the host clock", func(t *testing.T) {
		before := time.Now()
		if code, text := post("advanceClock", `{"duration":"36h"}`); code != http.StatusOK {
			t.Fatalf("advanceClock: %d %s", code, text)
		}
		if shift := h.Now().Sub(before); shift < 36*time.Hour {
			t.Errorf("clock shifted by %v, want at least 36h", shift)
		}
	})

	t.Run("a bad body is a 400", func(t *testing.T) {
		for _, c := range []struct{ kind, body string }{
			{"failNext", `not json`},
			{"failNext", `{"component":"c","operation":"o","errorCode":"x","extra":1}`},
			{"failNext", `{"component":"","operation":"o","errorCode":"x"}`},
			{"advanceClock", `{"duration":"soon"}`},
			{"advanceClock", `{"duration":"-1h"}`},
			{"seedResource", `{"resource":`},
		} {
			if code, text := post(c.kind, c.body); code != http.StatusBadRequest {
				t.Errorf("POST /%s %s: %d %s, want 400", c.kind, c.body, code, text)
			}
		}
	})

	t.Run("a failing arrange is a 500 carrying its text", func(t *testing.T) {
		code, text := post("seedResource", `{"resource":"nonesuch","fixture":{}}`)
		if code != http.StatusInternalServerError || !strings.Contains(text, "unknown resource kind") {
			t.Errorf("seedResource of an unknown kind: %d %q, want 500 naming the kind", code, text)
		}
	})

	t.Run("only POST arranges, only GET takes", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/failNext")
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("GET /failNext: %d, want 405", res.StatusCode)
		}
		res, err = http.Post(srv.URL+"/faults/take", "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST /faults/take: %d, want 405", res.StatusCode)
		}
	})
}

// seedResource over the wire reaches the host's database.
func TestArrangeServer_SeedsPostgres(t *testing.T) {
	h := Start(t)
	if h.PostgresURL == "" {
		t.Skip("postgres is not booted under -short")
	}
	srv, err := h.ServeArrange(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/seedResource", "application/json", strings.NewReader(`{"resource":"postgres","fixture":{"sql":"select 1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if raw, _ := io.ReadAll(res.Body); res.StatusCode != http.StatusOK {
		t.Errorf("seedResource postgres: %d %s, want 200", res.StatusCode, raw)
	}
}

// Closing the server undoes what it arranged: a clock moved over the wire is back at real time, and
// the faults nothing took are dropped.
func TestArrangeServer_CloseUndoesWhatItArranged(t *testing.T) {
	h := &Host{}
	srv, err := h.ServeArrange(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for kind, body := range map[string]string{
		"advanceClock": `{"duration":"48h"}`,
		"failNext":     `{"component":"c","operation":"o","errorCode":"x"}`,
	} {
		res, err := http.Post(srv.URL+"/"+kind, "application/json", strings.NewReader(body))
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("POST /%s: %v %v", kind, res, err)
		}
		_ = res.Body.Close()
	}
	srv.Close()
	if shift := time.Until(h.Now()); shift > time.Minute {
		t.Errorf("the advance outlived the server: clock is %v ahead", shift)
	}
	if code, ok := h.TakeFault("c", "o"); ok {
		t.Errorf("a fault nothing took outlived the server: %q", code)
	}
}

// LaunchWeb is the launch a web scenario run makes: the app of LaunchDevstack, also told where the
// host's arrange server listens (<PREFIX>_SCENARIOHOST_URL), the process principal and every
// principal of the plan allowlisted, and a Stop that gives back everything the launch took.
func TestLaunchWeb_HandsTheLauncherTheArrangeURL(t *testing.T) {
	h := Start(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	web, err := h.LaunchWeb(ctx, fakeApp(t), Principal{Subject: "alice", Roles: []string{"member"}})
	if err != nil {
		t.Fatal(err)
	}
	log := web.App.Log()
	if got := envOf(log, "FAKE_SCENARIOHOST_URL"); got == "" || got != web.ArrangeURL {
		t.Errorf("FAKE_SCENARIOHOST_URL = %q, want the arrange server's %q\n%s", got, web.ArrangeURL, log)
	}
	if got := envOf(log, "FAKE_DEV_PRINCIPAL"); got != "alice" {
		t.Errorf("FAKE_DEV_PRINCIPAL = %q, want alice", got)
	}
	db := databaseOf(t, envOf(log, "FAKE_POSTGRES_URL"))
	if err := web.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if databaseExists(t, h, db) {
		t.Errorf("database %q outlived Stop", db)
	}
	if _, err := http.Get(web.ArrangeURL + "/faults/take?component=c&op=o"); err == nil {
		t.Errorf("the arrange server still answers after Stop")
	}
}

// Boot and Close are the host without a testing.T: what a production job binary (go tool
// webscenarios) boots. The Temporal dev server keeps its database in the module root, so a second
// host in this process would collide with the shared one the other tests started: the boot runs in a
// re-executed test binary working in a module root of its own.
func TestBoot_NeedsNoTestingT(t *testing.T) {
	if testing.Short() {
		t.Skip("boots a Temporal dev server and Postgres")
	}
	if os.Getenv("AIARCH_SUBPROCESS") != "boot" {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/bootprobe\n\ngo 1.25\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestBoot_NeedsNoTestingT$", "-test.v")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "AIARCH_SUBPROCESS=boot")
		if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "--- PASS: TestBoot_NeedsNoTestingT") {
			t.Fatalf("the re-executed boot failed: %v\n%s", err, out)
		}
		return
	}
	h, err := Boot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if h.TemporalHostPort == "" || h.PostgresURL == "" {
		t.Errorf("Boot left the host without Temporal (%q) or Postgres (%q)", h.TemporalHostPort, h.PostgresURL)
	}
	h.Close()
}
