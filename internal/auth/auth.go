// Package auth owns accounts: registering one and signing in to it.
//
// [Service] is the only writer of the user aggregate. Reading a user back by
// id, which every signed-in request does, goes straight to [Repo] — the CQRS
// split, at the scale of one table.
package auth

import (
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// Refusals a caller can act on. The web layer turns each into a sentence.
var (
	// ErrInvalidEmail reports an address that is not a bare e-mail address.
	ErrInvalidEmail = errors.New("auth: invalid email address")
	// ErrPasswordTooShort reports a password under [MinPasswordLength].
	ErrPasswordTooShort = errors.New("auth: password too short")
	// ErrPasswordTooLong reports a password over [MaxPasswordBytes].
	ErrPasswordTooLong = errors.New("auth: password too long")
	// ErrEmailTaken reports that the address already has an account.
	ErrEmailTaken = errors.New("auth: email already registered")
	// ErrBadCredentials is the ONE answer to every failed sign-in: an address
	// that does not parse, an address with no account, and a wrong password.
	// Anything that told them apart would make the sign-in screen an oracle for
	// which addresses are registered.
	ErrBadCredentials = errors.New("auth: invalid email or password")
	// ErrNotFound reports that no account has the requested id or address.
	ErrNotFound = errors.New("auth: account not found")
)

// MinPasswordLength is the shortest password accepted, in characters.
//
// Length is the only rule. Composition rules push people towards predictable
// substitutions, and there is no reset flow here to rescue a forgotten password.
const MinPasswordLength = 8

// MaxPasswordBytes is bcrypt's input limit. x/crypto refuses longer input, so
// it is refused here first with an error a person can act on.
const MaxPasswordBytes = 72

// maxEmailLength is the longest address SMTP permits.
const maxEmailLength = 254

// User is someone who can sign in.
type User struct {
	// ID is the database identifier.
	ID int64
	// Email is the sign-in identifier, stored lower-cased.
	Email string
	// PasswordHash is a bcrypt hash; the plaintext never leaves the service.
	PasswordHash string
	// Timezone is the IANA zone name the user's history is reckoned in.
	Timezone string
	// CreatedAt is when the account was registered, in UTC.
	CreatedAt time.Time
}

// Location returns the user's time zone, or UTC when the stored name does not
// resolve on this machine.
func (u User) Location() *time.Location {
	if u.Timezone == "" || u.Timezone == "Local" {
		return time.UTC
	}
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// NormalizeEmail trims and lower-cases an address and checks that it is a bare
// address ("ada@example.com") rather than a display-name form
// ("Ada <ada@example.com>"), which net/mail would otherwise accept.
func NormalizeEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > maxEmailLength {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return "", ErrInvalidEmail
	}
	return s, nil
}

// validatePassword checks a proposed password against the length rules.
func validatePassword(p string) error {
	if utf8.RuneCountInString(p) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(p) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}
	return nil
}

// normalizeTimezone returns tz when it names a real IANA zone, and "UTC"
// otherwise.
//
// The value comes from the browser, so it is untrusted and is empty when
// scripting is off; a bad value is not worth refusing a registration over.
// "Local" is refused because it would mean the SERVER's zone, which is exactly
// what storing a zone per user exists to avoid. time.LoadLocation itself
// rejects path traversal in the name.
func normalizeTimezone(tz string) string {
	tz = strings.TrimSpace(tz)
	if tz == "" || tz == "Local" {
		return "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "UTC"
	}
	return tz
}
