package telemetry

import (
	"context"
	"log/slog"
)

// fanoutHandler is a slog.Handler that forwards every record to several delegate
// handlers. The telemetry setup uses it to keep the service's existing stdout JSON
// logging while ADDITIONALLY emitting each record to the OTel log bridge (so logs
// reach the collector and carry the active trace/span ids). It is unexported — the
// only constructor is the one Setup calls.
type fanoutHandler struct {
	handlers []slog.Handler
}

func newFanoutHandler(handlers ...slog.Handler) slog.Handler {
	return &fanoutHandler{handlers: handlers}
}

// Enabled reports true if ANY delegate is enabled at the level, so a record is
// dropped only when no delegate wants it.
func (h *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, d := range h.handlers {
		if d.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle forwards the record to every delegate that is enabled at its level. The
// record is cloned per delegate because slog.Handler.Handle may retain or mutate
// it. The first delegate error is returned (after attempting all delegates) so one
// failing sink does not silently swallow the rest.
func (h *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, d := range h.handlers {
		if !d.Enabled(ctx, r.Level) {
			continue
		}
		if err := d.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, d := range h.handlers {
		next[i] = d.WithAttrs(attrs)
	}
	return &fanoutHandler{handlers: next}
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, d := range h.handlers {
		next[i] = d.WithGroup(name)
	}
	return &fanoutHandler{handlers: next}
}
