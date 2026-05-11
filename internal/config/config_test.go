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
