// Package tracking is the time-tracking context: the companies a person works
// for and the timers they run against them.
//
// Two aggregates live here. A Company is named once; a time Entry is started
// and later stopped, and any number may run at the same time. [Service] is the
// only writer of both. Pages read straight from [Repo], which is the CQRS split.
package tracking

import (
	"errors"
	"time"
)

// Refusals a caller can act on. The web layer turns each into a sentence.
var (
	// ErrCompanyNameRequired reports a blank company name.
	ErrCompanyNameRequired = errors.New("tracking: company name is required")
	// ErrCompanyNameTooLong reports a name over [MaxCompanyName] characters.
	ErrCompanyNameTooLong = errors.New("tracking: company name is too long")
	// ErrCompanyExists reports that the user already has a company of that name.
	ErrCompanyExists = errors.New("tracking: company already exists")
	// ErrTaskTooLong reports a task name over [MaxTask] characters.
	ErrTaskTooLong = errors.New("tracking: task name is too long")
	// ErrNotFound reports a company or entry that does not exist FOR THIS
	// USER, or an entry that has been deleted. Someone else's id and an id
	// nobody has are the same answer, so ids cannot be probed for.
	ErrNotFound = errors.New("tracking: not found")
	// ErrInvalidPeriod reports a history filter that names no period.
	ErrInvalidPeriod = errors.New("tracking: invalid period")
)

// Length limits, in characters.
const (
	MaxCompanyName = 80
	MaxTask        = 200
)

// Company is someone a user tracks time for.
type Company struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	// Running is how many of the user's timers are running for this company.
	// It is filled by read queries only.
	Running int
}

// Entry is one timer: started, and stopped once it is done.
type Entry struct {
	ID          int64
	CompanyID   int64
	CompanyName string
	// Task is the short description typed when the timer was started, or put
	// right since. It may be empty.
	Task      string
	StartedAt time.Time
	// StoppedAt is the zero time while the timer is running.
	StoppedAt time.Time
	// Manual reports an entry logged by hand afterwards rather than timed live.
	Manual bool
}

// Running reports whether the timer has not been stopped.
func (e Entry) Running() bool { return e.StoppedAt.IsZero() }

// End returns when the entry ended, or now while it is still running.
func (e Entry) End(now time.Time) time.Time {
	if e.Running() {
		return now
	}
	return e.StoppedAt
}

// Elapsed returns how long the entry has run, as of now for a running one.
func (e Entry) Elapsed(now time.Time) time.Duration {
	d := e.End(now).Sub(e.StartedAt)
	if d < 0 {
		return 0
	}
	return d
}

// CompanyTotal is the time one company accumulated within a period.
type CompanyTotal struct {
	CompanyID   int64
	CompanyName string
	// Entries is how many entries overlapped the period.
	Entries int
	// Duration is the overlapping time only, not the entries' full length.
	Duration time.Duration
}
