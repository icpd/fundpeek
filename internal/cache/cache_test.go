package cache

import (
	"errors"
	"testing"
	"time"
)

func TestFileCacheReturnsFreshEntryWithoutFetching(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	store := NewFileCache(dir, func() time.Time { return now })
	want := map[string]any{"fund": "000001"}
	if err := store.Set("real_data", want); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	fetched := false
	err := store.GetOrFetch("real_data", 24*time.Hour, &got, func() (any, error) {
		fetched = true
		return map[string]any{"fund": "000002"}, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if fetched {
		t.Fatal("fetch should not be called for fresh cache")
	}
	if got["fund"] != "000001" {
		t.Fatalf("cached fund = %v, want 000001", got["fund"])
	}
}

func TestFileCacheFetchesExpiredEntryAndStoresResult(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	store := NewFileCache(dir, func() time.Time { return now })
	if err := store.Set("fund_holdings/000001", map[string]any{"report": "old"}); err != nil {
		t.Fatal(err)
	}

	now = now.Add(25 * time.Hour)
	var got map[string]any
	err := store.GetOrFetch("fund_holdings/000001", 24*time.Hour, &got, func() (any, error) {
		return map[string]any{"report": "new"}, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if got["report"] != "new" {
		t.Fatalf("report = %v, want new", got["report"])
	}

	now = now.Add(time.Hour)
	err = store.GetOrFetch("fund_holdings/000001", 24*time.Hour, &got, func() (any, error) {
		return nil, errors.New("should not fetch stored fresh value")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["report"] != "new" {
		t.Fatalf("stored report = %v, want new", got["report"])
	}
}

func TestFileCacheInvalidateRemovesEntry(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	store := NewFileCache(dir, func() time.Time { return now })
	if err := store.Set("real_data", map[string]any{"fund": "000001"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Invalidate("real_data"); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	err := store.GetOrFetch("real_data", time.Hour, &got, func() (any, error) {
		return map[string]any{"fund": "000002"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["fund"] != "000002" {
		t.Fatalf("fund = %v, want 000002", got["fund"])
	}
}
