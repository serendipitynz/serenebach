package auth

import "golang.org/x/crypto/bcrypt"

const Cost = 12

func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), Cost)
	return string(h), err
}

func VerifyPassword(hash, pw string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
}
