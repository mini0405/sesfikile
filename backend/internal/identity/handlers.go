package identity

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type Handlers struct {
	repo   *Repo
	tokens TokenIssuer
}

func NewHandlers(repo *Repo, tokens TokenIssuer) *Handlers {
	return &Handlers{repo: repo, tokens: tokens}
}

type registerRequest struct {
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type authResponse struct {
	Token  string `json:"token"`
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Phone = strings.TrimSpace(req.Phone)
	req.Email = strings.TrimSpace(req.Email)
	role := Role(req.Role)

	if req.Phone == "" || req.Password == "" || !role.Valid() {
		writeError(w, http.StatusBadRequest, "phone, password, and a valid role are required")
		return
	}

	passwordHash, err := HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to process password")
		return
	}

	var email *string
	if req.Email != "" {
		email = &req.Email
	}

	user, err := h.repo.CreateUser(r.Context(), req.Phone, email, passwordHash, role)
	if err != nil {
		if errors.Is(err, ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "a user with this phone or email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	token, err := h.tokens.Issue(user.ID, user.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{Token: token, UserID: user.ID.String(), Role: string(user.Role)})
}

type loginRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.repo.GetUserByPhone(r.Context(), strings.TrimSpace(req.Phone))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid phone or password")
		return
	}

	if !VerifyPassword(user.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid phone or password")
		return
	}

	token, err := h.tokens.Issue(user.ID, user.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Token: token, UserID: user.ID.String(), Role: string(user.Role)})
}

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"user_id": claims.UserID.String(),
		"role":    string(claims.Role),
	})
}
