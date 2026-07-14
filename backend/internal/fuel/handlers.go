package fuel

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/wallet"
)

type Handlers struct {
	repo               *Repo
	withholdPct        int
	pricePerLitreCents int64
}

func NewHandlers(repo *Repo, withholdPct int, pricePerLitreCents int64) *Handlers {
	return &Handlers{repo: repo, withholdPct: withholdPct, pricePerLitreCents: pricePerLitreCents}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// --- Real ledger: withholding + balance -----------------------------------

type allocateResponse struct {
	TransactionID  string `json:"transaction_id"`
	AllocatedCents int64  `json:"allocated_cents"`
	WithholdPct    int    `json:"withhold_pct"`
}

// Allocate handles POST /fuel/allocate (owner only): withholds
// withholdPct% of the caller's current owner_revenue balance into their
// fuel_account, as a real double-entry ledger transaction.
func (h *Handlers) Allocate(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	txn, amount, err := h.repo.Allocate(r.Context(), claims.UserID, h.withholdPct)
	if err != nil {
		switch {
		case errors.Is(err, ErrNothingToAllocate):
			writeError(w, http.StatusUnprocessableEntity, "owner_revenue balance is zero, nothing to allocate")
		default:
			writeError(w, http.StatusInternalServerError, "failed to allocate fuel funds")
		}
		return
	}

	writeJSON(w, http.StatusCreated, allocateResponse{
		TransactionID:  txn.ID.String(),
		AllocatedCents: amount,
		WithholdPct:    h.withholdPct,
	})
}

type balanceResponse struct {
	BalanceCents int64 `json:"balance_cents"`
}

// Balance handles GET /fuel/balance (owner only): the caller's current
// fuel_account ledger balance.
func (h *Handlers) Balance(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	balance, err := h.repo.Balance(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load fuel balance")
		return
	}
	writeJSON(w, http.StatusOK, balanceResponse{BalanceCents: balance})
}

// --- Per-vehicle quota -------------------------------------------------

type fundQuotaRequest struct {
	VehicleID   string `json:"vehicle_id"`
	AmountCents int64  `json:"amount_cents"`
}

type quotaResponse struct {
	VehicleID      string `json:"vehicle_id"`
	QuotaCents     int64  `json:"quota_cents"`
	ReservedCents  int64  `json:"reserved_cents"`
	UsedCents      int64  `json:"used_cents"`
	AvailableCents int64  `json:"available_cents"`
}

// FundVehicleQuota handles POST /fuel/vehicle/quota (owner only): earmarks
// amount_cents of the caller's fuel_account balance to one of their own
// vehicles.
func (h *Handlers) FundVehicleQuota(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req fundQuotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	vehicleID, err := uuid.Parse(req.VehicleID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "vehicle_id must be a valid uuid")
		return
	}
	if req.AmountCents <= 0 {
		writeError(w, http.StatusBadRequest, "amount_cents must be positive")
		return
	}

	q, err := h.repo.FundVehicleQuota(r.Context(), claims.UserID, vehicleID, req.AmountCents)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "vehicle not found")
		case errors.Is(err, ErrNotOwnersVehicle):
			writeError(w, http.StatusForbidden, "vehicle does not belong to this owner")
		case errors.Is(err, wallet.ErrInsufficientFunds):
			writeError(w, http.StatusUnprocessableEntity, "amount exceeds available fuel_account balance")
		default:
			writeError(w, http.StatusInternalServerError, "failed to fund vehicle fuel quota")
		}
		return
	}

	writeJSON(w, http.StatusCreated, quotaResponseFrom(q))
}

// VehicleQuota handles GET /fuel/vehicle/quota?vehicle_id=... (owner only):
// the current quota state for one of the caller's vehicles.
func (h *Handlers) VehicleQuota(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	vehicleID, err := uuid.Parse(r.URL.Query().Get("vehicle_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "vehicle_id query param must be a valid uuid")
		return
	}

	q, err := h.repo.VehicleQuotaFor(r.Context(), vehicleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load vehicle fuel quota")
		return
	}
	if q.OwnerUserID != uuid.Nil && q.OwnerUserID != claims.UserID {
		writeError(w, http.StatusForbidden, "vehicle does not belong to this owner")
		return
	}

	writeJSON(w, http.StatusOK, quotaResponseFrom(q))
}

func quotaResponseFrom(q VehicleQuota) quotaResponse {
	return quotaResponse{
		VehicleID:      q.VehicleID.String(),
		QuotaCents:     q.QuotaCents,
		ReservedCents:  q.ReservedCents,
		UsedCents:      q.UsedCents,
		AvailableCents: q.AvailableCents(),
	}
}

// --- MOCK VIU authorize/confirm -----------------------------------------
//
// These two handlers are the HTTP surface of the simulated VIU/pump
// boundary documented in viu_mock.go. Neither talks to any hardware.

type authorizeRequest struct {
	VehicleID   string   `json:"vehicle_id"`
	Litres      *float64 `json:"litres,omitempty"`
	AmountCents *int64   `json:"amount_cents,omitempty"`
}

type authorizeResponse struct {
	Authorized     bool   `json:"authorized"`
	Reason         string `json:"reason,omitempty"`
	AuthReference  string `json:"auth_reference,omitempty"`
	MaxAmountCents int64  `json:"max_amount_cents"`
}

// AuthorizePump handles POST /fuel/viu/authorize — see viu_mock.go. Accepts
// EITHER litres (converted to cents via the configured price per litre) OR
// amount_cents directly; exactly one must be given.
func (h *Handlers) AuthorizePump(w http.ResponseWriter, r *http.Request) {
	var req authorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	vehicleID, err := uuid.Parse(req.VehicleID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "vehicle_id must be a valid uuid")
		return
	}

	var litres float64
	var amountCents int64
	switch {
	case req.Litres != nil && req.AmountCents != nil:
		writeError(w, http.StatusBadRequest, "provide either litres or amount_cents, not both")
		return
	case req.Litres != nil:
		if *req.Litres <= 0 {
			writeError(w, http.StatusBadRequest, "litres must be positive")
			return
		}
		litres = *req.Litres
		amountCents = int64(litres * float64(h.pricePerLitreCents))
	case req.AmountCents != nil:
		if *req.AmountCents <= 0 {
			writeError(w, http.StatusBadRequest, "amount_cents must be positive")
			return
		}
		amountCents = *req.AmountCents
		litres = float64(amountCents) / float64(h.pricePerLitreCents)
	default:
		writeError(w, http.StatusBadRequest, "provide either litres or amount_cents")
		return
	}

	result, err := h.repo.AuthorizePump(r.Context(), vehicleID, litres, amountCents)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to authorize pump")
		return
	}

	resp := authorizeResponse{Authorized: result.Authorized, Reason: result.Reason, MaxAmountCents: result.MaxAmountCents}
	if result.Authorized {
		resp.AuthReference = result.AuthReference.String()
		writeJSON(w, http.StatusOK, resp)
		return
	}
	// A denial is a normal, well-formed response, not a server error — 200
	// with authorized:false, mirroring how a real VIU integration would
	// receive a clean decline rather than an HTTP failure.
	writeJSON(w, http.StatusOK, resp)
}

type confirmRequest struct {
	AuthReference string `json:"auth_reference"`
}

type confirmResponse struct {
	VehicleID        string `json:"vehicle_id"`
	AmountCents      int64  `json:"amount_cents"`
	AlreadyConfirmed bool   `json:"already_confirmed"`
}

// ConfirmPump handles POST /fuel/viu/confirm — see viu_mock.go.
func (h *Handlers) ConfirmPump(w http.ResponseWriter, r *http.Request) {
	var req confirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	authRef, err := uuid.Parse(req.AuthReference)
	if err != nil {
		writeError(w, http.StatusBadRequest, "auth_reference must be a valid uuid")
		return
	}

	result, err := h.repo.ConfirmPump(r.Context(), authRef)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "unknown auth_reference")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to confirm pump session")
		return
	}

	writeJSON(w, http.StatusOK, confirmResponse{
		VehicleID:        result.VehicleID.String(),
		AmountCents:      result.AmountCents,
		AlreadyConfirmed: result.AlreadyConfirmed,
	})
}
