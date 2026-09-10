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

func TestTheExportNamesThePeriodAndItsTotalsThenHowLongAndWhat(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	bob, _ := h.register(t, "bob@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	beta := h.addCompany(t, ada, adaUser, "Beta")
	now := time.Now().In(adaUser.Location())
	yesterday := now.AddDate(0, 0, -1).Format(tracking.DateLayout)
	today := now.Format(tracking.DateLayout)
	logTime := func(c tracking.Company, in view.LogSignals) {
		t.Helper()
		if r := h.action(t, ada, http.MethodPost, fmt.Sprintf("/companies/%d/entries", c.ID), in); !strings.Contains(r.body, "Added ") {
			t.Fatalf("logging %+v was refused:\n%s", in, r.body)
		}
	}
	logTime(acme, view.LogSignals{Task: "Planning", Duration: "1h", Date: yesterday, At: "10:00"})
	logTime(beta, view.LogSignals{Task: "Phone call", Duration: "25m", Date: today})
	query := "mode=range&from=" + yesterday + "&to=" + today

	r := h.page(t, ada, "/history/export?"+query)
	if r.status != http.StatusOK {
		t.Fatalf("GET /history/export = %d:\n%s", r.status, r.body)
	}
	if got := r.header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want plain text", got)
	}
	if got, want := r.header.Get("Content-Disposition"), fmt.Sprintf("attachment; filename=%q", "tasker-"+yesterday+"_to_"+today+".txt"); got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
	header := "period from " + yesterday + " to " + today + "\n"
	if want := header + "total for period 1h25m\ntotal hours 1.42\n\n1h00m Acme — Planning\n0h25m Beta — Phone call\n"; r.body != want {
		t.Errorf("report =\n%s\nwant\n%s", r.body, want)
	}

	// Narrowed to one company, the lines name the work rather than the company.
	company := "&company=" + strconv.FormatInt(acme.ID, 10)
	if r := h.page(t, ada, "/history/export?"+query+company); r.body != header+"total for period 1h00m\ntotal hours 1.00\n\n1h00m Planning\n" {
		t.Errorf("Acme's report =\n%s", r.body)
	}
	// Ada's report above has her entries in it, so Bob's empty one is about whose
	// time it is — even naming her company.
	if r := h.page(t, bob, "/history/export?"+query+company); r.status != http.StatusOK || r.body != header+"total for period 0h00m\ntotal hours 0.00\n" {
		t.Errorf("Bob's report for Ada's company = %d\n%s\nwant only a header with zero totals", r.status, r.body)
	}
	if r := h.page(t, ada, "/history/export?mode=range&from="+today+"&to="+yesterday); r.status != http.StatusBadRequest {
		t.Errorf("a range that ends before it starts = %d, want 400", r.status)
	}
}

func TestTheHistoryResultsLinkToTheExportOfWhatIsOnScreen(t *testing.T) {
	h := newHarness(t)
	ada, adaUser := h.register(t, "ada@example.com")
	acme := h.addCompany(t, ada, adaUser, "Acme")
	h.start(t, ada, acme.ID, "Invoices")
	today := time.Now().In(adaUser.Location()).Format(tracking.DateLayout)

	r := h.action(t, ada, http.MethodGet, "/history/results", view.HistorySignals{Mode: "day", Day: today})
	link := `href="/history/export?day=` + today + `&amp;mode=day"`
	if !strings.Contains(r.body, "Export report") || !strings.Contains(r.body, link) {
		t.Errorf("the results do not link to the export of the day on screen (%s):\n%s", link, r.body)
	}
}
