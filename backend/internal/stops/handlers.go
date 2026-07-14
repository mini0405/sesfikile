package stops

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/telemetry"
)

// Handlers wires the in-memory stop-request Store to REST endpoints. It
// reuses routing.Repo (route/leg/stop data, Stage 3), telemetry's
// VehicleStateStore + DriverAlertHub (live driver positions and the
// server-push channel, Stage 4), and identity.Repo (driver profile lookup
// for ack, Stage 1) — no new persistence, no duplicated lookups.
type Handlers struct {
	store        *Store
	routingRepo  *routing.Repo
	telemetry    *telemetry.VehicleStateStore
	alerts       *telemetry.DriverAlertHub
	identityRepo *identity.Repo
}

func NewHandlers(store *Store, routingRepo *routing.Repo, telemetryStore *telemetry.VehicleStateStore, alerts *telemetry.DriverAlertHub, identityRepo *identity.Repo) *Handlers {
	return &Handlers{
		store:        store,
		routingRepo:  routingRepo,
		telemetry:    telemetryStore,
		alerts:       alerts,
		identityRepo: identityRepo,
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

// loadRouteStops builds a route's ordered stop sequence from its legs: the
// first leg's from-stop, then each leg's to-stop in Sequence order (legs
// are already sequence-ordered by routing.Repo.ListLegsForRoute). The
// seeded/demo dataset has only a handful of stops per route, so fetching
// each one individually (rather than a batched query) is fine at this
// scale — same reasoning routing.AllRoutesWithLegs already relies on.
func (h *Handlers) loadRouteStops(ctx context.Context, legs []routing.RouteLeg) ([]RouteStop, error) {
	ids := make([]uuid.UUID, 0, len(legs)+1)
	seen := make(map[uuid.UUID]bool, len(legs)+1)

	ids = append(ids, legs[0].FromStopID)
	seen[legs[0].FromStopID] = true
	for _, leg := range legs {
		if !seen[leg.ToStopID] {
			ids = append(ids, leg.ToStopID)
			seen[leg.ToStopID] = true
		}
	}

	result := make([]RouteStop, 0, len(ids))
	for _, id := range ids {
		stop, err := h.routingRepo.GetStopByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if !stop.CoordinatesKnown() {
			// Always a catalogue-imported stop (internal/catalogue) — its
			// source CSV has no coordinates at all. Live stop-request
			// matching needs a real position, so bail cleanly rather than
			// treating an unknown position as (0, 0).
			return nil, ErrCoordinatesUnknown
		}
		result = append(result, RouteStop{StopID: stop.ID, Lat: *stop.Latitude, Lng: *stop.Longitude})
	}
	return result, nil
}

type requestStopRequest struct {
	RouteID string `json:"route_id"`
	StopID  string `json:"stop_id"`
}

type requestStopResponse struct {
	RequestID       string `json:"request_id"`
	Status          string `json:"status"`
	DriverAvailable bool   `json:"driver_available"`
	Message         string `json:"message,omitempty"`
}

// RequestStop handles POST /stops/request (commuter only): {route_id,
// stop_id}. Finds the nearest approaching online driver on that route (see
// match.go for the exact rule) and pushes them a live alert over their
// existing /ws/driver connection. If no driver qualifies, returns a clean
// "unmatched" result (200), not an error.
func (h *Handlers) RequestStop(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req requestStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	routeID, err := uuid.Parse(req.RouteID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "route_id must be a valid uuid")
		return
	}
	stopID, err := uuid.Parse(req.StopID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "stop_id must be a valid uuid")
		return
	}

	if _, err := h.routingRepo.GetRouteByID(r.Context(), routeID); err != nil {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	legs, err := h.routingRepo.ListLegsForRoute(r.Context(), routeID)
	if err != nil || len(legs) == 0 {
		writeError(w, http.StatusInternalServerError, "failed to load route legs")
		return
	}
	routeStops, err := h.loadRouteStops(r.Context(), legs)
	if err != nil {
		if errors.Is(err, ErrCoordinatesUnknown) {
			writeError(w, http.StatusUnprocessableEntity,
				"this route has no known stop coordinates (likely an imported catalogue route with no map data) and cannot be used for live stop requests")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load route stops")
		return
	}

	targetIdx, onRoute := StopSequenceIndex(routeStops, stopID)
	if !onRoute {
		writeError(w, http.StatusNotFound, "stop is not on this route")
		return
	}
	targetStop := routeStops[targetIdx]

	stop, err := h.routingRepo.GetStopByID(r.Context(), stopID)
	if err != nil {
		writeError(w, http.StatusNotFound, "stop not found")
		return
	}

	states := h.telemetry.ListByRoute(routeID)
	drivers := make([]DriverPosition, 0, len(states))
	for _, s := range states {
		drivers = append(drivers, DriverPosition{DriverID: s.DriverID, VehicleID: s.VehicleID, Lat: s.Lat, Lng: s.Lng})
	}

	now := time.Now()
	request := Request{
		ID:          uuid.New(),
		CommuterID:  claims.UserID,
		RouteID:     routeID,
		StopID:      stopID,
		StopName:    stop.Name,
		RequestedAt: now,
		Status:      StatusUnmatched,
	}

	candidate, found := FindApproachingDriver(drivers, routeStops, targetStop)
	if found {
		delivered := h.alerts.Send(candidate.DriverID, telemetry.AlertMessage{
			Type:        "stop_request",
			RequestID:   request.ID.String(),
			RouteID:     routeID.String(),
			StopID:      stopID.String(),
			StopName:    stop.Name,
			RequestedAt: now.UTC().Format(time.RFC3339),
		})
		if delivered {
			request.Status = StatusPending
			request.MatchedDriverID = &candidate.DriverID
		}
		// delivered == false: the matched driver's telemetry said online but
		// their alert mailbox wasn't reachable at send time (e.g. it just
		// disconnected) — fall through as unmatched rather than claiming
		// success.
	}
	h.store.Put(request)

	if request.Status != StatusPending {
		writeJSON(w, http.StatusOK, requestStopResponse{
			RequestID:       request.ID.String(),
			Status:          string(request.Status),
			DriverAvailable: false,
			Message:         "no driver is currently available for this stop",
		})
		return
	}

	writeJSON(w, http.StatusCreated, requestStopResponse{
		RequestID:       request.ID.String(),
		Status:          string(request.Status),
		DriverAvailable: true,
	})
}

type ackResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}

// AckRequest handles POST /stops/request/{id}/ack (driver only). Only the
// driver matched to the request may acknowledge it. Acknowledging an
// already-acknowledged request is idempotent (see Store.Acknowledge).
func (h *Handlers) AckRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a valid uuid")
		return
	}

	driver, err := h.identityRepo.GetDriverByUserID(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusForbidden, "no driver profile for this account")
		return
	}

	request, ok := h.store.Get(requestID)
	if !ok {
		writeError(w, http.StatusNotFound, "stop request not found")
		return
	}
	if request.MatchedDriverID == nil || *request.MatchedDriverID != driver.ID {
		writeError(w, http.StatusForbidden, "not the matched driver for this request")
		return
	}

	updated, err := h.store.Acknowledge(requestID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "stop request not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to acknowledge request")
		return
	}

	writeJSON(w, http.StatusOK, ackResponse{RequestID: updated.ID.String(), Status: string(updated.Status)})
}
