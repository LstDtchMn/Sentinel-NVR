package auth

import (
	"strings"
	"testing"
)

func TestHashAndCheckPassword(t *testing.T) {
	password := "test-password-123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash == "" {
		t.Fatal("hash should not be empty")
	}
	if hash == password {
		t.Fatal("hash should not equal plaintext password")
	}

	// Correct password should match
	if err := VerifyPassword(password, hash); err != nil {
		t.Errorf("VerifyPassword with correct password failed: %v", err)
	}

	// Wrong password should not match
	if err := VerifyPassword("wrong-password", hash); err == nil {
		t.Error("VerifyPassword with wrong password should fail")
	}
}

func TestHashPasswordDifferentSalts(t *testing.T) {
	hash1, _ := HashPassword("same-password")
	hash2, _ := HashPassword("same-password")
	if hash1 == hash2 {
		t.Error("two hashes of the same password should differ (different salts)")
	}
}

func TestErrors(t *testing.T) {
	// Ensure sentinel error types are distinct
	if ErrNotFound == ErrTokenExpired {
		t.Error("ErrNotFound should be distinct from ErrTokenExpired")
	}
	if ErrNotFound.Error() == "" {
		t.Error("ErrNotFound should have a message")
	}
	if ErrTokenExpired.Error() == "" {
		t.Error("ErrTokenExpired should have a message")
	}
	if ErrSetupAlreadyDone.Error() == "" {
		t.Error("ErrSetupAlreadyDone should have a message")
	}
}

func TestErrorMessages(t *testing.T) {
	if !strings.Contains(ErrNotFound.Error(), "not found") {
		t.Error("ErrNotFound message should contain 'not found'")
	}
}
