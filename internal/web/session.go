package web

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
)

// sessionUserKey is the session field holding the signed-in user's id.
//
// The session carries the id ONLY. The account itself is re-read on every
// request, so an account that disappears stops working at its next request
// rather than when the cookie happens to expire.
const sessionUserKey = "uid"

// SessionLifetime is how long a sign-in lasts.
//
// A time tracker is opened every working day and left open, and a short
// lifetime would sign people out with timers running. Signing out still revokes
// a session at once, because it lives in the database rather than in the cookie.
const SessionLifetime = 30 * 24 * time.Hour

// NewSessions configures the session manager, storing sessions through the
// writer handle.
func NewSessions(write *sql.DB, secure bool) *scs.SessionManager {
	m := scs.New()
	m.Store = sqlite3store.New(write)
	m.Lifetime = SessionLifetime
	m.Cookie.Name = "tasker_session"
	m.Cookie.HttpOnly = true
	m.Cookie.Path = "/"
	m.Cookie.Persist = true
	// Lax rather than Strict: Strict drops the cookie when someone follows a link
	// to the app from elsewhere, landing them on sign-in while signed in. Lax
	// still withholds it from the cross-site POST that SameSite exists to stop.
	m.Cookie.SameSite = http.SameSiteLaxMode
	// Secure is configuration rather than a constant. A Secure cookie is never
	// sent over plain HTTP, so on a local machine sign-in would appear to work
	// and every following request would arrive signed out.
	m.Cookie.Secure = secure
	return m
}

// ctxUserKey types the request-context key for the signed-in user.
type ctxUserKey struct{}

// withUser returns a context carrying the signed-in user.
func withUser(ctx context.Context, u auth.User) context.Context {
	return context.WithValue(ctx, ctxUserKey{}, u)
}

// userFrom returns the user [App.requireUser] put in the context.
func userFrom(ctx context.Context) auth.User {
	u, _ := ctx.Value(ctxUserKey{}).(auth.User)
	return u
}
