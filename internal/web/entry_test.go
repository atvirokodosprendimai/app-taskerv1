package web_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/view"
)

// historyEntrySignals is what an entry action on the history page sends: its own
// field, and the filter on screen.
type historyEntrySignals struct {
	view.TaskSignals
	view.HistorySignals
}

func TestARunningTimersTaskNameIsPutRightFromTheDashboard(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	h.start(t, ada, acme.ID, "")
	e := h.running(t, adaUser)[0]

	if page := h.page(t, ada, "/").body; !strings.Contains(page, `id="`+view.EntryOpenerID(e.ID)+`"`) {
		t.Fatalf("the running timer has no Edit button with id %s", view.EntryOpenerID(e.ID))
	}
	open := h.action(t, ada, http.MethodGet, fmt.Sprintf("/entries/%d/edit", e.ID), map[string]string{})
	for _, want := range []string{"<dialog", "Edit entry", "Acme · ", `data-bind="entryTask"`, "Delete entry"} {
		if !strings.Contains(open.body, want) {
			t.Errorf("the entry dialog is missing %q:\n%s", want, open.body)
		}
	}

	r := h.action(t, ada, http.MethodPost, fmt.Sprintf("/entries/%d/task", e.ID), view.TaskSignals{Task: "  Quarterly   report "})
	for _, want := range []string{`<div id="dialog"></div>`, `id="running-timers"`, "Quarterly report", view.EntryOpenerID(e.ID)} {
		if !strings.Contains(r.body, want) {
			t.Errorf("saving the task name did not answer with %q:\n%s", want, r.body)
		}
	}
	if got := h.running(t, adaUser); len(got) != 1 || got[0].Task != "Quarterly report" {
		t.Errorf("running = %+v, want the one timer, named Quarterly report", got)
	}
}

// A task name is text anyone can type, and datastar compiles a data-signals
// attribute as an expression — rewriting @name( even inside a quoted string — so a
// name written there keeps the dialog from opening. It is sent as a signal instead.
func TestTheEntryDialogSendsTheTaskNameAsASignalNotInItsMarkup(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	const name = "Call @Anna(invoices)"
	h.start(t, ada, acme.ID, name)
	e := h.running(t, adaUser)[0]

	open := h.action(t, ada, http.MethodGet, fmt.Sprintf("/entries/%d/edit", e.ID), map[string]string{})
	if !strings.Contains(open.body, "<dialog") {
		t.Fatalf("the entry dialog did not open:\n%s", open.body)
	}
	if !strings.Contains(open.body, "datastar-patch-signals") || !strings.Contains(open.body, `{"entryTask":"`+name+`"}`) {
		t.Errorf("the dialog's task name was not sent as a signal patch:\n%s", open.body)
	}
	if strings.Contains(open.body, "data-signals") {
		t.Errorf("the entry dialog writes signals into its markup, where datastar compiles them:\n%s", open.body)
	}
}

func TestDeletingATimerIsSoftAndUndoBringsItBack(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	h.start(t, ada, acme.ID, "Started by mistake")
	e := h.running(t, adaUser)[0]
	s := h.openStream(t, ada)
	s.next(t, "Started by mistake", 5*time.Second)
	restore := fmt.Sprintf("/entries/%d/restore", e.ID)

	r := h.action(t, ada, http.MethodPost, fmt.Sprintf("/entries/%d/delete", e.ID), map[string]string{})
	for _, want := range []string{`<div id="dialog"></div>`, "Nothing running", "Deleted “Started by mistake” for Acme.", `id="undo"`, restore} {
		if !strings.Contains(r.body, want) {
			t.Errorf("deleting did not answer with %q:\n%s", want, r.body)
		}
	}
	if got := h.running(t, adaUser); len(got) != 0 {
		t.Fatalf("running after the delete = %+v, want none", got)
	}
	// Another open dashboard hears of it through its stream.
	s.next(t, "Nothing running", 5*time.Second)

	back := h.action(t, ada, http.MethodPost, restore, map[string]string{})
	if !strings.Contains(back.body, "Brought back “Started by mistake” for Acme.") || !strings.Contains(back.body, `id="running-timers"`) {
		t.Errorf("Undo did not bring the timer back on the page:\n%s", back.body)
	}
	got := h.running(t, adaUser)
	if len(got) != 1 || got[0].ID != e.ID || got[0].Task != e.Task || !got[0].StartedAt.Equal(e.StartedAt) {
		t.Fatalf("running after Undo = %+v, want %+v back as it was", got, e)
	}
	if again := h.action(t, ada, http.MethodPost, restore, map[string]string{}); !strings.Contains(again.body, "nothing to bring back") {
		t.Errorf("a second Undo was not answered on the page:\n%s", again.body)
	}
}

func TestAnEntryPutRightInTheHistoryRendersItsResultsAgain(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	today := time.Now().In(adaUser.Location()).Format(tracking.DateLayout)
	logged := h.action(t, ada, http.MethodPost, fmt.Sprintf("/companies/%d/entries", acme.ID),
		view.LogSignals{Task: "Phone cal", Duration: "20m", Date: today})
	if !strings.Contains(logged.body, "Added ") {
		t.Fatalf("logging was refused:\n%s", logged.body)
	}
	day, err := tracking.Filter{Mode: tracking.ModeDay, Day: today}.Period(adaUser.Location())
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	entries, err := h.timers.Entries(t.Context(), adaUser.ID, day, 0, time.Now(), 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %+v, %v; want the logged call", entries, err)
	}
	e := entries[0]
	page := h.page(t, ada, "/history?mode=day&day="+today).body
	if !strings.Contains(page, `id="`+view.EntryOpenerID(e.ID)+`"`) || !strings.Contains(page, `<div id="dialog"></div>`) {
		t.Fatalf("the history lists the entry without its Edit button, or has no dialog slot:\n%s", page)
	}
	// A year rather than the default month, so the period label shows which filter
	// each response was rendered for.
	year := time.Now().In(adaUser.Location()).Format(tracking.YearLayout)
	filter := view.HistorySignals{Mode: "year", Year: year}
	label := `period-label">` + year + `</p>`
	path := func(action string) string { return fmt.Sprintf("/entries/%d/%s?from=history", e.ID, action) }

	r := h.action(t, ada, http.MethodPost, path("task"), historyEntrySignals{view.TaskSignals{Task: "Phone call"}, filter})
	for _, want := range []string{`id="history-results"`, label, " — Phone call<", `<div id="dialog"></div>`} {
		if !strings.Contains(r.body, want) {
			t.Errorf("saving in the history did not answer with %q:\n%s", want, r.body)
		}
	}
	// The history page has no running list; a patch aimed at one would find no
	// target in the browser.
	if strings.Contains(r.body, `id="running-timers"`) {
		t.Errorf("a save on the history page sent the dashboard's running list:\n%s", r.body)
	}

	del := h.action(t, ada, http.MethodPost, path("delete"), historyEntrySignals{HistorySignals: filter})
	for _, want := range []string{label, "No time recorded", "Deleted “Phone call” for Acme.", path("restore")} {
		if !strings.Contains(del.body, want) {
			t.Errorf("deleting in the history did not answer with %q:\n%s", want, del.body)
		}
	}
	back := h.action(t, ada, http.MethodPost, path("restore"), historyEntrySignals{HistorySignals: filter})
	for _, want := range []string{label, " — Phone call<", "Brought back “Phone call” for Acme."} {
		if !strings.Contains(back.body, want) {
			t.Errorf("Undo in the history did not answer with %q:\n%s", want, back.body)
		}
	}
}

func TestNobodyChangesSomeoneElsesEntry(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	bob, _ := h.register(t, "bob@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	h.start(t, ada, acme.ID, "Invoices")
	e := h.running(t, adaUser)[0]
	path := func(action string) string { return fmt.Sprintf("/entries/%d/%s", e.ID, action) }

	// Ada's own dialog opens, so Bob's refusals below are about whose entry it is.
	if r := h.action(t, ada, http.MethodGet, path("edit"), map[string]string{}); !strings.Contains(r.body, "<dialog") {
		t.Fatalf("Ada's own entry dialog did not open:\n%s", r.body)
	}
	if r := h.action(t, bob, http.MethodGet, path("edit"), map[string]string{}); strings.Contains(r.body, "<dialog") || !strings.Contains(r.body, "no longer exists") {
		t.Errorf("Bob was offered a dialog for Ada's entry:\n%s", r.body)
	}
	for _, action := range []string{"task", "delete"} {
		r := h.action(t, bob, http.MethodPost, path(action), view.TaskSignals{Task: "not mine"})
		if !strings.Contains(r.body, "no longer exists") || strings.Contains(r.body, `<div id="dialog"></div>`) {
			t.Errorf("Bob's %s on Ada's entry was not refused inside the dialog:\n%s", action, r.body)
		}
	}
	if got := h.running(t, adaUser); len(got) != 1 || got[0].Task != "Invoices" {
		t.Fatalf("Ada's timers after Bob's attempts = %+v, want Invoices untouched", got)
	}

	long := h.action(t, ada, http.MethodPost, path("task"), view.TaskSignals{Task: strings.Repeat("x", tracking.MaxTask+1)})
	if !strings.Contains(long.body, `id="entry-message"`) || !strings.Contains(long.body, strconv.Itoa(tracking.MaxTask)+" characters") ||
		strings.Contains(long.body, `<div id="dialog"></div>`) {
		t.Errorf("an overlong name was not refused inside the dialog, leaving it open:\n%s", long.body)
	}

	h.action(t, ada, http.MethodPost, path("delete"), map[string]string{})
	if r := h.action(t, bob, http.MethodPost, path("restore"), map[string]string{}); !strings.Contains(r.body, "nothing to bring back") {
		t.Errorf("Bob's Undo of Ada's delete was not refused:\n%s", r.body)
	}
	if got := h.running(t, adaUser); len(got) != 0 {
		t.Errorf("Ada's deleted timer is back after Bob's Undo, or her delete did not happen: %+v", got)
	}
}
