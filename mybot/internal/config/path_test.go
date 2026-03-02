package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMessageDBPathUsesConfigDir(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	cfg := New()
	cfg.Storage.MessageDBPath = filepath.Join("data", "messages.db")

	got, err := ResolveMessageDBPath(configPath, cfg)
	if err != nil {
		t.Fatalf("ResolveMessageDBPath() error = %v", err)
	}

	want := filepath.Join(tempDir, "data", "messages.db")
	if got != want {
		t.Fatalf("ResolveMessageDBPath() = %q, want %q", got, want)
	}
}

func TestResolveMessageDBPathFallsBackToExecutableDir(t *testing.T) {
	cfg := New()
	cfg.Storage.MessageDBPath = filepath.Join("data", "messages.db")

	got, err := ResolveMessageDBPath(filepath.Join(t.TempDir(), "missing-config.json"), cfg)
	if err != nil {
		t.Fatalf("ResolveMessageDBPath() error = %v", err)
	}

	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	want := filepath.Join(filepath.Dir(exePath), "data", "messages.db")
	if got != want {
		t.Fatalf("ResolveMessageDBPath() = %q, want %q", got, want)
	}
}
