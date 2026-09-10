package render_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/render"
)

// finishesHeadersOnWrite stands in for middleware that adds a header at the
// moment the response starts — the session manager adds its cookie there — and
// that, like it, offers Unwrap but no Flush.
type finishesHeadersOnWrite struct {
	http.ResponseWriter
	started bool
}

func (f *finishesHeadersOnWrite) start() {
	if !f.started {
		f.started = true
		f.Header().Set("Set-Cookie", "session=renewed")
	}
}

func (f *finishesHeadersOnWrite) WriteHeader(code int) {
	f.start()
	f.ResponseWriter.WriteHeader(code)
}

func (f *finishesHeadersOnWrite) Write(b []byte) (int, error) {
	f.start()
	return f.ResponseWriter.Write(b)
}

func (f *finishesHeadersOnWrite) Unwrap() http.ResponseWriter { return f.ResponseWriter }

// Opening a stream flushes the headers before anything is written. A flush
// through http.ResponseController unwraps past a writer with no Flush of its
// own, so unless the stream starts the response through the middleware first,
// the headers leave without what the middleware adds: sign-in "succeeds" and the
// next request arrives signed out.
func TestOpeningAStreamLetsMiddlewareFinishTheHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)

	sse := render.NewSSE(&finishesHeadersOnWrite{ResponseWriter: rec}, req)
	if err := sse.Redirect("/"); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Result reports the headers as they were when the response started, which
	// is what the browser received.
	res := rec.Result()
	if got := res.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream: the stream's own headers must still arrive", got)
	}
	if got := res.Header.Get("Set-Cookie"); got != "session=renewed" {
		t.Errorf("Set-Cookie = %q, want the header the middleware adds as the response starts", got)
	}
}
