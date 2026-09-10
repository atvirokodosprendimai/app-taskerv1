package view

import (
	"testing"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

// reportDay is 10 September 2026 in Vilnius, as one period, with a clock to
// place entries in it.
func reportDay(t *testing.T) (tracking.Period, func(h, m, s int) time.Time) {
	t.Helper()
	vilnius, err := time.LoadLocation("Europe/Vilnius")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	day := time.Date(2026, 9, 10, 0, 0, 0, 0, vilnius)
	at := func(h, m, s int) time.Time {
		return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second)
	}
	return tracking.Period{From: day, To: day.AddDate(0, 0, 1)}, at
}

func TestAReportOpensWithTheTotalThenSaysHowLongAndWhat(t *testing.T) {
	p, at := reportDay(t)
	now := at(15, 0, 0)
	// Newest first, the order Repo.Entries returns.
	entries := []tracking.Entry{
		{CompanyName: "Beta", StartedAt: at(14, 30, 0)},
		{CompanyName: "Acme", Task: "Phone call", StartedAt: at(11, 0, 0), StoppedAt: at(11, 3, 15), Manual: true},
		{CompanyName: "Acme", Task: "Misclick", StartedAt: at(10, 0, 0), StoppedAt: at(10, 0, 20)},
		// Started the evening before: only the 1h15m inside the day counts.
		{CompanyName: "Acme", Task: "Night deploy", StartedAt: at(-1, 30, 0), StoppedAt: at(1, 15, 0)},
	}

	got := Report(p, now, entries, false)
	want := "total for period 1h48m\n" +
		"1h15m Acme — Night deploy\n" +
		"0h03m Acme — Phone call\n" +
		"0h30m Beta (running)\n"
	if got != want {
		t.Errorf("report =\n%s\nwant\n%s", got, want)
	}
}

func TestAReportsTotalIsTheSumOfItsLines(t *testing.T) {
	p, at := reportDay(t)
	// Three entries of 10m20s: exactly 31m between them, but each line reads 10m,
	// and a total that disagreed with its own lines would be the first thing a
	// reader checks.
	var entries []tracking.Entry
	for _, h := range []int{12, 11, 10} {
		entries = append(entries, tracking.Entry{CompanyName: "Acme", StartedAt: at(h, 0, 0), StoppedAt: at(h, 10, 20)})
	}

	want := "total for period 0h30m\n0h10m Acme\n0h10m Acme\n0h10m Acme\n"
	if got := Report(p, at(15, 0, 0), entries, false); got != want {
		t.Errorf("report =\n%s\nwant\n%s", got, want)
	}
}

func TestAOneCompanyReportNamesTheWorkNotTheCompany(t *testing.T) {
	p, at := reportDay(t)
	entries := []tracking.Entry{
		{CompanyName: "Acme", StartedAt: at(13, 0, 0), StoppedAt: at(13, 20, 0)},
		{CompanyName: "Acme", Task: "Invoices", StartedAt: at(9, 0, 0), StoppedAt: at(10, 0, 0)},
	}

	want := "total for period 1h20m\n1h00m Invoices\n0h20m Acme\n"
	if got := Report(p, at(15, 0, 0), entries, true); got != want {
		t.Errorf("report =\n%s\nwant\n%s", got, want)
	}
	if got := Report(p, at(15, 0, 0), nil, true); got != "total for period 0h00m\n" {
		t.Errorf("an empty period's report = %q, want only its zero total", got)
	}
}
