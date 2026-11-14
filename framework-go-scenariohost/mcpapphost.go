package scenariohost

import (
	"context"
	_ "embed" // the host page and its bundle
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// hostPage and hostBundle are the harness MCP host: index.html names the surface it opens in its
// <body data-surface>, and host.js (built from mcpapphost/src/host.ts; see build.mjs) connects to the
// app's /mcp, reads the surface's ui:// resource and renders it in a sandboxed iframe.
//
//go:embed mcpapphost/index.html
var hostPage string

//go:embed mcpapphost/dist/host.js
var hostBundle []byte

// AppHost is the MCP Apps harness host an mcpApp surface's generated specs run in: a page that
// connects to the launched app's /mcp (proxied on the host's own origin), opens the surface's ui://
// resource in a sandboxed iframe and bridges it, as a real MCP host does.
type AppHost struct {
	URL   string       // the host page's origin: Playwright's baseURL
	Close func() error // stops the host's listener; safe to call twice
}

// surfaceID is the shape of a surface id the host page can carry: kebab-case, as webgen requires.
var surfaceID = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ServeMCPAppHost serves the harness host for the surface of the launched app: the page at /, its
// bundle at /host.js and a reverse proxy of every other route, /mcp first, to the app (so the host's
// MCP client is same-origin and the app needs no CORS). It listens on 127.0.0.1 on a free port until Close or ctx ends.
func ServeMCPAppHost(ctx context.Context, app *App, surface string) (*AppHost, error) {
	if !surfaceID.MatchString(surface) {
		return nil, fmt.Errorf("scenariohost: mcp app host: surface %q is not a kebab-case surface id", surface)
	}
	target, err := url.Parse(app.BaseURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("scenariohost: mcp app host: the app's base URL %q is not an origin: %v", app.BaseURL, err)
	}
	page := strings.ReplaceAll(hostPage, "@@SURFACE@@", surface)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(page))
	})
	mux.HandleFunc("GET /host.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(hostBundle)
	})
	// Everything else is the app's: /mcp for the host's MCP client, and the REST routes and the
	// arrange endpoint a spec's arrange, probes and contract calls reach through the same origin
	// (their base URL is this host's).
	mux.Handle("/", mcpProxy(target))

	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("scenariohost: mcp app host: %w", err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	var once sync.Once
	var closeErr error
	closeHost := func() error {
		once.Do(func() {
			shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			closeErr = srv.Shutdown(shutdown)
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				closeErr = errors.Join(closeErr, err)
			}
		})
		return closeErr
	}
	go func() {
		<-ctx.Done()
		_ = closeHost()
	}()
	return &AppHost{URL: "http://" + ln.Addr().String(), Close: closeHost}, nil
}

// mcpProxy forwards a request to the app unchanged (body, Mcp-Session-Id and the event stream a
// Streamable HTTP response may be), flushing as it reads.
func mcpProxy(target *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	return proxy
}
