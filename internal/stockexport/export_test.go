package stockexport

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	fundapp "github.com/icpd/fundpeek/internal/app"
	fundcache "github.com/icpd/fundpeek/internal/cache"
	"github.com/icpd/fundpeek/internal/config"
	"github.com/icpd/fundpeek/internal/valuation"
	"github.com/icpd/fundpeek/internal/watchlist"
)

func TestBuildQuoteDocumentIncludesStocksAndErrors(t *testing.T) {
	generatedAt := time.Date(2026, 7, 9, 10, 30, 0, 0, time.UTC)
	rows := []StockRow{
		{
			Item: watchlist.Item{Market: "sh", Code: "600519", Name: "贵州茅台"},
			Quote: valuation.StockQuote{
				Code:             "s_sh600519",
				Name:             "贵州茅台",
				Price:            1800.5,
				HasPrice:         true,
				ChangePercent:    1.23,
				HasChangePercent: true,
			},
		},
		{
			Item:     watchlist.Item{Market: "sz", Code: "000001", Name: "平安银行"},
			QuoteErr: errors.New("quote unavailable"),
		},
	}
	errs := map[string]error{"sz000001": errors.New("quote unavailable")}

	doc := BuildQuoteDocument(rows, errs, generatedAt)

	if doc.GeneratedAt != "2026-07-09T10:30:00Z" {
		t.Fatalf("GeneratedAt = %q, want RFC3339 UTC", doc.GeneratedAt)
	}
	if len(doc.Stocks) != 2 {
		t.Fatalf("len(stocks) = %d, want 2", len(doc.Stocks))
	}
	first := doc.Stocks[0]
	if first.Market != "sh" || first.Code != "600519" || first.TencentCode != "s_sh600519" || first.Name != "贵州茅台" {
		t.Fatalf("first stock identity = %#v", first)
	}
	if !first.QuoteAvailable {
		t.Fatalf("first stock quote should be available: %#v", first)
	}
	if !first.Price.Available || first.Price.Value != 1800.5 {
		t.Fatalf("first price = %#v, want 1800.5", first.Price)
	}
	if !first.ChangePercent.Available || math.Abs(first.ChangePercent.Value-1.23) > 0.000001 {
		t.Fatalf("first change percent = %#v, want 1.23", first.ChangePercent)
	}
	if doc.Stocks[1].QuoteAvailable {
		t.Fatalf("second stock quote should be unavailable: %#v", doc.Stocks[1])
	}
	if len(doc.Errors) != 1 || doc.Errors[0].Code != "sz000001" || doc.Errors[0].Scope != "quote" || doc.Errors[0].Message != "quote unavailable" {
		t.Fatalf("errors = %#v, want quote error for sz000001", doc.Errors)
	}

	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "\x1b[") {
		t.Fatalf("json should not include ANSI escape sequences: %q", body)
	}
	if strings.Contains(string(body), "\"Market\"") || strings.Contains(string(body), "\"Code\"") {
		t.Fatalf("json should use snake_case fields, got: %q", body)
	}
}

func TestBuildSearchDocumentIncludesResults(t *testing.T) {
	doc := BuildSearchDocument("茅台", []valuation.StockSearchResult{{
		Market: "sh",
		Code:   "600519",
		Name:   "贵州茅台",
	}}, nil, time.Date(2026, 7, 9, 10, 30, 0, 0, time.UTC))

	if doc.Query != "茅台" {
		t.Fatalf("query = %q, want 茅台", doc.Query)
	}
	if len(doc.Results) != 1 || doc.Results[0].Market != "sh" || doc.Results[0].Code != "600519" {
		t.Fatalf("results = %#v, want one stock result", doc.Results)
	}
	if len(doc.Errors) != 0 {
		t.Fatalf("errors = %#v, want none", doc.Errors)
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "\"market\"") || strings.Contains(string(body), "\"Market\"") {
		t.Fatalf("search JSON should use snake_case fields, got: %q", body)
	}
}

func TestBuildMinuteDocumentIncludesPoints(t *testing.T) {
	doc := BuildMinuteDocument([]StockRow{{
		Item: watchlist.Item{Market: "sh", Code: "600519", Name: "贵州茅台"},
		Minute: valuation.StockMinute{
			Market: "sh",
			Code:   "600519",
			Date:   "20260709",
			Points: []valuation.StockMinutePoint{{Time: "0930", Price: 1800, Volume: 10, Amount: 18000}},
		},
	}}, nil, time.Date(2026, 7, 9, 10, 30, 0, 0, time.UTC))

	if len(doc.Stocks) != 1 {
		t.Fatalf("len(stocks) = %d, want 1", len(doc.Stocks))
	}
	stock := doc.Stocks[0]
	if !stock.MinuteAvailable {
		t.Fatalf("minute should be available: %#v", stock)
	}
	if stock.Minute.Date != "20260709" || len(stock.Minute.Points) != 1 || stock.Minute.Points[0].Time != "0930" {
		t.Fatalf("minute = %#v, want one 0930 point", stock.Minute)
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "\"time\"") || strings.Contains(string(body), "\"Time\"") {
		t.Fatalf("minute JSON should use snake_case fields, got: %q", body)
	}
}

func TestWriteListUsesWatchlistAndCachesQuotes(t *testing.T) {
	oldFetchStockQuotes := fetchStockQuotes
	t.Cleanup(func() { fetchStockQuotes = oldFetchStockQuotes })
	fetchStockQuotes = func(_ context.Context, codes []string) (map[string]valuation.StockQuote, error) {
		if len(codes) != 1 || codes[0] != "s_sh600519" {
			t.Fatalf("codes = %#v, want s_sh600519", codes)
		}
		return map[string]valuation.StockQuote{
			"s_sh600519": {Code: "s_sh600519", Name: "贵州茅台", Price: 1800.5, HasPrice: true},
		}, nil
	}
	dir := t.TempDir()
	if _, err := watchlist.NewStore(dir + "/watchlist.json").Add(watchlist.Item{Market: "sh", Code: "600519", Name: "贵州茅台"}); err != nil {
		t.Fatal(err)
	}
	app := fundapp.New(config.Config{CacheDir: dir + "/cache", WatchlistPath: dir + "/watchlist.json"}, nil)
	var out strings.Builder

	if err := WriteList(context.Background(), app, &out); err != nil {
		t.Fatal(err)
	}

	var doc QuoteDocument
	if err := json.Unmarshal([]byte(out.String()), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Stocks) != 1 || doc.Stocks[0].TencentCode != "s_sh600519" || !doc.Stocks[0].QuoteAvailable {
		t.Fatalf("document = %#v, want quote for watchlist item", doc)
	}
	store := fundcache.NewFileCache(dir+"/cache", nil)
	var cached valuation.StockQuote
	ok, err := store.GetFresh("stock_quote/s_sh600519", time.Hour, &cached)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || cached.Price != 1800.5 {
		t.Fatalf("cached quote = %#v ok=%v, want refreshed quote", cached, ok)
	}
}

func TestWriteQuoteFetchesSingleStockAndCachesQuote(t *testing.T) {
	oldFetchStockQuotes := fetchStockQuotes
	t.Cleanup(func() { fetchStockQuotes = oldFetchStockQuotes })
	fetchStockQuotes = func(_ context.Context, codes []string) (map[string]valuation.StockQuote, error) {
		if len(codes) != 1 || codes[0] != "s_sh600519" {
			t.Fatalf("codes = %#v, want s_sh600519", codes)
		}
		return map[string]valuation.StockQuote{
			"s_sh600519": {Code: "s_sh600519", Name: "贵州茅台", ChangePercent: 1.23, HasChangePercent: true, Price: 1800.5, HasPrice: true},
		}, nil
	}
	dir := t.TempDir()
	app := fundapp.New(config.Config{CacheDir: dir + "/cache"}, nil)
	var out strings.Builder

	if err := WriteQuote(context.Background(), app, "600519", &out); err != nil {
		t.Fatal(err)
	}

	var doc QuoteDocument
	if err := json.Unmarshal([]byte(out.String()), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Stocks) != 1 || doc.Stocks[0].Market != "sh" || doc.Stocks[0].Code != "600519" || !doc.Stocks[0].QuoteAvailable {
		t.Fatalf("document = %#v, want one available stock quote", doc)
	}
}

func TestWriteSearchEncodesCandidates(t *testing.T) {
	oldSearchStocks := searchStocks
	t.Cleanup(func() { searchStocks = oldSearchStocks })
	searchStocks = func(_ context.Context, query string) ([]valuation.StockSearchResult, error) {
		if query != "茅台" {
			t.Fatalf("query = %q, want 茅台", query)
		}
		return []valuation.StockSearchResult{{Market: "sh", Code: "600519", Name: "贵州茅台"}}, nil
	}
	var out strings.Builder

	if err := WriteSearch(context.Background(), "茅台", &out); err != nil {
		t.Fatal(err)
	}

	var doc SearchDocument
	if err := json.Unmarshal([]byte(out.String()), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Results) != 1 || doc.Results[0].Code != "600519" {
		t.Fatalf("document = %#v, want search result", doc)
	}
}

func TestWriteMinuteFetchesSingleStockAndCachesMinute(t *testing.T) {
	oldFetchStockMinute := fetchStockMinute
	t.Cleanup(func() { fetchStockMinute = oldFetchStockMinute })
	fetchStockMinute = func(_ context.Context, code string) (valuation.StockMinute, error) {
		if code != "sh600519" {
			t.Fatalf("code = %q, want sh600519", code)
		}
		return valuation.StockMinute{
			Market: "sh",
			Code:   "600519",
			Date:   "20260709",
			Points: []valuation.StockMinutePoint{{Time: "0930", Price: 1800}},
		}, nil
	}
	dir := t.TempDir()
	app := fundapp.New(config.Config{CacheDir: dir + "/cache"}, nil)
	var out strings.Builder

	if err := WriteMinute(context.Background(), app, "600519", &out); err != nil {
		t.Fatal(err)
	}

	var doc MinuteDocument
	if err := json.Unmarshal([]byte(out.String()), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Stocks) != 1 || doc.Stocks[0].Market != "sh" || doc.Stocks[0].Code != "600519" || !doc.Stocks[0].MinuteAvailable {
		t.Fatalf("document = %#v, want one available stock minute", doc)
	}
	store := fundcache.NewFileCache(dir+"/cache", nil)
	var cached valuation.StockMinute
	ok, err := store.GetFresh("stock_minute/sh600519", time.Hour, &cached)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || cached.Date != "20260709" {
		t.Fatalf("cached minute = %#v ok=%v, want refreshed minute", cached, ok)
	}
}
