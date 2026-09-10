// Package web is the HTTP boundary: routes, handlers, and the read models they
// hand to templ.
//
// Handlers follow the CQRS split. A WRITE goes through a domain service, which
// owns the rules; a READ goes straight to a repository through a read-only
// interface. After a write succeeds the handler tells the [Bus], and every open
// dashboard of that user re-reads what changed.
package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/render"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/view"
)

// UserReader is the read side of accounts that the HTTP layer uses.
type UserReader interface {
	ByID(ctx context.Context, id int64) (auth.User, error)
}

// TrackingReader is the read side of time tracking that the HTTP layer uses.
//
// Handlers are given this rather than the repository, so a handler that meant
// to read has no method with which it could write.
type TrackingReader interface {
	Companies(ctx context.Context, userID int64) ([]tracking.Company, error)
	Running(ctx context.Context, userID int64) ([]tracking.Entry, error)
	Entries(ctx context.Context, userID int64, p tracking.Period, companyID int64, now time.Time, limit int) ([]tracking.Entry, error)
	Totals(ctx context.Context, userID int64, p tracking.Period, companyID int64, now time.Time) ([]tracking.CompanyTotal, error)
}

// App holds the handlers' dependencies.
type App struct {
	Log      *slog.Logger
	Sessions *scs.SessionManager

	// Read side — deliberately the narrow half.
	Users    UserReader
	Tracking TrackingReader

	// Write side.
	Auth    *auth.Service
	Tracker *tracking.Service

	// Bus tells a user's open dashboards to re-read after a write.
	Bus *Bus

	// Now is the clock handlers render with.
	Now func() time.Time

	// Closing, once closed, ends every open stream. The server closes it as it
	// shuts down: http.Server.Shutdown waits for requests to finish but never
	// cancels them, and a dashboard stream never finishes on its own, so without
	// this a single open tab would hold shutdown for its whole timeout.
	Closing <-chan struct{}
}

// page builds the chrome for a signed-in screen.
func (a *App) page(u auth.User, title, nav string) view.Page {
	return view.Page{Title: title, Nav: nav, User: u}
}

// userMessage turns a domain error into a sentence for the person using the
// page.
//
// Anything unrecognised is logged and replaced: an unexpected error's text can
// carry a query or a driver detail, none of which belongs on a screen.
func (a *App) userMessage(err error) string {
	switch {
	case errors.Is(err, tracking.ErrCompanyNameRequired):
		return "Type a company name first."
	case errors.Is(err, tracking.ErrCompanyNameTooLong):
		return fmt.Sprintf("Keep the company name to %d characters or fewer.", tracking.MaxCompanyName)
	case errors.Is(err, tracking.ErrCompanyExists):
		return "You already have a company with that name."
	case errors.Is(err, tracking.ErrTaskTooLong):
		return fmt.Sprintf("Keep the task name to %d characters or fewer.", tracking.MaxTask)
	case errors.Is(err, tracking.ErrNotFound):
		return "That company or timer no longer exists. The page has been brought up to date."
	case errors.Is(err, auth.ErrInvalidEmail):
		return "Enter a valid email address, like name@example.com."
	case errors.Is(err, auth.ErrPasswordTooShort):
		return fmt.Sprintf("Use at least %d characters for the password.", auth.MinPasswordLength)
	case errors.Is(err, auth.ErrPasswordTooLong):
		return fmt.Sprintf("That password is too long. Use %d characters or fewer.", auth.MaxPasswordBytes)
	case errors.Is(err, auth.ErrEmailTaken):
		return "An account with that email already exists. Sign in instead."
	}
	a.Log.Error("unexpected error", "err", err)
	return "Something went wrong on our side. Please try again."
}

// flash sends one message into the page's message slot and nothing else.
func (a *App) flash(w http.ResponseWriter, r *http.Request, msg string) {
	sse := render.NewSSE(w, r)
	_ = sse.PatchElementTempl(view.Flash(msg))
}
