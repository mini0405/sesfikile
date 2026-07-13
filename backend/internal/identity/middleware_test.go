package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func withClaims(r *http.Request, role Role) *http.Request {
	claims := &Claims{UserID: uuid.New(), Role: role}
	return r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
}

func TestRequireRole_Allows(t *testing.T) {
	called := false
	handler := RequireRole(RoleOwner, RoleDriver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := withClaims(httptest.NewRequest(http.MethodGet, "/", nil), RoleDriver)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Fatal("expected downstream handler to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequireRole_Blocks(t *testing.T) {
	called := false
	handler := RequireRole(RoleOwner)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := withClaims(httptest.NewRequest(http.MethodGet, "/", nil), RoleCommuter)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if called {
		t.Fatal("expected downstream handler not to be called")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestRequireRole_BlocksWithoutClaims(t *testing.T) {
	handler := RequireRole(RoleOwner)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireAuth_RejectsMissingHeader(t *testing.T) {
	tokens := NewTokenIssuer("test-secret")
	handler := RequireAuth(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireAuth_AllowsValidToken(t *testing.T) {
	tokens := NewTokenIssuer("test-secret")
	userID := uuid.New()
	tokenString, err := tokens.Issue(userID, RoleCommuter)
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	var gotClaims *Claims
	handler := RequireAuth(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClaims, _ = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotClaims == nil || gotClaims.UserID != userID {
		t.Errorf("expected claims with user id %s, got %+v", userID, gotClaims)
	}
}
