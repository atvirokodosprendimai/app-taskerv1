package tracking

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// Store is the persistence the service writes through. [Repo] satisfies it.
type Store interface {
	CreateCompany(ctx context.Context, userID int64, name string, at time.Time) (Company, error)
	StartEntry(ctx context.Context, userID, companyID int64, task string, at time.Time) (Entry, error)
	StopEntry(ctx context.Context, userID, entryID int64, at time.Time) error
}

// Service is the write side of time tracking: the single writer of companies
// and entries. It never notifies anyone; the caller publishes after a write
// succeeds, so a failed write can never be announced.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService returns a Service writing through store.
func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

// AddCompany creates a company for userID.
//
// Whitespace runs collapse to one space, so "Acme  Corp" and "Acme Corp" are
// the same company rather than two that look identical in every list.
func (s *Service) AddCompany(ctx context.Context, userID int64, name string) (Company, error) {
	name = collapseSpace(name)
	if name == "" {
		return Company{}, ErrCompanyNameRequired
	}
	if utf8.RuneCountInString(name) > MaxCompanyName {
		return Company{}, ErrCompanyNameTooLong
	}
	return s.store.CreateCompany(ctx, userID, name, s.now())
}

// Start starts a timer on one of userID's companies. The task name may be
// empty; any number of timers may run at once, including on one company.
func (s *Service) Start(ctx context.Context, userID, companyID int64, task string) (Entry, error) {
	task = collapseSpace(task)
	if utf8.RuneCountInString(task) > MaxTask {
		return Entry{}, ErrTaskTooLong
	}
	return s.store.StartEntry(ctx, userID, companyID, task, s.now())
}

// Stop stops one of userID's running timers.
func (s *Service) Stop(ctx context.Context, userID, entryID int64) error {
	return s.store.StopEntry(ctx, userID, entryID, s.now())
}

// collapseSpace trims s and collapses every run of whitespace, newlines
// included, to a single space. Both names are one-line labels.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
