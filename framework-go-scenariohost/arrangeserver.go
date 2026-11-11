package scenariohost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// The arrange server (Plan 3 Task 3.5): the host's half of the arrange endpoint of a web scenario
// run. A browser scenario cannot call h.FailNext or h.SeedResource - it is a Playwright test in
// another process - so the run serves the three typed setup actions over HTTP, the composed server
// forwards the browser test's POST /__scenariohost/<kind> here (composegen's arrangeForwarder), and
// the server's generated fault wrapper asks GET /faults/take before each Manager operation.

// ArrangeServer is a running arrange server.
type ArrangeServer struct {
	// URL is "http://127.0.0.1:<port>".
	URL string
	// Close stops serving and undoes what the server arranged that nothing consumed: the clock
	// returns to real time and the pending faults are dropped. It is safe to call twice.
	Close func()
}

// arrangeBodyLimit bounds a request body: a fixture is SQL or a few files.
const arrangeBodyLimit = 4 << 20

// arrangeShutdown bounds the wait for in-flight arrange calls when the server closes.
const arrangeShutdown = 5 * time.Second

// ServeArrange serves the arrange endpoint on a free loopback port until the returned server is
// closed or ctx ends:
//
//	POST /advanceClock {"duration"}                          -> h.advanceClock
//	POST /failNext {"component","operation","errorCode"}     -> h.failNext
//	POST /seedResource {"resource","fixture"}                -> h.seedResource
//	GET  /faults/take?component=&op=                         -> 200 {"errorCode"} | 204 (h.TakeFault)
//
// A body that does not decode strictly, or names nothing to arrange, is a 400; an arrange that fails
// is a 500 carrying the error text.
func (h *Host) ServeArrange(ctx context.Context) (*ArrangeServer, error) {
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("scenariohost: arrange server: %w", err)
	}
	a := &arrangeHandler{h: h}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /advanceClock", a.advanceClock)
	mux.HandleFunc("POST /failNext", a.failNext)
	mux.HandleFunc("POST /seedResource", a.seedResource)
	mux.HandleFunc("GET /faults/take", a.takeFault)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(l) }()

	var once sync.Once
	closeFn := func() {
		once.Do(func() {
			sctx, cancel := context.WithTimeout(context.Background(), arrangeShutdown)
			defer cancel()
			_ = srv.Shutdown(sctx)
			a.undoAll()
		})
	}
	go func() {
		<-ctx.Done()
		closeFn()
	}()
	return &ArrangeServer{URL: "http://" + l.Addr().String(), Close: closeFn}, nil
}

// arrangeHandler holds what the server arranged, to undo it on close.
type arrangeHandler struct {
	h     *Host
	mu    sync.Mutex
	undos []func()
}

func (a *arrangeHandler) keep(undo func()) {
	a.mu.Lock()
	a.undos = append(a.undos, undo)
	a.mu.Unlock()
}

func (a *arrangeHandler) undoAll() {
	a.mu.Lock()
	undos := a.undos
	a.undos = nil
	a.mu.Unlock()
	for i := len(undos) - 1; i >= 0; i-- {
		undos[i]()
	}
}

// decodeBody decodes the request's JSON body into v, refusing unknown fields and trailing data.
func decodeBody(r *http.Request, v any) error {
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, arrangeBodyLimit))
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}

func (a *arrangeHandler) advanceClock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Duration string `json:"duration"`
	}
	if err := decodeBody(r, &body); err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	d, err := time.ParseDuration(body.Duration)
	if err != nil {
		http.Error(w, "bad body: duration: "+err.Error(), http.StatusBadRequest)
		return
	}
	undo, err := a.h.advanceClock(d)
	if err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	a.keep(undo)
	w.WriteHeader(http.StatusOK)
}

func (a *arrangeHandler) failNext(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Component string `json:"component"`
		Operation string `json:"operation"`
		ErrorCode string `json:"errorCode"`
	}
	if err := decodeBody(r, &body); err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	undo, err := a.h.failNext(body.Component, body.Operation, body.ErrorCode)
	if err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	a.keep(undo)
	w.WriteHeader(http.StatusOK)
}

func (a *arrangeHandler) seedResource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Resource string          `json:"resource"`
		Fixture  json.RawMessage `json:"fixture"`
	}
	if err := decodeBody(r, &body); err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.h.seedResource(r.Context(), body.Resource, body.Fixture); err != nil {
		http.Error(w, fmt.Sprintf("seedResource %q: %v", body.Resource, err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *arrangeHandler) takeFault(w http.ResponseWriter, r *http.Request) {
	code, ok := a.h.TakeFault(r.URL.Query().Get("component"), r.URL.Query().Get("op"))
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"errorCode": code})
}
