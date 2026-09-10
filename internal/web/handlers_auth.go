package web

import (
	"errors"
	"net/http"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/render"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/view"
)

// loginSignals is what the sign-in screen sends.
type loginSignals struct {
	Email    string `json:"loginEmail"`
	Password string `json:"loginPassword"`
}

// registerSignals is what the registration screen sends.
type registerSignals struct {
	Email    string `json:"registerEmail"`
	Password string `json:"registerPassword"`
	// Timezone is filled by the page from the browser, not typed.
	Timezone string `json:"registerTimezone"`
}

// GetLogin renders sign-in, or sends someone already signed in to their timers.
func (a *App) GetLogin(w http.ResponseWriter, r *http.Request) {
	if a.Sessions.GetInt64(r.Context(), sessionUserKey) != 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	_ = view.AuthPage(view.Auth{}).Render(r.Context(), w)
}

// GetRegister renders registration, or sends someone already signed in to
// their timers.
func (a *App) GetRegister(w http.ResponseWriter, r *http.Request) {
	if a.Sessions.GetInt64(r.Context(), sessionUserKey) != 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	_ = view.AuthPage(view.Auth{Register: true}).Render(r.Context(), w)
}

// PostLogin checks an email and password and starts a session.
func (a *App) PostLogin(w http.ResponseWriter, r *http.Request) {
	// Signals are read BEFORE the stream opens: opening it flushes the response,
	// after which the request body can no longer be read.
	var in loginSignals
	if err := datastar.ReadSignals(r, &in); err != nil {
		a.authMessage(w, r, "We could not read the form. Reload the page and try again.")
		return
	}
	u, err := a.Auth.Authenticate(r.Context(), in.Email, in.Password)
	switch {
	case errors.Is(err, auth.ErrBadCredentials):
		// One message for every failure: telling "no such account" from "wrong
		// password" would make this screen a way to find out who has an account.
		a.authMessage(w, r, "That email and password do not match an account.")
		return
	case err != nil:
		a.Log.Error("sign-in failed", "err", err)
		a.authMessage(w, r, "Signing in is not working right now. Please try again shortly.")
		return
	}
	a.signIn(w, r, u)
}

// PostRegister creates an account and signs it in.
func (a *App) PostRegister(w http.ResponseWriter, r *http.Request) {
	var in registerSignals
	if err := datastar.ReadSignals(r, &in); err != nil {
		a.authMessage(w, r, "We could not read the form. Reload the page and try again.")
		return
	}
	u, err := a.Auth.Register(r.Context(), in.Email, in.Password, in.Timezone)
	if err != nil {
		a.authMessage(w, r, a.userMessage(err))
		return
	}
	a.Log.Info("account registered", "user_id", u.ID, "timezone", u.Timezone)
	a.signIn(w, r, u)
}

// signIn renews the session token, records who is signed in, and sends the
// browser to their timers.
func (a *App) signIn(w http.ResponseWriter, r *http.Request, u auth.User) {
	// Renewing the token before recording the user prevents session fixation: a
	// token planted before sign-in is discarded rather than promoted.
	if err := a.Sessions.RenewToken(r.Context()); err != nil {
		a.Log.Error("renew session token", "err", err)
		a.authMessage(w, r, "Signing in is not working right now. Please try again shortly.")
		return
	}
	a.Sessions.Put(r.Context(), sessionUserKey, u.ID)

	sse := render.NewSSE(w, r)
	_ = sse.Redirect("/")
}

// PostLogout ends the session.
func (a *App) PostLogout(w http.ResponseWriter, r *http.Request) {
	if err := a.Sessions.Destroy(r.Context()); err != nil {
		a.Log.Warn("destroy session", "err", err)
	}
	sse := render.NewSSE(w, r)
	_ = sse.Redirect("/login")
}

// authMessage shows a message on the sign-in or registration screen without
// leaving it.
func (a *App) authMessage(w http.ResponseWriter, r *http.Request, msg string) {
	sse := render.NewSSE(w, r)
	_ = sse.PatchElementTempl(view.AuthMessage(msg))
}
