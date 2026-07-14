package analytics

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"github.com/google/uuid"

	"sesfikile/backend/internal/fuel"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/telemetry"
)

// Handlers wires Repo to the /owner/* read routes. It reuses identity.Repo
// (Stage 1, vehicle/driver/assignment lookups), fuel.Repo (Stage 7,
// per-vehicle quota), routing.Repo (Stage 3, route names), and
// telemetry.VehicleStateStore (Stage 4, live online/route/seats) — no new
// persistence beyond Repo's read-only ledger queries.
type Handlers struct {
	repo         *Repo
	identityRepo *identity.Repo
	routingRepo  *routing.Repo
	fuelRepo     *fuel.Repo
	telemetry    *telemetry.VehicleStateStore
}

func NewHandlers(repo *Repo, identityRepo *identity.Repo, routingRepo *routing.Repo, fuelRepo *fuel.Repo, telemetryStore *telemetry.VehicleStateStore) *Handlers {
	return &Handlers{
		repo:         repo,
		identityRepo: identityRepo,
		routingRepo:  routingRepo,
		fuelRepo:     fuelRepo,
		telemetry:    telemetryStore,
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

// ownerFromContext resolves the calling owner's user id from the validated
// JWT claims RequireAuth/RequireRole(RoleOwner) already attached — every
// handler below scopes its query to exactly this owner's own id, which is
// what makes cross-owner access structurally impossible rather than merely
// filtered client-side.
func ownerFromContext(r *http.Request) (uuid.UUID, bool) {
	claims, ok := identity.ClaimsFromContext(r.Context())
	if !ok {
		return uuid.Nil, false
	}
	return claims.UserID, true
}

// Summary handles GET /owner/summary?from=&to=.
func (h *Handlers) Summary(w http.ResponseWriter, r *http.Request) {
	ownerUserID, ok := ownerFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	from, to, err := parseDateRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	summary, err := h.repo.Summary(r.Context(), ownerUserID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute summary")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

// Vehicles handles GET /owner/vehicles?from=&to=.
func (h *Handlers) Vehicles(w http.ResponseWriter, r *http.Request) {
	ownerUserID, ok := ownerFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	from, to, err := parseDateRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	vehicles, err := h.identityRepo.ListVehiclesByOwnerUserID(r.Context(), ownerUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load vehicles")
		return
	}

	stats, err := h.repo.VehicleStatsForOwner(r.Context(), ownerUserID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute vehicle stats")
		return
	}

	result := make([]VehicleStat, 0, len(vehicles))
	for _, v := range vehicles {
		vs := VehicleStat{
			VehicleID:    v.ID,
			Registration: v.Registration,
			SeatsTotal:   v.Capacity,
		}

		if assignment, err := h.identityRepo.GetActiveAssignmentByVehicleID(r.Context(), v.ID); err == nil {
			if driver, err := h.identityRepo.GetDriverByID(r.Context(), assignment.DriverID); err == nil {
				id := driver.ID
				name := driver.FullName
				vs.AssignedDriverID = &id
				vs.AssignedDriver = &name
			}
		}

		if state, online := h.telemetry.Get(v.ID); online {
			vs.Online = true
			routeID := state.RouteID
			vs.CurrentRouteID = &routeID
			seats := state.SeatsAvailable
			vs.SeatsAvailable = &seats
			if route, err := h.routingRepo.GetRouteByID(r.Context(), state.RouteID); err == nil {
				name := route.Name
				vs.CurrentRouteName = &name
			}
		}

		if stat, ok := stats[v.ID]; ok {
			vs.Trips = stat.Trips
			vs.RevenueCents = stat.RevenueCents
		}

		if quota, err := h.fuelRepo.VehicleQuotaFor(r.Context(), v.ID); err == nil {
			vs.FuelQuotaCents = quota.QuotaCents
			vs.FuelReservedCents = quota.ReservedCents
			vs.FuelUsedCents = quota.UsedCents
			vs.FuelAvailableCents = quota.AvailableCents()
		}

		result = append(result, vs)
	}

	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "vehicles": result})
}

// Drivers handles GET /owner/drivers?from=&to=.
func (h *Handlers) Drivers(w http.ResponseWriter, r *http.Request) {
	ownerUserID, ok := ownerFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	from, to, err := parseDateRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	drivers, err := h.identityRepo.ListDriversByOwnerUserID(r.Context(), ownerUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load drivers")
		return
	}

	stats, err := h.repo.DriverStatsForOwner(r.Context(), ownerUserID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute driver stats")
		return
	}

	result := make([]DriverStat, 0, len(drivers))
	for _, d := range drivers {
		ds := DriverStat{
			DriverID: d.ID,
			FullName: d.FullName,
		}

		if assignment, err := h.identityRepo.GetActiveVehicleAssignmentByDriverID(r.Context(), d.ID); err == nil {
			if vehicle, err := h.identityRepo.GetVehicleByID(r.Context(), assignment.VehicleID); err == nil {
				reg := vehicle.Registration
				ds.AssignedVehicle = &reg
				if state, online := h.telemetry.Get(vehicle.ID); online && state.DriverID == d.ID {
					ds.Online = true
				}
			}
		}

		if stat, ok := stats[d.UserID]; ok {
			ds.Trips = stat.Trips
			ds.EarningsCents = stat.EarningsCents
		}

		result = append(result, ds)
	}

	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "drivers": result})
}

// RevenueVsFuel handles GET /owner/revenue-vs-fuel?from=&to=.
func (h *Handlers) RevenueVsFuel(w http.ResponseWriter, r *http.Request) {
	ownerUserID, ok := ownerFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	from, to, err := parseDateRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	revenueSeries, err := h.repo.revenueSeriesForOwner(r.Context(), ownerUserID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute revenue series")
		return
	}
	fuelAllocatedSeries, err := h.repo.fuelAllocatedSeriesForOwner(r.Context(), ownerUserID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute fuel-allocated series")
		return
	}
	fuelConsumedSeries, err := h.repo.fuelConsumedSeriesForOwner(r.Context(), ownerUserID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute fuel-consumed series")
		return
	}

	days := map[string]*RevenueVsFuelDay{}
	order := []string{}
	get := func(day string) *RevenueVsFuelDay {
		if d, ok := days[day]; ok {
			return d
		}
		d := &RevenueVsFuelDay{Date: day}
		days[day] = d
		order = append(order, day)
		return d
	}

	var totalRevenue, totalAllocated, totalConsumed int64
	for _, b := range revenueSeries {
		day := b.Day.Format(dateOnlyLayout)
		get(day).RevenueCents = b.AmountCents
		totalRevenue += b.AmountCents
	}
	for _, b := range fuelAllocatedSeries {
		day := b.Day.Format(dateOnlyLayout)
		get(day).FuelAllocatedCents = b.AmountCents
		totalAllocated += b.AmountCents
	}
	for _, b := range fuelConsumedSeries {
		day := b.Day.Format(dateOnlyLayout)
		get(day).FuelConsumedCents = b.AmountCents
		totalConsumed += b.AmountCents
	}

	sort.Strings(order)
	series := make([]RevenueVsFuelDay, 0, len(order))
	for _, day := range order {
		series = append(series, *days[day])
	}

	writeJSON(w, http.StatusOK, RevenueVsFuel{
		From:               from,
		To:                 to,
		RevenueCents:       totalRevenue,
		FuelAllocatedCents: totalAllocated,
		FuelConsumedCents:  totalConsumed,
		Series:             series,
	})
}

// Ledger handles GET /owner/ledger?from=&to=&limit=&offset=.
func (h *Handlers) Ledger(w http.ResponseWriter, r *http.Request) {
	ownerUserID, ok := ownerFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	from, to, err := parseDateRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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

	entries, total, err := h.repo.Ledger(r.Context(), ownerUserID, from, to, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load ledger")
		return
	}

	writeJSON(w, http.StatusOK, LedgerPage{
		Entries: entries,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	})
}
