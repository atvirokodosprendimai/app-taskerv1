package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/store"
)

// Repo stores users. Writes go to the writer handle and reads to the reader,
// which the driver keeps read-only.
type Repo struct {
	read  *sql.DB
	write *sql.DB
}

// NewRepo returns a Repo over the two handles of one database.
func NewRepo(read, write *sql.DB) *Repo {
	return &Repo{read: read, write: write}
}

// Create inserts u and returns it with its new ID. An address that already has
// an account is refused with [ErrEmailTaken].
//
// The UNIQUE constraint is the check, rather than a lookup beforehand: a lookup
// followed by an insert passes every sequential test and still lets two
// simultaneous registrations for one address both through.
func (r *Repo) Create(ctx context.Context, u User) (User, error) {
	res, err := r.write.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, timezone, created_at) VALUES (?, ?, ?, ?)`,
		u.Email, u.PasswordHash, u.Timezone, u.CreatedAt.Unix())
	if err != nil {
		if store.IsUniqueViolation(err) {
			return User{}, ErrEmailTaken
		}
		return User{}, fmt.Errorf("auth: create user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("auth: create user: %w", err)
	}
	u.ID = id
	u.CreatedAt = time.Unix(u.CreatedAt.Unix(), 0).UTC()
	return u, nil
}

// userColumns is the select list [scanUser] reads, in its order.
const userColumns = `id, email, password_hash, timezone, created_at`

// ByEmail returns the account for an already-normalised address, or
// [ErrNotFound].
func (r *Repo) ByEmail(ctx context.Context, email string) (User, error) {
	return r.one(ctx, `SELECT `+userColumns+` FROM users WHERE email = ?`, email)
}

// ByID returns the account with id, or [ErrNotFound].
func (r *Repo) ByID(ctx context.Context, id int64) (User, error) {
	return r.one(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
}

// one runs a single-row user query on the reader.
func (r *Repo) one(ctx context.Context, query string, arg any) (User, error) {
	var (
		u       User
		created int64
	)
	err := r.read.QueryRowContext(ctx, query, arg).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Timezone, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: load user: %w", err)
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	return u, nil
}
