package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/render"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/view"
)

// historyLimit bounds how many entries the history lists. The totals cover
// every entry whatever it is; it only stops a busy year being rendered into one
// page.
const historyLimit = 500

// GetHistory renders the history screen for the filter named in the query
// string, or for this month when it names none.
//
// The filter lives in the URL, and the results handler keeps the address bar in
// step with it, so the period someone is looking at survives a reload and can be
// bookmarked.
func (a *App) GetHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	now := a.Now()
	f := filterFromQuery(r.URL.Query(), now.In(u.Location()))

	companies, err := a.Tracking.Companies(ctx, u.ID)
	if err != nil {
		a.Log.Error("load history companies", "err", err)
		http.Error(w, "Your history could not be loaded. Please reload the page.", http.StatusInternalServerError)
		return
	}
	res, err := a.history(ctx, u, f, now)
	if err != nil {
		a.Log.Error("load history", "err", err)
		http.Error(w, "Your history could not be loaded. Please reload the page.", http.StatusInternalServerError)
		return
	}
	_ = view.HistoryPage(view.History{
		Page:      a.page(u, "History", "history"),
		Filter:    f,
		Companies: companies,
		Results:   res,
	}).Render(ctx, w)
}

// GetHistoryResults re-renders the results for the filter the page sends.
//
// A quick-period button adds ?preset= and the stepper adds ?shift=; either is
// applied on top of the filter the signals describe. When one of them moves the
// filter, the page's signals are patched to match, so the inputs show the period
// the results are for.
func (a *App) GetHistoryResults(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	loc := u.Location()
	now := a.Now()

	var in view.HistorySignals
	if err := datastar.ReadSignals(r, &in); err != nil {
		a.flash(w, r, "We could not read the filter. Reload the page and try again.")
		return
	}
	f := in.Filter(now.In(loc))
	moved := false
	q := r.URL.Query()
	if p, ok := presetFilter(q.Get("preset"), f, now.In(loc)); ok {
		f, moved = p, true
	}
	if n, err := strconv.Atoi(q.Get("shift")); err == nil && (n == -1 || n == 1) {
		// A filter naming no period cannot be shifted; the results say why.
		if s, err := f.Shift(n, loc); err == nil {
			f, moved = s, true
		}
	}

	res, err := a.history(ctx, u, f, now)
	sse := render.NewSSE(w, r)
	if err != nil {
		a.Log.Error("load history", "err", err)
		_ = sse.PatchElementTempl(view.Flash("Your history could not be loaded. Please try again."))
		return
	}
	if moved {
		_ = sse.MarshalAndPatchSignals(view.SignalsFor(f))
	}
	_ = sse.PatchElementTempl(view.Flash(""))
	_ = sse.PatchElementTempl(view.HistoryResultsFragment(res))
	_ = sse.ReplaceURL(url.URL{Path: "/history", RawQuery: historyQuery(f).Encode()})
}

// history is the history results' read model: the period a filter names, its
// per-company totals, and its newest entries.
func (a *App) history(ctx context.Context, u auth.User, f tracking.Filter, now time.Time) (view.HistoryResults, error) {
	loc := u.Location()
	p, err := f.Period(loc)
	if err != nil {
		// A period that does not parse is the person's input rather than a
		// failure, so it is answered on the page, where it can be corrected.
		return view.HistoryResults{
			Error: "That is not a period we can show. Pick a date, and make a range end on or after the day it starts.",
		}, nil
	}
	totals, err := a.Tracking.Totals(ctx, u.ID, p, f.CompanyID, now)
	if err != nil {
		return view.HistoryResults{}, err
	}
	entries, err := a.Tracking.Entries(ctx, u.ID, p, f.CompanyID, now, historyLimit)
	if err != nil {
		return view.HistoryResults{}, err
	}
	res := view.BuildHistory(f, p, loc, now, totals, entries)
	res.Export = "/history/export?" + historyQuery(f).Encode()
	return res, nil
}

// filterFromQuery reads a filter from a history URL. A missing field takes its
// default around now, which must already be in the user's location.
func filterFromQuery(q url.Values, now time.Time) tracking.Filter {
	return view.HistorySignals{
		Mode:    q.Get("mode"),
		Day:     q.Get("day"),
		Month:   q.Get("month"),
		Year:    q.Get("year"),
		From:    q.Get("from"),
		To:      q.Get("to"),
		Company: q.Get("company"),
	}.Filter(now)
}

// historyQuery writes a filter as a history URL's query: the mode, the fields
// that mode reads, and the company. [filterFromQuery] reads it back.
func historyQuery(f tracking.Filter) url.Values {
	q := url.Values{"mode": {string(f.Mode)}}
	switch f.Mode {
	case tracking.ModeDay:
		q.Set("day", f.Day)
	case tracking.ModeMonth:
		q.Set("month", f.Month)
	case tracking.ModeYear:
		q.Set("year", f.Year)
	case tracking.ModeRange:
		q.Set("from", f.From)
		q.Set("to", f.To)
	}
	if f.CompanyID > 0 {
		q.Set("company", strconv.FormatInt(f.CompanyID, 10))
	}
	return q
}

// presetFilter applies a quick-period button to f. It changes the mode and that
// mode's field, and keeps everything else — the company, and the values the
// other modes last used. It reports false for a name it does not know. now must
// be in the user's location.
func presetFilter(name string, f tracking.Filter, now time.Time) (tracking.Filter, bool) {
	switch name {
	case "today":
		f.Mode, f.Day = tracking.ModeDay, now.Format(tracking.DateLayout)
	case "week":
		// Weeks start on Monday, as ISO 8601 counts them.
		back := (int(now.Weekday()) + 6) % 7
		monday := now.AddDate(0, 0, -back)
		f.Mode = tracking.ModeRange
		f.From = monday.Format(tracking.DateLayout)
		f.To = monday.AddDate(0, 0, 6).Format(tracking.DateLayout)
	case "month":
		f.Mode, f.Month = tracking.ModeMonth, now.Format(tracking.MonthLayout)
	case "last-month":
		// From the first of the month: stepping back a month from the 31st would
		// land on a day some months do not have, and normalise into the wrong one.
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		f.Mode, f.Month = tracking.ModeMonth, first.AddDate(0, -1, 0).Format(tracking.MonthLayout)
	case "year":
		f.Mode, f.Year = tracking.ModeYear, now.Format(tracking.YearLayout)
	default:
		return f, false
	}
	return f, true
}
