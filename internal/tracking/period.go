package tracking

import (
	"fmt"
	"math"
	"time"
)

// Layouts the history filter speaks. They are also the value formats of the
// browser's date and month inputs, so a filter round-trips through the page
// unchanged.
const (
	DateLayout  = "2006-01-02"
	MonthLayout = "2006-01"
	YearLayout  = "2006"
)

// Period is a half-open interval of time, [From, To).
type Period struct {
	From, To time.Time
}

// Clip returns how much of the interval [start, end) falls inside the period.
//
// History totals count only the overlap, so a timer running from 23:00 to 01:00
// contributes an hour to each of the two days it touches instead of two hours
// to the day it started on.
func (p Period) Clip(start, end time.Time) time.Duration {
	if start.Before(p.From) {
		start = p.From
	}
	if end.After(p.To) {
		end = p.To
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start)
}

// Mode is how a history filter names its period.
type Mode string

// The four ways a period can be named.
const (
	ModeDay   Mode = "day"
	ModeMonth Mode = "month"
	ModeYear  Mode = "year"
	ModeRange Mode = "range"
)

// Filter is a history query as a person states it: one day, one month, one
// year, or a range of days, optionally narrowed to one company.
//
// Every field is kept even when Mode ignores it, so switching modes on the page
// shows the value last used there rather than a blank.
type Filter struct {
	Mode  Mode
	Day   string // DateLayout
	Month string // MonthLayout
	Year  string // YearLayout
	// From and To are DateLayout, and To is INCLUSIVE.
	From, To string
	// CompanyID narrows the query to one company; zero means every company.
	CompanyID int64
}

// DefaultFilter is the month containing now, with every other field set to its
// equivalent around now. Pass now already in the user's location.
func DefaultFilter(now time.Time) Filter {
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	return Filter{
		Mode:  ModeMonth,
		Day:   now.Format(DateLayout),
		Month: now.Format(MonthLayout),
		Year:  now.Format(YearLayout),
		From:  first.Format(DateLayout),
		To:    now.Format(DateLayout),
	}
}

// Period resolves the filter to the interval it names, with day boundaries at
// midnight in loc — so a day is 23 or 25 hours long across a daylight-saving
// change, which is what the person reading the history means by "that day".
func (f Filter) Period(loc *time.Location) (Period, error) {
	switch f.Mode {
	case ModeDay:
		d, err := time.ParseInLocation(DateLayout, f.Day, loc)
		if err != nil {
			return Period{}, fmt.Errorf("%w: day %q", ErrInvalidPeriod, f.Day)
		}
		return Period{From: d, To: d.AddDate(0, 0, 1)}, nil
	case ModeMonth:
		m, err := time.ParseInLocation(MonthLayout, f.Month, loc)
		if err != nil {
			return Period{}, fmt.Errorf("%w: month %q", ErrInvalidPeriod, f.Month)
		}
		return Period{From: m, To: m.AddDate(0, 1, 0)}, nil
	case ModeYear:
		y, err := time.ParseInLocation(YearLayout, f.Year, loc)
		if err != nil {
			return Period{}, fmt.Errorf("%w: year %q", ErrInvalidPeriod, f.Year)
		}
		return Period{From: y, To: y.AddDate(1, 0, 0)}, nil
	case ModeRange:
		from, err := time.ParseInLocation(DateLayout, f.From, loc)
		if err != nil {
			return Period{}, fmt.Errorf("%w: from %q", ErrInvalidPeriod, f.From)
		}
		to, err := time.ParseInLocation(DateLayout, f.To, loc)
		if err != nil {
			return Period{}, fmt.Errorf("%w: to %q", ErrInvalidPeriod, f.To)
		}
		if to.Before(from) {
			return Period{}, fmt.Errorf("%w: %s is before %s", ErrInvalidPeriod, f.To, f.From)
		}
		// The last day is inclusive: "1 to 10 September" runs to the end of the
		// 10th, which is how a person reads a range of dates.
		return Period{From: from, To: to.AddDate(0, 0, 1)}, nil
	default:
		return Period{}, fmt.Errorf("%w: unknown mode %q", ErrInvalidPeriod, f.Mode)
	}
}

// Shift moves the filter by n of its own units — days, months or years — or,
// for a range, by n times the range's length in days. It is what "previous" and
// "next" mean on the history page.
func (f Filter) Shift(n int, loc *time.Location) (Filter, error) {
	p, err := f.Period(loc)
	if err != nil {
		return f, err
	}
	switch f.Mode {
	case ModeDay:
		f.Day = p.From.AddDate(0, 0, n).Format(DateLayout)
	case ModeMonth:
		f.Month = p.From.AddDate(0, n, 0).Format(MonthLayout)
	case ModeYear:
		f.Year = p.From.AddDate(n, 0, 0).Format(YearLayout)
	case ModeRange:
		// Rounded rather than truncated: across a daylight-saving change the
		// interval is a whole number of days give or take an hour.
		days := int(math.Round(p.To.Sub(p.From).Hours() / 24))
		f.From = p.From.AddDate(0, 0, n*days).Format(DateLayout)
		f.To = p.To.AddDate(0, 0, n*days-1).Format(DateLayout)
	}
	return f, nil
}
