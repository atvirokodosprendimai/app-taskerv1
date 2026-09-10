package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// UserStore is the persistence the service writes through. [Repo] satisfies it.
type UserStore interface {
	Create(ctx context.Context, u User) (User, error)
	ByEmail(ctx context.Context, email string) (User, error)
}

// Service is the write side of accounts: the only place one is created or a
// password is checked.
type Service struct {
	users UserStore
	now   func() time.Time
}

// NewService returns a Service writing through users.
func NewService(users UserStore) *Service {
	return &Service{users: users, now: time.Now}
}

// dummyHash is a real bcrypt hash of a password nobody holds, compared against
// when no account matched the address.
//
// Without it, "no such address" answers in microseconds while "wrong password"
// spends the tens of milliseconds bcrypt deliberately costs, and that difference
// reveals which addresses are registered — undoing [ErrBadCredentials]. It is
// built once, at bcrypt.DefaultCost, so it stays exactly as expensive as a real
// hash.
var dummyHash = sync.OnceValue(func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("no account holds this password"), bcrypt.DefaultCost)
	if err != nil {
		panic("auth: cannot hash the dummy password: " + err.Error())
	}
	return h
})

// Register creates an account and returns it.
//
// timezone is whatever the browser reported; a name that does not resolve is
// stored as "UTC" rather than refused.
func (s *Service) Register(ctx context.Context, email, password, timezone string) (User, error) {
	addr, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	if err := validatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("auth: hash password: %w", err)
	}
	return s.users.Create(ctx, User{
		Email:        addr,
		PasswordHash: string(hash),
		Timezone:     normalizeTimezone(timezone),
		CreatedAt:    s.now().UTC(),
	})
}

// Authenticate resolves an address and password to an account.
//
// Every failure returns the bare [ErrBadCredentials] value — not a wrapped one —
// so no difference in wording can leak which path was taken. Every failure also
// pays one bcrypt comparison at the cost the stored hashes use, so bcrypt's cost
// dominates the time of each path. The paths are not identical — a malformed
// address skips the lookup, and a lookup that finds no row is not timed against
// one that does — so do not read this as "no difference in timing". A genuine
// store failure is returned as itself: an outage reported as a wrong password
// sends whoever is on call hunting for the wrong problem.
func (s *Service) Authenticate(ctx context.Context, email, password string) (User, error) {
	addr, err := NormalizeEmail(email)
	if err != nil {
		equaliseTiming(password)
		return User{}, ErrBadCredentials
	}
	u, err := s.users.ByEmail(ctx, addr)
	if errors.Is(err, ErrNotFound) {
		equaliseTiming(password)
		return User{}, ErrBadCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: authenticate: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return User{}, ErrBadCredentials
	}
	return u, nil
}

// equaliseTiming spends the bcrypt cost a real comparison would. The result is
// discarded by design: there is nothing to learn from a hash nobody holds.
func equaliseTiming(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
}
