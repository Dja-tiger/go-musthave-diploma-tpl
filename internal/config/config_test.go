package config

import (
	"flag"
	"os"
	"testing"
)

func TestLoadPrefersEnvironment(t *testing.T) {
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	defer func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
	}()

	os.Args = []string{
		"gophermart",
		"-a", "flag-address",
		"-d", "flag-db",
		"-r", "flag-accrual",
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	t.Setenv("RUN_ADDRESS", "env-address")
	t.Setenv("DATABASE_URI", "env-db")
	t.Setenv("ACCRUAL_SYSTEM_ADDRESS", "env-accrual")

	cfg := Load()

	if cfg.RunAddress != "env-address" {
		t.Fatalf("RunAddress = %q", cfg.RunAddress)
	}
	if cfg.DatabaseURI != "env-db" {
		t.Fatalf("DatabaseURI = %q", cfg.DatabaseURI)
	}
	if cfg.AccrualSystemAddress != "env-accrual" {
		t.Fatalf("AccrualSystemAddress = %q", cfg.AccrualSystemAddress)
	}
}
