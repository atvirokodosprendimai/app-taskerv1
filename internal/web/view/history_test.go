package view

import (
	"testing"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

func TestBuildHistoryGroupsByDayAndSharesTheTotal(t *testing.T) {
	utc := time.UTC
	at := func(d, h, m int) time.Time { return time.Date(2026, 9, d, h, m, 0, 0, utc) }
	f := tracking.Filter{Mode: tracking.ModeMonth, Month: "2026-09"}
	p, err := f.Period(utc)
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	now := at(10, 12, 0)
	totals := []tracking.CompanyTotal{
		{CompanyName: "Acme", Entries: 2, Duration: 3 * time.Hour},
		{CompanyName: "Beta", Entries: 2, Duration: time.Hour},
	}
	entries := []tracking.Entry{
		{CompanyName: "Acme", Task: "running", StartedAt: at(10, 11, 0)},
		{CompanyName: "Beta", StartedAt: at(10, 8, 0), StoppedAt: at(10, 8, 30)},
		{CompanyName: "Acme", Task: "overnight", StartedAt: at(9, 23, 0), StoppedAt: at(10, 1, 0)},
	}

	r := BuildHistory(f, p, utc, now, totals, entries)

	if r.Label != "September 2026" {
		t.Errorf("label = %q", r.Label)
	}
	if r.Total != 4*time.Hour || r.Entries != 4 {
		t.Errorf("total = %v over %d entries, want 4h over 4", r.Total, r.Entries)
	}
	if r.Companies[0].Percent != 75 || r.Companies[0].BarWidth != 100 || r.Companies[1].BarWidth != 33 {
		t.Errorf("shares = %+v", r.Companies)
	}
	if len(r.Days) != 2 || r.Days[0].Label != "Thu 10 Sep" || r.Days[1].Label != "Wed 9 Sep" {
		t.Fatalf("days = %+v", r.Days)
	}
	if got := r.Days[0].Total; got != 90*time.Minute {
		t.Errorf("10 Sep total = %v, want 1h30 (the running hour plus half an hour)", got)
	}
	if got := r.Days[0].Entries[0].Range; got != "11:00 – now" {
		t.Errorf("running range = %q", got)
	}
	if got := r.Days[1].Entries[0].Range; got != "23:00 – Thu 01:00" {
		t.Errorf("overnight range = %q", got)
	}
	if !r.Truncated() {
		t.Error("three listed of four counted should read as truncated")
	}
}

func TestPeriodLabels(t *testing.T) {
	utc := time.UTC
	cases := []struct {
		f    tracking.Filter
		want string
	}{
		{tracking.Filter{Mode: tracking.ModeDay, Day: "2026-09-10"}, "Thursday, 10 September 2026"},
		{tracking.Filter{Mode: tracking.ModeYear, Year: "2026"}, "2026"},
		{tracking.Filter{Mode: tracking.ModeRange, From: "2026-09-01", To: "2026-09-10"}, "1 Sep – 10 Sep 2026"},
		{tracking.Filter{Mode: tracking.ModeRange, From: "2025-12-29", To: "2026-01-04"}, "29 Dec 2025 – 4 Jan 2026"},
		{tracking.Filter{Mode: tracking.ModeRange, From: "2026-09-10", To: "2026-09-10"}, "Thursday, 10 September 2026"},
	}
	for _, c := range cases {
		p, err := c.f.Period(utc)
		if err != nil {
			t.Fatalf("period %+v: %v", c.f, err)
		}
		if got := PeriodLabel(c.f.Mode, p); got != c.want {
			t.Errorf("label %+v = %q, want %q", c.f, got, c.want)
		}
	}
}

func TestHistorySignalsRoundTripAFilter(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	f := tracking.Filter{Mode: tracking.ModeRange, Day: "2026-09-02", Month: "2026-08", Year: "2025",
		From: "2026-09-01", To: "2026-09-05", CompanyID: 7}
	if got := SignalsFor(f).Filter(now); got != f {
		t.Fatalf("round trip = %+v, want %+v", got, f)
	}
	blank := HistorySignals{Company: "not a number"}.Filter(now)
	if blank != tracking.DefaultFilter(now) {
		t.Fatalf("blank signals = %+v, want the default filter", blank)
	}
}

func TestAShareIsLeftOffUntilThePeriodHasAnyTime(t *testing.T) {
	if got := shareLabel(CompanyShare{Entries: 1}, 0); got != "1 entry" {
		t.Errorf("share with nothing accumulated = %q, want the entry count alone", got)
	}
	if got := shareLabel(CompanyShare{Entries: 2, Percent: 75}, time.Hour); got != "2 entries · 75% of the total" {
		t.Errorf("share = %q", got)
	}
}
