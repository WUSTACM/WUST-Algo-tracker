package service

import (
	"crypto/subtle"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const passwordHashCost = 12

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordHashCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func verifyPassword(stored, candidate string) (valid bool, needsRehash bool) {
	if isBcryptHash(stored) {
		err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(candidate))
		return err == nil, false
	}

	if subtle.ConstantTimeCompare([]byte(stored), []byte(candidate)) == 1 {
		return true, true
	}
	return false, false
}

func isBcryptHash(value string) bool {
	return strings.HasPrefix(value, "$2a$") ||
		strings.HasPrefix(value, "$2b$") ||
		strings.HasPrefix(value, "$2x$") ||
		strings.HasPrefix(value, "$2y$")
}
