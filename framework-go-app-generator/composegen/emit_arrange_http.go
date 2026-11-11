package composegen

import (
	"go/format"
)

// ArrangeFile is the generated arrange forwarder of a container that serves web-exposed Managers,
// beside main.gen.go. A web scenario run (go tool webscenarios, Plan 3 Task 3.5) serves the arrange
// endpoint - advanceClock, failNext, seedResource - on its own address; the browser test reaches it
// through the composed server's origin, which forwards /__scenariohost/<kind> there. The mount exists
// only when the generated Config yields a scenario host address, which it does under the local
// profile alone (configgen's LocalScenariohostURL).
const ArrangeFile = "arrange.gen.go"

// emitArrangeHTTP renders ArrangeFile.
func emitArrangeHTTP(r *resolved) ([]byte, error) {
	src := genHeader + "\npackage " + r.pkgName + arrangeBody
	out, err := format.Source([]byte(src))
	if err != nil {
		return nil, wrapFormatErr(err, src)
	}
	return out, nil
}

const arrangeBody = `

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// arrangeTimeout bounds one forwarded arrange call (a seedResource may run SQL, nothing here waits
// longer than a test would).
const arrangeTimeout = 30 * time.Second

// arrangeForwarder serves POST /__scenariohost/<kind> by forwarding it, as POST /<kind>, to the
// scenario host's arrange server at target. Any other method is refused with 405.
func arrangeForwarder(target string) http.Handler {
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "scenario host address is not a URL: "+target, http.StatusBadGateway)
		})
	}
	proxy := &httputil.ReverseProxy{
		Transport: &http.Transport{ResponseHeaderTimeout: arrangeTimeout},
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(u)
			kind := strings.TrimPrefix(strings.TrimPrefix(pr.In.URL.Path, "/__scenariohost"), "/")
			pr.Out.URL.Path = strings.TrimSuffix(u.Path, "/") + "/" + kind
			pr.Out.URL.RawPath = ""
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "the arrange endpoint takes POST only", http.StatusMethodNotAllowed)
			return
		}
		proxy.ServeHTTP(w, req)
	})
}
`
