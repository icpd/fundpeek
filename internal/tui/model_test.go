package tui

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	fundapp "github.com/icpd/fundpeek/internal/app"
	fundcache "github.com/icpd/fundpeek/internal/cache"
	"github.com/icpd/fundpeek/internal/config"
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

func TestRenderTableSummaryAlignsWithFundNames(t *testing.T) {
	out := renderTable([]Row{
		{Position: Position{Code: "000001", Name: "华夏成长"}},
	})

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	summaryLine := lines[len(lines)-1]
	if !strings.HasPrefix(summaryLine, "  汇总") {
		t.Fatalf("summary line should align with fund names: %q", summaryLine)
	}
}

func TestRenderTableHeaderAlignsWithRows(t *testing.T) {
	out := renderTableWithCursor([]Row{
		{Position: Position{Code: "000001", Name: "华夏成长"}},
	}, 0, 98)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 5 {
		t.Fatalf("rendered table should include header, separators, row, and summary:\n%s", out)
	}

	headerStart := strings.Index(lines[0], "基金名称/代码")
	rowStart := strings.Index(lines[2], "华夏成长")
	summaryStart := strings.Index(lines[4], "汇总")
	if headerStart != rowStart || rowStart != summaryStart {
		t.Fatalf("column starts header=%d row=%d summary=%d:\n%s", headerStart, rowStart, summaryStart, out)
	}

	wantWidth := 2 + 58 + 12 + 14 + 12
	if got := lipgloss.Width(lines[1]); got != wantWidth {
		t.Fatalf("top separator width = %d, want %d: %q", got, wantWidth, lines[1])
	}
	if got := lipgloss.Width(lines[3]); got != wantWidth {
		t.Fatalf("summary separator width = %d, want %d: %q", got, wantWidth, lines[3])
	}
}

func TestRenderTableTruncatesLongFundNameButKeepsCode(t *testing.T) {
	longName := "中欧时代先锋股票型发起式证券投资基金超长名称测试"
	out := renderTable([]Row{
		{Position: Position{Code: "001938", Name: longName}},
	})

	if !strings.Contains(out, "#001938") {
		t.Fatalf("rendered table should keep fund code:\n%s", out)
	}
	if strings.Contains(out, longName) {
		t.Fatalf("rendered table should not contain full long fund name:\n%s", out)
	}
	if !strings.Contains(out, "... #001938") {
		t.Fatalf("rendered table should truncate long fund name before code:\n%s", out)
	}
}

func TestRenderTableUsesDefaultFundNameWidthWithoutWindowWidth(t *testing.T) {
	longName := "中欧时代先锋股票型发起式证券投资基金超长名称测试"
	out := renderTableWithCursor([]Row{
		{Position: Position{Code: "001938", Name: longName}},
	}, -1, 0)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	wantWidth := 2 + 34 + 12 + 14 + 12
	if got := lipgloss.Width(lines[1]); got != wantWidth {
		t.Fatalf("separator width = %d, want %d: %q", got, wantWidth, lines[1])
	}
	if strings.Contains(out, longName) {
		t.Fatalf("rendered table should keep default truncation without window width:\n%s", out)
	}
	if !strings.Contains(out, "... #001938") {
		t.Fatalf("rendered table should truncate long fund name before code:\n%s", out)
	}
}

func TestRenderTableKeepsDefaultFundNameWidthAtCurrentTableWidth(t *testing.T) {
	longName := "中欧时代先锋股票型发起式证券投资基金超长名称测试"
	out := renderTableWithCursor([]Row{
		{Position: Position{Code: "001938", Name: longName}},
	}, -1, 74)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	wantWidth := 2 + 34 + 12 + 14 + 12
	if got := lipgloss.Width(lines[1]); got != wantWidth {
		t.Fatalf("separator width = %d, want %d: %q", got, wantWidth, lines[1])
	}
	if strings.Contains(out, longName) {
		t.Fatalf("rendered table should keep current truncation at width 74:\n%s", out)
	}
	if !strings.Contains(out, "... #001938") {
		t.Fatalf("rendered table should truncate long fund name before code:\n%s", out)
	}
}

func TestRenderTableExpandsFundNameWidthAtWideWindow(t *testing.T) {
	longName := "中欧时代先锋股票型发起式证券投资基金超长名称测试"
	out := renderTableWithCursor([]Row{
		{Position: Position{Code: "001938", Name: longName}},
	}, -1, 98)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	wantWidth := 2 + 58 + 12 + 14 + 12
	if got := lipgloss.Width(lines[1]); got != wantWidth {
		t.Fatalf("separator width = %d, want %d: %q", got, wantWidth, lines[1])
	}
	if !strings.Contains(out, longName+" #001938") {
		t.Fatalf("rendered table should keep long fund name at width 98:\n%s", out)
	}
}

func TestRenderTableKeepsShortFundNameWithCode(t *testing.T) {
	out := renderTable([]Row{
		{Position: Position{Code: "000001", Name: "华夏成长"}},
	})

	if !strings.Contains(out, "华夏成长 #000001") {
		t.Fatalf("rendered table should keep short fund label:\n%s", out)
	}
}

func TestListLoadingViewUsesConciseCopy(t *testing.T) {
	out := model{loading: true}.View()

	if !strings.Contains(out, "正在加载基金持仓和实时估值...") {
		t.Fatalf("loading view missing concise copy:\n%s", out)
	}
	if strings.Contains(out, "正在获取数据") {
		t.Fatalf("loading view should not use verbose read copy:\n%s", out)
	}
}

func TestStatusBarKeepsHelpStartStableAcrossStates(t *testing.T) {
	lastRefresh := time.Date(2026, 5, 12, 15, 4, 5, 0, time.Local)
	help := "↑/↓ select  → detail  r refresh"
	loading := renderStatusBar(true, false, lastRefresh, help, "⠋")
	idle := renderStatusBar(false, false, lastRefresh, help, "⠋")
	err := renderStatusBar(false, true, lastRefresh, help, "⠋")

	helpStart := lipgloss.Width(strings.Split(loading, help)[0])
	if got := lipgloss.Width(strings.Split(idle, help)[0]); got != helpStart {
		t.Fatalf("idle help starts at width %d, want %d:\nloading %q\nidle %q", got, helpStart, loading, idle)
	}
	if got := lipgloss.Width(strings.Split(err, help)[0]); got != helpStart {
		t.Fatalf("error help starts at width %d, want %d:\nloading %q\nerror %q", got, helpStart, loading, err)
	}
	if !strings.Contains(idle, "✓ updated  15:04:05") {
		t.Fatalf("idle status should show aligned updated time:\n%s", idle)
	}
	if !strings.Contains(err, "! updated  15:04:05") {
		t.Fatalf("error status should keep stale-data updated wording:\n%s", err)
	}
}

func TestStatusBarUsesPlaceholderTimeWithoutRefresh(t *testing.T) {
	out := renderStatusBar(true, false, time.Time{}, "r refresh", "⠋")

	if !strings.Contains(out, "⠋ updating --:--:--") {
		t.Fatalf("loading status should show placeholder time:\n%s", out)
	}
}

func TestListStatusBarShowsSpinnerRefreshState(t *testing.T) {
	lastRefresh := time.Date(2026, 5, 12, 15, 4, 5, 0, time.Local)
	loading := model{loading: true, lastRefresh: lastRefresh}.View()
	idle := model{lastRefresh: lastRefresh}.View()

	if strings.Contains(loading, "updating quotes...") {
		t.Fatalf("loading status should not use old copy:\n%s", loading)
	}
	if strings.Contains(idle, "ready") {
		t.Fatalf("idle status should not use old ready copy:\n%s", idle)
	}
	if !strings.Contains(idle, "✓ updated  15:04:05") {
		t.Fatalf("idle status should show updated time:\n%s", idle)
	}

	loadingLine := strings.Split(loading, "\n")[1]
	idleLine := strings.Split(idle, "\n")[1]
	help := "↑/↓ select"
	if strings.Index(loadingLine, help) != strings.Index(idleLine, help) {
		t.Fatalf("help start should stay stable:\nloading %q\nidle    %q", loadingLine, idleLine)
	}
}

func TestListViewKeepsRefreshHelpCompact(t *testing.T) {
	out := model{}.View()

	if !strings.Contains(out, "Enter detail") {
		t.Fatalf("list help should show enter detail:\n%s", out)
	}
	if !strings.Contains(out, "r refresh") {
		t.Fatalf("list help should show refresh:\n%s", out)
	}
	if strings.Contains(out, "q quit") {
		t.Fatalf("list help should not show quit:\n%s", out)
	}
	if strings.Contains(out, "R force reload") {
		t.Fatalf("list help should not show separate force reload:\n%s", out)
	}
}

func TestDetailLoadingViewUsesCacheAwareCopy(t *testing.T) {
	out := renderDetail(detailState{
		Fund:    Position{Code: "000001", Name: "华夏成长"},
		Loading: true,
	})

	if !strings.Contains(out, "正在加载持仓明细和实时行情...") {
		t.Fatalf("detail loading view missing cache-aware copy:\n%s", out)
	}
	if strings.Contains(out, "正在加载股票持仓") {
		t.Fatalf("detail loading view should not imply stock holdings are always fetched live:\n%s", out)
	}
}

func TestDetailStatusBarUsesUpdatedStateBeforeReportDate(t *testing.T) {
	lastRefresh := time.Date(2026, 5, 12, 15, 4, 5, 0, time.Local)
	loading := renderDetail(detailState{
		Fund:        Position{Code: "000001", Name: "华夏成长"},
		Loading:     true,
		LastRefresh: lastRefresh,
		Data:        DetailData{ReportDate: "2026-03-31"},
	})
	idle := renderDetail(detailState{
		Fund:        Position{Code: "000001", Name: "华夏成长"},
		LastRefresh: lastRefresh,
		Data:        DetailData{ReportDate: "2026-03-31"},
	})

	if strings.Contains(loading, "updating quotes...") {
		t.Fatalf("detail loading status should not use old copy:\n%s", loading)
	}
	if strings.Contains(idle, "ready") {
		t.Fatalf("detail idle status should not use old ready copy:\n%s", idle)
	}
	if !strings.Contains(idle, "✓ updated  15:04:05  report 2026-03-31") {
		t.Fatalf("detail status should put report date after updated time:\n%s", idle)
	}

	loadingLine := strings.Split(loading, "\n")[1]
	idleLine := strings.Split(idle, "\n")[1]
	help := "Esc back"
	if strings.Index(loadingLine, help) != strings.Index(idleLine, help) {
		t.Fatalf("detail help start should stay stable:\nloading %q\nidle    %q", loadingLine, idleLine)
	}
}

func TestDetailViewKeepsRefreshHelpCompact(t *testing.T) {
	out := renderDetail(detailState{Fund: Position{Code: "000001", Name: "华夏成长"}})

	if !strings.Contains(out, "Esc back") {
		t.Fatalf("detail help should show esc back:\n%s", out)
	}
	if !strings.Contains(out, "r refresh") {
		t.Fatalf("detail help should show refresh:\n%s", out)
	}
	if strings.Contains(out, "q quit") {
		t.Fatalf("detail help should not show quit:\n%s", out)
	}
}

func TestManualListRefreshKeepsRealDataCache(t *testing.T) {
	dir := t.TempDir()
	store := fundcache.NewFileCache(dir, func() time.Time { return time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC) })
	if err := store.Set("real_data", map[string]any{"fund": "stale"}); err != nil {
		t.Fatal(err)
	}
	m := model{
		app:  fundapp.New(config.Config{CacheDir: dir}, nil),
		rows: []Row{{Position: Position{Code: "000001"}}},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = updated.(model)

	var got map[string]any
	ok, err := store.GetFresh("real_data", time.Hour, &got)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("manual refresh should keep real data cache")
	}
}

func TestManualDetailRefreshKeepsFundHoldingsCache(t *testing.T) {
	dir := t.TempDir()
	store := fundcache.NewFileCache(dir, func() time.Time { return time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC) })
	if err := store.Set("fund_holdings/000001", map[string]any{"report": "stale"}); err != nil {
		t.Fatal(err)
	}
	m := model{
		app:  fundapp.New(config.Config{CacheDir: dir}, nil),
		page: pageDetail,
		detail: detailState{
			Fund: Position{Code: "000001"},
		},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = updated.(model)

	var got map[string]any
	ok, err := store.GetFresh("fund_holdings/000001", time.Hour, &got)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("manual detail refresh should keep fund holdings cache")
	}
}

func TestForceListRefreshKeepsPortfolioDataCache(t *testing.T) {
	dir := t.TempDir()
	store := fundcache.NewFileCache(dir, func() time.Time { return time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC) })
	if err := store.Set("portfolio_data", map[string]any{"fund": "stale"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("fund_quote/000001", valuation.Quote{Code: "000001", GSZZL: 1, HasGSZZL: true}); err != nil {
		t.Fatal(err)
	}
	m := model{
		app:  fundapp.New(config.Config{CacheDir: dir}, nil),
		rows: []Row{{Position: Position{Code: "000001"}}},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	_ = updated.(model)

	var got map[string]any
	ok, err := store.GetFresh("portfolio_data", time.Hour, &got)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("force refresh should keep portfolio data cache")
	}
	var quote valuation.Quote
	ok, err = store.GetFresh("fund_quote/000001", time.Hour, &quote)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("force refresh should invalidate fund quote cache: %#v", quote)
	}
}

func TestForceDetailRefreshInvalidatesFundHoldingsCache(t *testing.T) {
	dir := t.TempDir()
	store := fundcache.NewFileCache(dir, func() time.Time { return time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC) })
	if err := store.Set("fund_holdings/000001", map[string]any{"report": "stale"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("stock_quote/s_sh600519", valuation.StockQuote{Code: "s_sh600519", Price: 1800, HasPrice: true}); err != nil {
		t.Fatal(err)
	}
	m := model{
		app:  fundapp.New(config.Config{CacheDir: dir}, nil),
		page: pageDetail,
		detail: detailState{
			Fund: Position{Code: "000001"},
			Data: DetailData{
				Rows: []StockHoldingRow{{Holding: valuation.StockHolding{Code: "600519"}}},
			},
		},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	_ = updated.(model)

	var got map[string]any
	ok, err := store.GetFresh("fund_holdings/000001", time.Hour, &got)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("force detail refresh should invalidate fund holdings cache: %#v", got)
	}
	var quote valuation.StockQuote
	ok, err = store.GetFresh("stock_quote/s_sh600519", time.Hour, &quote)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("force detail refresh should invalidate stock quote cache: %#v", quote)
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

func TestLoadedRowsSortsAfterQuoteRefreshAndKeepsSelection(t *testing.T) {
	m := model{
		cursor:       1,
		selectedCode: "000001",
		rows: []Row{
			{Position: Position{Code: "000001"}},
			{Position: Position{Code: "000002"}},
		},
		loading: true,
	}

	updated, _ := m.Update(fundQuotesLoadedMsg{
		quotes: map[string]valuation.Quote{
			"000001": {Code: "000001", GSZZL: 3, HasGSZZL: true},
			"000002": {Code: "000002", GSZZL: 1, HasGSZZL: true},
		},
	})
	m = updated.(model)

	if m.loading {
		t.Fatal("quote refresh should mark list loading false")
	}
	if got := []string{m.rows[0].Code, m.rows[1].Code}; got[0] != "000001" || got[1] != "000002" {
		t.Fatalf("rows after quote refresh = %#v, want selected fund sorted first", got)
	}
	if m.cursor != 0 || m.selectedCode != "000001" {
		t.Fatalf("selection after quote refresh = cursor %d code %q, want 0/000001", m.cursor, m.selectedCode)
	}
}

func TestQuoteRefreshPreservesPortfolioWarning(t *testing.T) {
	m := model{
		rows:    []Row{{Position: Position{Code: "000001"}}},
		errText: "xiaobei unavailable",
		loading: true,
	}

	updated, _ := m.Update(fundQuotesLoadedMsg{
		quotes: map[string]valuation.Quote{
			"000001": {Code: "000001", GSZZL: 3, HasGSZZL: true},
		},
	})
	m = updated.(model)

	if m.errText != "xiaobei unavailable" {
		t.Fatalf("errText after quote refresh = %q, want portfolio warning", m.errText)
	}
}

func TestLoadRowsSnapshotUsesCachedQuoteWithoutWaitingForRefresh(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	store := fundcache.NewFileCache(dir, func() time.Time { return now })
	if err := store.Set("portfolio_data", testRealData()); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("fund_quote/000001", valuation.Quote{Code: "000001", GSZZL: 2.5, HasGSZZL: true, GSZ: 1.025, HasGSZ: true}); err != nil {
		t.Fatal(err)
	}

	rows, err := LoadRowsSnapshot(context.Background(), fundapp.New(config.Config{CacheDir: dir}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2: %#v", len(rows), rows)
	}
	if rows[0].Code != "000001" || !rows[0].Quote.HasGSZZL || rows[0].Quote.GSZZL != 2.5 {
		t.Fatalf("cached quote row = %#v, want 000001 with cached GSZZL", rows[0])
	}
	if rows[1].Code != "000002" || rows[1].Quote.HasGSZZL {
		t.Fatalf("missing quote row = %#v, want 000002 without quote", rows[1])
	}
}

func TestRefreshFundQuotesStoresCache(t *testing.T) {
	dir := t.TempDir()
	oldFundQuoteFetcher := newFundQuoteFetcher
	t.Cleanup(func() { newFundQuoteFetcher = oldFundQuoteFetcher })
	newFundQuoteFetcher = func() fundQuoteFetcher {
		return fundQuoteFetcherFunc(func(_ context.Context, code string) (valuation.Quote, error) {
			return valuation.Quote{Code: code, GSZZL: 4.2, HasGSZZL: true}, nil
		})
	}

	rows, errs := RefreshFundQuotes(context.Background(), fundapp.New(config.Config{CacheDir: dir}, nil), []Position{{Code: "000001", Share: 100}})

	if len(errs) != 0 {
		t.Fatalf("errs = %#v, want none", errs)
	}
	if len(rows) != 1 || rows[0].Code != "000001" || rows[0].Quote.GSZZL != 4.2 {
		t.Fatalf("rows = %#v, want fetched quote", rows)
	}
	var cached valuation.Quote
	store := fundcache.NewFileCache(dir, func() time.Time { return time.Now() })
	ok, err := store.GetFresh("fund_quote/000001", time.Hour, &cached)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || cached.GSZZL != 4.2 {
		t.Fatalf("cached quote = %#v ok=%v, want refreshed quote", cached, ok)
	}
}

func TestLoadDetailSnapshotUsesStaleHoldingsAndStockQuoteCache(t *testing.T) {
	dir := t.TempDir()
	store := fundcache.NewFileCache(dir, func() time.Time { return time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC) })
	if err := store.Set("fund_holdings/000001", valuation.FundStockHoldings{
		ReportDate: "2026-03-31",
		IsRecent:   true,
		Holdings:   []valuation.StockHolding{{Code: "600519", Name: "贵州茅台", Weight: 9.87, HasWeight: true}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("stock_quote/s_sh600519", valuation.StockQuote{Code: "s_sh600519", Price: 1800, HasPrice: true, ChangePercent: 1.5, HasChangePercent: true}); err != nil {
		t.Fatal(err)
	}

	data, ok, err := LoadDetailSnapshot(fundapp.New(config.Config{CacheDir: dir}, nil), Position{Code: "000001"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected stale detail snapshot")
	}
	if len(data.Rows) != 1 || data.Rows[0].Holding.Code != "600519" || data.Rows[0].Quote.Price != 1800 {
		t.Fatalf("detail snapshot = %#v, want cached holdings and quote", data)
	}
	if data.PartialQuoteErr || data.Rows[0].QuoteErr {
		t.Fatalf("detail snapshot should not mark complete cached quote as partial failure: %#v", data)
	}
}

func TestRefreshDetailStoresStockQuoteCache(t *testing.T) {
	dir := t.TempDir()
	oldStockQuoteFetcher := newStockQuoteFetcher
	t.Cleanup(func() { newStockQuoteFetcher = oldStockQuoteFetcher })
	newStockQuoteFetcher = func() stockQuoteFetcher {
		return stockQuoteFetcherFunc(func(_ context.Context, codes []string) (map[string]valuation.StockQuote, error) {
			return map[string]valuation.StockQuote{
				"s_sh600519": {Code: "s_sh600519", Price: 1810, HasPrice: true},
			}, nil
		})
	}
	store := fundcache.NewFileCache(dir, func() time.Time { return time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC) })
	if err := store.Set("fund_holdings/000001", valuation.FundStockHoldings{
		ReportDate: "2026-03-31",
		IsRecent:   true,
		Holdings:   []valuation.StockHolding{{Code: "600519", Name: "贵州茅台"}},
	}); err != nil {
		t.Fatal(err)
	}

	data, err := RefreshDetail(context.Background(), fundapp.New(config.Config{CacheDir: dir}, nil), Position{Code: "000001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 || data.Rows[0].Quote.Price != 1810 {
		t.Fatalf("refreshed detail = %#v, want fetched stock quote", data)
	}
	var cached valuation.StockQuote
	ok, err := store.GetFresh("stock_quote/s_sh600519", time.Hour, &cached)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || cached.Price != 1810 {
		t.Fatalf("cached stock quote = %#v ok=%v, want refreshed quote", cached, ok)
	}
}

func TestArrowKeysSwitchBetweenListAndDetail(t *testing.T) {
	m := model{rows: []Row{{Position: Position{Code: "000001", Name: "华夏成长"}}}}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(model)
	if m.page != pageDetail {
		t.Fatalf("page after right = %v, want detail", m.page)
	}
	if m.detail.Fund.Code != "000001" {
		t.Fatalf("detail fund = %#v, want code 000001", m.detail.Fund)
	}
	if cmd == nil {
		t.Fatal("expected detail load command")
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(model)
	if m.page != pageList {
		t.Fatalf("page after left = %v, want list", m.page)
	}
	if cmd != nil {
		t.Fatalf("left should not create command: %#v", cmd)
	}
}

func TestLeftOnListQuits(t *testing.T) {
	m := model{rows: []Row{{Position: Position{Code: "000001", Name: "华夏成长"}}}}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(model)
	if m.page != pageList {
		t.Fatalf("page after left on list = %v, want list", m.page)
	}
	if cmd == nil {
		t.Fatal("left on list should quit")
	}
}

func TestEnterOnListOpensDetail(t *testing.T) {
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
}

func TestEscOnDetailReturnsToList(t *testing.T) {
	m := model{
		page:   pageDetail,
		detail: detailState{Fund: Position{Code: "000001", Name: "华夏成长"}},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.page != pageList {
		t.Fatalf("page after esc = %v, want list", m.page)
	}
	if cmd != nil {
		t.Fatalf("esc should not create command: %#v", cmd)
	}
}

func TestEscOnListQuits(t *testing.T) {
	m := model{rows: []Row{{Position: Position{Code: "000001", Name: "华夏成长"}}}}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.page != pageList {
		t.Fatalf("page after esc on list = %v, want list", m.page)
	}
	if cmd == nil {
		t.Fatal("esc on list should quit")
	}
}

func TestQQuitsFromListAndDetail(t *testing.T) {
	for _, tt := range []struct {
		name string
		page page
	}{
		{name: "list", page: pageList},
		{name: "detail", page: pageDetail},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				page:   tt.page,
				rows:   []Row{{Position: Position{Code: "000001", Name: "华夏成长"}}},
				detail: detailState{Fund: Position{Code: "000001", Name: "华夏成长"}},
			}

			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
			m = updated.(model)
			if m.page != tt.page {
				t.Fatalf("page after q = %v, want %v", m.page, tt.page)
			}
			if cmd == nil {
				t.Fatal("q should quit")
			}
		})
	}
}

func TestCtrlCQuits(t *testing.T) {
	m := model{rows: []Row{{Position: Position{Code: "000001", Name: "华夏成长"}}}}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(model)
	if m.page != pageList {
		t.Fatalf("page after ctrl+c = %v, want list", m.page)
	}
	if cmd == nil {
		t.Fatal("ctrl+c should quit")
	}
}

func testRealData() map[string]any {
	return map[string]any{
		"funds": []any{
			map[string]any{"code": "000001", "name": "华夏成长"},
			map[string]any{"code": "000002", "name": "易方达测试"},
		},
		"groupHoldings": map[string]any{
			"import_yangjibao_default": map[string]any{
				"000001": map[string]any{"share": 100},
				"000002": map[string]any{"share": 200},
			},
		},
	}
}

type fundQuoteFetcherFunc func(context.Context, string) (valuation.Quote, error)

func (f fundQuoteFetcherFunc) FetchQuote(ctx context.Context, code string) (valuation.Quote, error) {
	return f(ctx, code)
}

type stockQuoteFetcherFunc func(context.Context, []string) (map[string]valuation.StockQuote, error)

func (f stockQuoteFetcherFunc) FetchTencentStockQuotes(ctx context.Context, codes []string) (map[string]valuation.StockQuote, error) {
	return f(ctx, codes)
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
