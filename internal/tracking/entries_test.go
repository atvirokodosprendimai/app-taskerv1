package tracking_test

import (
	"testing"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

func TestEntriesReturnsEveryEntryWhenAskedForAll(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	acme := f.addCompany(t, ada, "Acme")
	now := time.Now()
	for i := 1; i <= 3; i++ {
		start := now.Add(-time.Duration(i) * time.Hour)
		if _, err := f.repo.LogEntry(t.Context(), ada, acme.ID, "", start, start.Add(10*time.Minute)); err != nil {
			t.Fatalf("log entry %d: %v", i, err)
		}
	}
	p := tracking.Period{From: now.Add(-24 * time.Hour), To: now}

	// A limit of one returns one, so the three below come from AllEntries rather
	// than from a period that happens to hold exactly three.
	one, err := f.repo.Entries(t.Context(), ada, p, 0, now, 1)
	if err != nil {
		t.Fatalf("entries, limit 1: %v", err)
	}
	all, err := f.repo.Entries(t.Context(), ada, p, 0, now, tracking.AllEntries)
	if err != nil {
		t.Fatalf("entries, all: %v", err)
	}
	if len(one) != 1 || len(all) != 3 {
		t.Fatalf("limit 1 gave %d entries and AllEntries gave %d, want 1 and 3", len(one), len(all))
	}
}
