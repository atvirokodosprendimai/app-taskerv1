package web

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// slogFormatter adapts chi's request logger to the application's slog logger,
// so request lines arrive in the same structured stream, at a level, carrying
// the request id.
//
// ⚠ A long-lived response logs only when it ENDS. The dashboard stream lasts as
// long as the tab is open, which is why GetStream logs its own opening.
type slogFormatter struct{ log *slog.Logger }

// NewLogEntry starts a log entry for one request.
func (f slogFormatter) NewLogEntry(r *http.Request) middleware.LogEntry {
	return &slogEntry{log: f.log, r: r}
}

// slogEntry is one request's pending log line.
type slogEntry struct {
	log *slog.Logger
	r   *http.Request
}

// Write emits the line once the response has finished. Severity follows the
// status, so a failing run can be found by level rather than by reading.
func (e *slogEntry) Write(status, bytes int, _ http.Header, elapsed time.Duration, _ any) {
	attrs := []any{
		"method", e.r.Method,
		"path", e.r.URL.Path,
		"status", status,
		"bytes", bytes,
		"elapsed", elapsed.Round(time.Microsecond).String(),
		"request_id", middleware.GetReqID(e.r.Context()),
	}
	switch {
	case status >= 500:
		e.log.Error("request failed", attrs...)
	case status >= 400:
		e.log.Warn("request refused", attrs...)
	default:
		e.log.Info("request", attrs...)
	}
}

// Panic records a panic, with its stack and request id, before chi's Recoverer
// turns it into a 500.
func (e *slogEntry) Panic(v any, stack []byte) {
	e.log.Error("request panicked",
		"method", e.r.Method,
		"path", e.r.URL.Path,
		"panic", v,
		"request_id", middleware.GetReqID(e.r.Context()),
		"stack", string(stack),
	)
}
