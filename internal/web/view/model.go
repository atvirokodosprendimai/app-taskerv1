// Package view holds the read models and the templ components that render them.
//
// A read model is a plain struct a handler builds and hands to a templ function —
// json.Marshal for HTML. The same struct serves the first page load and every
// SSE patch, so a fragment can never drift from the page it lives in.
package view

import (
	"fmt"
	"strconv"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

// DatastarBundle is the client bundle every page loads.
//
// It is pinned rather than floating, so behaviour measured against this build
// cannot move underneath the code. The Go SDK versions independently and names
// no client version, so this pin is a decision, not a lookup.
const DatastarBundle = "https://cdn.jsdelivr.net/gh/starfederation/datastar@1.0.3/bundles/datastar.js"

// DatastarIntegrity is the bundle's Subresource Integrity hash. A CDN serving
// altered bytes has them refused by the browser instead of run with the page's
// session. It was computed from the bytes the CDN serves for DatastarBundle, so
// moving the pin means recomputing it:
//
//	curl -sS <DatastarBundle> | openssl dgst -sha384 -binary | openssl base64 -A
const DatastarIntegrity = "sha384-yHqFPXJio1slWODLf67ZFfuDhhPUccOzGpGJF0d5fitjEfihEVrVvzMsfH8TxCiO"

// Page is the chrome every signed-in screen shares.
type Page struct {
	// Title is the browser title.
	Title string
	// Nav is which top-bar link is current: "timers" or "history".
	Nav string
	// User is who is signed in.
	User auth.User
}

// Auth is the sign-in or registration screen.
type Auth struct {
	// Register selects the registration form instead of sign-in.
	Register bool
}

// Dashboard is the timers screen.
type Dashboard struct {
	Page      Page
	Companies []tracking.Company
	Timers    Timers
}

// Timers is the running-timers list as of one instant.
//
// Now is part of the read model, not a global the template reaches for: the
// stream re-renders the same entries with a new Now every second, and that is
// the whole of what a tick changes.
type Timers struct {
	Entries []tracking.Entry
	Now     time.Time
	// Loc is the user's zone, for showing when each timer started.
	Loc *time.Location
}

// Elapsed renders how long one entry has been running.
func (t Timers) Elapsed(e tracking.Entry) string { return Clock(e.Elapsed(t.Now)) }

// Combined renders the running timers' elapsed times added together.
func (t Timers) Combined() string {
	var sum time.Duration
	for _, e := range t.Entries {
		sum += e.Elapsed(t.Now)
	}
	return Clock(sum)
}

// Started renders when an entry started: the time alone for today, with the
// date otherwise.
func (t Timers) Started(e tracking.Entry) string {
	loc := t.Loc
	if loc == nil {
		loc = time.UTC
	}
	s, n := e.StartedAt.In(loc), t.Now.In(loc)
	if s.Year() == n.Year() && s.YearDay() == n.YearDay() {
		return "since " + s.Format("15:04")
	}
	return "since " + s.Format("Mon 2 Jan, 15:04")
}

// TaskSignal is the datastar signal holding what is typed into one company's
// task box.
//
// ⚠ It is bound with the VALUE form, data-bind="task_12", never the key form
// data-bind:task_12. HTML lower-cases attribute NAMES and datastar camel-cases a
// key, so a key-form name is rewritten before it becomes a signal; a value is
// used exactly as written, which is the one form in which this name and the
// server's lookup of it are guaranteed to agree.
func TaskSignal(companyID int64) string {
	return "task_" + strconv.FormatInt(companyID, 10)
}

// addCompanyAction creates a company, sending only the new-company signal.
const addCompanyAction = "@post('/companies', {filterSignals: {include: /^newCompany$/}})"

// startAction starts a timer, sending only that company's task signal rather
// than every company's half-typed box.
func startAction(companyID int64) string {
	return fmt.Sprintf("@post('/companies/%d/timers', {filterSignals: {include: /^%s$/}})",
		companyID, TaskSignal(companyID))
}

// stopAction stops a timer. It needs no signals, so it sends none.
func stopAction(entryID int64) string {
	return fmt.Sprintf("@post('/timers/%d/stop', {filterSignals: {include: /^$/}})", entryID)
}

// enterSubmits runs action when Enter is pressed. Without a <form> there is no
// implicit submit, so the behaviour everyone expects has to be stated.
func enterSubmits(action string) string { return "evt.key === 'Enter' && " + action }

// busySignal names a frontend-only loading signal for one control. The leading
// underscore keeps it out of every request.
func busySignal(what string, id int64) string { return "_" + what + strconv.FormatInt(id, 10) }

func companyDOMID(id int64) string { return "company-" + strconv.FormatInt(id, 10) }
func timerDOMID(id int64) string   { return "timer-" + strconv.FormatInt(id, 10) }
func taskInputID(id int64) string  { return "task-" + strconv.FormatInt(id, 10) }

// runningLabel renders a count of running timers.
func runningLabel(n int) string { return strconv.Itoa(n) + " running" }

// entriesLabel renders a count of entries.
func entriesLabel(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return strconv.Itoa(n) + " entries"
}

// stopLabel completes the Stop button's accessible name, so a screen reader
// hears which of several identical buttons this is.
func stopLabel(e tracking.Entry) string {
	if e.Task == "" {
		return "timer for " + e.CompanyName
	}
	return "timer for " + e.CompanyName + ": " + e.Task
}
