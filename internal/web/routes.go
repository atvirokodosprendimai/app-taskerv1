package web

import (
	"embed"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/render"
)

// assets holds the stylesheet and icon, served from the binary so a deployment
// is one file with no static directory beside it to fall out of step.
//
//go:embed assets
var assets embed.FS

// ReadHeaderTimeout bounds how long a client may take to send its headers.
//
// ⚠ There is deliberately NO WriteTimeout on the server this is used with. A
// write deadline bounds the whole response, and a dashboard stream is a response
// that lasts as long as the tab is open — so any value kills every healthy
// stream at that mark, with no error in the handler and nothing in the log.
const ReadHeaderTimeout = 10 * time.Second

// Routes builds the HTTP handler.
//
// The route table is the clearest statement of the trust boundaries, so it is
// kept in one place: everything outside the group is public, everything inside
// requires a signed-in user.
func (a *App) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RequestLogger(slogFormatter{log: a.Log}))
	r.Use(middleware.Recoverer)
	r.Use(requireDatastarAction)
	r.Use(a.Sessions.LoadAndSave)

	r.Handle("/assets/*", staticHandler())
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/assets/favicon.svg", http.StatusMovedPermanently)
	})
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	r.Get("/login", a.GetLogin)
	r.Post("/login", a.PostLogin)
	r.Get("/register", a.GetRegister)
	r.Post("/register", a.PostRegister)
	r.Post("/logout", a.PostLogout)

	r.Group(func(r chi.Router) {
		r.Use(a.requireUser)

		r.Get("/", a.GetDashboard)
		// The dashboard's one stream: running timers every second, companies on
		// a change.
		r.Get("/stream", a.GetStream)
		r.Post("/companies", a.PostCompany)
		r.Post("/companies/{id}/timers", a.PostStart)
		r.Post("/timers/{id}/stop", a.PostStop)
		// Time logged by hand. The server sends the dialog and takes it away, so
		// opening and closing are round trips rather than a toggle hidden in the
		// page.
		r.Get("/companies/{id}/log", a.GetLogDialog)
		r.Get("/companies/{id}/log/close", a.GetCloseLogDialog)
		r.Post("/companies/{id}/entries", a.PostLog)

		// One entry, running or in the history: its task name put right, or the
		// entry deleted — softly, so the confirmation can offer Undo. An action
		// taken on the history page says so with ?from=history, and that page's
		// results are rendered again for the filter it sends.
		r.Get("/entries/{id}/edit", a.GetEntryDialog)
		r.Get("/entries/{id}/edit/close", a.GetCloseEntryDialog)
		r.Post("/entries/{id}/task", a.PostEntryTask)
		r.Post("/entries/{id}/delete", a.PostEntryDelete)
		r.Post("/entries/{id}/restore", a.PostEntryRestore)

		r.Get("/history", a.GetHistory)
		r.Get("/history/results", a.GetHistoryResults)
		// The period on screen as a plain-text report, downloaded by a plain link.
		r.Get("/history/export", a.GetHistoryExport)
	})
	return r
}

// requireDatastarAction refuses a state-changing request that a datastar action
// did not send.
//
// This is the cross-site request forgery defence. Every datastar request carries
// a Datastar-Request header, and a page on another origin cannot add a custom
// header to a request without the browser first asking this server's permission
// in a preflight, which it never grants. SameSite=Lax on the session cookie is
// the second layer, not the only one.
func requireDatastarAction(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("Datastar-Request") == "" {
				http.Error(w, "This action has to be sent from the application's own page.", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireUser resolves the signed-in user, re-reading the account on every
// request, and sends anyone else to sign in.
func (a *App) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := a.Sessions.GetInt64(r.Context(), sessionUserKey)
		if id == 0 {
			a.toLogin(w, r)
			return
		}
		u, err := a.Users.ByID(r.Context(), id)
		if errors.Is(err, auth.ErrNotFound) {
			// The account is gone: drop the session rather than keep a cookie
			// that will be refused on every future request.
			_ = a.Sessions.Destroy(r.Context())
			a.toLogin(w, r)
			return
		}
		if err != nil {
			a.Log.Error("load signed-in user", "err", err)
			http.Error(w, "The database is not reachable right now. Please try again.", http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

// toLogin sends a signed-out visitor to sign in.
//
// A datastar request is told to navigate over SSE instead of being given a 303,
// which it would not follow: the action would swallow it and the page would look
// as if nothing had happened.
func (a *App) toLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Datastar-Request") != "" {
		sse := render.NewSSE(w, r)
		_ = sse.Redirect("/login")
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// staticHandler serves the embedded assets.
func staticHandler() http.Handler {
	fs := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The assets change only when the binary does, and there is no content
		// hash in their URLs to bust a cache with, so an hour rather than forever.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fs.ServeHTTP(w, r)
	})
}
