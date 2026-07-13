package config

import "os"

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
	Port        string
	DatabaseURL string
	JWTSecret   string
	FareSplit   FareSplit
}

func Load() Config {
	return Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://sesfikile:sesfikile_dev@localhost:5432/sesfikile?sslmode=disable"),
		// Dev-only default — override with a real secret via JWT_SECRET in any
		// shared or production environment.
		JWTSecret: getEnv("JWT_SECRET", "dev-only-insecure-secret-change-me"),
		FareSplit: FareSplit{PlatformPct: 10, DriverPct: 25, OwnerPct: 65},
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
