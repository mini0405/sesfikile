package identity

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hash == "correct-horse-battery-staple" {
		t.Fatal("password was not hashed")
	}

	if !VerifyPassword(hash, "correct-horse-battery-staple") {
		t.Error("expected correct password to verify")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Error("expected incorrect password to fail verification")
	}
}
