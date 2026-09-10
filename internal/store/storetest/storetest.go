// Package storetest opens a migrated, throwaway database for tests.
//
// Every package's tests run the REAL migrations rather than a copied schema. A
// hand-copied CREATE TABLE drifts from production at the first schema change,
// and the suite then passes against a database nobody runs.
package storetest

import (
	"path/filepath"
	"testing"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/store"
	"github.com/atvirokodosprendimai/app-taskerv1/migrations"
)

// Open returns a freshly migrated database in a temporary directory. It is
// closed when the test ends.
func Open(t testing.TB) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context(), migrations.FS); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return db
}
