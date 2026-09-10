// Package store opens the application's SQLite database and runs its
// migrations.
//
// It deliberately opens the same file twice; [DB] says why.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strings"

	"github.com/pressly/goose/v3"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// busyTimeoutMS is how long SQLite waits for an ordinary lock before giving up.
const busyTimeoutMS = 5000

// maxReaders bounds the reader pool. WAL lets readers run beside the writer and
// beside each other, so this is sized for request concurrency rather than for
// lock contention.
const maxReaders = 8

// DB holds the two handles the application uses to reach one SQLite file.
//
// Write and Read address the SAME file through different DSNs, which is a
// correctness decision rather than redundancy:
//
//   - Write is capped at one connection and takes its lock at BEGIN
//     (_txlock=immediate). SQLite admits a single writer regardless, and a
//     deferred transaction that reads before it writes has to UPGRADE its lock
//     — an upgrade conflict returns SQLITE_BUSY at once without consulting
//     busy_timeout. Every write in this application is a single statement today;
//     taking the lock at BEGIN means the first read-then-write transaction
//     somebody adds cannot fall into that trap.
//   - Read is an ordinary pool carrying query_only(1), so the driver itself
//     refuses a write on the read path. "Read models do not write" is then a
//     property of the handle rather than a code-review rule.
type DB struct {
	// Write is the single-writer handle. Every mutation and every migration
	// goes through it.
	Write *sql.DB
	// Read is the many-reader handle. The driver refuses writes on it.
	Read *sql.DB
}

// Open opens path as a WAL-mode SQLite database and returns both handles.
//
// The writer is opened and pinged first so that the file and WAL mode exist
// before any reader connects: a reader carrying query_only(1) cannot create
// the database itself.
func Open(path string) (*DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store: empty database path")
	}

	w, err := sql.Open("sqlite", writerDSN(path))
	if err != nil {
		return nil, fmt.Errorf("store: open writer: %w", err)
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	if err := w.Ping(); err != nil {
		w.Close()
		return nil, fmt.Errorf("store: ping writer: %w", err)
	}

	r, err := sql.Open("sqlite", readerDSN(path))
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("store: open reader: %w", err)
	}
	r.SetMaxOpenConns(maxReaders)
	r.SetMaxIdleConns(maxReaders)
	if err := r.Ping(); err != nil {
		w.Close()
		r.Close()
		return nil, fmt.Errorf("store: ping reader: %w", err)
	}

	return &DB{Write: w, Read: r}, nil
}

// Close closes both handles and returns the first error encountered.
func (db *DB) Close() error {
	werr := db.Write.Close()
	rerr := db.Read.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

// Migrate brings the schema up to date from the given goose SQL migrations.
//
// It runs on the WRITER handle; the reader would be refused by the driver,
// which is the guarantee working. goose's Provider is used rather than its
// package-level functions because those keep the dialect and filesystem in
// global state, which two tests migrating in parallel would race on.
func (db *DB) Migrate(ctx context.Context, migrations fs.FS) error {
	p, err := goose.NewProvider(goose.DialectSQLite3, db.Write, migrations)
	if err != nil {
		return fmt.Errorf("store: load migrations: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// IsUniqueViolation reports whether err is SQLite refusing a write because it
// would duplicate a UNIQUE key.
//
// Repositories map it to their own domain error ("that address is taken"),
// which is why it is matched by the driver's extended code rather than by the
// message text.
func IsUniqueViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

// dsn builds a modernc.org/sqlite DSN for path with the given pragmas and an
// optional transaction lock mode.
func dsn(path, txlock string, pragmas ...string) string {
	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	if txlock != "" {
		q.Set("_txlock", txlock)
	}
	return "file:" + path + "?" + q.Encode()
}

// writerDSN returns the DSN for the single-writer handle.
func writerDSN(path string) string {
	return dsn(path, "immediate",
		"journal_mode(WAL)",
		fmt.Sprintf("busy_timeout(%d)", busyTimeoutMS),
		"foreign_keys(1)",
		"synchronous(NORMAL)",
	)
}

// readerDSN returns the DSN for the many-reader handle.
func readerDSN(path string) string {
	return dsn(path, "",
		"journal_mode(WAL)",
		fmt.Sprintf("busy_timeout(%d)", busyTimeoutMS),
		"foreign_keys(1)",
		"query_only(1)",
	)
}
