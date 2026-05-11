package tui

import (
	"math"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestListLoadingViewUsesConciseCopy(t *testing.T) {
	out := model{loading: true}.View()

	if !strings.Contains(out, "正在获取数据...") {
		t.Fatalf("loading view missing concise copy:\n%s", out)
	}
	if strings.Contains(out, "正在读取") {
		t.Fatalf("loading view should not use verbose read copy:\n%s", out)
	}
}

func TestMoveCursorClampsToRows(t *testing.T) {
	m := model{rows: []Row{
		{Position: Position{Code: "000001"}},
		{Position: Position{Code: "000002"}},
	}}

	m.moveCursor(1)
	m.moveCursor(1)
	if m.cursor != 1 {
		t.Fatalf("cursor after moving down = %d, want 1", m.cursor)
	}
	m.moveCursor(-1)
	m.moveCursor(-1)
	if m.cursor != 0 {
		t.Fatalf("cursor after moving up = %d, want 0", m.cursor)
	}
}

func TestLoadedRowsKeepsSelectionByCodeAfterSort(t *testing.T) {
	m := model{
		cursor:       1,
		selectedCode: "000002",
		rows: []Row{
			{Position: Position{Code: "000001"}},
			{Position: Position{Code: "000002"}},
		},
	}

	next := []Row{
		{Position: Position{Code: "000002"}, Quote: valuation.Quote{GSZZL: 3, HasGSZZL: true}},
		{Position: Position{Code: "000001"}, Quote: valuation.Quote{GSZZL: 1, HasGSZZL: true}},
	}
	m.applyLoadedRows(next)

	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 for selected code after refresh", m.cursor)
	}
	if m.selectedCode != "000002" {
		t.Fatalf("selectedCode = %q, want 000002", m.selectedCode)
	}
}

func TestEnterAndEscSwitchBetweenListAndDetail(t *testing.T) {
	m := model{rows: []Row{{Position: Position{Code: "000001", Name: "华夏成长"}}}}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.page != pageDetail {
		t.Fatalf("page after enter = %v, want detail", m.page)
	}
	if m.detail.Fund.Code != "000001" {
		t.Fatalf("detail fund = %#v, want code 000001", m.detail.Fund)
	}
	if cmd == nil {
		t.Fatal("expected detail load command")
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.page != pageList {
		t.Fatalf("page after esc = %v, want list", m.page)
	}
	if cmd != nil {
		t.Fatalf("esc should not create command: %#v", cmd)
	}
}

func TestRenderDetailShowsHoldingsAndPartialQuoteFailure(t *testing.T) {
	out := renderDetail(detailState{
		Fund: Position{Code: "000001", Name: "华夏成长"},
		Data: DetailData{
			ReportDate: "2026-03-31",
			Rows: []StockHoldingRow{
				{
					Holding: valuation.StockHolding{Code: "600519", Name: "贵州茅台", Weight: 9.87, HasWeight: true, Shares: 12300, HasShares: true, MarketValue: 1820.5, HasMarketValue: true},
					Quote:   valuation.StockQuote{Name: "贵州茅台", ChangePercent: 1.23, HasChangePercent: true, Price: 1820.5, HasPrice: true},
				},
				{
					Holding:  valuation.StockHolding{Code: "00700.HK", Name: "腾讯控股", Weight: 8.01, HasWeight: true},
					QuoteErr: true,
				},
			},
			PartialQuoteErr: true,
		},
	})

	for _, want := range []string{"华夏成长 #000001", "2026-03-31", "贵州茅台 #600519", "+1.23%", "1820.50", "9.87%", "腾讯控股 #00700.HK", "行情不完整"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderDetail missing %q:\n%s", want, out)
		}
	}
}
