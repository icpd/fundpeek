package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileCache struct {
	dir string
	now func() time.Time
}

type envelope struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Value     json.RawMessage `json:"value"`
}

type Entry struct {
	FetchedAt time.Time
}

func NewFileCache(dir string, now func() time.Time) *FileCache {
	if now == nil {
		now = time.Now
	}
	return &FileCache{dir: dir, now: now}
}

func (c *FileCache) Now() time.Time {
	return c.now()
}

func (c *FileCache) GetOrFetch(key string, ttl time.Duration, out any, fetch func() (any, error)) error {
	if out == nil {
		return fmt.Errorf("cache output is nil")
	}
	if ttl > 0 {
		if ok, err := c.GetFresh(key, ttl, out); err != nil {
			return err
		} else if ok {
			return nil
		}
	}
	value, err := fetch()
	if err != nil {
		return err
	}
	if err := c.Set(key, value); err != nil {
		return err
	}
	return assign(out, value)
}

func (c *FileCache) GetFresh(key string, ttl time.Duration, out any) (bool, error) {
	if out == nil {
		return false, fmt.Errorf("cache output is nil")
	}
	entry, err := c.read(key)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if ttl <= 0 || c.now().Sub(entry.FetchedAt) > ttl {
		return false, nil
	}
	if err := json.Unmarshal(entry.Value, out); err != nil {
		return false, err
	}
	return true, nil
}

func (c *FileCache) Get(key string, out any) (Entry, bool, error) {
	if out == nil {
		return Entry{}, false, fmt.Errorf("cache output is nil")
	}
	entry, err := c.read(key)
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, err
	}
	if err := json.Unmarshal(entry.Value, out); err != nil {
		return Entry{}, false, err
	}
	return Entry{FetchedAt: entry.FetchedAt}, true, nil
}

func (c *FileCache) GetFreshOrFetch(key string, fresh func(Entry) bool, out any, fetch func() (any, error)) error {
	entry, ok, err := c.Get(key, out)
	if err != nil {
		return err
	}
	if ok && fresh(entry) {
		return nil
	}
	value, err := fetch()
	if err != nil {
		return err
	}
	if err := c.Set(key, value); err != nil {
		return err
	}
	return assign(out, value)
}

func (c *FileCache) Set(key string, value any) error {
	path, err := c.path(key)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(struct {
		FetchedAt time.Time `json:"fetched_at"`
		Value     any       `json:"value"`
	}{
		FetchedAt: c.now().UTC(),
		Value:     value,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (c *FileCache) Invalidate(key string) error {
	path, err := c.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (c *FileCache) read(key string) (envelope, error) {
	path, err := c.path(key)
	if err != nil {
		return envelope{}, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return envelope{}, err
	}
	var entry envelope
	if err := json.Unmarshal(body, &entry); err != nil {
		return envelope{}, err
	}
	return entry, nil
}

func (c *FileCache) path(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("cache key is required")
	}
	clean := filepath.Clean(key)
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("invalid cache key %q", key)
	}
	return filepath.Join(c.dir, clean+".json"), nil
}

func assign(out any, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}
