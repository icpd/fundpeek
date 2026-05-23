package jsonexport

import (
	"bytes"
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
	"github.com/icpd/fundpeek/internal/model"
	"github.com/icpd/fundpeek/internal/tui"
	"github.com/icpd/fundpeek/internal/valuation"
)

func TestBuildDocumentIncludesSummaryFundsAndErrors(t *testing.T) {
	generatedAt := time.Date(2026, 5, 19, 10, 30, 0, 0, time.UTC)
	rows := []tui.Row{
		{
			Position: tui.Position{
				Code:             "000001",
				Name:             "华夏成长",
				Share:            100,
				HoldingAmount:    101,
				HasHoldingAmount: true,
				CostAmount:       95,
				HasCostAmount:    true,
				CostNAV:          0.95,
				HasCostNAV:       true,
			},
			Quote: valuation.Quote{
				Code:     "000001",
				Name:     "华夏成长混合",
				GSZ:      1.02,
				HasGSZ:   true,
				DWJZ:     1.01,
				HasDWJZ:  true,
				GSZZL:    2,
				HasGSZZL: true,
				ZZL:      1.1,
				HasZZL:   true,
				JZRQ:     "2026-05-18",
				GZTime:   "2026-05-19 14:30",
			},
			TodayProfit: 2,
			HasProfit:   true,
		},
		{
			Position: tui.Position{Code: "000002", Name: "失败基金", Share: 50},
			QuoteErr: errors.New("quote unavailable"),
		},
	}
	errs := map[string]error{"000002": errors.New("quote unavailable")}

	doc := BuildDocument(rows, errs, generatedAt)

	if doc.GeneratedAt != "2026-05-19T10:30:00Z" {
		t.Fatalf("GeneratedAt = %q, want RFC3339 UTC", doc.GeneratedAt)
	}
	if doc.Summary.FundCount != 2 {
		t.Fatalf("fund count = %d, want 2", doc.Summary.FundCount)
	}
	if !doc.Summary.TodayProfitAmount.Available || doc.Summary.TodayProfitAmount.Value != 2 {
		t.Fatalf("today profit summary = %#v, want available value 2", doc.Summary.TodayProfitAmount)
	}
	if !doc.Summary.EstimatedChangePercent.Available || math.Abs(doc.Summary.EstimatedChangePercent.Value-2) > 0.000001 {
		t.Fatalf("estimated change summary = %#v, want available value 2", doc.Summary.EstimatedChangePercent)
	}
	if !doc.Summary.TotalHoldingAmount.Available || doc.Summary.TotalHoldingAmount.Value != 101 {
		t.Fatalf("holding amount summary = %#v, want available value 101", doc.Summary.TotalHoldingAmount)
	}
	if !doc.Summary.TotalCostAmount.Available || doc.Summary.TotalCostAmount.Value != 95 {
		t.Fatalf("cost amount summary = %#v, want available value 95", doc.Summary.TotalCostAmount)
	}
	if !doc.Summary.TotalEstimatedAmount.Available || doc.Summary.TotalEstimatedAmount.Value != 102 {
		t.Fatalf("estimated amount summary = %#v, want available value 102", doc.Summary.TotalEstimatedAmount)
	}
	if !doc.Summary.TotalLatestNAVAmount.Available || doc.Summary.TotalLatestNAVAmount.Value != 101 {
		t.Fatalf("latest nav amount summary = %#v, want available value 101", doc.Summary.TotalLatestNAVAmount)
	}
	if len(doc.Funds) != 2 {
		t.Fatalf("len(funds) = %d, want 2", len(doc.Funds))
	}
	if doc.Funds[0].Code != "000001" || doc.Funds[0].Name != "华夏成长" {
		t.Fatalf("first fund identity = %#v", doc.Funds[0])
	}
	if !doc.Funds[0].QuoteAvailable {
		t.Fatalf("first fund quote should be available: %#v", doc.Funds[0])
	}
	if !doc.Funds[0].EstimatedChangePercent.Available || doc.Funds[0].EstimatedChangePercent.Value != 2 {
		t.Fatalf("first fund estimated change = %#v, want 2", doc.Funds[0].EstimatedChangePercent)
	}
	if !doc.Funds[0].HoldingAmount.Available || doc.Funds[0].HoldingAmount.Value != 101 {
		t.Fatalf("first fund holding amount = %#v, want available 101", doc.Funds[0].HoldingAmount)
	}
	if !doc.Funds[0].CostAmount.Available || doc.Funds[0].CostAmount.Value != 95 {
		t.Fatalf("first fund cost amount = %#v, want available 95", doc.Funds[0].CostAmount)
	}
	if !doc.Funds[0].CostNAV.Available || doc.Funds[0].CostNAV.Value != 0.95 {
		t.Fatalf("first fund cost nav = %#v, want available 0.95", doc.Funds[0].CostNAV)
	}
	if !doc.Funds[0].EstimatedAmount.Available || doc.Funds[0].EstimatedAmount.Value != 102 {
		t.Fatalf("first fund estimated amount = %#v, want available 102", doc.Funds[0].EstimatedAmount)
	}
	if !doc.Funds[0].LatestNAVAmount.Available || doc.Funds[0].LatestNAVAmount.Value != 101 {
		t.Fatalf("first fund latest nav amount = %#v, want available 101", doc.Funds[0].LatestNAVAmount)
	}
	if doc.Funds[1].QuoteAvailable {
		t.Fatalf("second fund quote should be unavailable: %#v", doc.Funds[1])
	}
	if len(doc.Errors) != 1 || doc.Errors[0].Code != "000002" || doc.Errors[0].Message != "quote unavailable" {
		t.Fatalf("errors = %#v, want quote error for 000002", doc.Errors)
	}

	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "\x1b[") {
		t.Fatalf("json should not include ANSI escape sequences: %q", body)
	}
	if strings.Contains(string(body), "access_token") || strings.Contains(string(body), "account_id") {
		t.Fatalf("json should not include credentials or raw account fields: %q", body)
	}
}

func TestWriteReturnsPortfolioErrorWithoutLocalData(t *testing.T) {
	var out bytes.Buffer

	err := Write(context.Background(), fundapp.New(config.Config{CacheDir: t.TempDir()}, nil), &out)

	if err == nil || !strings.Contains(err.Error(), "no local portfolio data") {
		t.Fatalf("Write err = %v, want no local portfolio data", err)
	}
	if out.Len() != 0 {
		t.Fatalf("Write should not emit JSON on portfolio error: %q", out.String())
	}
}

func TestWriteRefreshesQuotesAndEncodesDocument(t *testing.T) {
	oldRefreshFundQuotes := refreshFundQuotes
	t.Cleanup(func() { refreshFundQuotes = oldRefreshFundQuotes })
	refreshFundQuotes = func(_ context.Context, _ *fundapp.App, positions []tui.Position) ([]tui.Row, map[string]error) {
		if len(positions) != 1 || positions[0].Code != "000001" || positions[0].Share != 100 {
			t.Fatalf("positions = %#v, want portfolio position", positions)
		}
		return []tui.Row{{
			Position: positions[0],
			Quote: valuation.Quote{
				Code:     "000001",
				GSZ:      1.02,
				HasGSZ:   true,
				GSZZL:    2,
				HasGSZZL: true,
			},
			TodayProfit: 2,
			HasProfit:   true,
		}}, nil
	}
	dir := t.TempDir()
	store := fundcache.NewFileCache(dir, func() time.Time { return time.Date(2026, 5, 19, 9, 0, 0, 0, time.UTC) })
	if err := store.Set("portfolio_data", map[string]any{
		"funds": []any{
			map[string]any{"code": "000001", "name": "测试基金"},
		},
		"groupHoldings": map[string]any{
			"import_yangjibao_default": map[string]any{
				"000001": map[string]any{"share": 100, "cost": 0.9},
			},
		},
		model.PortfolioHoldingDetailsKey: map[string]any{
			"import_yangjibao_default": map[string]any{
				"000001": map[string]any{"amount": 100.5},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	if err := Write(context.Background(), fundapp.New(config.Config{CacheDir: dir}, nil), &out); err != nil {
		t.Fatal(err)
	}

	var doc Document
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Funds) != 1 || doc.Funds[0].Code != "000001" || !doc.Funds[0].QuoteAvailable {
		t.Fatalf("document = %#v, want refreshed fund JSON", doc)
	}
}
