package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/render"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/view"
)

// companySignals is what the add-company box sends.
type companySignals struct {
	Name string `json:"newCompany"`
}

// GetDashboard renders the timers screen.
func (a *App) GetDashboard(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	d, err := a.dashboard(r.Context(), u)
	if err != nil {
		a.Log.Error("load dashboard", "err", err)
		http.Error(w, "Your timers could not be loaded. Please reload the page.", http.StatusInternalServerError)
		return
	}
	_ = view.DashboardPage(d).Render(r.Context(), w)
}

// dashboard is the timers screen's read model: a function of the user alone,
// shared by the page load, the stream, and every write's own response.
func (a *App) dashboard(ctx context.Context, u auth.User) (view.Dashboard, error) {
	companies, err := a.Tracking.Companies(ctx, u.ID)
	if err != nil {
		return view.Dashboard{}, err
	}
	running, err := a.Tracking.Running(ctx, u.ID)
	if err != nil {
		return view.Dashboard{}, err
	}
	return view.Dashboard{
		Page:      a.page(u, "Timers", "timers"),
		Companies: companies,
		Timers:    view.Timers{Entries: running, Now: a.Now(), Loc: u.Location()},
	}, nil
}

// PostCompany adds a company.
func (a *App) PostCompany(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	var in companySignals
	if err := datastar.ReadSignals(r, &in); err != nil {
		a.flash(w, r, "We could not read that. Reload the page and try again.")
		return
	}
	if _, err := a.Tracker.AddCompany(r.Context(), u.ID, in.Name); err != nil {
		a.flash(w, r, a.userMessage(err))
		return
	}
	// Persisted, so now — and only now — tell the user's other open pages.
	a.Bus.Broadcast(u.ID)

	sse := render.NewSSE(w, r)
	// Clearing the box is state the server owns: the name was accepted.
	_ = sse.MarshalAndPatchSignals(companySignals{Name: ""})
	a.patchDashboard(r.Context(), sse, u)
}

// PostStart starts a timer on one company, with whatever was typed in that
// company's box as the task name.
func (a *App) PostStart(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	companyID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.flash(w, r, a.userMessage(tracking.ErrNotFound))
		return
	}
	// The signal's name depends on the company, so it is read as a map. The page
	// sends only this company's signal (see view.TaskSignal).
	var in map[string]any
	if err := datastar.ReadSignals(r, &in); err != nil {
		a.flash(w, r, "We could not read that. Reload the page and try again.")
		return
	}
	key := view.TaskSignal(companyID)
	task, _ := in[key].(string)

	if _, err := a.Tracker.Start(r.Context(), u.ID, companyID, task); err != nil {
		if errors.Is(err, tracking.ErrNotFound) {
			sse := render.NewSSE(w, r)
			a.patchDashboard(r.Context(), sse, u)
			_ = sse.PatchElementTempl(view.Flash(a.userMessage(err)))
			return
		}
		a.flash(w, r, a.userMessage(err))
		return
	}
	a.Bus.Broadcast(u.ID)

	sse := render.NewSSE(w, r)
	_ = sse.MarshalAndPatchSignals(map[string]string{key: ""})
	a.patchDashboard(r.Context(), sse, u)
}

// PostStop stops a running timer.
func (a *App) PostStop(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	err := tracking.ErrNotFound
	if id, perr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64); perr == nil {
		err = a.Tracker.Stop(r.Context(), u.ID, id)
	}
	if err != nil && !errors.Is(err, tracking.ErrNotFound) {
		a.flash(w, r, a.userMessage(err))
		return
	}
	if err == nil {
		a.Bus.Broadcast(u.ID)
	}

	sse := render.NewSSE(w, r)
	a.patchDashboard(r.Context(), sse, u)
	if err != nil {
		// Usually a second tab, or a double press, stopping it first. The page is
		// brought up to date either way; say so rather than stay silent.
		_ = sse.PatchElementTempl(view.Flash("That timer had already been stopped."))
	}
}

// patchDashboard sends the writer's own page its fresh state, rather than
// leaving it to wait for its stream: a response and a stream event are two
// separate round trips, and the person who pressed the button should not see
// the gap between them.
func (a *App) patchDashboard(ctx context.Context, sse *datastar.ServerSentEventGenerator, u auth.User) {
	d, err := a.dashboard(ctx, u)
	if err != nil {
		a.Log.Error("reload dashboard", "err", err)
		_ = sse.PatchElementTempl(view.Flash("Saved, but the page could not refresh. Reload to see the change."))
		return
	}
	_ = sse.PatchElementTempl(view.Flash(""))
	_ = sse.PatchElementTempl(view.CompanyList(d.Companies))
	_ = sse.PatchElementTempl(view.RunningTimers(d.Timers))
}
