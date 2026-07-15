package routing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

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
	// Source is "seed" (cmd/seed's hand-seeded demo corridors) or
	// "catalogue" (cmd/importcatalogue's real-but-unverified City of Cape
	// Town rows) — see internal/catalogue.
	Source string `json:"source"`
}

func toRouteResponse(r Route) routeResponse {
	return routeResponse{ID: r.ID.String(), Name: r.Name, AssociationName: r.AssociationName, Source: r.Source}
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
	// FareEstimated is true only for a catalogue-imported leg: its
	// fare_cents was derived from distance (internal/catalogue.
	// EstimateFareCents), NOT an actual association tariff. Always false
	// for every hand-seeded leg.
	FareEstimated  bool     `json:"fare_estimated"`
	DistanceMeters *float64 `json:"distance_meters,omitempty"`
}

func toLegResponse(l RouteLeg, stops map[uuid.UUID]Stop) legResponse {
	return legResponse{
		ID:             l.ID.String(),
		RouteID:        l.RouteID.String(),
		Sequence:       l.Sequence,
		FromStopID:     l.FromStopID.String(),
		FromStopName:   stops[l.FromStopID].Name,
		ToStopID:       l.ToStopID.String(),
		ToStopName:     stops[l.ToStopID].Name,
		FareCents:      l.FareCents,
		FareEstimated:  l.FareEstimated,
		DistanceMeters: l.DistanceMeters,
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
	ID   string `json:"id"`
	Name string `json:"name"`
	// Latitude/Longitude are null only for a coordinate-less stop (a
	// defensive edge case — see internal/catalogue). A catalogue-imported
	// stop normally DOES have a coordinate since the GeoJSON upgrade, but
	// it's an APPROXIMATE (median-derived rank centroid) one, not surveyed —
	// see Source. Check CoordinatesKnown before using either field, never
	// assume (0, 0).
	Latitude         *float64 `json:"latitude"`
	Longitude        *float64 `json:"longitude"`
	CoordinatesKnown bool     `json:"coordinates_known"`
	// Source is "seed" (exact, hand-placed coordinates) or "catalogue"
	// (approximate, median-derived) — see internal/catalogue.
	Source string `json:"source"`
}

func toStopResponse(s Stop) stopResponse {
	return stopResponse{
		ID:               s.ID.String(),
		Name:             s.Name,
		Latitude:         s.Latitude,
		Longitude:        s.Longitude,
		CoordinatesKnown: s.CoordinatesKnown(),
		Source:           s.Source,
	}
}

// ListStops handles GET /stops and GET /stops?route_id=<id>. Public read,
// consistent with /routes being public reference data (Stage 3) — a
// commuter should be able to see the stop list before logging in.
//
// With no route_id, this is the MAP-FACING read: every stop WITH KNOWN
// COORDINATES, alphabetical (Repo.ListStopsWithCoordinates). Since the
// GeoJSON upgrade (internal/catalogue) a catalogue-imported stop normally
// HAS one (an APPROXIMATE, median-derived rank centroid — see
// stopResponse.Source) and so is now included here too; the only stops
// still excluded are genuinely coordinate-less ones (a defensive edge
// case), so nothing consuming this list for a map ever tries to place a
// marker at an unknown/zero position. With route_id, it returns that
// specific route's own stops in physical sequence order regardless of
// whether their coordinates are known — a browse/search use (picking a
// from/to pair on one named route) that never needs a position, so a
// catalogue route's endpoints are still browsable here.
func (h *Handlers) ListStops(w http.ResponseWriter, r *http.Request) {
	routeIDParam := r.URL.Query().Get("route_id")
	if routeIDParam == "" {
		stops, err := h.repo.ListStopsWithCoordinates(r.Context())
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

type geometryResponse struct {
	RouteID    string `json:"route_id"`
	PointCount int    `json:"point_count"`
	// Points is [lon, lat] pairs, WGS84 — matches GeoJSON coordinate order
	// (and Leaflet's L.polyline, once a frontend consumes this: swap to
	// [lat, lon] there, same as any GeoJSON-to-Leaflet conversion).
	Points [][2]float64 `json:"points"`
}

// GetRouteGeometry handles GET /routes/{id}/geometry — a catalogue-imported
// route's real display polyline (internal/catalogue, the GeoJSON upgrade).
// A hand-seeded route has no geometry recorded and 404s here, same as an
// unknown route id — this endpoint doesn't distinguish "route doesn't
// exist" from "route exists but has no geometry" in its status code, only
// in the message, since neither case has anything useful to draw.
func (h *Handlers) GetRouteGeometry(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a valid uuid")
		return
	}

	if _, err := h.repo.GetRouteByID(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "route not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load route")
		return
	}

	points, err := h.repo.GetRouteGeometry(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "no geometry recorded for this route (likely a hand-seeded corridor, not a catalogue import)")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load route geometry")
		return
	}

	writeJSON(w, http.StatusOK, geometryResponse{RouteID: id.String(), PointCount: len(points), Points: points})
}

type routeGeometrySummary struct {
	RouteID            string `json:"route_id"`
	OriginalPointCount int    `json:"original_point_count"`
	// Points is decimated (see decimatePoints) for a bulk response — use
	// GET /routes/{id}/geometry for a route's exact, undecimated polyline.
	Points [][2]float64 `json:"points"`
}

// decimatePoints keeps at most max points from pts, always keeping the
// first and last (an endpoint dropped from a rank's polyline would visibly
// clip the drawn route short of its actual rank). Simple stride selection,
// not Douglas-Peucker — sufficient for a muted backdrop layer that isn't the
// map's focal content; noted as a future upgrade if truer shape fidelity is
// ever needed at closer zoom.
func decimatePoints(pts [][2]float64, max int) [][2]float64 {
	if max < 2 || len(pts) <= max {
		return pts
	}
	out := make([][2]float64, 0, max)
	last := len(pts) - 1
	step := float64(last) / float64(max-1)
	for i := 0; i < max; i++ {
		idx := int(float64(i) * step)
		if idx > last {
			idx = last
		}
		out = append(out, pts[idx])
	}
	return out
}

// ListRouteGeometries handles GET /routes/geometries[?max_points=N] — a bulk
// read of every catalogue route's display polyline in one response, built
// for the commuter map's "network coverage" layer (see repo.
// ListRouteGeometries's doc comment for why this exists as one endpoint
// rather than N per-route requests). Points are decimated server-side to
// max_points per route (default 40, 0 = no decimation) to keep the payload
// drawable — 1447 routes averaging ~395 points each is ~577k points
// undecimated, which both bloats the response and is far more detail than a
// muted backdrop line needs.
func (h *Handlers) ListRouteGeometries(w http.ResponseWriter, r *http.Request) {
	maxPoints := 40
	if v := r.URL.Query().Get("max_points"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			maxPoints = n
		}
	}

	rows, err := h.repo.ListRouteGeometries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list route geometries")
		return
	}

	resp := make([]routeGeometrySummary, 0, len(rows))
	for _, row := range rows {
		pts := row.Points
		if maxPoints > 0 {
			pts = decimatePoints(pts, maxPoints)
		}
		resp = append(resp, routeGeometrySummary{
			RouteID:            row.RouteID.String(),
			OriginalPointCount: len(row.Points),
			Points:             pts,
		})
	}
	writeJSON(w, http.StatusOK, resp)
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
