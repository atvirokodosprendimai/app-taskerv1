package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/store"
)

// Repo stores companies and time entries.
//
// Its write methods are called by [Service] alone. Its read methods are called
// by pages directly, and run on the reader handle, which cannot write.
//
// Every statement is scoped by user id. There is no method that reads or
// writes a row without naming whose it is.
type Repo struct {
	read  *sql.DB
	write *sql.DB
}

// NewRepo returns a Repo over the two handles of one database.
func NewRepo(read, write *sql.DB) *Repo {
	return &Repo{read: read, write: write}
}

// ---- write side -------------------------------------------------------------

// CreateCompany inserts a company for userID. A name the user already has, in
// any letter case, is refused with [ErrCompanyExists].
func (r *Repo) CreateCompany(ctx context.Context, userID int64, name string, at time.Time) (Company, error) {
	res, err := r.write.ExecContext(ctx,
		`INSERT INTO companies (user_id, name, created_at) VALUES (?, ?, ?)`,
		userID, name, at.Unix())
	if err != nil {
		if store.IsUniqueViolation(err) {
			return Company{}, ErrCompanyExists
		}
		return Company{}, fmt.Errorf("tracking: create company: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Company{}, fmt.Errorf("tracking: create company: %w", err)
	}
	return Company{ID: id, Name: name, CreatedAt: unix(at.Unix())}, nil
}

// StartEntry starts a timer for one of userID's companies. A company that is
// not the user's is [ErrNotFound], and nothing is written.
//
// Ownership is checked by the INSERT itself — it selects the company row
// scoped to the user — rather than by a lookup first, so there is no window in
// which the check and the write disagree.
func (r *Repo) StartEntry(ctx context.Context, userID, companyID int64, task string, at time.Time) (Entry, error) {
	res, err := r.write.ExecContext(ctx,
		`INSERT INTO time_entries (user_id, company_id, task, started_at)
		 SELECT user_id, id, ?, ? FROM companies WHERE id = ? AND user_id = ?`,
		task, at.Unix(), companyID, userID)
	if err != nil {
		return Entry{}, fmt.Errorf("tracking: start entry: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Entry{}, fmt.Errorf("tracking: start entry: %w", err)
	}
	if n == 0 {
		return Entry{}, ErrNotFound
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Entry{}, fmt.Errorf("tracking: start entry: %w", err)
	}
	return Entry{ID: id, CompanyID: companyID, Task: task, StartedAt: unix(at.Unix())}, nil
}

// StopEntry stops one of userID's running timers. A timer that is not the
// user's, or is already stopped, is [ErrNotFound].
//
// The stop time is never earlier than the start: a clock that stepped
// backwards between the two would otherwise violate the table's CHECK and turn
// a Stop press into an error.
func (r *Repo) StopEntry(ctx context.Context, userID, entryID int64, at time.Time) error {
	res, err := r.write.ExecContext(ctx,
		`UPDATE time_entries SET stopped_at = MAX(?, started_at)
		 WHERE id = ? AND user_id = ? AND stopped_at IS NULL`,
		at.Unix(), entryID, userID)
	if err != nil {
		return fmt.Errorf("tracking: stop entry: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("tracking: stop entry: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- read side --------------------------------------------------------------

// Companies returns userID's companies by name, each with its count of running
// timers.
func (r *Repo) Companies(ctx context.Context, userID int64) ([]Company, error) {
	rows, err := r.read.QueryContext(ctx,
		`SELECT c.id, c.name, c.created_at,
		        (SELECT count(*) FROM time_entries e
		          WHERE e.user_id = c.user_id AND e.company_id = c.id AND e.stopped_at IS NULL)
		   FROM companies c
		  WHERE c.user_id = ?
		  ORDER BY c.name COLLATE NOCASE, c.id`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("tracking: list companies: %w", err)
	}
	defer rows.Close()

	out := []Company{}
	for rows.Next() {
		var (
			c       Company
			created int64
		)
		if err := rows.Scan(&c.ID, &c.Name, &created, &c.Running); err != nil {
			return nil, fmt.Errorf("tracking: list companies: %w", err)
		}
		c.CreatedAt = unix(created)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tracking: list companies: %w", err)
	}
	return out, nil
}

// Running returns userID's running timers, oldest first.
func (r *Repo) Running(ctx context.Context, userID int64) ([]Entry, error) {
	return r.entries(ctx,
		`SELECT e.id, e.company_id, c.name, e.task, e.started_at, e.stopped_at
		   FROM time_entries e JOIN companies c ON c.id = e.company_id
		  WHERE e.user_id = ? AND e.stopped_at IS NULL
		  ORDER BY e.started_at, e.id`,
		userID)
}

// Entries returns userID's entries that overlap p, newest first, at most limit
// of them. A running entry overlaps p when it started before p ends. companyID
// narrows the list to one company; zero means all.
func (r *Repo) Entries(ctx context.Context, userID int64, p Period, companyID int64, now time.Time, limit int) ([]Entry, error) {
	where, args := overlap(userID, p, companyID, now)
	return r.entries(ctx,
		`SELECT e.id, e.company_id, c.name, e.task, e.started_at, e.stopped_at
		   FROM time_entries e JOIN companies c ON c.id = e.company_id
		  WHERE `+where+`
		  ORDER BY e.started_at DESC, e.id DESC
		  LIMIT ?`,
		append(args, limit)...)
}

// Totals returns, per company, how many of userID's entries overlap p and how
// much of their time falls inside it — largest first. A running entry counts up
// to now.
//
// The clipping is done in SQL, over every matching entry, so a total stays
// right however many entries [Repo.Entries] was limited to showing.
func (r *Repo) Totals(ctx context.Context, userID int64, p Period, companyID int64, now time.Time) ([]CompanyTotal, error) {
	where, args := overlap(userID, p, companyID, now)
	args = append([]any{now.Unix(), p.To.Unix(), p.From.Unix()}, args...)
	rows, err := r.read.QueryContext(ctx,
		`SELECT e.company_id, c.name, count(*),
		        sum(min(coalesce(e.stopped_at, ?), ?) - max(e.started_at, ?))
		   FROM time_entries e JOIN companies c ON c.id = e.company_id
		  WHERE `+where+`
		  GROUP BY e.company_id, c.name
		  ORDER BY 4 DESC, c.name COLLATE NOCASE`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("tracking: totals: %w", err)
	}
	defer rows.Close()

	out := []CompanyTotal{}
	for rows.Next() {
		var (
			t       CompanyTotal
			seconds int64
		)
		if err := rows.Scan(&t.CompanyID, &t.CompanyName, &t.Entries, &seconds); err != nil {
			return nil, fmt.Errorf("tracking: totals: %w", err)
		}
		t.Duration = time.Duration(seconds) * time.Second
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tracking: totals: %w", err)
	}
	return out, nil
}

// overlap builds the WHERE clause selecting userID's entries that overlap p,
// with a running entry treated as ending at now.
func overlap(userID int64, p Period, companyID int64, now time.Time) (string, []any) {
	where := `e.user_id = ? AND e.started_at < ? AND coalesce(e.stopped_at, ?) > ?`
	args := []any{userID, p.To.Unix(), now.Unix(), p.From.Unix()}
	if companyID > 0 {
		where += ` AND e.company_id = ?`
		args = append(args, companyID)
	}
	return where, args
}

// entries runs a query selecting the entry columns, in that order.
func (r *Repo) entries(ctx context.Context, query string, args ...any) ([]Entry, error) {
	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("tracking: list entries: %w", err)
	}
	defer rows.Close()

	out := []Entry{}
	for rows.Next() {
		var (
			e       Entry
			started int64
			stopped sql.NullInt64
		)
		if err := rows.Scan(&e.ID, &e.CompanyID, &e.CompanyName, &e.Task, &started, &stopped); err != nil {
			return nil, fmt.Errorf("tracking: list entries: %w", err)
		}
		e.StartedAt = unix(started)
		if stopped.Valid {
			e.StoppedAt = unix(stopped.Int64)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tracking: list entries: %w", err)
	}
	return out, nil
}

// unix converts stored unix seconds to a UTC time.
func unix(s int64) time.Time { return time.Unix(s, 0).UTC() }
