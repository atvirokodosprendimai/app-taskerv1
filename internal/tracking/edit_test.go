package tracking_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

func TestRenamePutsRightTheTaskNameOfARunningOrStoppedEntry(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	acme := f.addCompany(t, ada, "Acme")
	running, err := f.svc.Start(t.Context(), ada, acme.ID, "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	stopped, err := f.svc.Start(t.Context(), ada, acme.ID, "Invoces")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := f.svc.Stop(t.Context(), ada, stopped.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	rename := func(id int64, task string) {
		t.Helper()
		if err := f.svc.Rename(t.Context(), ada, id, task); err != nil {
			t.Fatalf("rename %d to %q: %v", id, task, err)
		}
	}
	entry := func(id int64) tracking.Entry {
		t.Helper()
		e, err := f.repo.Entry(t.Context(), ada, id)
		if err != nil {
			t.Fatalf("entry %d: %v", id, err)
		}
		return e
	}

	rename(running.ID, "  Quarterly \t report ")
	if e := entry(running.ID); e.Task != "Quarterly report" || !e.Running() || e.CompanyName != "Acme" {
		t.Errorf("renamed running entry = %+v, want Acme's timer still running, named Quarterly report", e)
	}
	rename(stopped.ID, "Invoices")
	if e := entry(stopped.ID); e.Task != "Invoices" || e.Running() {
		t.Errorf("renamed stopped entry = %+v, want it stopped and named Invoices", e)
	}
	// The name the entry already has is not a missing entry.
	rename(stopped.ID, "Invoices")
	// And a name can be taken away again.
	rename(stopped.ID, " ")
	if e := entry(stopped.ID); e.Task != "" {
		t.Errorf("task after clearing it = %q, want empty", e.Task)
	}
}

func TestRenameRefusesAnOverlongNameAndSomeoneElsesEntry(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	bob := f.addUser(t, "bob@example.com")
	acme := f.addCompany(t, ada, "Acme")
	e, err := f.svc.Start(t.Context(), ada, acme.ID, "mine")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := f.svc.Rename(t.Context(), ada, e.ID, strings.Repeat("x", tracking.MaxTask+1)); !errors.Is(err, tracking.ErrTaskTooLong) {
		t.Errorf("overlong rename err = %v, want ErrTaskTooLong", err)
	}
	if err := f.svc.Rename(t.Context(), bob, e.ID, "not mine"); !errors.Is(err, tracking.ErrNotFound) {
		t.Errorf("bob's rename err = %v, want ErrNotFound", err)
	}
	if err := f.svc.Rename(t.Context(), ada, e.ID+1000, "nobody's"); !errors.Is(err, tracking.ErrNotFound) {
		t.Errorf("renaming a missing entry err = %v, want ErrNotFound", err)
	}
	if _, err := f.repo.Entry(t.Context(), bob, e.ID); !errors.Is(err, tracking.ErrNotFound) {
		t.Errorf("bob reading ada's entry err = %v, want ErrNotFound", err)
	}
	if got, err := f.repo.Entry(t.Context(), ada, e.ID); err != nil || got.Task != "mine" {
		t.Fatalf("ada's entry after the refused renames = %+v, %v; want it still named mine", got, err)
	}
	// Ada's own rename of the same entry goes through, so the refusals above were
	// about the name's length and whose entry it is.
	if err := f.svc.Rename(t.Context(), ada, e.ID, "still mine"); err != nil {
		t.Fatalf("ada's rename: %v", err)
	}
}

// shown is what the reads show of one user's entries.
type shown struct {
	running        int
	listed         int
	companyRunning int
	total          time.Duration
}

func (f fixture) shown(t *testing.T, userID int64, p tracking.Period, now time.Time) shown {
	t.Helper()
	var s shown
	running, err := f.repo.Running(t.Context(), userID)
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	s.running = len(running)
	listed, err := f.repo.Entries(t.Context(), userID, p, 0, now, tracking.AllEntries)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	s.listed = len(listed)
	totals, err := f.repo.Totals(t.Context(), userID, p, 0, now)
	if err != nil {
		t.Fatalf("totals: %v", err)
	}
	for _, c := range totals {
		s.total += c.Duration
	}
	companies, err := f.repo.Companies(t.Context(), userID)
	if err != nil {
		t.Fatalf("companies: %v", err)
	}
	for _, c := range companies {
		s.companyRunning += c.Running
	}
	return s
}

func TestADeletedEntryLeavesEveryReadAndComesBackAsItWas(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	acme := f.addCompany(t, ada, "Acme")
	start := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	logged, err := f.svc.Log(t.Context(), ada, acme.ID, "Phone call", start, 30*time.Minute)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	running, err := f.svc.Start(t.Context(), ada, acme.ID, "Started by mistake")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	now := time.Now().Add(time.Minute)
	p := tracking.Period{From: start.Add(-time.Hour), To: now.Add(time.Hour)}

	before := f.shown(t, ada, p, now)
	if before.running != 1 || before.listed != 2 || before.companyRunning != 1 || before.total < 30*time.Minute {
		t.Fatalf("before deleting, the reads show %+v; want both entries", before)
	}

	for _, id := range []int64{logged.ID, running.ID} {
		if err := f.svc.Delete(t.Context(), ada, id); err != nil {
			t.Fatalf("delete %d: %v", id, err)
		}
	}
	if got := f.shown(t, ada, p, now); got != (shown{}) {
		t.Fatalf("after deleting both, the reads show %+v; want nothing", got)
	}
	if _, err := f.repo.Entry(t.Context(), ada, running.ID); !errors.Is(err, tracking.ErrNotFound) {
		t.Errorf("reading a deleted entry err = %v, want ErrNotFound", err)
	}
	// Soft: both rows are still in the table.
	if n := f.countEntries(t); n != 2 {
		t.Fatalf("time_entries holds %d rows after the deletes, want both kept", n)
	}
	// A deleted timer cannot be stopped, renamed or deleted again.
	if err := f.svc.Stop(t.Context(), ada, running.ID); !errors.Is(err, tracking.ErrNotFound) {
		t.Errorf("stopping a deleted timer err = %v, want ErrNotFound", err)
	}
	if err := f.svc.Rename(t.Context(), ada, running.ID, "renamed"); !errors.Is(err, tracking.ErrNotFound) {
		t.Errorf("renaming a deleted entry err = %v, want ErrNotFound", err)
	}
	if err := f.svc.Delete(t.Context(), ada, running.ID); !errors.Is(err, tracking.ErrNotFound) {
		t.Errorf("deleting a deleted entry err = %v, want ErrNotFound", err)
	}

	for _, id := range []int64{logged.ID, running.ID} {
		if err := f.svc.Restore(t.Context(), ada, id); err != nil {
			t.Fatalf("restore %d: %v", id, err)
		}
	}
	if got := f.shown(t, ada, p, now); got != before {
		t.Fatalf("after restoring, the reads show %+v; want %+v, as before the delete", got, before)
	}
	e, err := f.repo.Entry(t.Context(), ada, running.ID)
	if err != nil || e.Task != "Started by mistake" || !e.Running() || !e.StartedAt.Equal(running.StartedAt) {
		t.Fatalf("restored entry = %+v, %v; want the running timer as it was", e, err)
	}
	if err := f.svc.Restore(t.Context(), ada, running.ID); !errors.Is(err, tracking.ErrNotFound) {
		t.Errorf("restoring an entry that is not deleted err = %v, want ErrNotFound", err)
	}
}

func TestNobodyDeletesOrRestoresSomeoneElsesEntry(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	bob := f.addUser(t, "bob@example.com")
	acme := f.addCompany(t, ada, "Acme")
	e, err := f.svc.Start(t.Context(), ada, acme.ID, "mine")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	runningFor := func(user int64) int {
		t.Helper()
		r, err := f.repo.Running(t.Context(), user)
		if err != nil {
			t.Fatalf("running: %v", err)
		}
		return len(r)
	}

	if err := f.svc.Delete(t.Context(), bob, e.ID); !errors.Is(err, tracking.ErrNotFound) {
		t.Errorf("bob's delete err = %v, want ErrNotFound", err)
	}
	if n := runningFor(ada); n != 1 {
		t.Fatalf("ada has %d running after bob's delete, want her timer untouched", n)
	}
	if err := f.svc.Delete(t.Context(), ada, e.ID); err != nil {
		t.Fatalf("ada's delete: %v", err)
	}
	if err := f.svc.Restore(t.Context(), bob, e.ID); !errors.Is(err, tracking.ErrNotFound) {
		t.Errorf("bob's restore err = %v, want ErrNotFound", err)
	}
	if n := runningFor(ada); n != 0 {
		t.Fatalf("ada has %d running after bob's restore, want her timer still deleted", n)
	}
	if err := f.svc.Restore(t.Context(), ada, e.ID); err != nil {
		t.Fatalf("ada's restore: %v", err)
	}
}
