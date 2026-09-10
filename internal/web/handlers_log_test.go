package web

import (
	"errors"
	"testing"
	"time"
)

func TestWhenLoggedTimeStarted(t *testing.T) {
	vilnius, err := time.LoadLocation("Europe/Vilnius")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	now := time.Date(2026, 9, 10, 15, 30, 0, 0, vilnius)
	at := func(d, h, m int) time.Time { return time.Date(2026, 9, d, h, m, 0, 0, vilnius) }
	cases := []struct {
		name, date, at string
		d              time.Duration
		want           time.Time
		err            error
	}{
		{"today with no start time ends now", "2026-09-10", "", 20 * time.Minute, at(10, 15, 10), nil},
		{"a blank day means today", "", "", time.Hour, at(10, 14, 30), nil},
		{"today with a start time starts then", "2026-09-10", "09:05", time.Hour, at(10, 9, 5), nil},
		{"another day with a start time", "2026-09-09", "09:15", time.Hour, at(9, 9, 15), nil},
		{"seconds from a time picker", "2026-09-09", "09:15:00", time.Hour, at(9, 9, 15), nil},
		{"another day needs a start time", "2026-09-09", "", time.Hour, time.Time{}, errStartTimeRequired},
		{"a day that is not a date", "yesterday", "09:00", time.Hour, time.Time{}, errInvalidWhen},
		{"a time that is not a time", "2026-09-09", "9am", time.Hour, time.Time{}, errInvalidWhen},
	}
	for _, c := range cases {
		got, err := logStart(c.date, c.at, c.d, now)
		if !errors.Is(err, c.err) || !got.Equal(c.want) {
			t.Errorf("%s: logStart(%q, %q, %v) = %v, %v; want %v, %v", c.name, c.date, c.at, c.d, got, err, c.want, c.err)
		}
	}
}
