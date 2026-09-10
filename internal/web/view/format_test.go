package view

import (
	"testing"
	"time"
)

func TestClock(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0:00:00"},
		{59 * time.Second, "0:00:59"},
		{61 * time.Second, "0:01:01"},
		{time.Hour + 2*time.Minute + 3*time.Second, "1:02:03"},
		{61*time.Hour + 2*time.Minute + 15*time.Second, "61:02:15"},
		{1500 * time.Millisecond, "0:00:01"},
		{-time.Minute, "0:00:00"},
	}
	for _, c := range cases {
		if got := Clock(c.d); got != c.want {
			t.Errorf("Clock(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestHoursMinutes(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0m"},
		{29 * time.Second, "0m"},
		{30 * time.Second, "1m"},
		{59 * time.Minute, "59m"},
		{59*time.Minute + 45*time.Second, "1h 00m"},
		{3*time.Hour + 5*time.Minute, "3h 05m"},
		{125 * time.Hour, "125h 00m"},
	}
	for _, c := range cases {
		if got := HoursMinutes(c.d); got != c.want {
			t.Errorf("HoursMinutes(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestDecimalHours(t *testing.T) {
	if got := DecimalHours(3*time.Hour + 5*time.Minute); got != "3.08" {
		t.Errorf("DecimalHours = %q, want 3.08", got)
	}
}
