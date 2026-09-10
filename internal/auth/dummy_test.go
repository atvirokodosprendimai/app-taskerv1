package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// A dummy hash at a lower cost, or one bcrypt rejects before doing any work,
// would reopen the timing oracle while every behavioural test still passed.
func TestTheDummyHashCostsWhatARealHashCosts(t *testing.T) {
	cost, err := bcrypt.Cost(dummyHash())
	if err != nil {
		t.Fatalf("dummy hash is not a valid bcrypt hash: %v", err)
	}
	if cost != bcrypt.DefaultCost {
		t.Fatalf("dummy hash cost = %d, want bcrypt.DefaultCost (%d)", cost, bcrypt.DefaultCost)
	}
}
