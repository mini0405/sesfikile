package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	os.Unsetenv("PORT")
	os.Unsetenv("DATABASE_URL")

	c := Load()

	if c.Port != "8080" {
		t.Errorf("expected default port 8080, got %s", c.Port)
	}
	if c.DatabaseURL == "" {
		t.Errorf("expected default database url, got empty")
	}
}

func TestLoad_Overrides(t *testing.T) {
	os.Setenv("PORT", "9090")
	os.Setenv("DATABASE_URL", "postgres://example")
	defer os.Unsetenv("PORT")
	defer os.Unsetenv("DATABASE_URL")

	c := Load()

	if c.Port != "9090" {
		t.Errorf("expected overridden port 9090, got %s", c.Port)
	}
	if c.DatabaseURL != "postgres://example" {
		t.Errorf("expected overridden database url, got %s", c.DatabaseURL)
	}
}
