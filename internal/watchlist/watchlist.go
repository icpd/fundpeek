package watchlist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Item struct {
	Code   string `json:"code"`
	Name   string `json:"name,omitempty"`
	Market string `json:"market"`
}

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) List() ([]Item, error) {
	if strings.TrimSpace(s.path) == "" {
		return nil, nil
	}
	body, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("decode watchlist: %w", err)
	}
	return compact(items), nil
}

func (s *Store) Save(items []Item) error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	body, err := json.MarshalIndent(compact(items), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".watchlist-*.json")
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
	return os.Rename(tmpName, s.path)
}

func (s *Store) Add(item Item) ([]Item, error) {
	item = Normalize(item)
	if item.Code == "" || item.Market == "" {
		return nil, fmt.Errorf("watchlist item requires code and market")
	}
	items, err := s.List()
	if err != nil {
		return nil, err
	}
	for i := range items {
		if sameIdentity(items[i], item) {
			if item.Name != "" {
				items[i].Name = item.Name
			}
			if err := s.Save(items); err != nil {
				return nil, err
			}
			return items, nil
		}
	}
	items = append(items, item)
	if err := s.Save(items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) Remove(code string) ([]Item, bool, error) {
	code = strings.TrimSpace(code)
	items, err := s.List()
	if err != nil {
		return nil, false, err
	}
	out := items[:0]
	removed := false
	for _, item := range items {
		if item.Code == code || item.Market+item.Code == code {
			removed = true
			continue
		}
		out = append(out, item)
	}
	if !removed {
		return items, false, nil
	}
	if err := s.Save(out); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func Normalize(item Item) Item {
	return Item{
		Code:   strings.TrimSpace(item.Code),
		Name:   strings.TrimSpace(item.Name),
		Market: strings.ToLower(strings.TrimSpace(item.Market)),
	}
}

func compact(items []Item) []Item {
	out := make([]Item, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item = Normalize(item)
		if item.Code == "" || item.Market == "" {
			continue
		}
		key := item.Market + "/" + item.Code
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func sameIdentity(left, right Item) bool {
	left = Normalize(left)
	right = Normalize(right)
	return left.Code == right.Code && left.Market == right.Market
}
