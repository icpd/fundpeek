package valuation

import (
	"context"
	"testing"
	"time"

	fundcache "github.com/icpd/fundpeek/internal/cache"
)

func TestCachedClientKeepsFundStockHoldingsBeforeNextDisclosureWindow(t *testing.T) {
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

func TestCachedClientFetchesFundStockHoldingsDailyInDisclosureWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	store := fundcache.NewFileCache(dir, func() time.Time { return now })
	if err := store.Set("fund_holdings/000001", FundStockHoldings{ReportDate: "2026-03-31"}); err != nil {
		t.Fatal(err)
	}
	fetches := 0
	client := NewCachedClient(store, ClientFuncs{
		FetchFundStockHoldingsFunc: func(_ context.Context, code string) (FundStockHoldings, error) {
			fetches++
			if code != "000001" {
				t.Fatalf("code = %q, want 000001", code)
			}
			return FundStockHoldings{ReportDate: "2026-06-30", IsRecent: true}, nil
		},
	})

	got, err := client.FetchFundStockHoldings(context.Background(), "000001")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReportDate != "2026-03-31" {
		t.Fatalf("ReportDate = %q, want 2026-03-31", got.ReportDate)
	}
	if fetches != 0 {
		t.Fatalf("fetches before daily interval elapsed = %d, want 0", fetches)
	}

	now = time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	got, err = client.FetchFundStockHoldings(context.Background(), "000001")
	if err != nil {
		t.Fatal(err)
	}
	if fetches != 1 {
		t.Fatalf("fetches after daily interval elapsed = %d, want 1", fetches)
	}
	if got.ReportDate != "2026-06-30" {
		t.Fatalf("ReportDate = %q, want 2026-06-30", got.ReportDate)
	}
}

func TestCachedClientFetchesFundStockHoldingsWeeklyAfterMissedDisclosureWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	store := fundcache.NewFileCache(dir, func() time.Time { return now })
	if err := store.Set("fund_holdings/000001", FundStockHoldings{ReportDate: "2026-03-31"}); err != nil {
		t.Fatal(err)
	}
	client := NewCachedClient(store, ClientFuncs{
		FetchFundStockHoldingsFunc: func(context.Context, string) (FundStockHoldings, error) {
			return FundStockHoldings{ReportDate: "2026-06-30", IsRecent: true}, nil
		},
	})

	got, err := client.FetchFundStockHoldings(context.Background(), "000001")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReportDate != "2026-03-31" {
		t.Fatalf("ReportDate within weekly grace = %q, want 2026-03-31", got.ReportDate)
	}

	now = time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	got, err = client.FetchFundStockHoldings(context.Background(), "000001")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReportDate != "2026-06-30" {
		t.Fatalf("ReportDate after weekly interval = %q, want 2026-06-30", got.ReportDate)
	}
}

func TestCachedClientFetchesFundStockHoldingsDailyWhenReportDateMissing(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	store := fundcache.NewFileCache(dir, func() time.Time { return now })
	if err := store.Set("fund_holdings/000001", FundStockHoldings{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(25 * time.Hour)
	client := NewCachedClient(store, ClientFuncs{
		FetchFundStockHoldingsFunc: func(context.Context, string) (FundStockHoldings, error) {
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
