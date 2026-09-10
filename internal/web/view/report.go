package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

// Report renders a period's entries as a plain-text report to send on: the
// period's total on the first line, then one line per entry saying how long it
// took and what it was.
//
// When each entry ran is left out on purpose — M: "dont add "since when to
// when" just how long, what". Entries arrive newest first, as Repo.Entries
// returns them, and are listed oldest first, the order work is read back in.
//
// Each entry counts only its time inside the period, rounded to the minute, and
// the total is the sum of those lines, so the lines always add up to the first
// one. That can be a minute off the history page, which rounds the exact sum
// instead. An entry that rounds to no time at all is left out: it would add a
// line and nothing to the total. With oneCompany the company name leaves every
// line, since it would be the same on all of them.
func Report(p tracking.Period, now time.Time, entries []tracking.Entry, oneCompany bool) string {
	var (
		lines []string
		total int64
	)
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		minutes := wholeMinutes(p.Clip(e.StartedAt, e.End(now)))
		if minutes == 0 {
			continue
		}
		total += minutes
		lines = append(lines, reportDuration(minutes)+" "+reportWhat(e, oneCompany))
	}

	var b strings.Builder
	b.WriteString("total for period " + reportDuration(total) + "\n")
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
	return b.String()
}

// reportDuration renders minutes the way the report asks for them, "1h05m".
// Hours are always shown, so every line starts with the same shape.
func reportDuration(minutes int64) string {
	return fmt.Sprintf("%dh%02dm", minutes/60, minutes%60)
}

// reportWhat names an entry for the report: its task, after its company unless
// the report is for one company. An entry with no task is named by its company.
// A timer still running says so, because its time is only as of now.
func reportWhat(e tracking.Entry, oneCompany bool) string {
	what := e.CompanyName
	switch {
	case e.Task != "" && oneCompany:
		what = e.Task
	case e.Task != "":
		what = e.CompanyName + " — " + e.Task
	}
	if e.Running() {
		what += " (running)"
	}
	return what
}
