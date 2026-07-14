package config

import (
	"os"
	"strconv"
	"time"
)

// FareSplit holds the percentages a fare is divided into. PlatformPct and
// DriverPct are applied first (rounded down); OwnerPct's share is whatever
// remains, so the three always sum to exactly the fare with no remainder
// lost or invented.
type FareSplit struct {
	PlatformPct int
	DriverPct   int
	OwnerPct    int
}

type Config struct {
	Port               string
	DatabaseURL        string
	JWTSecret          string
	FareSplit          FareSplit
	BoardingHMACSecret string
	BoardingPassTTL    time.Duration
	// FuelWithholdPct is the percentage of an owner's current owner_revenue
	// balance that /fuel/allocate moves into their fuel_account (Stage 7).
	FuelWithholdPct int
	// FuelPricePerLitreCents converts litres <-> cents for the MOCK VIU
	// authorize endpoint — a configurable price, not a real fuel-price feed.
	FuelPricePerLitreCents int64
	// CatalogueFare configures internal/catalogue's distance-derived fare
	// estimate for the opt-in real route catalogue import
	// (cmd/importcatalogue). Not a real tariff — see CatalogueFareModel.
	CatalogueFare CatalogueFareModel
}

// CatalogueFareModel derives an INDICATIVE fare (int64 cents) for an
// opt-in, catalogue-imported route (internal/catalogue) from its
// straight-line distance in metres: base + per-kilometre rate, rounded to
// the nearest cent, clamped to [MinFareCents, MaxFareCents]. This is NOT a
// real association tariff — the source CSV (backend/data/taxi_routes.csv)
// carries no fare data at all. Every fare this produces is stored with
// fare_estimated = true (routes.route_legs) and must be presented
// everywhere as "estimated from distance, not an actual association fare."
// Real fares require association tariff data, which does not exist as an
// input to this MVP.
type CatalogueFareModel struct {
	BaseCents    int64
	PerKmCents   int64
	MinFareCents int64
	MaxFareCents int64
}

func Load() Config {
	return Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://sesfikile:sesfikile_dev@localhost:5432/sesfikile?sslmode=disable"),
		// Dev-only default — override with a real secret via JWT_SECRET in any
		// shared or production environment.
		JWTSecret: getEnv("JWT_SECRET", "dev-only-insecure-secret-change-me"),
		FareSplit: FareSplit{PlatformPct: 10, DriverPct: 25, OwnerPct: 65},
		// Dev-only default — override with a real secret via
		// BOARDING_HMAC_SECRET in any shared or production environment. This
		// signs boarding passes (the token a QR code carries) and must stay
		// secret, or anyone could forge a valid pass.
		BoardingHMACSecret: getEnv("BOARDING_HMAC_SECRET", "dev-only-insecure-boarding-secret-change-me"),
		BoardingPassTTL:    getEnvSeconds("BOARDING_PASS_TTL_SECONDS", 180),
		FuelWithholdPct:    getEnvInt("FUEL_WITHHOLD_PCT", 30),
		// R22.00/litre — a plausible dev-only default, not a live price feed.
		FuelPricePerLitreCents: int64(getEnvInt("FUEL_PRICE_PER_LITRE_CENTS", 2200)),
		CatalogueFare: CatalogueFareModel{
			BaseCents:    int64(getEnvInt("CATALOGUE_FARE_BASE_CENTS", 500)),
			PerKmCents:   int64(getEnvInt("CATALOGUE_FARE_PER_KM_CENTS", 150)),
			MinFareCents: int64(getEnvInt("CATALOGUE_FARE_MIN_CENTS", 600)),
			MaxFareCents: int64(getEnvInt("CATALOGUE_FARE_MAX_CENTS", 6000)),
		},
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvSeconds(key string, fallbackSeconds int) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return time.Duration(fallbackSeconds) * time.Second
}
