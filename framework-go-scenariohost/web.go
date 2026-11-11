package scenariohost

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// WebApp is the composed app a web scenario run drives (go tool webscenarios): launched into the
// host's substrates like LaunchDevstack's, and told where the host's arrange server listens, so the
// browser test's POST /__scenariohost/<kind> reaches the host and the app's Managers take the faults
// it registered.
type WebApp struct {
	// App is the launched app; its BaseURL is what the browser tests run against.
	App *App
	// ArrangeURL is the arrange server the app was told of (<PREFIX>_SCENARIOHOST_URL).
	ArrangeURL string
	// Stop stops the app, the arrange server and gives back the launch's database and namespace
	// bookkeeping. It returns what could not be given back, and is safe to call twice.
	Stop func() error
}

// LaunchWeb launches the app at moduleDir ("" = the host's module root) for a web scenario run: the
// launch of LaunchDevstack, without a testing.T, plus the host's arrange server (ServeArrange) whose
// address the app is given. The first principal is the app's process principal and every principal
// given is allowlisted, as in LaunchDevstack. A failed launch gives back what it took before it
// returns the error.
func (h *Host) LaunchWeb(ctx context.Context, moduleDir string, principals ...Principal) (*WebApp, error) {
	arrange, err := h.ServeArrange(ctx)
	if err != nil {
		return nil, err
	}
	s := &session{ctx: ctx}
	var app *App
	if err := s.do(func() { app = h.launchApp(s, moduleDir, principals, arrange.URL) }); err != nil {
		arrange.Close()
		return nil, errors.Join(err, s.close())
	}
	var once sync.Once
	var stopErr error
	stop := func() error {
		once.Do(func() {
			stopErr = errors.Join(app.Stop(), s.close())
			arrange.Close()
		})
		return stopErr
	}
	return &WebApp{App: app, ArrangeURL: arrange.URL, Stop: stop}, nil
}

// session is launchT for a caller with no testing.T: Fatalf and Skip end the launch (do returns
// them as errors), Cleanup registers what close gives back, in reverse order.
type session struct {
	ctx      context.Context
	mu       sync.Mutex
	cleanups []func()
	errs     []error
}

// fatal is the panic value a session's Fatalf and Skip end a launch with.
type fatal struct{ msg string }

func (s *session) Helper() {}

func (s *session) Fatalf(format string, args ...any) { panic(fatal{fmt.Sprintf(format, args...)}) }

func (s *session) Skip(args ...any) { panic(fatal{"skipped: " + fmt.Sprint(args...)}) }

func (s *session) Errorf(format string, args ...any) {
	s.mu.Lock()
	s.errs = append(s.errs, fmt.Errorf(format, args...))
	s.mu.Unlock()
}

func (s *session) Cleanup(f func()) {
	s.mu.Lock()
	s.cleanups = append(s.cleanups, f)
	s.mu.Unlock()
}

func (s *session) Context() context.Context { return s.ctx }

// do runs f, returning the Fatalf or Skip that ended it as an error.
func (s *session) do(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			fa, ok := r.(fatal)
			if !ok {
				panic(r)
			}
			err = errors.New(fa.msg)
		}
	}()
	f()
	return nil
}

// close runs the registered cleanups, last first, and returns what they reported.
func (s *session) close() error {
	s.mu.Lock()
	cleanups := s.cleanups
	s.cleanups = nil
	s.mu.Unlock()
	for i := len(cleanups) - 1; i >= 0; i-- {
		if err := s.do(cleanups[i]); err != nil {
			s.Errorf("%v", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return errors.Join(s.errs...)
}
