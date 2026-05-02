package app_test

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/serendipitynz/serenebach/internal/auth"
)

// TestMain lowers bcrypt cost for the entire package. Production keeps
// cost 12; tests use MinCost (4) since they only need round-trip
// correctness, not hashing strength. Saves ~45s on a full test run.
func TestMain(m *testing.M) {
	auth.Cost = bcrypt.MinCost
	os.Exit(m.Run())
}
