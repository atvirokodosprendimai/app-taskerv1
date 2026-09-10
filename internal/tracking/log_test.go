package tracking_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

func TestLoggedTimeIsAStoppedManualEntryThatHistoryCounts(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	acme := f.addCompany(t, ada, "Acme")
	start := time.Now().Add(-3 * time.Hour).Truncate(time.Second)

	e, err := f.svc.Log(t.Context(), ada, acme.ID, "  Phone \t call ", start, 45*time.Minute)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !e.Manual || e.Task != "Phone call" || e.Running() || e.StoppedAt.Sub(e.StartedAt) != 45*time.Minute {
		t.Fatalf("logged entry = %+v, want a stopped, manual, 45-minute Phone call", e)
	}
	if running, _ := f.repo.Running(t.Context(), ada); len(running) != 0 {
		t.Fatalf("logged time shows as %d running timers", len(running))
	}

	p := tracking.Period{From: start.Add(-time.Hour), To: start.Add(2 * time.Hour)}
	totals, err := f.repo.Totals(t.Context(), ada, p, 0, time.Now())
	if err != nil {
		t.Fatalf("totals: %v", err)
	}
	if len(totals) != 1 || totals[0].Duration != 45*time.Minute || totals[0].Entries != 1 {
		t.Fatalf("totals = %+v, want Acme with one 45-minute entry", totals)
	}
	entries, err := f.repo.Entries(t.Context(), ada, p, 0, time.Now(), 10)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 1 || !entries[0].Manual {
		t.Fatalf("entries = %+v, want the one entry, marked manual", entries)
	}
}

func TestLoggingRefusesTheFutureAndDurationsOutsideADay(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	acme := f.addCompany(t, ada, "Acme")
	now := time.Now()

	cases := []struct {
		name  string
		start time.Time
		d     time.Duration
		want  error
	}{
		{"ends an hour from now", now.Add(-30 * time.Minute), 90 * time.Minute, tracking.ErrLoggedInFuture},
		{"zero long", now.Add(-time.Hour), 0, tracking.ErrInvalidDuration},
		{"negative", now.Add(-time.Hour), -time.Minute, tracking.ErrInvalidDuration},
		{"over a day", now.Add(-48 * time.Hour), tracking.MaxLogged + time.Minute, tracking.ErrDurationTooLong},
	}
	for _, c := range cases {
		if _, err := f.svc.Log(t.Context(), ada, acme.ID, "", c.start, c.d); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if n := f.countEntries(t); n != 0 {
		t.Fatalf("refused logs wrote %d entries", n)
	}
	// A whole day that has already ended is accepted, so the refusals above are
	// about the time and the length rather than logging as such.
	if _, err := f.svc.Log(t.Context(), ada, acme.ID, "", now.Add(-48*time.Hour), tracking.MaxLogged); err != nil {
		t.Fatalf("a full past day was refused: %v", err)
	}
}

func TestLoggingOnSomeoneElsesCompanyWritesNothing(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	bob := f.addUser(t, "bob@example.com")
	acme := f.addCompany(t, ada, "Acme")
	start := time.Now().Add(-2 * time.Hour)

	if _, err := f.svc.Log(t.Context(), ada, acme.ID, "mine", start, time.Hour); err != nil {
		t.Fatalf("owner's log: %v", err)
	}
	before := f.countEntries(t)
	if _, err := f.svc.Log(t.Context(), bob, acme.ID, "not mine", start, time.Hour); !errors.Is(err, tracking.ErrNotFound) {
		t.Fatalf("bob's log err = %v, want ErrNotFound", err)
	}
	if after := f.countEntries(t); after != before {
		t.Fatalf("entries went from %d to %d on a refused log", before, after)
	}
}

func TestTheSchemaRefusesAManualEntryThatIsStillRunning(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	acme := f.addCompany(t, ada, "Acme")

	const insert = `INSERT INTO time_entries (user_id, company_id, started_at, stopped_at, manual) VALUES (?, ?, 1, ?, 1)`
	// The same statement with a stop is accepted, so the refusal below is the
	// CHECK and not something else about the insert.
	if _, err := f.db.Write.ExecContext(t.Context(), insert, ada, acme.ID, 2); err != nil {
		t.Fatalf("a stopped manual entry was refused: %v", err)
	}
	_, err := f.db.Write.ExecContext(t.Context(), insert, ada, acme.ID, nil)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("a running manual entry was not refused by the schema: %v", err)
	}
}
