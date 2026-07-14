package wallet

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/identity"
)

type Handlers struct {
	repo         *Repo
	split        config.FareSplit
	identityRepo *identity.Repo
}

// NewHandlers wires wallet.Repo to /wallet/*. identityRepo is only used by
// Transactions, to resolve a fare posting's vehicle_id (carried in ledger
// metadata) into a human-readable registration — reusing Stage 1's data
// rather than duplicating vehicle lookups.
func NewHandlers(repo *Repo, split config.FareSplit, identityRepo *identity.Repo) *Handlers {
	return &Handlers{repo: repo, split: split, identityRepo: identityRepo}
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

type transactionResponse struct {
	TransactionID       string    `json:"transaction_id"`
	Kind                string    `json:"kind"`
	AmountCents         int64     `json:"amount_cents"`
	OccurredAt          time.Time `json:"occurred_at"`
	VehicleID           *string   `json:"vehicle_id,omitempty"`
	VehicleRegistration *string   `json:"vehicle_registration,omitempty"`
}

type transactionsPageResponse struct {
	Transactions []transactionResponse `json:"transactions"`
	Total        int64                 `json:"total"`
	Limit        int                   `json:"limit"`
	Offset       int                   `json:"offset"`
}

// Transactions handles GET /wallet/transactions?limit=&offset= (commuter
// only). Identity is taken from the validated JWT (claims.UserID), never a
// request parameter — the query is scoped server-side to the caller's own
// commuter_wallet account id, the same structural-not-filtered scoping
// pattern Stage 8's /owner/* handlers use (ownerFromContext).
func (h *Handlers) Transactions(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	acc, err := h.repo.GetOrCreateAccount(r.Context(), h.repo.pool, &claims.UserID, AccountCommuterWallet)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load account")
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	txns, total, err := h.repo.ListTransactionsForAccount(r.Context(), acc.ID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load transactions")
		return
	}

	resp := make([]transactionResponse, 0, len(txns))
	for _, t := range txns {
		tr := transactionResponse{
			TransactionID: t.TransactionID.String(),
			Kind:          string(t.Kind),
			AmountCents:   t.AmountCents,
			OccurredAt:    t.OccurredAt,
		}
		if t.VehicleID != nil {
			vid := t.VehicleID.String()
			tr.VehicleID = &vid
			if vehicle, err := h.identityRepo.GetVehicleByID(r.Context(), *t.VehicleID); err == nil {
				reg := vehicle.Registration
				tr.VehicleRegistration = &reg
			}
		}
		resp = append(resp, tr)
	}

	writeJSON(w, http.StatusOK, transactionsPageResponse{
		Transactions: resp,
		Total:        total,
		Limit:        limit,
		Offset:       offset,
	})
}
