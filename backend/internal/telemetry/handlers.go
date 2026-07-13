package telemetry

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
)

// Handlers wires the in-memory store/hub to REST + WebSocket endpoints. It
// depends on identity.Repo (driver/vehicle/assignment lookups) and
// routing.Repo (route existence checks) directly rather than duplicating
// that data — telemetry is the live-state layer on top of Stage 1/3's
// registries, not a replacement for them.
//
// WebSocket library choice: gorilla/websocket over coder/websocket — it's
// the library CLAUDE.md's stack list already anticipates, is battle-tested,
// and its Upgrader/Conn API is a direct fit for the hub/fan-out pattern
// used here (explicit non-blocking WriteJSON per connection, no implicit
// goroutines to reason about).
type Handlers struct {
	store        *VehicleStateStore
	hub          *Hub
	alerts       *DriverAlertHub
	identityRepo *identity.Repo
	routingRepo  *routing.Repo
	tokens       identity.TokenIssuer
	upgrader     websocket.Upgrader
}

func NewHandlers(store *VehicleStateStore, hub *Hub, alerts *DriverAlertHub, identityRepo *identity.Repo, routingRepo *routing.Repo, tokens identity.TokenIssuer) *Handlers {
	return &Handlers{
		store:        store,
		hub:          hub,
		alerts:       alerts,
		identityRepo: identityRepo,
		routingRepo:  routingRepo,
		tokens:       tokens,
		upgrader: websocket.Upgrader{
			// Dev-only MVP: no browser origin restrictions yet, matching the
			// rest of this repo's "no auth hardening beyond JWT" posture.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
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

// bearerToken extracts a JWT from either the Authorization header or a
// "token" query param. Browsers' WebSocket API cannot set custom headers on
// the handshake request, so a query param fallback is required for a
// real commuter/driver web app; a Go client (see cmd/wsdriver) can use
// either. This is documented here rather than silently only supporting one.
func bearerToken(r *http.Request) string {
	if header := r.Header.Get("Authorization"); header != "" {
		parts := strings.SplitN(header, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return parts[1]
		}
	}
	return r.URL.Query().Get("token")
}

// driverMessage is what a driver's WS connection sends. All fields are
// optional per-message: a position update sets Lat/Lng, a seat change sets
// exactly one of SeatsAvailable/SeatsDelta, and Heartbeat is a no-op ping
// that just keeps the read loop alive.
type driverMessage struct {
	Lat            *float64 `json:"lat,omitempty"`
	Lng            *float64 `json:"lng,omitempty"`
	SeatsAvailable *int     `json:"seats_available,omitempty"`
	SeatsDelta     *int     `json:"seats_delta,omitempty"`
	Heartbeat      bool     `json:"heartbeat,omitempty"`
}

// DriverWS handles GET /ws/driver?route_id=<id>[&token=<jwt>]. It requires
// the caller to be a driver currently assigned to a vehicle, and requires
// an explicit route_id up front — "going online" means "online on a route",
// not just connected. The vehicle is marked online for the duration of the
// connection and offline the moment it closes (clean close or otherwise).
func (h *Handlers) DriverWS(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing token")
		return
	}
	claims, err := h.tokens.Parse(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}
	if claims.Role != identity.RoleDriver {
		writeError(w, http.StatusForbidden, "only drivers may open this connection")
		return
	}

	routeID, err := uuid.Parse(r.URL.Query().Get("route_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "route_id must be a valid uuid")
		return
	}
	if _, err := h.routingRepo.GetRouteByID(r.Context(), routeID); err != nil {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}

	driver, err := h.identityRepo.GetDriverByUserID(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusForbidden, "no driver profile for this account")
		return
	}
	assignment, err := h.identityRepo.GetActiveVehicleAssignmentByDriverID(r.Context(), driver.ID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "driver has no active vehicle assignment")
		return
	}
	vehicle, err := h.identityRepo.GetVehicleByID(r.Context(), assignment.VehicleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load vehicle")
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote an HTTP error response.
	}
	defer conn.Close()

	state := h.store.GoOnline(vehicle.ID, routeID, driver.ID, vehicle.Capacity)
	h.hub.Publish(routeID, Event{Type: EventUpdate, Vehicle: toView(state)})
	defer func() {
		h.store.GoOffline(vehicle.ID)
		h.hub.Publish(routeID, Event{Type: EventOffline, VehicleID: vehicle.ID.String()})
	}()

	// Bidirectional from here: this connection both reads position/seat
	// updates from the driver AND receives server-pushed alerts (Stage 6's
	// stop-requests). gorilla/websocket allows exactly one concurrent reader
	// and one concurrent writer per connection, so reading happens on its
	// own goroutine (forwarding decoded messages over a channel) while this
	// goroutine is the sole writer, selecting between driver messages and
	// pushed alerts — same single-writer discipline as CommuterWS.
	alertSub := h.alerts.Subscribe(driver.ID)
	defer h.alerts.Unsubscribe(alertSub)

	incoming := make(chan driverMessage, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			var msg driverMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return // client disconnected or sent garbage — end the session
			}
			incoming <- msg
		}
	}()

	for {
		select {
		case msg := <-incoming:
			var (
				newState VehicleState
				changed  bool
			)
			switch {
			case msg.Lat != nil && msg.Lng != nil:
				newState, changed = h.store.UpdatePosition(vehicle.ID, *msg.Lat, *msg.Lng)
			case msg.SeatsAvailable != nil:
				newState, changed = h.store.SetSeatsAbsolute(vehicle.ID, *msg.SeatsAvailable)
			case msg.SeatsDelta != nil:
				newState, changed = h.store.AdjustSeats(vehicle.ID, *msg.SeatsDelta)
			default:
				continue // heartbeat-only or empty message: nothing to broadcast
			}
			if changed {
				h.hub.Publish(routeID, Event{Type: EventUpdate, Vehicle: toView(newState)})
			}
		case alert := <-alertSub.ch:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(alert); err != nil {
				return
			}
		case <-readDone:
			return
		}
	}
}

// CommuterWS handles GET /ws/commuter?route_id=<id>. Deliberately public
// (no auth) — live vehicle position/seat-state isn't sensitive data (unlike
// wallet/fare endpoints), and a commuter should be able to see the map
// before logging in, matching Stage 3's decision to leave /routes public.
func (h *Handlers) CommuterWS(w http.ResponseWriter, r *http.Request) {
	routeID, err := uuid.Parse(r.URL.Query().Get("route_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "route_id must be a valid uuid")
		return
	}
	if _, err := h.routingRepo.GetRouteByID(r.Context(), routeID); err != nil {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sub := h.hub.Subscribe(routeID)
	defer h.hub.Unsubscribe(sub)

	snapshot := make([]VehicleView, 0)
	for _, state := range h.store.ListByRoute(routeID) {
		snapshot = append(snapshot, toView(state))
	}
	if err := conn.WriteJSON(map[string]any{"type": "snapshot", "vehicles": snapshot}); err != nil {
		return
	}

	// gorilla/websocket requires an active reader to detect the peer closing
	// the connection; this connection never expects incoming application
	// messages, so the reader just discards until it errors (close/EOF).
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case evt := <-sub.ch:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(evt); err != nil {
				return
			}
		case <-closed:
			return
		}
	}
}

type vehiclesSnapshotResponse struct {
	RouteID  string        `json:"route_id"`
	Vehicles []VehicleView `json:"vehicles"`
}

// VehiclesSnapshot handles GET /telemetry/vehicles?route_id=<id> — a plain
// REST read of current live vehicles on a route, for debugging and for a
// map's initial load before the WS connection is established.
func (h *Handlers) VehiclesSnapshot(w http.ResponseWriter, r *http.Request) {
	routeID, err := uuid.Parse(r.URL.Query().Get("route_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "route_id must be a valid uuid")
		return
	}

	states := h.store.ListByRoute(routeID)
	vehicles := make([]VehicleView, 0, len(states))
	for _, s := range states {
		vehicles = append(vehicles, toView(s))
	}
	writeJSON(w, http.StatusOK, vehiclesSnapshotResponse{RouteID: routeID.String(), Vehicles: vehicles})
}

type seatsRequest struct {
	Delta          *int `json:"delta,omitempty"`
	SeatsAvailable *int `json:"seats_available,omitempty"`
}

// UpdateSeats handles POST /telemetry/seats (driver only) — an alternative
// to sending seat changes over the driver's own WS message stream, useful
// for a simple REST-only driver client. Requires the driver's vehicle to
// currently be online (i.e. connected via /ws/driver).
func (h *Handlers) UpdateSeats(w http.ResponseWriter, r *http.Request) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req seatsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Delta == nil && req.SeatsAvailable == nil {
		writeError(w, http.StatusBadRequest, "delta or seats_available is required")
		return
	}

	driver, err := h.identityRepo.GetDriverByUserID(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusForbidden, "no driver profile for this account")
		return
	}
	assignment, err := h.identityRepo.GetActiveVehicleAssignmentByDriverID(r.Context(), driver.ID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "driver has no active vehicle assignment")
		return
	}

	var (
		newState VehicleState
		changed  bool
	)
	if req.SeatsAvailable != nil {
		newState, changed = h.store.SetSeatsAbsolute(assignment.VehicleID, *req.SeatsAvailable)
	} else {
		newState, changed = h.store.AdjustSeats(assignment.VehicleID, *req.Delta)
	}
	if !changed {
		writeError(w, http.StatusConflict, "vehicle is not currently online")
		return
	}

	h.hub.Publish(newState.RouteID, Event{Type: EventUpdate, Vehicle: toView(newState)})
	writeJSON(w, http.StatusOK, toView(newState))
}
