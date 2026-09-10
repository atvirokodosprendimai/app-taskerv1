package web

import (
	"testing"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

func TestThisWeekRunsFromMondayToSunday(t *testing.T) {
	vilnius, err := time.LoadLocation("Europe/Vilnius")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	cases := []struct {
		name     string
		now      time.Time
		from, to string
	}{
		{"a Thursday", time.Date(2026, 9, 10, 9, 0, 0, 0, vilnius), "2026-09-07", "2026-09-13"},
		{"Monday just after midnight", time.Date(2026, 9, 7, 0, 5, 0, 0, vilnius), "2026-09-07", "2026-09-13"},
		{"Sunday night", time.Date(2026, 9, 13, 23, 55, 0, 0, vilnius), "2026-09-07", "2026-09-13"},
		{"a week across two months", time.Date(2026, 10, 1, 12, 0, 0, 0, vilnius), "2026-09-28", "2026-10-04"},
		{"the Sunday the clocks go back", time.Date(2026, 10, 25, 12, 0, 0, 0, vilnius), "2026-10-19", "2026-10-25"},
	}
	for _, c := range cases {
		f, ok := presetFilter("week", tracking.Filter{}, c.now)
		if !ok || f.Mode != tracking.ModeRange || f.From != c.from || f.To != c.to {
			t.Errorf("%s: week = %+v (ok %v), want a range from %s to %s", c.name, f, ok, c.from, c.to)
		}
	}
}

func TestPresetsKeepTheCompanyAndEveryOtherModesValue(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC) // a Thursday
	// Every field differs from its default around now, so a preset that reset
	// the fields it does not use, rather than keeping them, is caught.
	start := tracking.Filter{
		Mode: tracking.ModeRange, Day: "2019-03-04", Month: "2019-03", Year: "2019",
		From: "2019-03-01", To: "2019-03-31", CompanyID: 7,
	}
	with := func(edit func(*tracking.Filter)) tracking.Filter {
		f := start
		edit(&f)
		return f
	}
	cases := []struct {
		preset string
		want   tracking.Filter
	}{
		{"today", with(func(f *tracking.Filter) { f.Mode, f.Day = tracking.ModeDay, "2026-01-15" })},
		{"week", with(func(f *tracking.Filter) { f.From, f.To = "2026-01-12", "2026-01-18" })},
		{"month", with(func(f *tracking.Filter) { f.Mode, f.Month = tracking.ModeMonth, "2026-01" })},
		{"last-month", with(func(f *tracking.Filter) { f.Mode, f.Month = tracking.ModeMonth, "2025-12" })},
		{"year", with(func(f *tracking.Filter) { f.Mode, f.Year = tracking.ModeYear, "2026" })},
	}
	for _, c := range cases {
		got, ok := presetFilter(c.preset, start, now)
		if !ok || got != c.want {
			t.Errorf("preset %q = %+v (ok %v), want %+v", c.preset, got, ok, c.want)
		}
	}
	if got, ok := presetFilter("next-decade", start, now); ok || got != start {
		t.Errorf("an unknown preset = %+v (ok %v), want the filter unchanged and ok false", got, ok)
	}
}

func TestAHistoryURLNamesTheSamePeriodAsItsFilter(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	// Each field the mode reads differs from its default around now, so a query
	// that dropped it would come back naming a different period.
	for _, f := range []tracking.Filter{
		{Mode: tracking.ModeDay, Day: "2026-08-31"},
		{Mode: tracking.ModeMonth, Month: "2025-12", CompanyID: 3},
		{Mode: tracking.ModeYear, Year: "2024"},
		{Mode: tracking.ModeRange, From: "2026-08-20", To: "2026-09-05"},
	} {
		got := filterFromQuery(historyQuery(f), now)
		want, err := f.Period(time.UTC)
		if err != nil {
			t.Fatalf("period of %+v: %v", f, err)
		}
		p, err := got.Period(time.UTC)
		if err != nil || got.Mode != f.Mode || got.CompanyID != f.CompanyID ||
			!p.From.Equal(want.From) || !p.To.Equal(want.To) {
			t.Errorf("%+v came back as %+v (period %v to %v, err %v)", f, got, p.From, p.To, err)
		}
	}
}
