package auth

import "golang.org/x/crypto/bcrypt"

// Cost is bcrypt's work factor. Production uses 12; tests override
// to bcrypt.MinCost via TestMain to avoid spending ~175ms per hash.
var Cost = 12

func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), Cost)
	return string(h), err
}

func VerifyPassword(hash, pw string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
}
