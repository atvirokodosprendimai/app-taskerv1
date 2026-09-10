package tracking_test

import (
	"errors"
	"testing"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

func TestParseDurationReadsWhatPeopleType(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"45m", 45 * time.Minute},
		{"45 min", 45 * time.Minute},
		{"20M", 20 * time.Minute},
		{" 20m ", 20 * time.Minute},
		{"1h30m", 90 * time.Minute},
		{"1h30", 90 * time.Minute},
		{"1 h 30 mins", 90 * time.Minute},
		{"2 hours", 2 * time.Hour},
		{"1,5h", 90 * time.Minute},
		{"1:30", 90 * time.Minute},
		{"0:05", 5 * time.Minute},
		{"1.5", 90 * time.Minute},
		{"1,5", 90 * time.Minute},
		{"2", 2 * time.Hour},
		{"0.01", time.Minute},
		{"24h", 24 * time.Hour},
		{"24:00", 24 * time.Hour},
	}
	for _, c := range cases {
		got, err := tracking.ParseDuration(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
}

func TestParseDurationRefusesWhatItCannotRead(t *testing.T) {
	cases := []struct {
		in   string
		want error
	}{
		{"", tracking.ErrInvalidDuration},
		{"soon", tracking.ErrInvalidDuration},
		{"0", tracking.ErrInvalidDuration},
		{"0:00", tracking.ErrInvalidDuration},
		{"20s", tracking.ErrInvalidDuration},
		{"-1", tracking.ErrInvalidDuration},
		{"-30m", tracking.ErrInvalidDuration},
		{"1:5", tracking.ErrInvalidDuration},
		{"1:60", tracking.ErrInvalidDuration},
		{"1e3", tracking.ErrInvalidDuration},
		{"1..5", tracking.ErrInvalidDuration},
		{"h", tracking.ErrInvalidDuration},
		{"25h", tracking.ErrDurationTooLong},
		{"24:01", tracking.ErrDurationTooLong},
		{"45", tracking.ErrDurationTooLong},
		{"99999999999999999999", tracking.ErrDurationTooLong},
		{"99999999999999999999:00", tracking.ErrInvalidDuration},
	}
	for _, c := range cases {
		if got, err := tracking.ParseDuration(c.in); !errors.Is(err, c.want) {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
}
