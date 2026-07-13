package identity_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/identity"
)

// TestRegisterLoginMe requires a reachable Postgres (see infra/docker-compose.yml).
// It skips rather than failing when no database is available, matching the
// approach used for Stage 0's DB-dependent health check test.
func TestRegisterLoginMe(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://sesfikile:sesfikile_dev@localhost:5432/sesfikile?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil || pool.Ping(ctx) != nil {
		t.Skip("skipping integration test: no reachable Postgres database")
	}
	defer pool.Close()

	if err := db.Migrate(databaseURL); err != nil {
		t.Fatalf("failed to apply migrations: %v", err)
	}

	repo := identity.NewRepo(pool)
	tokens := identity.NewTokenIssuer("integration-test-secret")
	handlers := identity.NewHandlers(repo, tokens)

	r := chi.NewRouter()
	r.Post("/auth/register", handlers.Register)
	r.Post("/auth/login", handlers.Login)
	r.Group(func(r chi.Router) {
		r.Use(identity.RequireAuth(tokens))
		r.Get("/me", handlers.Me)
	})

	// A fixed phone number would collide with the row a previous run left
	// behind in a persistent (not reset-between-runs) database — see
	// wallet.uniquePhone's doc comment for the same reasoning.
	phone := fmt.Sprintf("+27%d", time.Now().UnixNano())
	registerBody := `{"phone":"` + phone + `","password":"IntegrationTest123!","role":"commuter"}`

	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(registerBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 from register, got %d: %s", w.Code, w.Body.String())
	}

	loginBody := `{"phone":"` + phone + `","password":"IntegrationTest123!"}`
	req = httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(loginBody))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from login, got %d: %s", w.Code, w.Body.String())
	}

	var loginResp struct {
		Token  string `json:"token"`
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(w.Body).Decode(&loginResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	if loginResp.Token == "" {
		t.Fatal("expected non-empty token from login")
	}

	req = httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Token)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from /me, got %d: %s", w.Code, w.Body.String())
	}

	var meResp struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(w.Body).Decode(&meResp); err != nil {
		t.Fatalf("failed to decode /me response: %v", err)
	}
	if meResp.UserID != loginResp.UserID {
		t.Errorf("expected /me user id %s, got %s", loginResp.UserID, meResp.UserID)
	}
	if meResp.Role != "commuter" {
		t.Errorf("expected role commuter, got %s", meResp.Role)
	}
}
