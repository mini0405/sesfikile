package identity

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestTokenIssueAndParse(t *testing.T) {
	tokens := NewTokenIssuer("test-secret")
	userID := uuid.New()

	tokenString, err := tokens.Issue(userID, RoleDriver)
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	claims, err := tokens.Parse(tokenString)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("expected user id %s, got %s", userID, claims.UserID)
	}
	if claims.Role != RoleDriver {
		t.Errorf("expected role %s, got %s", RoleDriver, claims.Role)
	}
}

func TestParse_RejectsWrongSecret(t *testing.T) {
	tokens := NewTokenIssuer("test-secret")
	other := NewTokenIssuer("other-secret")

	tokenString, err := tokens.Issue(uuid.New(), RoleCommuter)
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	if _, err := other.Parse(tokenString); err == nil {
		t.Error("expected parse with wrong secret to fail")
	}
}

func TestParse_RejectsExpiredToken(t *testing.T) {
	tokens := NewTokenIssuer("test-secret")

	claims := Claims{
		UserID: uuid.New(),
		Role:   RoleOwner,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(tokens.secret)
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}

	if _, err := tokens.Parse(tokenString); err == nil {
		t.Error("expected parse of expired token to fail")
	}
}
