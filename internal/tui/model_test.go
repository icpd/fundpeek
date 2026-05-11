package tui

import (
	"math"
	"strings"
	"testing"

	"github.com/icpd/fundpeek/internal/valuation"
)

func TestBuildPositionsAggregatesImportedGroupHoldingsOnly(t *testing.T) {
	data := map[string]any{
		"funds": []any{
			map[string]any{"code": "000001", "name": "华夏成长"},
			map[string]any{"code": "000002", "name": "手动基金"},
		},
		"groupHoldings": map[string]any{
			"manual": map[string]any{
				"000002": map[string]any{"share": 99},
			},
			"import_yangjibao_a": map[string]any{
				"000001": map[string]any{"share": 10},
			},
			"import_xiaobei_b": map[string]any{
				"000001": map[string]any{"share": 2.5},
			},
		},
	}

	got := BuildPositions(data)
	if len(got) != 1 {
		t.Fatalf("len(BuildPositions) = %d, want 1: %#v", len(got), got)
	}
	if got[0].Code != "000001" || got[0].Name != "华夏成长" {
		t.Fatalf("unexpected position identity: %#v", got[0])
	}
	if math.Abs(got[0].Share-12.5) > 0.000001 {
		t.Fatalf("share = %f, want 12.5", got[0].Share)
	}
}

func TestTodayProfitUsesValuationFirst(t *testing.T) {
	got, ok := TodayProfit(Position{Code: "000001", Share: 100}, valuation.Quote{
		GSZ:        1.02,
		HasGSZ:     true,
		GSZZL:      2,
		HasGSZZL:   true,
		DWJZ:       1,
		HasDWJZ:    true,
		LastNAV:    0.99,
		HasLastNAV: true,
	})
	if !ok {
		t.Fatal("expected profit")
	}
	want := 100*1.02 - (100*1.02)/(1+0.02)
	if math.Abs(got-want) > 0.000001 {
		t.Fatalf("profit = %f, want %f", got, want)
	}
}

func TestTodayProfitFallsBackToLatestNetValue(t *testing.T) {
	got, ok := TodayProfit(Position{Code: "000001", Share: 100}, valuation.Quote{
		DWJZ:       1.05,
		HasDWJZ:    true,
		LastNAV:    1.00,
		HasLastNAV: true,
	})
	if !ok {
		t.Fatal("expected profit")
	}
	if math.Abs(got-5) > 0.000001 {
		t.Fatalf("profit = %f, want 5", got)
	}
}

func TestSortRowsByEstimatedChangeDescendingWithMissingValuesLast(t *testing.T) {
	rows := []Row{
		{Position: Position{Code: "000004"}, Quote: valuation.Quote{GSZZL: 2.10, HasGSZZL: true}},
		{Position: Position{Code: "000001"}, Quote: valuation.Quote{GSZZL: -0.50, HasGSZZL: true}},
		{Position: Position{Code: "000003"}},
		{Position: Position{Code: "000002"}, Quote: valuation.Quote{GSZZL: 2.10, HasGSZZL: true}},
		{Position: Position{Code: "000005"}},
	}

	sortRows(rows)

	got := []string{rows[0].Code, rows[1].Code, rows[2].Code, rows[3].Code, rows[4].Code}
	want := []string{"000002", "000004", "000001", "000003", "000005"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted codes = %#v, want %#v", got, want)
		}
	}
}

func TestSummarizeRowsTotalsProfitAndWeightedEstimatedChange(t *testing.T) {
	rows := []Row{
		{
			Position:    Position{Code: "000001", Share: 100},
			Quote:       valuation.Quote{GSZ: 1.02, HasGSZ: true, GSZZL: 2, HasGSZZL: true},
			TodayProfit: 2,
			HasProfit:   true,
		},
		{
			Position:    Position{Code: "000002", Share: 200},
			Quote:       valuation.Quote{GSZ: 0.99, HasGSZ: true, GSZZL: -1, HasGSZZL: true},
			TodayProfit: -2,
			HasProfit:   true,
		},
		{
			Position:    Position{Code: "000003", Share: 10},
			TodayProfit: 5,
			HasProfit:   true,
		},
	}

	got := summarizeRows(rows)

	if !got.HasProfit {
		t.Fatal("expected total profit")
	}
	if math.Abs(got.TodayProfit-5) > 0.000001 {
		t.Fatalf("total profit = %f, want 5", got.TodayProfit)
	}
	if !got.HasEstimatedChange {
		t.Fatal("expected estimated change")
	}
	wantEstimatedChange := (2.0 - 2.0) / (100.0 + 200.0) * 100.0
	if math.Abs(got.EstimatedChange-wantEstimatedChange) > 0.000001 {
		t.Fatalf("estimated change = %f, want %f", got.EstimatedChange, wantEstimatedChange)
	}
}

func TestRenderTableSummaryDoesNotShowLatestChangePlaceholder(t *testing.T) {
	out := renderTable([]Row{
		{
			Position:    Position{Code: "000001", Name: "测试基金", Share: 100},
			Quote:       valuation.Quote{GSZ: 1.02, HasGSZ: true, GSZZL: 2, HasGSZZL: true, ZZL: 1, HasZZL: true},
			TodayProfit: 2,
			HasProfit:   true,
		},
	})

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	summaryLine := lines[len(lines)-1]
	if strings.Contains(summaryLine, "--") {
		t.Fatalf("summary line should not show latest-change placeholder: %q", summaryLine)
	}
}
