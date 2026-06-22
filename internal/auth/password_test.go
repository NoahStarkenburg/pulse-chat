package auth

import (
	"strings"
	"testing"
)

func TestHashPassword_VerifiesCorrectAndRejectsWrong(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := VerifyPassword("correct horse battery staple", hash); err != nil {
		t.Errorf("correct password failed to verify: %v", err)
	}
	if err := VerifyPassword("wrong password", hash); err == nil {
		t.Error("wrong password verified, want failure")
	}
}

func TestHashPassword_DoesNotStorePlaintext(t *testing.T) {
	const pw = "hunter2"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if strings.Contains(hash, pw) {
		t.Error("hash contains the plaintext password")
	}
}

func TestHashPassword_SaltsEachHash(t *testing.T) {
	// bcrypt embeds a random salt, so the same password hashes to different
	// strings each time.
	a, err := HashPassword("same")
	if err != nil {
		t.Fatalf("hash a: %v", err)
	}
	b, err := HashPassword("same")
	if err != nil {
		t.Fatalf("hash b: %v", err)
	}
	if a == b {
		t.Error("two hashes of the same password are identical; salt is missing")
	}
}
