package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/render"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/view"
)

// entrySignals is what the entry dialog's actions send: the task name when it is
// saved and, on the history page, the filter on screen, so the results can be
// rendered again for the period being looked at.
type entrySignals struct {
	view.TaskSignals
	view.HistorySignals
}

// GetEntryDialog opens the dialog for one of the user's entries, where its task
// name is put right or the entry is deleted.
func (a *App) GetEntryDialog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	e, err := a.ownEntry(ctx, u, chi.URLParam(r, "id"))
	sse := render.NewSSE(w, r)
	if err != nil {
		_ = sse.PatchElementTempl(view.NoDialog())
		_ = sse.PatchElementTempl(view.Flash(a.entryRefusal(err)))
		return
	}
	_ = sse.PatchElementTempl(view.EntryDialog(view.NewEntryForm(e, a.Now(), u.Location(), fromHistory(r))))
}

// GetCloseEntryDialog takes the dialog away.
func (a *App) GetCloseEntryDialog(w http.ResponseWriter, r *http.Request) {
	sse := render.NewSSE(w, r)
	_ = sse.PatchElementTempl(view.NoDialog())
	if id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64); err == nil {
		focusEntry(sse, id)
	}
}

// PostEntryTask saves an entry's task name: the one typed wrong, or not typed at
// all, when its timer was started.
func (a *App) PostEntryTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in entrySignals
	if err := datastar.ReadSignals(r, &in); err != nil {
		a.entryMessage(w, r, "We could not read that. Close the dialog and try again.")
		return
	}
	err := tracking.ErrNotFound
	id, perr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if perr == nil {
		err = a.Tracker.Rename(ctx, u.ID, id, in.Task)
	}
	if err != nil {
		a.entryMessage(w, r, a.entryRefusal(err))
		return
	}
	// A running timer's name is on every open dashboard.
	a.Bus.Broadcast(u.ID)

	sse := render.NewSSE(w, r)
	a.patchAfterEntryChange(ctx, sse, u, r, in.HistorySignals)
	_ = sse.PatchElementTempl(view.NoDialog())
	focusEntry(sse, id)
}

// PostEntryDelete deletes an entry softly and says so with an Undo button, which
// is where focus goes: the entry's own controls have just left the page.
func (a *App) PostEntryDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in entrySignals
	if err := datastar.ReadSignals(r, &in); err != nil {
		a.entryMessage(w, r, "We could not read that. Close the dialog and try again.")
		return
	}
	// Read first, so the confirmation can say what was deleted.
	e, err := a.ownEntry(ctx, u, chi.URLParam(r, "id"))
	if err == nil {
		err = a.Tracker.Delete(ctx, u.ID, e.ID)
	}
	if err != nil {
		a.entryMessage(w, r, a.entryRefusal(err))
		return
	}
	a.Bus.Broadcast(u.ID)

	sse := render.NewSSE(w, r)
	_ = sse.PatchElementTempl(view.NoDialog())
	msg := "Deleted " + view.EntryName(e) + "."
	if !a.patchAfterEntryChange(ctx, sse, u, r, in.HistorySignals) {
		msg = "Deleted " + view.EntryName(e) + ", but the page could not refresh. Reload to see the change."
	}
	// Undo is offered even when the page could not refresh: the delete happened
	// either way, and Undo is the one thing that takes it back.
	_ = sse.PatchElementTempl(view.Confirmation(msg, view.RestoreAction(e.ID, fromHistory(r))))
	_ = sse.ExecuteScript(fmt.Sprintf("document.getElementById(%q)?.focus()", view.UndoID))
}

// PostEntryRestore undoes a delete. The entry comes back exactly as it was, and
// focus goes to its Edit button when the page shows it.
func (a *App) PostEntryRestore(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in entrySignals
	if err := datastar.ReadSignals(r, &in); err != nil {
		a.flash(w, r, "We could not read that. Reload the page and try again.")
		return
	}
	err := tracking.ErrNotFound
	id, perr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if perr == nil {
		err = a.Tracker.Restore(ctx, u.ID, id)
	}
	switch {
	case errors.Is(err, tracking.ErrNotFound):
		// Usually Undo pressed twice, or pressed first in another tab.
		a.flash(w, r, "There is nothing to bring back: it may have been brought back already. Reload the page to see it.")
		return
	case err != nil:
		a.flash(w, r, a.userMessage(err))
		return
	}
	a.Bus.Broadcast(u.ID)

	sse := render.NewSSE(w, r)
	if !a.patchAfterEntryChange(ctx, sse, u, r, in.HistorySignals) {
		return
	}
	msg := "Brought back."
	if e, err := a.Tracking.Entry(ctx, u.ID, id); err == nil {
		msg = "Brought back " + view.EntryName(e) + "."
	}
	_ = sse.PatchElementTempl(view.Confirmation(msg, ""))
	focusEntry(sse, id)
}

// patchAfterEntryChange brings the page an entry was changed from up to date,
// and reports whether it could: the dashboard's lists, or — on the history page,
// which has no stream — the results for the filter that page sent.
func (a *App) patchAfterEntryChange(ctx context.Context, sse *datastar.ServerSentEventGenerator, u auth.User, r *http.Request, hist view.HistorySignals) bool {
	if !fromHistory(r) {
		return a.patchDashboard(ctx, sse, u)
	}
	now := a.Now()
	res, err := a.history(ctx, u, hist.Filter(now.In(u.Location())), now)
	if err != nil {
		a.Log.Error("reload history", "err", err)
		_ = sse.PatchElementTempl(view.Flash("Saved, but the page could not refresh. Reload to see the change."))
		return false
	}
	_ = sse.PatchElementTempl(view.Flash(""))
	_ = sse.PatchElementTempl(view.HistoryResultsFragment(res))
	return true
}

// entryMessage shows a refusal inside the entry dialog, leaving what was typed.
func (a *App) entryMessage(w http.ResponseWriter, r *http.Request, msg string) {
	sse := render.NewSSE(w, r)
	_ = sse.PatchElementTempl(view.EntryMessage(msg))
}

// entryRefusal words a refusal about one entry. A missing entry gets its own
// sentence: the general one promises that the page has been brought up to date,
// which these paths do not do.
func (a *App) entryRefusal(err error) string {
	if errors.Is(err, tracking.ErrNotFound) {
		return "That entry no longer exists. Reload the page to bring it up to date."
	}
	return a.userMessage(err)
}

// ownEntry finds one of the user's entries by the id in a URL. Someone else's
// entry, a deleted one and an id nobody has are the same answer, ErrNotFound.
func (a *App) ownEntry(ctx context.Context, u auth.User, idParam string) (tracking.Entry, error) {
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return tracking.Entry{}, tracking.ErrNotFound
	}
	return a.Tracking.Entry(ctx, u.ID, id)
}

// fromHistory reports whether an entry action was taken on the history page
// rather than the dashboard, which decides what the response brings up to date.
func fromHistory(r *http.Request) bool { return r.URL.Query().Get("from") == "history" }

// focusEntry puts focus on an entry's Edit button when the page shows one, so
// someone using the keyboard carries on from the entry rather than from the top
// of the page. The id is a number, so nothing typed can reach the script.
func focusEntry(sse *datastar.ServerSentEventGenerator, id int64) {
	_ = sse.ExecuteScript(fmt.Sprintf("document.getElementById(%q)?.focus()", view.EntryOpenerID(id)))
}
