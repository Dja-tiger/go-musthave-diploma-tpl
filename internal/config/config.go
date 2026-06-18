// Package config parses runtime settings for the Gophermart service.
package config

import (
	"flag"
	"os"
)

// Config stores runtime settings.
type Config struct {
	RunAddress           string
	DatabaseURI          string
	AccrualSystemAddress string
}

// Load reads configuration from command-line flags and environment variables.
// Environment variables take precedence over flags, as required by the
// praktikum test suite conventions.
func Load() Config {
	var cfg Config
	flag.StringVar(&cfg.RunAddress, "a", "localhost:8080", "HTTP server address")
	flag.StringVar(&cfg.DatabaseURI, "d", "", "PostgreSQL connection URI")
	flag.StringVar(&cfg.AccrualSystemAddress, "r", "", "accrual system address")
	flag.Parse()

	if value := os.Getenv("RUN_ADDRESS"); value != "" {
		cfg.RunAddress = value
	}
	if value := os.Getenv("DATABASE_URI"); value != "" {
		cfg.DatabaseURI = value
	}
	if value := os.Getenv("ACCRUAL_SYSTEM_ADDRESS"); value != "" {
		cfg.AccrualSystemAddress = value
	}

	return cfg
}
