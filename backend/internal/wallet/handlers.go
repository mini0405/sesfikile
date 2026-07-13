package wallet

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/identity"
)

type Handlers struct {
	repo  *Repo
	split config.FareSplit
}

func NewHandlers(repo *Repo, split config.FareSplit) *Handlers {
	return &Handlers{repo: repo, split: split}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// accountTypeForRole maps a caller's role to the account type /wallet/balance
// reports on.
func accountTypeForRole(role identity.Role) (AccountType, bool) {
	switch role {
	case identity.RoleCommuter:
		return AccountCommuterWallet, true
	case identity.RoleDriver:
		return AccountDriverEarnings, true
	case identity.RoleOwner:
		return AccountOwnerRevenue, true
	default:
		return "", false
	}
}

type topupRequest struct {
	AmountCents int64 `json:"amount_cents"`
}

type topupResponse struct {
	TransactionID string `json:"transaction_id"`
	BalanceCents  int64  `json:"balance_cents"`
}

func (h *Handlers) Topup(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req topupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AmountCents <= 0 {
		writeError(w, http.StatusBadRequest, "amount_cents must be positive")
		return
	}

	txn, balance, err := h.repo.Topup(r.Context(), claims.UserID, req.AmountCents)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to process top-up")
		return
	}

	writeJSON(w, http.StatusCreated, topupResponse{TransactionID: txn.ID.String(), BalanceCents: balance})
}

type balanceResponse struct {
	AccountType  string `json:"account_type"`
	BalanceCents int64  `json:"balance_cents"`
}

func (h *Handlers) Balance(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	accountType, ok := accountTypeForRole(claims.Role)
	if !ok {
		writeError(w, http.StatusForbidden, "role has no wallet account")
		return
	}

	acc, err := h.repo.GetOrCreateAccount(r.Context(), h.repo.pool, &claims.UserID, accountType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load account")
		return
	}

	balance, err := h.repo.AccountBalance(r.Context(), h.repo.pool, acc.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute balance")
		return
	}

	writeJSON(w, http.StatusOK, balanceResponse{AccountType: string(accountType), BalanceCents: balance})
}

type chargeFareRequest struct {
	CommuterID     string `json:"commuter_id"`
	VehicleID      string `json:"vehicle_id"`
	FareCents      int64  `json:"fare_cents"`
	IdempotencyKey string `json:"idempotency_key"`
}

type chargeFareResponse struct {
	TransactionID string `json:"transaction_id"`
	Replayed      bool   `json:"replayed"`
	FareCents     int64  `json:"fare_cents"`
	PlatformCents int64  `json:"platform_cents"`
	DriverCents   int64  `json:"driver_cents"`
	OwnerCents    int64  `json:"owner_cents"`
}

func (h *Handlers) ChargeFare(w http.ResponseWriter, r *http.Request) {
	var req chargeFareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	commuterID, err := uuid.Parse(req.CommuterID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "commuter_id must be a valid uuid")
		return
	}
	vehicleID, err := uuid.Parse(req.VehicleID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "vehicle_id must be a valid uuid")
		return
	}
	if req.FareCents <= 0 {
		writeError(w, http.StatusBadRequest, "fare_cents must be positive")
		return
	}
	if req.IdempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "idempotency_key is required")
		return
	}

	txn, split, replayed, err := h.repo.ChargeFare(r.Context(), commuterID, vehicleID, req.FareCents, req.IdempotencyKey, h.split.PlatformPct, h.split.DriverPct)
	if err != nil {
		switch {
		case errors.Is(err, ErrInsufficientFunds):
			writeError(w, http.StatusPaymentRequired, "insufficient funds")
		case errors.Is(err, ErrNoActiveDriver):
			writeError(w, http.StatusUnprocessableEntity, "vehicle has no active driver assignment")
		default:
			writeError(w, http.StatusInternalServerError, "failed to process fare charge")
		}
		return
	}

	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, chargeFareResponse{
		TransactionID: txn.ID.String(),
		Replayed:      replayed,
		FareCents:     req.FareCents,
		PlatformCents: split.PlatformCents,
		DriverCents:   split.DriverCents,
		OwnerCents:    split.OwnerCents,
	})
}
