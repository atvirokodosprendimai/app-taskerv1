package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/render"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/view"
)

// Refusals about WHEN logged time happened. They concern the shape of what was
// typed rather than a rule of the domain, so they live here, beside the parsing.
var (
	errStartTimeRequired = errors.New("web: a start time is required for a day other than today")
	errInvalidWhen       = errors.New("web: the day or start time does not parse")
)

// GetLogDialog opens the log-time dialog for one of the user's companies.
func (a *App) GetLogDialog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	c, err := a.ownCompany(ctx, u, chi.URLParam(r, "id"))
	sse := render.NewSSE(w, r)
	if err != nil {
		_ = sse.PatchElementTempl(view.NoDialog())
		_ = sse.PatchElementTempl(view.Flash(a.logRefusal(err)))
		return
	}
	_ = sse.PatchElementTempl(view.LogDialog(view.LogForm{
		CompanyID:   c.ID,
		CompanyName: c.Name,
		Today:       a.Now().In(u.Location()).Format(tracking.DateLayout),
	}))
}

// GetCloseLogDialog takes the dialog away.
func (a *App) GetCloseLogDialog(w http.ResponseWriter, r *http.Request) {
	sse := render.NewSSE(w, r)
	a.closeLogDialog(sse, chi.URLParam(r, "id"))
}

// PostLog adds time spent earlier — a phone call, a meeting — to the history.
//
// The company is looked up first, so someone else's company is refused before
// anything about what was typed, and the confirmation can name it. The INSERT
// checks ownership again by itself; this lookup is for the words.
func (a *App) PostLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in view.LogSignals
	if err := datastar.ReadSignals(r, &in); err != nil {
		a.logMessage(w, r, "We could not read that. Close the dialog and try again.")
		return
	}
	c, err := a.ownCompany(ctx, u, chi.URLParam(r, "id"))
	if err != nil {
		a.logMessage(w, r, a.logRefusal(err))
		return
	}
	d, err := tracking.ParseDuration(in.Duration)
	if err != nil {
		a.logMessage(w, r, a.logRefusal(err))
		return
	}
	loc := u.Location()
	start, err := logStart(in.Date, in.At, d, a.Now().In(loc))
	if err != nil {
		a.logMessage(w, r, a.logRefusal(err))
		return
	}
	if _, err := a.Tracker.Log(ctx, u.ID, c.ID, in.Task, start, d); err != nil {
		a.logMessage(w, r, a.logRefusal(err))
		return
	}
	// Nothing is broadcast. An open dashboard shows running timers and
	// companies, and logged time changes neither; the history is read fresh
	// whenever it is opened.

	day := tracking.Filter{Mode: tracking.ModeDay, Day: start.In(loc).Format(tracking.DateLayout)}
	sse := render.NewSSE(w, r)
	_ = sse.PatchElementTempl(view.Notice("Added "+view.HoursMinutes(d)+" to "+c.Name+".",
		"/history?"+historyQuery(day).Encode(), "See that day"))
	a.closeLogDialog(sse, chi.URLParam(r, "id"))
}

// closeLogDialog empties the dialog slot and puts focus back on the company's
// Log time button, so someone using the keyboard carries on from where they
// were instead of starting again from the top of the page.
func (a *App) closeLogDialog(sse *datastar.ServerSentEventGenerator, idParam string) {
	_ = sse.PatchElementTempl(view.NoDialog())
	if id, err := strconv.ParseInt(idParam, 10, 64); err == nil {
		// The id went through ParseInt, so nothing typed can reach the script.
		_ = sse.ExecuteScript(fmt.Sprintf("document.getElementById(%q)?.focus()", view.LogOpenerID(id)))
	}
}

// logMessage shows a refusal inside the dialog, leaving what was typed.
func (a *App) logMessage(w http.ResponseWriter, r *http.Request, msg string) {
	sse := render.NewSSE(w, r)
	_ = sse.PatchElementTempl(view.LogMessage(msg))
}

// logRefusal words a refusal for the log-time dialog. A company that is not
// found gets its own sentence, because the general one promises that the page
// has been brought up to date, which this path does not do.
func (a *App) logRefusal(err error) string {
	if errors.Is(err, tracking.ErrNotFound) {
		return "That company no longer exists. Reload the page to bring it up to date."
	}
	return a.userMessage(err)
}

// ownCompany finds one of the user's companies by the id in a URL. Another
// user's company and a company nobody has are the same answer, ErrNotFound.
func (a *App) ownCompany(ctx context.Context, u auth.User, idParam string) (tracking.Company, error) {
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return tracking.Company{}, tracking.ErrNotFound
	}
	companies, err := a.Tracking.Companies(ctx, u.ID)
	if err != nil {
		return tracking.Company{}, err
	}
	for _, c := range companies {
		if c.ID == id {
			return c, nil
		}
	}
	return tracking.Company{}, tracking.ErrNotFound
}

// logStart works out when logged time began, from the dialog's day and start
// time, in the user's location — which now must already be in.
//
// With no start time, time logged for today ends now: the call that has just
// finished. Another day needs a start time, because there is no honest default
// for when a call on some other day began, and a guessed one would put it in the
// wrong place in the history.
func logStart(date, at string, d time.Duration, now time.Time) (time.Time, error) {
	date, at = strings.TrimSpace(date), strings.TrimSpace(at)
	today := now.Format(tracking.DateLayout)
	if date == "" {
		date = today
	}
	if _, err := time.ParseInLocation(tracking.DateLayout, date, now.Location()); err != nil {
		return time.Time{}, errInvalidWhen
	}
	if at == "" {
		if date != today {
			return time.Time{}, errStartTimeRequired
		}
		return now.Add(-d), nil
	}
	// A time picker sends "15:04", or "15:04:05" when it offers seconds.
	for _, layout := range []string{"15:04", "15:04:05"} {
		if start, err := time.ParseInLocation(tracking.DateLayout+" "+layout, date+" "+at, now.Location()); err == nil {
			return start, nil
		}
	}
	return time.Time{}, errInvalidWhen
}
