// Package render opens datastar SSE streams with the settings a long-lived
// stream needs.
//
// Nothing in this application calls datastar.NewSSE directly. Three things must
// be right before the first byte, and each is invisible when wrong: the write
// deadline must be cleared, or a healthy stream dies partway through a session
// with no error anywhere; proxies must be told not to buffer, or a stream
// arrives as one delivery when it ends; and the response must be started
// through the middleware wrapping it, or a header that middleware adds as the
// response starts — the session cookie — never leaves. One constructor is what
// stops each new handler having to remember them.
package render

import (
	"net/http"
	"time"

	"github.com/starfederation/datastar-go/datastar"
)

// NewSSE opens a datastar stream on w.
//
// ⚠ Read request signals BEFORE calling this. Opening the stream flushes the
// response, after which the request body can no longer be read.
func NewSSE(w http.ResponseWriter, r *http.Request) *datastar.ServerSentEventGenerator {
	// A zero time means "no deadline". The error is ignored on purpose: not
	// every ResponseWriter supports deadlines (an httptest recorder does not),
	// and a stream that cannot set one still works.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	// Nginx and similar proxies buffer responses by default, which turns a
	// stream into a single delivery at the end. Proxies that do not buffer
	// ignore the header.
	w.Header().Set("X-Accel-Buffering", "no")
	return datastar.NewSSE(&startsOnFlush{ResponseWriter: w}, r)
}

// startsOnFlush makes the first flush start the response through the writer it
// wraps.
//
// datastar.NewSSE flushes the headers before it writes anything, and
// http.ResponseController flushes by unwrapping to the first writer that can.
// A middleware writer that finishes the headers in WriteHeader but has no Flush
// of its own is stepped over — the session manager's is one — so the headers
// leave without the cookie it adds, which then lands in a header map nobody
// sends. Calling WriteHeader down the chain first runs that middleware while its
// header can still travel.
type startsOnFlush struct {
	http.ResponseWriter
	started bool
}

func (s *startsOnFlush) WriteHeader(code int) {
	s.started = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *startsOnFlush) Write(b []byte) (int, error) {
	s.started = true
	return s.ResponseWriter.Write(b)
}

// FlushError starts the response if nothing has started it yet, then flushes.
// http.ResponseController looks for this method before it unwraps.
func (s *startsOnFlush) FlushError() error {
	if !s.started {
		s.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(s.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController reach the writers beneath, for deadlines.
func (s *startsOnFlush) Unwrap() http.ResponseWriter { return s.ResponseWriter }
