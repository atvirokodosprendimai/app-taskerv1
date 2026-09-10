package auth_test

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/store/storetest"
)

func newAuth(t *testing.T) (*auth.Service, *auth.Repo) {
	t.Helper()
	db := storetest.Open(t)
	repo := auth.NewRepo(db.Read, db.Write)
	return auth.NewService(repo), repo
}

func TestRegisterStoresANormalisedAddressAndAHashNotThePassword(t *testing.T) {
	svc, repo := newAuth(t)

	u, err := svc.Register(t.Context(), "  Ada@Example.COM ", "correct horse", "Europe/Vilnius")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("registered user has no id")
	}

	got, err := repo.ByID(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Email != "ada@example.com" {
		t.Errorf("email = %q, want the trimmed lower-cased address", got.Email)
	}
	if got.Timezone != "Europe/Vilnius" {
		t.Errorf("timezone = %q, want Europe/Vilnius", got.Timezone)
	}
	if got.PasswordHash == "correct horse" {
		t.Fatal("the plaintext password was stored")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("correct horse")); err != nil {
		t.Errorf("stored hash does not verify the password: %v", err)
	}
}

func TestRegisterStoresUTCForAZoneThatDoesNotResolve(t *testing.T) {
	svc, repo := newAuth(t)
	for i, tz := range []string{"", "Local", "Mars/Olympus_Mons", "../../etc/passwd"} {
		email := "tz" + string(rune('a'+i)) + "@example.com"
		u, err := svc.Register(t.Context(), email, "correct horse", tz)
		if err != nil {
			t.Fatalf("register with tz %q: %v", tz, err)
		}
		got, err := repo.ByID(t.Context(), u.ID)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got.Timezone != "UTC" {
			t.Errorf("tz %q stored as %q, want UTC", tz, got.Timezone)
		}
	}
}

func TestRegisterRefusesASecondAccountForTheSameAddressInAnyCase(t *testing.T) {
	svc, _ := newAuth(t)
	if _, err := svc.Register(t.Context(), "ada@example.com", "correct horse", "UTC"); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	_, err := svc.Register(t.Context(), "ADA@example.com", "another horse", "UTC")
	if !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("second registration err = %v, want ErrEmailTaken", err)
	}
}

func TestRegisterRefusesMalformedInput(t *testing.T) {
	svc, _ := newAuth(t)
	cases := []struct {
		name, email, password string
		want                  error
	}{
		{"not an address", "not-an-address", "correct horse", auth.ErrInvalidEmail},
		{"display-name form", "Ada <ada@example.com>", "correct horse", auth.ErrInvalidEmail},
		{"empty address", "   ", "correct horse", auth.ErrInvalidEmail},
		{"short password", "short@example.com", "seven77", auth.ErrPasswordTooShort},
		{"password over bcrypt's limit", "long@example.com", strings.Repeat("x", auth.MaxPasswordBytes+1), auth.ErrPasswordTooLong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.Register(t.Context(), c.email, c.password, "UTC")
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}

	// The boundaries themselves are accepted, so the refusals above are about
	// the rule and not about something else in the input.
	if _, err := svc.Register(t.Context(), "eight@example.com", "eight888", "UTC"); err != nil {
		t.Fatalf("an 8-character password was refused: %v", err)
	}
	if _, err := svc.Register(t.Context(), "max@example.com", strings.Repeat("x", auth.MaxPasswordBytes), "UTC"); err != nil {
		t.Fatalf("a 72-byte password was refused: %v", err)
	}
}

func TestAuthenticateAcceptsTheRightPasswordWithTheAddressInAnyCase(t *testing.T) {
	svc, _ := newAuth(t)
	reg, err := svc.Register(t.Context(), "ada@example.com", "correct horse", "UTC")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	u, err := svc.Authenticate(t.Context(), " ADA@example.com", "correct horse")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if u.ID != reg.ID {
		t.Fatalf("authenticated as user %d, want %d", u.ID, reg.ID)
	}
}

func TestEveryFailedSignInIsTheSameError(t *testing.T) {
	svc, _ := newAuth(t)
	if _, err := svc.Register(t.Context(), "ada@example.com", "correct horse", "UTC"); err != nil {
		t.Fatalf("register: %v", err)
	}
	cases := map[string][2]string{
		"wrong password":    {"ada@example.com", "wrong horse"},
		"unknown address":   {"bob@example.com", "correct horse"},
		"malformed address": {"not an address", "correct horse"},
		"empty everything":  {"", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Authenticate(t.Context(), c[0], c[1])
			// Identity, not errors.Is: a wrapped variant could carry different
			// text on different paths and leak which one was taken.
			if err != auth.ErrBadCredentials {
				t.Fatalf("err = %#v, want the bare ErrBadCredentials", err)
			}
		})
	}
}
