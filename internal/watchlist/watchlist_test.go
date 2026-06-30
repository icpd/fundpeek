package watchlist

import "testing"

func TestStoreAddDeduplicatesAndUpdatesName(t *testing.T) {
	store := NewStore(t.TempDir() + "/watchlist.json")

	items, err := store.Add(Item{Code: "600519", Name: "贵州茅台", Market: "SH"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Market != "sh" {
		t.Fatalf("items after first add = %#v, want normalized single item", items)
	}
	items, err = store.Add(Item{Code: "600519", Name: "茅台", Market: "sh"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "茅台" {
		t.Fatalf("items after duplicate add = %#v, want name update", items)
	}
}

func TestStoreRemoveByMarketCode(t *testing.T) {
	store := NewStore(t.TempDir() + "/watchlist.json")
	if _, err := store.Add(Item{Code: "600519", Name: "贵州茅台", Market: "sh"}); err != nil {
		t.Fatal(err)
	}

	items, removed, err := store.Remove("sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if !removed || len(items) != 0 {
		t.Fatalf("remove = removed %v items %#v, want removed empty", removed, items)
	}
}
