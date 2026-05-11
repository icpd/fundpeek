package valuation

import (
	"context"
	"testing"
	"time"

	fundcache "github.com/icpd/fundpeek/internal/cache"
)

func TestCachedClientReturnsFreshFundStockHoldingsWithoutFetching(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	store := fundcache.NewFileCache(dir, func() time.Time { return now })
	seed := FundStockHoldings{
		ReportDate: "2026-03-31",
		IsRecent:   true,
		Holdings:   []StockHolding{{Code: "600519", Name: "贵州茅台", Weight: 9.87, HasWeight: true}},
	}
	if err := store.Set("fund_holdings/000001", seed); err != nil {
		t.Fatal(err)
	}
	client := NewCachedClient(store, ClientFuncs{
		FetchFundStockHoldingsFunc: func(context.Context, string) (FundStockHoldings, error) {
			t.Fatal("fetch should not be called for fresh cached holdings")
			return FundStockHoldings{}, nil
		},
	})

	got, err := client.FetchFundStockHoldings(context.Background(), "000001")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReportDate != seed.ReportDate || len(got.Holdings) != 1 || got.Holdings[0].Code != "600519" {
		t.Fatalf("unexpected cached holdings: %#v", got)
	}
}

func TestCachedClientFetchesExpiredFundStockHoldings(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	store := fundcache.NewFileCache(dir, func() time.Time { return now })
	if err := store.Set("fund_holdings/000001", FundStockHoldings{ReportDate: "2025-12-31"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(8 * 24 * time.Hour)
	client := NewCachedClient(store, ClientFuncs{
		FetchFundStockHoldingsFunc: func(_ context.Context, code string) (FundStockHoldings, error) {
			if code != "000001" {
				t.Fatalf("code = %q, want 000001", code)
			}
			return FundStockHoldings{ReportDate: "2026-03-31", IsRecent: true}, nil
		},
	})

	got, err := client.FetchFundStockHoldings(context.Background(), "000001")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReportDate != "2026-03-31" {
		t.Fatalf("ReportDate = %q, want 2026-03-31", got.ReportDate)
	}
}
