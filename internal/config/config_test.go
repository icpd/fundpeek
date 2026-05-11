package config

import (
	"os"
	"testing"
)

func TestLoadPersistsDeviceID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FUNDPEEK_CONFIG_DIR", dir)
	t.Setenv("FUNDPEEK_DEVICE_ID", "")

	first, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if first.DeviceID == "" {
		t.Fatal("device id is empty")
	}
	if first.DeviceID != second.DeviceID {
		t.Fatalf("device id was not persisted: %q != %q", first.DeviceID, second.DeviceID)
	}
	if _, err := os.Stat(first.CredentialPath); !os.IsNotExist(err) {
		t.Fatalf("credential file should not be created by config load, stat err: %v", err)
	}
}

func TestLoadCreatesCacheDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FUNDPEEK_CONFIG_DIR", dir)
	t.Setenv("FUNDPEEK_DEVICE_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.CacheDir != dir+"/cache" {
		t.Fatalf("CacheDir = %q, want %q", cfg.CacheDir, dir+"/cache")
	}
	info, err := os.Stat(cfg.CacheDir)
	if err != nil {
		t.Fatalf("cache dir was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("cache path is not a directory: %s", cfg.CacheDir)
	}
}
