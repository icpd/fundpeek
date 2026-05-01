package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Snapshot struct {
	UserID    string         `json:"user_id"`
	CreatedAt string         `json:"created_at"`
	Data      map[string]any `json:"data"`
}

func Save(dir, userID string, data map[string]any) (string, error) {
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}
	s := Snapshot{
		UserID:    userID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Data:      data,
	}
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("real-user-config-%s-%s.json", userID, time.Now().UTC().Format("20060102T150405.000000000Z"))
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.Write(body); err != nil {
		return "", err
	}
	if err := file.Chmod(0o600); err != nil {
		return "", err
	}
	return path, nil
}

func Load(path string) (Snapshot, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var s Snapshot
	if err := json.Unmarshal(body, &s); err != nil {
		return Snapshot{}, err
	}
	if s.UserID == "" {
		return Snapshot{}, fmt.Errorf("backup missing user_id")
	}
	if s.Data == nil {
		s.Data = map[string]any{}
	}
	return s, nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}
