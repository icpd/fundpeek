package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/icpd/fundpeek/internal/model"
)

const (
	defaultSupabaseURL     = "https://mouvsqlmgymsaxikvqsh.supabase.co"
	defaultSupabaseAnonKey = "sb_publishable_c5f58knbVz8UgOh6L88MUQ_p9j8c1Q-"
)

type Config struct {
	SupabaseURL    string
	SupabaseAnon   string
	DeviceID       string
	ConfigDir      string
	CacheDir       string
	CredentialPath string
}

func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	configDir := getenv("FUNDPEEK_CONFIG_DIR", filepath.Join(home, ".fundpeek"))
	if err := ensurePrivateDir(configDir); err != nil {
		return Config{}, err
	}
	deviceID, err := loadDeviceID(configDir)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		SupabaseURL:    getenv("FUNDPEEK_SUPABASE_URL", defaultSupabaseURL),
		SupabaseAnon:   getenv("FUNDPEEK_SUPABASE_ANON_KEY", defaultSupabaseAnonKey),
		DeviceID:       getenv("FUNDPEEK_DEVICE_ID", deviceID),
		ConfigDir:      configDir,
		CacheDir:       filepath.Join(configDir, "cache"),
		CredentialPath: filepath.Join(configDir, "credentials.json"),
	}
	if err := validateSupabaseURL(cfg.SupabaseURL); err != nil {
		return Config{}, err
	}
	if err := ensurePrivateDir(cfg.CacheDir); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadDeviceID(configDir string) (string, error) {
	path := filepath.Join(configDir, "device_id")
	body, err := os.ReadFile(path)
	if err == nil {
		if err := os.Chmod(path, 0o600); err != nil {
			return "", err
		}
		if id := strings.TrimSpace(string(body)); id != "" {
			return id, nil
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	id := model.NewDeviceID()
	return id, writePrivateFile(path, []byte(id+"\n"))
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func writePrivateFile(path string, body []byte) error {
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func validateSupabaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid FUNDPEEK_SUPABASE_URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("FUNDPEEK_SUPABASE_URL must use https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("FUNDPEEK_SUPABASE_URL must include host")
	}
	return nil
}
