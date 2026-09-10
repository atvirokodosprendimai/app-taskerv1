package web

import (
	"fmt"
	"io"
	"net/http"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/view"
)

// GetHistoryExport downloads a history period as a plain-text report: a header
// naming the period and its totals, then how long each entry took and what it
// was.
//
// It reads the same query as the history page, and the results render their
// Export link from the filter on screen, so the file is the period and company
// being looked at. It includes every entry in the period, not only the ones the
// page lists.
func (a *App) GetHistoryExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	loc := u.Location()
	now := a.Now()
	f := filterFromQuery(r.URL.Query(), now.In(loc))
	p, err := f.Period(loc)
	if err != nil {
		http.Error(w, "That is not a period we can export. Pick it again on the history page.", http.StatusBadRequest)
		return
	}
	entries, err := a.Tracking.Entries(ctx, u.ID, p, f.CompanyID, now, tracking.AllEntries)
	if err != nil {
		a.Log.Error("export history", "err", err)
		http.Error(w, "Your history could not be exported right now. Please try again.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", exportFilename(f)))
	_, _ = io.WriteString(w, view.Report(p, now, entries, f.CompanyID > 0))
}

// exportFilename names a report after its period, spelled as the history query
// spells it. It is called only once the filter has resolved to a period, so each
// part is a date, a month or a year.
func exportFilename(f tracking.Filter) string {
	name := f.Month
	switch f.Mode {
	case tracking.ModeDay:
		name = f.Day
	case tracking.ModeYear:
		name = f.Year
	case tracking.ModeRange:
		name = f.From + "_to_" + f.To
	}
	return "tasker-" + name + ".txt"
}
