package web

import (
	"net/http"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/render"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/view"
)

// keepaliveAfter is how long a stream may go without sending anything before it
// sends an empty signal patch.
//
// With nothing running, a dashboard stream is silent between changes, and
// proxies and load balancers commonly close a response that has been idle for
// about a minute. An empty patch changes nothing on the page.
const keepaliveAfter = 25 * time.Second

// GetStream is the dashboard's one live connection.
//
// It sends the running timers once a second while any are running, and both the
// company list and the running timers whenever one of the user's writes lands —
// from this tab, another tab, or another device.
//
// A tick re-renders the rows the last read returned, with a new Now, rather than
// querying again: elapsed time is the only thing a second changes, and every
// change to the rows themselves arrives on the bus first.
//
// The stream ends when the browser goes away or the server shuts down. The page
// opens it with retry 'always', so a stream the server ended is reopened by the
// browser rather than left frozen.
func (a *App) GetStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)

	// Subscribe before the first read. In the other order, a write landing
	// between the read and the subscription would go unseen until the next one.
	changes, unsubscribe := a.Bus.Subscribe(u.ID)
	defer unsubscribe()

	sse := render.NewSSE(w, r)
	a.Log.Info("stream opened", "user_id", u.ID)
	defer a.Log.Info("stream closed", "user_id", u.ID)

	var (
		timers   view.Timers
		lastSend time.Time
		// paused records that a reload failed and the page was told so, so the
		// notice comes down again once a reload succeeds.
		paused bool
	)
	// reload re-reads the dashboard and sends both live fragments. Only a failure
	// to SEND ends the stream: a failure to read is shown on the page, and the
	// next change tries again.
	reload := func() error {
		d, err := a.dashboard(ctx, u)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastSend = time.Now()
		if err != nil {
			a.Log.Error("stream: reload dashboard", "user_id", u.ID, "err", err)
			paused = true
			return sse.PatchElementTempl(view.Flash("Live updates are paused because your timers could not be loaded. Reload the page to try again."))
		}
		timers = d.Timers
		if paused {
			paused = false
			if err := sse.PatchElementTempl(view.Flash("")); err != nil {
				return err
			}
		}
		if err := sse.PatchElementTempl(view.CompanyList(d.Companies)); err != nil {
			return err
		}
		return sse.PatchElementTempl(view.RunningTimers(timers))
	}

	// The first send brings the page up to date with anything that changed
	// between rendering it and opening this stream.
	if reload() != nil {
		return
	}
	tick := time.NewTimer(untilNextSecond(time.Now()))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.Closing:
			return
		case <-changes:
			if reload() != nil {
				return
			}
		case <-tick.C:
			var err error
			switch {
			case len(timers.Entries) > 0:
				timers.Now = a.Now()
				err = sse.PatchElementTempl(view.RunningTimers(timers))
				lastSend = time.Now()
			case time.Since(lastSend) >= keepaliveAfter:
				err = sse.PatchSignals([]byte("{}"))
				lastSend = time.Now()
			}
			if err != nil {
				return
			}
			tick.Reset(untilNextSecond(time.Now()))
		}
	}
}

// untilNextSecond is how long from now until just past the next whole second.
//
// Ticks are aligned to the clock rather than spaced a second apart from whenever
// the stream opened. An elapsed time counts whole seconds from a whole-second
// start, so a tick landing just after each boundary shows every second exactly
// once; a free-running ticker drifts across the boundary, and the display then
// skips or repeats a second now and again.
func untilNextSecond(now time.Time) time.Duration {
	return now.Truncate(time.Second).Add(time.Second + 25*time.Millisecond).Sub(now)
}
