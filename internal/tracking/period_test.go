package tracking

import (
	"errors"
	"testing"
	"time"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load zone %s: %v", name, err)
	}
	return loc
}

func TestADayIsMidnightToMidnightInTheUsersZone(t *testing.T) {
	vilnius := mustZone(t, "Europe/Vilnius")

	p, err := Filter{Mode: ModeDay, Day: "2026-09-10"}.Period(vilnius)
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	wantFrom := time.Date(2026, 9, 10, 0, 0, 0, 0, vilnius)
	if !p.From.Equal(wantFrom) || !p.To.Equal(wantFrom.AddDate(0, 0, 1)) {
		t.Fatalf("period = %v – %v, want local midnight to midnight", p.From, p.To)
	}
	// Vilnius is UTC+3 in September, so the day starts at 21:00 UTC the
	// evening before. A server reckoning days in UTC would be three hours out.
	if got := p.From.UTC(); got != time.Date(2026, 9, 9, 21, 0, 0, 0, time.UTC) {
		t.Fatalf("day starts at %v UTC, want 2026-09-09 21:00", got)
	}
}

func TestTheDayClocksGoForwardIs23Hours(t *testing.T) {
	// Europe/Vilnius moves to summer time on the last Sunday of March.
	p, err := Filter{Mode: ModeDay, Day: "2026-03-29"}.Period(mustZone(t, "Europe/Vilnius"))
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	if got := p.To.Sub(p.From); got != 23*time.Hour {
		t.Fatalf("day length = %v, want 23h", got)
	}
}

func TestMonthYearAndRangeBoundaries(t *testing.T) {
	utc := time.UTC
	cases := []struct {
		name     string
		f        Filter
		from, to time.Time
	}{
		{"february", Filter{Mode: ModeMonth, Month: "2026-02"},
			time.Date(2026, 2, 1, 0, 0, 0, 0, utc), time.Date(2026, 3, 1, 0, 0, 0, 0, utc)},
		{"year", Filter{Mode: ModeYear, Year: "2026"},
			time.Date(2026, 1, 1, 0, 0, 0, 0, utc), time.Date(2027, 1, 1, 0, 0, 0, 0, utc)},
		{"range includes its last day", Filter{Mode: ModeRange, From: "2026-09-01", To: "2026-09-10"},
			time.Date(2026, 9, 1, 0, 0, 0, 0, utc), time.Date(2026, 9, 11, 0, 0, 0, 0, utc)},
		{"one-day range", Filter{Mode: ModeRange, From: "2026-09-10", To: "2026-09-10"},
			time.Date(2026, 9, 10, 0, 0, 0, 0, utc), time.Date(2026, 9, 11, 0, 0, 0, 0, utc)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := c.f.Period(utc)
			if err != nil {
				t.Fatalf("period: %v", err)
			}
			if !p.From.Equal(c.from) || !p.To.Equal(c.to) {
				t.Fatalf("period = %v – %v, want %v – %v", p.From, p.To, c.from, c.to)
			}
		})
	}
}

func TestAFilterThatNamesNoPeriodIsRefused(t *testing.T) {
	for name, f := range map[string]Filter{
		"bad day":         {Mode: ModeDay, Day: "2026-13-01"},
		"empty month":     {Mode: ModeMonth},
		"bad year":        {Mode: ModeYear, Year: "twenty"},
		"range backwards": {Mode: ModeRange, From: "2026-09-10", To: "2026-09-01"},
		"unknown mode":    {Mode: "week", Day: "2026-09-10"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.Period(time.UTC); !errors.Is(err, ErrInvalidPeriod) {
				t.Fatalf("err = %v, want ErrInvalidPeriod", err)
			}
		})
	}
}

func TestClipCountsOnlyTheOverlap(t *testing.T) {
	day := Period{
		From: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC),
	}
	at := func(d, h, m int) time.Time { return time.Date(2026, 9, d, h, m, 0, 0, time.UTC) }
	cases := []struct {
		name       string
		start, end time.Time
		want       time.Duration
	}{
		{"inside", at(10, 9, 0), at(10, 10, 30), 90 * time.Minute},
		{"crosses the start", at(9, 23, 0), at(10, 1, 0), time.Hour},
		{"crosses the end", at(10, 23, 30), at(11, 0, 30), 30 * time.Minute},
		{"spans the whole day", at(9, 12, 0), at(11, 12, 0), 24 * time.Hour},
		{"before", at(9, 8, 0), at(9, 9, 0), 0},
		{"after", at(11, 8, 0), at(11, 9, 0), 0},
		{"ends exactly at the start", at(9, 23, 0), at(10, 0, 0), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := day.Clip(c.start, c.end); got != c.want {
				t.Fatalf("clip = %v, want %v", got, c.want)
			}
		})
	}
}

func TestShiftMovesByTheFiltersOwnUnit(t *testing.T) {
	utc := time.UTC
	cases := []struct {
		name string
		f    Filter
		n    int
		want Filter
	}{
		{"previous month across a year", Filter{Mode: ModeMonth, Month: "2026-01"}, -1,
			Filter{Mode: ModeMonth, Month: "2025-12"}},
		{"next day across a month", Filter{Mode: ModeDay, Day: "2026-09-30"}, 1,
			Filter{Mode: ModeDay, Day: "2026-10-01"}},
		{"previous year", Filter{Mode: ModeYear, Year: "2026"}, -1,
			Filter{Mode: ModeYear, Year: "2025"}},
		{"next ten-day range", Filter{Mode: ModeRange, From: "2026-09-01", To: "2026-09-10"}, 1,
			Filter{Mode: ModeRange, From: "2026-09-11", To: "2026-09-20"}},
		{"previous one-day range", Filter{Mode: ModeRange, From: "2026-09-10", To: "2026-09-10"}, -1,
			Filter{Mode: ModeRange, From: "2026-09-09", To: "2026-09-09"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.f.Shift(c.n, utc)
			if err != nil {
				t.Fatalf("shift: %v", err)
			}
			if got != c.want {
				t.Fatalf("shift = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestTheDefaultFilterIsThisMonth(t *testing.T) {
	now := time.Date(2026, 9, 10, 14, 0, 0, 0, time.UTC)
	f := DefaultFilter(now)
	want := Filter{Mode: ModeMonth, Day: "2026-09-10", Month: "2026-09", Year: "2026", From: "2026-09-01", To: "2026-09-10"}
	if f != want {
		t.Fatalf("default = %+v, want %+v", f, want)
	}
}
