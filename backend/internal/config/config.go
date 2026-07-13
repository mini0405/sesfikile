package config

import "os"

type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
}

func Load() Config {
	return Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://sesfikile:sesfikile_dev@localhost:5432/sesfikile?sslmode=disable"),
		// Dev-only default — override with a real secret via JWT_SECRET in any
		// shared or production environment.
		JWTSecret: getEnv("JWT_SECRET", "dev-only-insecure-secret-change-me"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
