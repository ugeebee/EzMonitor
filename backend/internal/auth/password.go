package auth

import (
	"golang.org/x/crypto/bcrypt"
)

// HashPassword hashes a plaintext password using bcrypt with standard cost 10
// (consistent with PostgreSQL's gen_salt('bf', 10) in dev_seed.sql).
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// CheckPasswordHash compares a plaintext password against a bcrypt hash.
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

