package view

import (
	"fmt"
	"time"
)

// Clock renders a duration as H:MM:SS — the running timers' display.
//
// Hours are not capped at 24: a timer left running over a weekend reads
// 61:02:15, which is the truth, rather than wrapping to a plausible time of day.
func Clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int64(d / time.Second)
	return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
}

// HoursMinutes renders a duration as "3h 05m", or "12m" under an hour — the
// history's display, where seconds are noise.
//
// It rounds to the nearest minute rather than truncating, so a column of entries
// and its total do not visibly disagree by the minute each entry lost.
func HoursMinutes(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := wholeMinutes(d)
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

// wholeMinutes rounds a duration to the nearest minute, the precision every
// figure the history shows is given in.
func wholeMinutes(d time.Duration) int64 { return int64((d + 30*time.Second) / time.Minute) }

// DecimalHours renders a duration as hours to two places, "3.08" — the figure
// an invoice wants.
func DecimalHours(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%.2f", d.Hours())
}
