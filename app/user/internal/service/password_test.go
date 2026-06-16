package service

import "testing"

func TestVerifyPasswordSupportsBcrypt(t *testing.T) {
	hash, err := hashPassword("secret-password")
	if err != nil {
		t.Fatalf("hashPassword returned error: %v", err)
	}
	if hash == "secret-password" {
		t.Fatal("hashPassword returned the plaintext password")
	}

	valid, needsRehash := verifyPassword(hash, "secret-password")
	if !valid {
		t.Fatal("verifyPassword rejected a matching bcrypt password")
	}
	if needsRehash {
		t.Fatal("verifyPassword requested rehash for a bcrypt password")
	}
}

func TestVerifyPasswordSupportsLegacyPlaintextMigration(t *testing.T) {
	valid, needsRehash := verifyPassword("legacy-password", "legacy-password")
	if !valid {
		t.Fatal("verifyPassword rejected a matching legacy plaintext password")
	}
	if !needsRehash {
		t.Fatal("verifyPassword did not request rehash for a legacy plaintext password")
	}

	valid, needsRehash = verifyPassword("legacy-password", "wrong-password")
	if valid || needsRehash {
		t.Fatal("verifyPassword accepted an incorrect legacy plaintext password")
	}
}
