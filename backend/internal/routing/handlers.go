package routing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handlers struct {
	repo *Repo
}

func NewHandlers(repo *Repo) *Handlers {
	return &Handlers{repo: repo}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type routeResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	AssociationName string `json:"association_name"`
}

func toRouteResponse(r Route) routeResponse {
	return routeResponse{ID: r.ID.String(), Name: r.Name, AssociationName: r.AssociationName}
}

type legResponse struct {
	ID           string `json:"id"`
	RouteID      string `json:"route_id"`
	Sequence     int    `json:"sequence"`
	FromStopID   string `json:"from_stop_id"`
	FromStopName string `json:"from_stop_name"`
	ToStopID     string `json:"to_stop_id"`
	ToStopName   string `json:"to_stop_name"`
	FareCents    int64  `json:"fare_cents"`
}

func toLegResponse(l RouteLeg, stops map[uuid.UUID]Stop) legResponse {
	return legResponse{
		ID:           l.ID.String(),
		RouteID:      l.RouteID.String(),
		Sequence:     l.Sequence,
		FromStopID:   l.FromStopID.String(),
		FromStopName: stops[l.FromStopID].Name,
		ToStopID:     l.ToStopID.String(),
		ToStopName:   stops[l.ToStopID].Name,
		FareCents:    l.FareCents,
	}
}

func (h *Handlers) stopsByID(ctx context.Context) (map[uuid.UUID]Stop, error) {
	stops, err := h.repo.ListStops(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]Stop, len(stops))
	for _, s := range stops {
		byID[s.ID] = s
	}
	return byID, nil
}

// ListRoutes handles GET /routes.
func (h *Handlers) ListRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := h.repo.ListRoutes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list routes")
		return
	}

	resp := make([]routeResponse, 0, len(routes))
	for _, rt := range routes {
		resp = append(resp, toRouteResponse(rt))
	}
	writeJSON(w, http.StatusOK, resp)
}

type stopResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func toStopResponse(s Stop) stopResponse {
	return stopResponse{ID: s.ID.String(), Name: s.Name, Latitude: s.Latitude, Longitude: s.Longitude}
}

// ListStops handles GET /stops and GET /stops?route_id=<id>. Public read,
// consistent with /routes being public reference data (Stage 3) — a
// commuter should be able to see the stop list before logging in.
//
// With no route_id, it returns every stop, alphabetical (same ordering as
// Repo.ListStops/ListRoutes). With route_id, it returns that route's own
// stops in physical sequence order — derived from the route's ordered legs,
// not alphabetical — which is what a commuter app needs to build
// from/to pickers that can't produce an invalid (out-of-sequence) pair.
func (h *Handlers) ListStops(w http.ResponseWriter, r *http.Request) {
	routeIDParam := r.URL.Query().Get("route_id")
	if routeIDParam == "" {
		stops, err := h.repo.ListStops(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list stops")
			return
		}
		resp := make([]stopResponse, 0, len(stops))
		for _, s := range stops {
			resp = append(resp, toStopResponse(s))
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	routeID, err := uuid.Parse(routeIDParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, "route_id must be a valid uuid")
		return
	}
	if _, err := h.repo.GetRouteByID(r.Context(), routeID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "route not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load route")
		return
	}

	legs, err := h.repo.ListLegsForRoute(r.Context(), routeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load route legs")
		return
	}

	stops, err := h.stopsByID(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load stops")
		return
	}

	resp := make([]stopResponse, 0, len(legs)+1)
	if len(legs) > 0 {
		resp = append(resp, toStopResponse(stops[legs[0].FromStopID]))
		for _, l := range legs {
			resp = append(resp, toStopResponse(stops[l.ToStopID]))
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type routeDetailResponse struct {
	Route routeResponse `json:"route"`
	Legs  []legResponse `json:"legs"`
}

// GetRoute handles GET /routes/{id} — a route's ordered stops/legs.
func (h *Handlers) GetRoute(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a valid uuid")
		return
	}

	route, err := h.repo.GetRouteByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "route not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load route")
		return
	}

	legs, err := h.repo.ListLegsForRoute(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load route legs")
		return
	}

	stops, err := h.stopsByID(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load stops")
		return
	}

	legResp := make([]legResponse, 0, len(legs))
	for _, l := range legs {
		legResp = append(legResp, toLegResponse(l, stops))
	}

	writeJSON(w, http.StatusOK, routeDetailResponse{Route: toRouteResponse(route), Legs: legResp})
}

type segmentResponse struct {
	RouteID   string        `json:"route_id"`
	RouteName string        `json:"route_name"`
	Legs      []legResponse `json:"legs"`
	FareCents int64         `json:"fare_cents"`
}

type searchResponse struct {
	Transfers      int               `json:"transfers"`
	TotalFareCents int64             `json:"total_fare_cents"`
	Segments       []segmentResponse `json:"segments"`
}

// resolveStop accepts either a stop UUID or (falling back) an exact stop
// name, so callers can search by id or by name.
func (h *Handlers) resolveStop(ctx context.Context, value string) (Stop, error) {
	if id, err := uuid.Parse(value); err == nil {
		return h.repo.GetStopByID(ctx, id)
	}
	return h.repo.GetStopByName(ctx, value)
}

// Search handles GET /routes/search?from=<stop id or name>&to=<stop id or name>.
// It returns the best path from origin to destination: direct if both stops
// are on one route, otherwise a single-transfer path across a shared
// interchange stop. See graph.go for the fewest-transfers-then-lowest-fare
// ordering. A 404 with an empty body means no path exists.
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	fromParam := r.URL.Query().Get("from")
	toParam := r.URL.Query().Get("to")
	if fromParam == "" || toParam == "" {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return
	}

	from, err := h.resolveStop(r.Context(), fromParam)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "from stop not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to resolve from stop")
		return
	}

	to, err := h.resolveStop(r.Context(), toParam)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "to stop not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to resolve to stop")
		return
	}

	routes, err := h.repo.AllRoutesWithLegs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load routes")
		return
	}

	result, found := Search(routes, from.ID, to.ID)
	if !found {
		writeError(w, http.StatusNotFound, "no route found between the given stops")
		return
	}

	stops, err := h.stopsByID(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load stops")
		return
	}

	segments := make([]segmentResponse, 0, len(result.Segments))
	for _, seg := range result.Segments {
		legResp := make([]legResponse, 0, len(seg.Legs))
		for _, l := range seg.Legs {
			legResp = append(legResp, toLegResponse(l, stops))
		}
		segments = append(segments, segmentResponse{
			RouteID:   seg.RouteID.String(),
			RouteName: seg.RouteName,
			Legs:      legResp,
			FareCents: seg.FareCents,
		})
	}

	writeJSON(w, http.StatusOK, searchResponse{
		Transfers:      result.Transfers,
		TotalFareCents: result.TotalFareCents,
		Segments:       segments,
	})
}
