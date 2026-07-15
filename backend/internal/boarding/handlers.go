package boarding

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/telemetry"
	"sesfikile/backend/internal/wallet"
)

// Handlers fuses Stage 1 (identity/auth), Stage 2 (the wallet ledger),
// Stage 3 (routing/fares), and Stage 4 (live vehicle state) into the
// boarding flow. It does not reimplement any of them — it only adds the
// HMAC pass format and the scan verification/ratify sequence on top.
type Handlers struct {
	routingRepo  *routing.Repo
	walletRepo   *wallet.Repo
	identityRepo *identity.Repo
	telemetry    *telemetry.VehicleStateStore
	hub          *telemetry.Hub
	signer       Signer
	ttl          time.Duration
	split        config.FareSplit
}

func NewHandlers(routingRepo *routing.Repo, walletRepo *wallet.Repo, identityRepo *identity.Repo, store *telemetry.VehicleStateStore, hub *telemetry.Hub, signer Signer, ttl time.Duration, split config.FareSplit) *Handlers {
	return &Handlers{
		routingRepo:  routingRepo,
		walletRepo:   walletRepo,
		identityRepo: identityRepo,
		telemetry:    store,
		hub:          hub,
		signer:       signer,
		ttl:          ttl,
		split:        split,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type issuePassRequest struct {
	RouteID    string `json:"route_id"`
	FromStopID string `json:"from_stop_id"`
	ToStopID   string `json:"to_stop_id"`
}

type issuePassResponse struct {
	PassToken string `json:"pass_token"`
	ExpiresAt string `json:"expires_at"`
	FareCents int64  `json:"fare_cents"`
}

// IssuePass handles POST /boarding/pass (commuter only). It prices the
// from->to ride on route_id using Stage 3 routing (rejecting if there's no
// valid direct path on that route) and returns a short-TTL HMAC-signed pass
// — the token a QR code would carry.
func (h *Handlers) IssuePass(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req issuePassRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	routeID, err := uuid.Parse(req.RouteID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "route_id must be a valid uuid")
		return
	}
	fromStopID, err := uuid.Parse(req.FromStopID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "from_stop_id must be a valid uuid")
		return
	}
	toStopID, err := uuid.Parse(req.ToStopID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "to_stop_id must be a valid uuid")
		return
	}

	route, err := h.routingRepo.GetRouteByID(r.Context(), routeID)
	if err != nil {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	if route.Source == routing.SourceCatalogue {
		// Explicit, intentional guard — mirrors stops.Handlers.RequestStop's
		// same-shaped check. Gated on Source, not on fare_estimated or
		// coordinates or name: a catalogue route's fields can otherwise look
		// arbitrarily "real" (real coordinates since the GeoJSON upgrade,
		// a plausible-looking distance-estimated fare), so Source is the one
		// field that can never be spoofed into looking live. No vehicle can
		// ever go online on a catalogue route, so a pass issued against one
		// could never be scanned — reject at issue time rather than letting
		// a commuter hold a token that's meaningless by construction.
		writeError(w, http.StatusUnprocessableEntity,
			"this is a catalogue-imported route with no live vehicles — boarding passes aren't available on it")
		return
	}
	legs, err := h.routingRepo.ListLegsForRoute(r.Context(), routeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load route legs")
		return
	}
	fareCents, ok := routing.FareForSegment(legs, fromStopID, toStopID)
	if !ok {
		writeError(w, http.StatusNotFound, "no valid path from from_stop_id to to_stop_id on this route")
		return
	}

	now := time.Now()
	payload := PassPayload{
		CommuterID: claims.UserID,
		RouteID:    routeID,
		FromStopID: fromStopID,
		ToStopID:   toStopID,
		FareCents:  fareCents,
		IssuedAt:   now,
		ExpiresAt:  now.Add(h.ttl),
		Nonce:      uuid.NewString(),
	}

	token, err := h.signer.Sign(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign pass")
		return
	}

	writeJSON(w, http.StatusCreated, issuePassResponse{
		PassToken: token,
		ExpiresAt: payload.ExpiresAt.UTC().Format(time.RFC3339),
		FareCents: fareCents,
	})
}

type scanPassRequest struct {
	PassToken string `json:"pass_token"`
}

type scanPassResponse struct {
	TransactionID  string `json:"transaction_id"`
	FareCents      int64  `json:"fare_cents"`
	PlatformCents  int64  `json:"platform_cents"`
	DriverCents    int64  `json:"driver_cents"`
	OwnerCents     int64  `json:"owner_cents"`
	SeatsRemaining int    `json:"seats_remaining"`
	Replayed       bool   `json:"replayed"`
}

// ScanPass handles POST /boarding/scan (driver only) — the hero moment.
// Verification order, each failing distinctly:
//  1. HMAC signature (constant-time compare) — tampered pass rejected.
//  2. Expiry — expired pass rejected.
//  3. Driver is online (Stage 4) and assigned (Stage 1) to a vehicle on the
//     pass's route — mismatch rejected.
//  4. Fare charged through the Stage 2 ledger, using the pass's nonce as the
//     idempotency_key — a double-scan charges exactly once.
//  5. Seats decremented by exactly 1, but ONLY on a fresh charge — an
//     idempotent replay must not double-decrement seats either.
func (h *Handlers) ScanPass(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req scanPassRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.PassToken == "" {
		writeError(w, http.StatusBadRequest, "pass_token is required")
		return
	}

	// 1. Signature.
	payload, err := h.signer.Verify(req.PassToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or tampered pass")
		return
	}

	// 2. Expiry.
	if payload.Expired(time.Now()) {
		writeError(w, http.StatusGone, "pass has expired")
		return
	}

	// 3. Driver online, assigned, on this pass's route.
	driver, err := h.identityRepo.GetDriverByUserID(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusForbidden, "no driver profile for this account")
		return
	}
	assignment, err := h.identityRepo.GetActiveVehicleAssignmentByDriverID(r.Context(), driver.ID)
	if err != nil {
		writeError(w, http.StatusConflict, "driver has no active vehicle assignment")
		return
	}
	state, online := h.telemetry.Get(assignment.VehicleID)
	if !online || !state.Online || state.RouteID != payload.RouteID {
		writeError(w, http.StatusConflict, "driver is not online on this pass's route")
		return
	}

	// 4. Charge through the existing Stage 2 ledger, keyed by the pass nonce.
	txn, split, replayed, err := h.walletRepo.ChargeFare(r.Context(), payload.CommuterID, assignment.VehicleID, payload.FareCents, payload.Nonce, h.split.PlatformPct, h.split.DriverPct)
	if err != nil {
		switch {
		case errors.Is(err, wallet.ErrInsufficientFunds):
			writeError(w, http.StatusPaymentRequired, "insufficient funds")
		case errors.Is(err, wallet.ErrNoActiveDriver):
			writeError(w, http.StatusUnprocessableEntity, "vehicle has no active driver assignment")
		default:
			writeError(w, http.StatusInternalServerError, "failed to process fare charge")
		}
		return
	}

	// 5. Seat decrement tied to freshness, not to every scan.
	seatsRemaining := state.SeatsAvailable
	if !replayed {
		newState, changed := h.telemetry.AdjustSeats(assignment.VehicleID, -1)
		if changed {
			seatsRemaining = newState.SeatsAvailable
			h.hub.Publish(newState.RouteID, telemetry.Event{Type: telemetry.EventUpdate, Vehicle: telemetry.ToView(newState)})
		}
	}

	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, scanPassResponse{
		TransactionID:  txn.ID.String(),
		FareCents:      payload.FareCents,
		PlatformCents:  split.PlatformCents,
		DriverCents:    split.DriverCents,
		OwnerCents:     split.OwnerCents,
		SeatsRemaining: seatsRemaining,
		Replayed:       replayed,
	})
}
