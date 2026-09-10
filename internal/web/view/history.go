package view

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

// History is the history screen.
type History struct {
	Page      Page
	Filter    tracking.Filter
	Companies []tracking.Company
	Results   HistoryResults
}

// HistorySignals is the history filter as the page's datastar signals.
//
// One type serves both directions — the page seeds its data-signals from it and
// the results handler decodes requests into it — so the JSON names cannot drift
// apart. The inputs carry no value= of their own: an ancestor's data-signals
// that declares a key wins over a bound input's value, so giving both a value
// would leave the page with two sources that can disagree.
type HistorySignals struct {
	Mode    string `json:"histMode"`
	Day     string `json:"histDay"`
	Month   string `json:"histMonth"`
	Year    string `json:"histYear"`
	From    string `json:"histFrom"`
	To      string `json:"histTo"`
	Company string `json:"histCompany"`
}

// SignalsFor renders a filter as signals.
func SignalsFor(f tracking.Filter) HistorySignals {
	s := HistorySignals{
		Mode: string(f.Mode), Day: f.Day, Month: f.Month, Year: f.Year, From: f.From, To: f.To,
	}
	if f.CompanyID > 0 {
		s.Company = strconv.FormatInt(f.CompanyID, 10)
	}
	return s
}

// Filter converts signals back into a filter. A blank field takes its default
// around now, and a company that does not parse means every company. Whether
// the result names a real period is decided later, by [tracking.Filter.Period].
func (s HistorySignals) Filter(now time.Time) tracking.Filter {
	f := tracking.DefaultFilter(now)
	if v := strings.TrimSpace(s.Mode); v != "" {
		f.Mode = tracking.Mode(v)
	}
	set := func(dst *string, v string) {
		if v = strings.TrimSpace(v); v != "" {
			*dst = v
		}
	}
	set(&f.Day, s.Day)
	set(&f.Month, s.Month)
	set(&f.Year, s.Year)
	set(&f.From, s.From)
	set(&f.To, s.To)
	if id, err := strconv.ParseInt(strings.TrimSpace(s.Company), 10, 64); err == nil && id > 0 {
		f.CompanyID = id
	}
	return f
}

// HistoryResults is everything below the filter bar.
type HistoryResults struct {
	// Error replaces the results when the filter names no period.
	Error string
	// Label names the period in words, "September 2026".
	Label string
	// Total is the tracked time inside the period, across every matching entry.
	Total time.Duration
	// Entries is how many entries overlap the period.
	Entries int
	// Companies is the per-company breakdown, largest first.
	Companies []CompanyShare
	// Days groups the listed entries by the day each started, newest first.
	Days []Day
	// Shown is how many entries are listed; fewer than Entries when the list
	// was limited. Totals always cover every entry.
	Shown int
}

// Truncated reports whether the list shows fewer entries than the totals count.
func (r HistoryResults) Truncated() bool { return r.Shown < r.Entries }

// CompanyShare is one company's part of a period.
type CompanyShare struct {
	Name     string
	Entries  int
	Duration time.Duration
	// Percent is this company's share of the period's total.
	Percent int
	// BarWidth is its duration relative to the largest company's, 0–100, so
	// the longest bar always fills its track.
	BarWidth int
}

// Day is the entries that started on one day.
type Day struct {
	Label   string
	Total   time.Duration
	Entries []EntryRow
}

// EntryRow is one listed entry.
type EntryRow struct {
	Company string
	Task    string
	// Range is when it ran, "09:00 – 10:30".
	Range string
	// Duration is the part inside the period, which is what the totals count.
	Duration time.Duration
	Running  bool
	// Manual marks an entry logged by hand afterwards rather than timed live.
	Manual bool
}

// BuildHistory assembles the results for a period from its totals and its
// listed entries. Times are shown in loc; a running entry counts up to now.
func BuildHistory(f tracking.Filter, p tracking.Period, loc *time.Location, now time.Time,
	totals []tracking.CompanyTotal, entries []tracking.Entry) HistoryResults {
	r := HistoryResults{Label: PeriodLabel(f.Mode, p), Shown: len(entries)}

	var largest time.Duration
	for _, t := range totals {
		r.Total += t.Duration
		r.Entries += t.Entries
		largest = max(largest, t.Duration)
	}
	for _, t := range totals {
		share := CompanyShare{Name: t.CompanyName, Entries: t.Entries, Duration: t.Duration}
		if r.Total > 0 {
			share.Percent = int(math.Round(float64(t.Duration) * 100 / float64(r.Total)))
		}
		if largest > 0 {
			share.BarWidth = int(math.Round(float64(t.Duration) * 100 / float64(largest)))
		}
		r.Companies = append(r.Companies, share)
	}

	today := now.In(loc)
	for _, e := range entries {
		start, end := e.StartedAt.In(loc), e.End(now).In(loc)
		row := EntryRow{
			Company:  e.CompanyName,
			Task:     e.Task,
			Range:    entryRange(start, end, e.Running()),
			Duration: p.Clip(e.StartedAt, e.End(now)),
			Running:  e.Running(),
			Manual:   e.Manual,
		}
		// Entries arrive newest first, so one day's entries are contiguous.
		label := dayLabel(start, today)
		if n := len(r.Days); n == 0 || r.Days[n-1].Label != label {
			r.Days = append(r.Days, Day{Label: label})
		}
		d := &r.Days[len(r.Days)-1]
		d.Entries = append(d.Entries, row)
		d.Total += row.Duration
	}
	return r
}

// PeriodLabel names a period in words. A range shows its last day, which is
// inclusive, rather than the exclusive end the period stores.
func PeriodLabel(mode tracking.Mode, p tracking.Period) string {
	last := p.To.AddDate(0, 0, -1)
	switch mode {
	case tracking.ModeDay:
		return p.From.Format("Monday, 2 January 2006")
	case tracking.ModeMonth:
		return p.From.Format("January 2006")
	case tracking.ModeYear:
		return p.From.Format("2006")
	}
	switch {
	case sameDate(p.From, last):
		return p.From.Format("Monday, 2 January 2006")
	case p.From.Year() == last.Year():
		return p.From.Format("2 Jan") + " – " + last.Format("2 Jan 2006")
	default:
		return p.From.Format("2 Jan 2006") + " – " + last.Format("2 Jan 2006")
	}
}

// dayLabel names the day an entry started, adding the year only when it is not
// the current one.
func dayLabel(t, today time.Time) string {
	if t.Year() == today.Year() {
		return t.Format("Mon 2 Jan")
	}
	return t.Format("Mon 2 Jan 2006")
}

// entryRange renders when an entry ran.
func entryRange(start, end time.Time, running bool) string {
	switch {
	case running:
		return start.Format("15:04") + " – now"
	case sameDate(start, end):
		return start.Format("15:04") + " – " + end.Format("15:04")
	default:
		return start.Format("15:04") + " – " + end.Format("Mon 15:04")
	}
}

func sameDate(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// barStyle sets a breakdown bar's fill.
func barStyle(width int) string { return "width:" + strconv.Itoa(width) + "%" }

// shareLabel describes one company's line in the breakdown: how many entries it
// has and its share of the period. The share is left off while the period's
// total is still zero — a timer started this very second — where every share
// would read 0% and the only company would look as if it had none of the time.
func shareLabel(c CompanyShare, total time.Duration) string {
	if total <= 0 {
		return entriesLabel(c.Entries)
	}
	return entriesLabel(c.Entries) + " · " + strconv.Itoa(c.Percent) + "% of the total"
}
