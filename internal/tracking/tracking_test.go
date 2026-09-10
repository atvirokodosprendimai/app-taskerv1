package tracking_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/store"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/store/storetest"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

// fixture is a migrated database with the tracking repo and service over it.
type fixture struct {
	db   *store.DB
	repo *tracking.Repo
	svc  *tracking.Service
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := storetest.Open(t)
	repo := tracking.NewRepo(db.Read, db.Write)
	return fixture{db: db, repo: repo, svc: tracking.NewService(repo)}
}

// addUser inserts an account directly; these tests are about tracking, not
// about how an account is made.
func (f fixture) addUser(t *testing.T, email string) int64 {
	t.Helper()
	res, err := f.db.Write.ExecContext(t.Context(),
		`INSERT INTO users (email, password_hash, timezone, created_at) VALUES (?, 'x', 'UTC', 0)`, email)
	if err != nil {
		t.Fatalf("add user: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("add user: %v", err)
	}
	return id
}

func (f fixture) addCompany(t *testing.T, userID int64, name string) tracking.Company {
	t.Helper()
	c, err := f.svc.AddCompany(t.Context(), userID, name)
	if err != nil {
		t.Fatalf("add company %q: %v", name, err)
	}
	return c
}

func (f fixture) countEntries(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.Read.QueryRowContext(t.Context(), `SELECT count(*) FROM time_entries`).Scan(&n); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	return n
}

func TestAddCompanyCollapsesSpaceAndRefusesADuplicateInAnyCase(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")

	c := f.addCompany(t, ada, "  Acme \t Corp ")
	if c.Name != "Acme Corp" {
		t.Fatalf("name = %q, want %q", c.Name, "Acme Corp")
	}
	if _, err := f.svc.AddCompany(t.Context(), ada, "ACME corp"); !errors.Is(err, tracking.ErrCompanyExists) {
		t.Fatalf("duplicate err = %v, want ErrCompanyExists", err)
	}
}

func TestCompanyNamesAreScopedToTheirUser(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	bob := f.addUser(t, "bob@example.com")

	f.addCompany(t, ada, "Acme")
	f.addCompany(t, bob, "Acme")

	companies, err := f.repo.Companies(t.Context(), ada)
	if err != nil {
		t.Fatalf("companies: %v", err)
	}
	if len(companies) != 1 || companies[0].Name != "Acme" {
		t.Fatalf("ada sees %+v, want exactly her own Acme", companies)
	}
}

func TestAddCompanyRefusesABlankOrOverlongName(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	if _, err := f.svc.AddCompany(t.Context(), ada, " \n "); !errors.Is(err, tracking.ErrCompanyNameRequired) {
		t.Fatalf("blank err = %v, want ErrCompanyNameRequired", err)
	}
	long := strings.Repeat("é", tracking.MaxCompanyName+1)
	if _, err := f.svc.AddCompany(t.Context(), ada, long); !errors.Is(err, tracking.ErrCompanyNameTooLong) {
		t.Fatalf("long err = %v, want ErrCompanyNameTooLong", err)
	}
	// The limit counts characters, not bytes: exactly the maximum is accepted
	// even though each "é" is two bytes.
	f.addCompany(t, ada, strings.Repeat("é", tracking.MaxCompanyName))
}

func TestSeveralTimersRunAtOnceIncludingOnOneCompany(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	acme := f.addCompany(t, ada, "Acme")
	beta := f.addCompany(t, ada, "Beta")

	for _, s := range []struct {
		company int64
		task    string
	}{{acme.ID, "design"}, {acme.ID, "review"}, {beta.ID, ""}} {
		if _, err := f.svc.Start(t.Context(), ada, s.company, s.task); err != nil {
			t.Fatalf("start %q: %v", s.task, err)
		}
	}

	running, err := f.repo.Running(t.Context(), ada)
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if len(running) != 3 {
		t.Fatalf("running = %d timers, want 3", len(running))
	}
	companies, err := f.repo.Companies(t.Context(), ada)
	if err != nil {
		t.Fatalf("companies: %v", err)
	}
	counts := map[string]int{}
	for _, c := range companies {
		counts[c.Name] = c.Running
	}
	if counts["Acme"] != 2 || counts["Beta"] != 1 {
		t.Fatalf("running per company = %v, want Acme 2, Beta 1", counts)
	}
}

func TestStartOnSomeoneElsesCompanyWritesNothing(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	bob := f.addUser(t, "bob@example.com")
	acme := f.addCompany(t, ada, "Acme")

	// The owner's start succeeds, so the refusal below is about ownership.
	if _, err := f.svc.Start(t.Context(), ada, acme.ID, "mine"); err != nil {
		t.Fatalf("owner start: %v", err)
	}
	before := f.countEntries(t)

	if _, err := f.svc.Start(t.Context(), bob, acme.ID, "not mine"); !errors.Is(err, tracking.ErrNotFound) {
		t.Fatalf("bob's start err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.Start(t.Context(), ada, 9999, "nobody's"); !errors.Is(err, tracking.ErrNotFound) {
		t.Fatalf("missing company err = %v, want ErrNotFound", err)
	}
	if after := f.countEntries(t); after != before {
		t.Fatalf("entries went from %d to %d on refused starts", before, after)
	}
}

func TestStopEndsOnlyTheUsersOwnRunningTimerAndOnlyOnce(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	bob := f.addUser(t, "bob@example.com")
	acme := f.addCompany(t, ada, "Acme")
	e, err := f.svc.Start(t.Context(), ada, acme.ID, "design")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := f.svc.Stop(t.Context(), bob, e.ID); !errors.Is(err, tracking.ErrNotFound) {
		t.Fatalf("bob's stop err = %v, want ErrNotFound", err)
	}
	if running, _ := f.repo.Running(t.Context(), ada); len(running) != 1 {
		t.Fatalf("after bob's stop, ada has %d running, want 1", len(running))
	}

	if err := f.svc.Stop(t.Context(), ada, e.ID); err != nil {
		t.Fatalf("ada's stop: %v", err)
	}
	if running, _ := f.repo.Running(t.Context(), ada); len(running) != 0 {
		t.Fatalf("after ada's stop, %d still running", len(running))
	}
	if err := f.svc.Stop(t.Context(), ada, e.ID); !errors.Is(err, tracking.ErrNotFound) {
		t.Fatalf("second stop err = %v, want ErrNotFound", err)
	}
}

func TestStartRefusesAnOverlongTask(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	acme := f.addCompany(t, ada, "Acme")
	if _, err := f.svc.Start(t.Context(), ada, acme.ID, strings.Repeat("x", tracking.MaxTask+1)); !errors.Is(err, tracking.ErrTaskTooLong) {
		t.Fatalf("err = %v, want ErrTaskTooLong", err)
	}
	if n := f.countEntries(t); n != 0 {
		t.Fatalf("a refused start wrote %d entries", n)
	}
}

func TestHistoryClipsEntriesToThePeriodAndCountsARunningTimerToNow(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	bob := f.addUser(t, "bob@example.com")
	acme := f.addCompany(t, ada, "Acme")
	beta := f.addCompany(t, ada, "Beta")
	bobs := f.addCompany(t, bob, "Acme")

	at := func(d, h, m int) time.Time { return time.Date(2026, 9, d, h, m, 0, 0, time.UTC) }
	record := func(user, company int64, task string, start, stop time.Time) {
		t.Helper()
		e, err := f.repo.StartEntry(t.Context(), user, company, task, start)
		if err != nil {
			t.Fatalf("start %s: %v", task, err)
		}
		if !stop.IsZero() {
			if err := f.repo.StopEntry(t.Context(), user, e.ID, stop); err != nil {
				t.Fatalf("stop %s: %v", task, err)
			}
		}
	}
	record(ada, acme.ID, "overnight", at(9, 23, 0), at(10, 1, 0))  // 1h inside
	record(ada, acme.ID, "morning", at(10, 10, 0), at(10, 12, 30)) // 2h30 inside
	record(ada, beta.ID, "late", at(10, 23, 30), at(11, 0, 30))    // 30m inside
	record(ada, beta.ID, "earlier", at(8, 10, 0), at(8, 11, 0))    // outside
	record(ada, beta.ID, "running", at(10, 20, 0), time.Time{})    // 1h to now
	record(bob, bobs.ID, "bob's", at(10, 9, 0), at(10, 17, 0))     // someone else's

	now := at(10, 21, 0)
	day, err := tracking.Filter{Mode: tracking.ModeDay, Day: "2026-09-10"}.Period(time.UTC)
	if err != nil {
		t.Fatalf("period: %v", err)
	}

	totals, err := f.repo.Totals(t.Context(), ada, day, 0, now)
	if err != nil {
		t.Fatalf("totals: %v", err)
	}
	want := []tracking.CompanyTotal{
		{CompanyID: acme.ID, CompanyName: "Acme", Entries: 2, Duration: 3*time.Hour + 30*time.Minute},
		{CompanyID: beta.ID, CompanyName: "Beta", Entries: 2, Duration: 90 * time.Minute},
	}
	if len(totals) != len(want) {
		t.Fatalf("totals = %+v, want %+v", totals, want)
	}
	for i := range want {
		if totals[i] != want[i] {
			t.Fatalf("totals[%d] = %+v, want %+v", i, totals[i], want[i])
		}
	}

	entries, err := f.repo.Entries(t.Context(), ada, day, 0, now, 100)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	var tasks []string
	for _, e := range entries {
		tasks = append(tasks, e.Task)
	}
	if got := strings.Join(tasks, ","); got != "late,running,morning,overnight" {
		t.Fatalf("entries = %s, want late,running,morning,overnight (newest first)", got)
	}

	onlyBeta, err := f.repo.Entries(t.Context(), ada, day, beta.ID, now, 100)
	if err != nil {
		t.Fatalf("beta entries: %v", err)
	}
	if len(onlyBeta) != 2 {
		t.Fatalf("beta entries = %d, want 2", len(onlyBeta))
	}
	limited, err := f.repo.Entries(t.Context(), ada, day, 0, now, 1)
	if err != nil {
		t.Fatalf("limited entries: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("limit 1 returned %d entries", len(limited))
	}
}

func TestTheSchemaRefusesAnEntryUnderSomeoneElsesCompany(t *testing.T) {
	f := newFixture(t)
	ada := f.addUser(t, "ada@example.com")
	bob := f.addUser(t, "bob@example.com")
	acme := f.addCompany(t, ada, "Acme")

	const insert = `INSERT INTO time_entries (user_id, company_id, started_at) VALUES (?, ?, 1)`
	// The owner's row is accepted by the same statement, so the refusal below is
	// the composite foreign key and not something else about the insert.
	if _, err := f.db.Write.ExecContext(t.Context(), insert, ada, acme.ID); err != nil {
		t.Fatalf("owner's direct insert: %v", err)
	}
	_, err := f.db.Write.ExecContext(t.Context(), insert, bob, acme.ID)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("an entry owned by bob under ada's company was not refused: %v", err)
	}
}
