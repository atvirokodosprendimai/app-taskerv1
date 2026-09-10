package web_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web/view"
)

func TestLoggingTimeByHandAddsAStoppedManualEntry(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	loc := adaUser.Location()
	today := time.Now().In(loc).Format(tracking.DateLayout)

	open := h.action(t, ada, http.MethodGet, fmt.Sprintf("/companies/%d/log", acme.ID), map[string]string{})
	for _, want := range []string{"<dialog", "For Acme", today} {
		if !strings.Contains(open.body, want) {
			t.Errorf("the log dialog is missing %q:\n%s", want, open.body)
		}
	}

	r := h.action(t, ada, http.MethodPost, fmt.Sprintf("/companies/%d/entries", acme.ID),
		view.LogSignals{Task: "Phone call", Duration: "20m", Date: today})
	for _, want := range []string{"Added 20m to Acme.", `<div id="dialog"></div>`, view.LogOpenerID(acme.ID)} {
		if !strings.Contains(r.body, want) {
			t.Errorf("logging did not answer with %q:\n%s", want, r.body)
		}
	}

	day, err := tracking.Filter{Mode: tracking.ModeDay, Day: today}.Period(loc)
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	entries, err := h.timers.Entries(t.Context(), adaUser.ID, day, 0, time.Now(), 10)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 1 || !entries[0].Manual || entries[0].Task != "Phone call" || entries[0].Running() ||
		entries[0].StoppedAt.Sub(entries[0].StartedAt) != 20*time.Minute {
		t.Fatalf("entries = %+v, want one stopped, manual, 20-minute Phone call", entries)
	}
	if running := h.running(t, adaUser); len(running) != 0 {
		t.Errorf("logged time shows as %d running timers", len(running))
	}
	page := h.page(t, ada, "/history?mode=day&day="+today)
	if !strings.Contains(page.body, "Phone call") || !strings.Contains(page.body, ">manual<") {
		t.Errorf("the day's history does not show the entry marked manual")
	}

	closed := h.action(t, ada, http.MethodGet, fmt.Sprintf("/companies/%d/log/close", acme.ID), map[string]string{})
	if !strings.Contains(closed.body, `<div id="dialog"></div>`) {
		t.Errorf("closing did not empty the dialog slot:\n%s", closed.body)
	}
}

func TestTheLogDialogExplainsWhatItCannotAccept(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	now := time.Now().In(adaUser.Location())
	today := now.Format(tracking.DateLayout)
	yesterday := now.AddDate(0, 0, -1).Format(tracking.DateLayout)
	tomorrow := now.AddDate(0, 0, 1).Format(tracking.DateLayout)
	path := fmt.Sprintf("/companies/%d/entries", acme.ID)

	cases := []struct {
		name string
		in   view.LogSignals
		want string
	}{
		{"no duration", view.LogSignals{Task: "Call", Date: today}, "Enter how long it took"},
		{"longer than a day", view.LogSignals{Duration: "25h", Date: today}, "at most 24 hours"},
		{"another day without a start time", view.LogSignals{Duration: "1h", Date: yesterday}, "Add a start time"},
		{"time still to come", view.LogSignals{Duration: "1h", Date: tomorrow, At: "10:00"}, "has not happened yet"},
		{"a day that is not a date", view.LogSignals{Duration: "1h", Date: "soon", At: "10:00"}, "Pick the day"},
	}
	for _, c := range cases {
		r := h.action(t, ada, http.MethodPost, path, c.in)
		if !strings.Contains(r.body, `id="log-message"`) || !strings.Contains(r.body, c.want) {
			t.Errorf("%s: the dialog did not explain %q:\n%s", c.name, c.want, r.body)
		}
		if strings.Contains(r.body, `<div id="dialog"></div>`) {
			t.Errorf("%s: a refused entry closed the dialog, throwing away what was typed", c.name)
		}
	}

	year, err := tracking.Filter{Mode: tracking.ModeRange, From: yesterday, To: tomorrow}.Period(adaUser.Location())
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	if entries, _ := h.timers.Entries(t.Context(), adaUser.ID, year, 0, time.Now(), 10); len(entries) != 0 {
		t.Fatalf("refused entries were written: %+v", entries)
	}
	// The same day WITH a start time is accepted, so the refusals above are about
	// what each one lacked.
	if r := h.action(t, ada, http.MethodPost, path, view.LogSignals{Duration: "1h", Date: yesterday, At: "10:00"}); !strings.Contains(r.body, "Added 1h 00m to Acme.") {
		t.Errorf("a complete entry for yesterday was not added:\n%s", r.body)
	}
}

func TestNobodyLogsTimeOnSomeoneElsesCompany(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	bob, _ := h.register(t, "bob@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	today := time.Now().In(adaUser.Location()).Format(tracking.DateLayout)
	path := fmt.Sprintf("/companies/%d/entries", acme.ID)
	in := view.LogSignals{Duration: "10m", Date: today}

	if r := h.action(t, ada, http.MethodPost, path, in); !strings.Contains(r.body, "Added 10m to Acme.") {
		t.Fatalf("Ada could not log time on her own company, so Bob's refusal below would prove nothing:\n%s", r.body)
	}
	r := h.action(t, bob, http.MethodGet, fmt.Sprintf("/companies/%d/log", acme.ID), map[string]string{})
	if strings.Contains(r.body, "<dialog") || !strings.Contains(r.body, "no longer exists") {
		t.Errorf("Bob was offered a log dialog for Ada's company:\n%s", r.body)
	}
	if r := h.action(t, bob, http.MethodPost, path, in); !strings.Contains(r.body, "no longer exists") {
		t.Errorf("Bob's time on Ada's company was not refused:\n%s", r.body)
	}
	day, err := tracking.Filter{Mode: tracking.ModeDay, Day: today}.Period(adaUser.Location())
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	if entries, err := h.timers.Entries(t.Context(), adaUser.ID, day, 0, time.Now(), 10); err != nil || len(entries) != 1 {
		t.Errorf("Ada's entries after Bob's attempt = %d (%v), want her one", len(entries), err)
	}
}
