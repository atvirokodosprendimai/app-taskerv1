package store_test

import (
	"strings"
	"testing"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/store/storetest"
	"github.com/atvirokodosprendimai/app-taskerv1/migrations"
)

const insertUser = `INSERT INTO users (email, password_hash, timezone, created_at) VALUES (?, 'x', 'UTC', 1)`

func TestTheReadHandleRefusesWrites(t *testing.T) {
	db := storetest.Open(t)

	// The same statement must succeed on the writer first, so that a refusal on
	// the reader is about the handle and not about the SQL.
	if _, err := db.Write.ExecContext(t.Context(), insertUser, "writer@example.com"); err != nil {
		t.Fatalf("writer insert: %v", err)
	}

	_, err := db.Read.ExecContext(t.Context(), insertUser, "reader@example.com")
	if err == nil {
		t.Fatal("the read handle accepted a write; query_only(1) is not in effect")
	}
	if !strings.Contains(err.Error(), "readonly") {
		t.Fatalf("the read handle refused for an unexpected reason: %v", err)
	}
}

func TestTheReadHandleSeesWhatTheWriterCommitted(t *testing.T) {
	db := storetest.Open(t)
	if _, err := db.Write.ExecContext(t.Context(), insertUser, "seen@example.com"); err != nil {
		t.Fatalf("writer insert: %v", err)
	}
	var n int
	if err := db.Read.QueryRowContext(t.Context(),
		`SELECT count(*) FROM users WHERE email = 'seen@example.com'`).Scan(&n); err != nil {
		t.Fatalf("reader select: %v", err)
	}
	if n != 1 {
		t.Fatalf("reader saw %d rows, want 1", n)
	}
}

func TestMigratingAnUpToDateDatabaseIsANoOp(t *testing.T) {
	db := storetest.Open(t)
	if err := db.Migrate(t.Context(), migrations.FS); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestForeignKeysAreEnforcedOnTheWriter(t *testing.T) {
	db := storetest.Open(t)
	_, err := db.Write.ExecContext(t.Context(),
		`INSERT INTO companies (user_id, name, created_at) VALUES (999, 'Orphan', 1)`)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("a company for a user that does not exist was not refused: %v", err)
	}
}
