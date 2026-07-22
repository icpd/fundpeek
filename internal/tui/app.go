package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	fundapp "github.com/icpd/fundpeek/internal/app"
	"github.com/icpd/fundpeek/internal/valuation"
	"github.com/icpd/fundpeek/internal/watchlist"
)

const (
	refreshEvery      = 30 * time.Second
	watchRefreshEvery = 10 * time.Second
)

type model struct {
	ctx context.Context
	app *fundapp.App

	rows         []Row
	cursor       int
	selectedCode string
	loading      bool
	errText      string
	lastRefresh  time.Time
	width        int
	height       int
	spinner      spinner.Model

	page   page
	detail detailState

	listMode listMode
	watch    watchState
}

type loadedMsg struct {
	rows    []Row
	err     error
	warning string
}

type fundQuotesLoadedMsg struct {
	quotes map[string]valuation.Quote
	errs   map[string]error
}

type detailLoadedMsg struct {
	data DetailData
	err  error
}

type detailSnapshotMsg struct {
	data DetailData
	ok   bool
	err  error
}

type watchLoadedMsg struct {
	rows []WatchRow
	errs map[string]error
	err  error
}

type watchAddCandidatesMsg struct {
	items []watchlist.Item
	err   error
}

type watchAddedMsg struct {
	item watchlist.Item
	err  error
}

type watchRemovedMsg struct {
	code string
	err  error
}

type tickMsg time.Time
type watchTickMsg time.Time

type page int

const (
	pageList page = iota
	pageDetail
	pageWatchDetail
)

type listMode int

const (
	listFunds listMode = iota
	listWatch
)

type summary struct {
	EstimatedTodayProfit    float64
	HasEstimatedTodayProfit bool
	EstimatedChange         float64
	HasEstimatedChange      bool
	LatestChange            float64
	HasLatestChange         bool
}

type detailState struct {
	Fund        Position
	Data        DetailData
	Loading     bool
	ErrText     string
	LastRefresh time.Time
}

type DetailData struct {
	ReportDate        string
	IsRecent          bool
	Rows              []StockHoldingRow
	PartialQuoteErr   bool
	HoldingsAvailable bool
}

type StockHoldingRow struct {
	Holding  valuation.StockHolding
	Quote    valuation.StockQuote
	QuoteErr bool
}

type watchState struct {
	Rows            []WatchRow
	Cursor          int
	SelectedCode    string
	Loading         bool
	ErrText         string
	LastRefresh     time.Time
	Adding          bool
	Input           textinput.Model
	Candidates      []watchlist.Item
	CandidateCursor int
	Detail          *WatchRow
}

var (
	tuiForegroundColor = lipgloss.Color("244")
	tuiTextStyle       = lipgloss.NewStyle().Foreground(tuiForegroundColor)
	tuiTitleStyle      = tuiTextStyle.Bold(true)
	tuiHelpStyle       = tuiTextStyle
	tuiErrStyle        = tuiTextStyle
	tuiHeaderStyle     = tuiTextStyle.Bold(true)
)

func Run(ctx context.Context, a *fundapp.App) error {
	m := model{ctx: ctx, app: a, loading: true, spinner: newStatusSpinner(), watch: newWatchState()}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}

func (m model) Init() tea.Cmd {
	if m.watch.Input.Prompt == "" {
		m.watch = newWatchState()
	}
	return tea.Batch(m.load(), m.loadWatch(), tick(), watchTick(), m.spinnerTickCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.loading && !m.watch.Loading && (m.page != pageDetail || !m.detail.Loading) {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.statusSpinner().Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if m.watch.Adding && m.page == pageList && m.listMode == listWatch {
			return m.updateWatchAdd(msg)
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			if m.page == pageList {
				m.toggleListMode()
			}
		case "left", "backspace", "esc":
			if m.page == pageDetail {
				m.page = pageList
				return m, nil
			}
			if m.page == pageWatchDetail {
				m.page = pageList
				return m, nil
			}
			return m, tea.Quit
		case "up", "k":
			if m.page == pageList {
				if m.listMode == listWatch {
					m.moveWatchCursor(-1)
				} else {
					m.moveCursor(-1)
				}
			}
		case "down", "j":
			if m.page == pageList {
				if m.listMode == listWatch {
					m.moveWatchCursor(1)
				} else {
					m.moveCursor(1)
				}
			}
		case "right", "enter":
			if m.page == pageList && m.listMode == listFunds && len(m.rows) > 0 {
				m.openDetail(m.rows[m.cursor].Position)
				return m, tea.Batch(m.loadDetail(), m.spinnerTickCmd())
			}
			if m.page == pageList && m.listMode == listWatch && len(m.watch.Rows) > 0 {
				m.openWatchDetail(m.watch.Rows[m.watch.Cursor])
				return m, nil
			}
		case "a":
			if m.page == pageList && m.listMode == listWatch {
				m.startWatchAdd()
				return m, m.watch.Input.Focus()
			}
		case "d":
			if m.page == pageList && m.listMode == listWatch && len(m.watch.Rows) > 0 && !m.watch.Loading {
				row := m.watch.Rows[m.watch.Cursor]
				m.watch.Loading = true
				m.watch.ErrText = ""
				return m, tea.Batch(m.removeWatch(row.Item), m.spinnerTickCmd())
			}
		case "r":
			if m.page == pageDetail {
				if !m.detail.Loading {
					m.detail.Loading = true
					m.detail.ErrText = ""
					return m, tea.Batch(m.loadDetail(), m.spinnerTickCmd())
				}
				break
			}
			if m.listMode == listWatch {
				if !m.watch.Loading {
					m.watch.Loading = true
					m.watch.ErrText = ""
					return m, tea.Batch(m.loadWatch(), m.spinnerTickCmd())
				}
				break
			}
			if !m.loading {
				m.loading = true
				m.errText = ""
				return m, tea.Batch(m.load(), m.spinnerTickCmd())
			}
		case "R":
			if m.page == pageDetail {
				if !m.detail.Loading {
					m.app.InvalidateFundStockHoldings(m.detail.Fund.Code)
					for _, row := range m.detail.Data.Rows {
						tc := valuation.NormalizeTencentCode(row.Holding.Code)
						if tc != "" {
							m.app.InvalidateStockQuote(tc)
						}
					}
					m.detail.Loading = true
					m.detail.ErrText = ""
					return m, tea.Batch(m.loadDetail(), m.spinnerTickCmd())
				}
				break
			}
			if m.listMode == listWatch {
				if !m.watch.Loading {
					for _, row := range m.watch.Rows {
						m.app.InvalidateStockQuote(watchQuoteKey(row.Item))
						m.app.InvalidateStockMinute(watchMinuteKey(row.Item))
					}
					m.watch.Loading = true
					m.watch.ErrText = ""
					return m, tea.Batch(m.loadWatch(), m.spinnerTickCmd())
				}
				break
			}
			if !m.loading {
				for _, row := range m.rows {
					m.app.InvalidateFundQuote(row.Code)
				}
				m.loading = true
				m.errText = ""
				return m, tea.Batch(m.refreshPortfolio(), m.spinnerTickCmd())
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		if m.page == pageDetail {
			if !m.detail.Loading {
				m.detail.Loading = true
				m.detail.ErrText = ""
				return m, tea.Batch(tick(), m.loadDetail(), m.spinnerTickCmd())
			}
			return m, tick()
		}
		if m.page == pageWatchDetail || m.listMode == listWatch {
			return m, tick()
		}
		if !m.loading {
			m.loading = true
			m.errText = ""
			return m, tea.Batch(tick(), m.load(), m.spinnerTickCmd())
		}
		return m, tick()
	case watchTickMsg:
		if m.page == pageList && m.listMode == listWatch && !m.watch.Loading {
			m.watch.Loading = true
			m.watch.ErrText = ""
			return m, tea.Batch(watchTick(), m.loadWatch(), m.spinnerTickCmd())
		}
		return m, watchTick()
	case loadedMsg:
		if msg.err != nil {
			m.loading = false
			m.errText = msg.err.Error()
			break
		}
		m.errText = msg.warning
		m.applyLoadedRows(msg.rows)
		m.lastRefresh = time.Now()
		return m, m.refreshFundQuotes()
	case fundQuotesLoadedMsg:
		m.loading = false
		if quoteErr := firstErrText(msg.errs); quoteErr != "" {
			m.errText = joinStatusText(m.errText, quoteErr)
		}
		positions := make([]Position, 0, len(m.rows))
		for _, row := range m.rows {
			positions = append(positions, row.Position)
		}
		rows := BuildRows(positions, msg.quotes, msg.errs)
		sortRows(rows)
		m.applyLoadedRows(rows)
		m.lastRefresh = time.Now()
	case detailSnapshotMsg:
		if msg.err != nil {
			m.detail.Loading = false
			m.detail.ErrText = msg.err.Error()
			break
		}
		if msg.ok {
			m.detail.ErrText = ""
			m.detail.Data = msg.data
			m.detail.LastRefresh = time.Now()
		}
		return m, m.refreshDetail()
	case detailLoadedMsg:
		m.detail.Loading = false
		if msg.err != nil {
			m.detail.ErrText = msg.err.Error()
			break
		}
		m.detail.ErrText = ""
		m.detail.Data = msg.data
		m.detail.LastRefresh = time.Now()
	case watchLoadedMsg:
		m.watch.Loading = false
		if msg.err != nil {
			m.watch.ErrText = msg.err.Error()
			break
		}
		m.watch.ErrText = firstErrText(msg.errs)
		m.applyWatchRows(msg.rows)
		m.watch.LastRefresh = time.Now()
	case watchAddCandidatesMsg:
		m.watch.Loading = false
		if msg.err != nil {
			m.watch.ErrText = msg.err.Error()
			break
		}
		if len(msg.items) == 0 {
			m.watch.ErrText = "没有匹配的 A 股"
			break
		}
		if len(msg.items) == 1 {
			m.watch.Loading = true
			return m, tea.Batch(m.addWatch(msg.items[0]), m.spinnerTickCmd())
		}
		m.watch.Candidates = msg.items
		m.watch.CandidateCursor = 0
	case watchAddedMsg:
		m.watch.Loading = false
		if msg.err != nil {
			m.watch.ErrText = msg.err.Error()
			break
		}
		m.watch.Adding = false
		m.watch.Candidates = nil
		m.watch.Input.SetValue("")
		m.watch.SelectedCode = msg.item.Code
		m.watch.Loading = true
		return m, tea.Batch(m.loadWatch(), m.spinnerTickCmd())
	case watchRemovedMsg:
		m.watch.Loading = false
		if msg.err != nil {
			m.watch.ErrText = msg.err.Error()
			break
		}
		m.watch.SelectedCode = ""
		m.watch.Loading = true
		return m, tea.Batch(m.loadWatch(), m.spinnerTickCmd())
	}
	return m, nil
}

func (m model) View() string {
	if m.page == pageDetail {
		return renderDetailWithSpinner(m.detail, m.statusSpinner().View())
	}
	if m.page == pageWatchDetail && m.watch.Detail != nil {
		return renderWatchDetail(*m.watch.Detail, m.width)
	}
	if m.listMode == listWatch {
		return renderWatchWithSpinner(m.watch, m.width, m.statusSpinner().View())
	}
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("fundpeek tui"))
	b.WriteString("\n")

	b.WriteString(tuiHelpStyle.Render(renderStatusBar(
		m.loading,
		m.errText != "",
		m.lastRefresh,
		"Tab watch  ↑/↓ select  Enter detail  r refresh",
		m.statusSpinner().View(),
	)))
	b.WriteString("\n\n")

	if m.errText != "" {
		b.WriteString(tuiErrStyle.Render(m.errText))
		b.WriteString("\n\n")
	}
	if len(m.rows) == 0 {
		if m.loading {
			b.WriteString(tuiTextStyle.Render("正在加载基金持仓和实时估值..."))
			b.WriteString("\n")
		} else {
			b.WriteString(tuiTextStyle.Render("没有找到 fundpeek 导入分组下的基金持仓。"))
			b.WriteString("\n")
			b.WriteString(tuiHelpStyle.Render("先执行 fundpeek sync yjb / fundpeek sync xb / fundpeek sync all。"))
			b.WriteString("\n")
		}
		return b.String()
	}

	b.WriteString(renderTableWithCursor(m.rows, m.cursor, m.width))
	b.WriteString("\n")
	return b.String()
}

func (m model) load() tea.Cmd {
	return func() tea.Msg {
		rows, err := LoadRowsSnapshot(m.ctx, m.app)
		return loadedMsg{rows: rows, err: err}
	}
}

func (m model) refreshPortfolio() tea.Cmd {
	return func() tea.Msg {
		if err := m.app.RefreshPortfolio(m.ctx); err != nil {
			rows, loadErr := LoadRowsSnapshot(m.ctx, m.app)
			if loadErr != nil {
				return loadedMsg{err: fmt.Errorf("%v; %w", err, loadErr)}
			}
			return loadedMsg{rows: rows, warning: err.Error()}
		}
		rows, err := LoadRowsSnapshot(m.ctx, m.app)
		return loadedMsg{rows: rows, err: err}
	}
}

func (m model) refreshFundQuotes() tea.Cmd {
	positions := make([]Position, 0, len(m.rows))
	for _, row := range m.rows {
		positions = append(positions, row.Position)
	}
	return func() tea.Msg {
		rows, errs := RefreshFundQuotes(m.ctx, m.app, positions)
		quotes := make(map[string]valuation.Quote, len(rows))
		for _, row := range rows {
			quotes[row.Code] = row.Quote
		}
		return fundQuotesLoadedMsg{quotes: quotes, errs: errs}
	}
}

func (m model) loadDetail() tea.Cmd {
	fund := m.detail.Fund
	return func() tea.Msg {
		data, ok, err := LoadDetailSnapshot(m.app, fund)
		return detailSnapshotMsg{data: data, ok: ok, err: err}
	}
}

func (m model) refreshDetail() tea.Cmd {
	fund := m.detail.Fund
	return func() tea.Msg {
		data, err := RefreshDetail(m.ctx, m.app, fund)
		return detailLoadedMsg{data: data, err: err}
	}
}

func (m model) loadWatch() tea.Cmd {
	return func() tea.Msg {
		rows, errs, err := LoadWatchRows(m.ctx, m.app)
		return watchLoadedMsg{rows: rows, errs: errs, err: err}
	}
}

func (m model) searchWatch(query string) tea.Cmd {
	return func() tea.Msg {
		items, err := m.app.SearchWatchlistCandidates(m.ctx, query)
		return watchAddCandidatesMsg{items: items, err: err}
	}
}

func (m model) addWatch(item watchlist.Item) tea.Cmd {
	return func() tea.Msg {
		_, err := m.app.AddWatchlistItem(item)
		return watchAddedMsg{item: item, err: err}
	}
}

func (m model) removeWatch(item watchlist.Item) tea.Cmd {
	return func() tea.Msg {
		_, removed, err := m.app.RemoveWatchlistItem(item.Market + item.Code)
		if err == nil && !removed {
			err = fmt.Errorf("stock %s is not in watchlist", item.Code)
		}
		return watchRemovedMsg{code: item.Code, err: err}
	}
}

func (m *model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		m.cursor = 0
		m.selectedCode = ""
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor >= len(m.rows) {
		m.cursor = 0
	}
	m.selectedCode = m.rows[m.cursor].Code
}

func (m *model) applyLoadedRows(rows []Row) {
	m.rows = rows
	if len(rows) == 0 {
		m.cursor = 0
		m.selectedCode = ""
		return
	}
	code := m.selectedCode
	if code == "" && m.cursor >= 0 && m.cursor < len(m.rows) {
		code = m.rows[m.cursor].Code
	}
	m.cursor = 0
	for i, row := range rows {
		if row.Code == code {
			m.cursor = i
			break
		}
	}
	m.selectedCode = rows[m.cursor].Code
}

func (m *model) applyWatchRows(rows []WatchRow) {
	m.watch.Rows = rows
	if len(rows) == 0 {
		m.watch.Cursor = 0
		m.watch.SelectedCode = ""
		return
	}
	code := m.watch.SelectedCode
	if code == "" && m.watch.Cursor >= 0 && m.watch.Cursor < len(rows) {
		code = rows[m.watch.Cursor].Item.Code
	}
	m.watch.Cursor = 0
	for i, row := range rows {
		if row.Item.Code == code {
			m.watch.Cursor = i
			break
		}
	}
	m.watch.SelectedCode = rows[m.watch.Cursor].Item.Code
	if m.page == pageWatchDetail && m.watch.Detail != nil {
		for i := range rows {
			if rows[i].Item.Code == m.watch.Detail.Item.Code && rows[i].Item.Market == m.watch.Detail.Item.Market {
				m.watch.Detail = &rows[i]
				return
			}
		}
		m.page = pageList
		m.watch.Detail = nil
	}
}

func (m *model) moveWatchCursor(delta int) {
	if len(m.watch.Rows) == 0 {
		m.watch.Cursor = 0
		m.watch.SelectedCode = ""
		return
	}
	m.watch.Cursor += delta
	if m.watch.Cursor < 0 {
		m.watch.Cursor = len(m.watch.Rows) - 1
	}
	if m.watch.Cursor >= len(m.watch.Rows) {
		m.watch.Cursor = 0
	}
	m.watch.SelectedCode = m.watch.Rows[m.watch.Cursor].Item.Code
}

func (m *model) toggleListMode() {
	if m.listMode == listWatch {
		m.listMode = listFunds
		m.watch.Adding = false
		m.watch.Candidates = nil
		return
	}
	m.listMode = listWatch
}

func (m *model) startWatchAdd() {
	m.watch.Adding = true
	m.watch.ErrText = ""
	m.watch.Candidates = nil
	m.watch.CandidateCursor = 0
	m.watch.Input.SetValue("")
	m.watch.Input.Placeholder = "股票代码或名称"
	m.watch.Input.Focus()
}

func (m model) updateWatchAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.watch.Adding = false
		m.watch.Candidates = nil
		m.watch.Input.SetValue("")
		return m, nil
	case "up", "k":
		if len(m.watch.Candidates) > 0 {
			m.watch.CandidateCursor--
			if m.watch.CandidateCursor < 0 {
				m.watch.CandidateCursor = len(m.watch.Candidates) - 1
			}
		}
		return m, nil
	case "down", "j":
		if len(m.watch.Candidates) > 0 {
			m.watch.CandidateCursor++
			if m.watch.CandidateCursor >= len(m.watch.Candidates) {
				m.watch.CandidateCursor = 0
			}
		}
		return m, nil
	case "enter":
		if len(m.watch.Candidates) > 0 {
			item := m.watch.Candidates[m.watch.CandidateCursor]
			m.watch.Loading = true
			m.watch.ErrText = ""
			return m, tea.Batch(m.addWatch(item), m.spinnerTickCmd())
		}
		query := strings.TrimSpace(m.watch.Input.Value())
		if query == "" {
			m.watch.ErrText = "请输入股票代码或名称"
			return m, nil
		}
		m.watch.Loading = true
		m.watch.ErrText = ""
		return m, tea.Batch(m.searchWatch(query), m.spinnerTickCmd())
	}
	var cmd tea.Cmd
	m.watch.Input, cmd = m.watch.Input.Update(msg)
	return m, cmd
}

func (m *model) openDetail(fund Position) {
	m.page = pageDetail
	m.detail = detailState{Fund: fund, Loading: true}
}

func (m *model) openWatchDetail(row WatchRow) {
	m.page = pageWatchDetail
	m.watch.Detail = &row
}

func (m *model) ensureStatusSpinner() {
	if m.spinner.ID() == 0 {
		m.spinner = newStatusSpinner()
	}
}

func (m model) statusSpinner() spinner.Model {
	if m.spinner.ID() == 0 {
		return newStatusSpinner()
	}
	return m.spinner
}

func (m *model) spinnerTickCmd() tea.Cmd {
	m.ensureStatusSpinner()
	return m.spinner.Tick
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func watchTick() tea.Cmd {
	return tea.Tick(watchRefreshEvery, func(t time.Time) tea.Msg {
		return watchTickMsg(t)
	})
}

type fundQuoteFetcher interface {
	FetchQuote(context.Context, string) (valuation.Quote, error)
}

type stockQuoteFetcher interface {
	FetchTencentStockQuotes(context.Context, []string) (map[string]valuation.StockQuote, error)
}

type stockMinuteFetcher interface {
	FetchStockMinute(context.Context, string) (valuation.StockMinute, error)
}

var (
	newFundQuoteFetcher = func() fundQuoteFetcher {
		return valuation.NewClient()
	}
	newStockQuoteFetcher = func() stockQuoteFetcher {
		return valuation.NewClient()
	}
	newStockMinuteFetcher = func() stockMinuteFetcher {
		return valuation.NewClient()
	}
)

func LoadRowsSnapshot(ctx context.Context, a *fundapp.App) ([]Row, error) {
	data, err := a.PortfolioData(ctx)
	if err != nil {
		return nil, err
	}
	positions := BuildPositions(data)
	if len(positions) == 0 {
		return nil, nil
	}
	quotes := make(map[string]valuation.Quote, len(positions))
	errs := make(map[string]error, len(positions))
	for _, pos := range positions {
		quote, ok, err := a.CachedFundQuote(pos.Code)
		if err != nil {
			errs[pos.Code] = err
			continue
		}
		if ok {
			quotes[pos.Code] = quote
		}
	}
	rows := BuildRows(positions, quotes, errs)
	sortRows(rows)
	return rows, nil
}

func LoadRows(ctx context.Context, a *fundapp.App) ([]Row, error) {
	rows, err := LoadRowsSnapshot(ctx, a)
	if err != nil {
		return nil, err
	}
	positions := make([]Position, 0, len(rows))
	for _, row := range rows {
		positions = append(positions, row.Position)
	}
	refreshed, _ := RefreshFundQuotes(ctx, a, positions)
	return refreshed, nil
}

func RefreshFundQuotes(ctx context.Context, a *fundapp.App, positions []Position) ([]Row, map[string]error) {
	client := newFundQuoteFetcher()
	quotes := make(map[string]valuation.Quote, len(positions))
	errs := make(map[string]error, len(positions))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for _, pos := range positions {
		pos := pos
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				errs[pos.Code] = ctx.Err()
				mu.Unlock()
				return
			}
			q, err := client.FetchQuote(ctx, pos.Code)
			fetchedValues := fundQuoteHasValues(q)
			if !fetchedValues {
				cached, ok, cacheErr := a.CachedFundQuote(pos.Code)
				if cacheErr == nil && ok {
					q = cached
				} else if err == nil && cacheErr != nil {
					err = cacheErr
				}
			}
			mu.Lock()
			if err != nil {
				errs[pos.Code] = err
			}
			if q.Code != "" {
				quotes[pos.Code] = q
			}
			if fetchedValues {
				_ = a.SetFundQuote(pos.Code, q)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	rows := BuildRows(positions, quotes, errs)
	sortRows(rows)
	return rows, errs
}

func fundQuoteHasValues(quote valuation.Quote) bool {
	return quote.HasDWJZ || quote.HasGSZ || quote.HasGSZZL || quote.HasZZL || quote.HasLastNAV
}

func LoadDetailSnapshot(a *fundapp.App, fund Position) (DetailData, bool, error) {
	holdings, ok, err := a.CachedFundStockHoldings(fund.Code)
	if err != nil || !ok {
		return DetailData{}, ok, err
	}
	data := buildDetailData(holdings)
	data.PartialQuoteErr = false
	for i := range data.Rows {
		tc := valuation.NormalizeTencentCode(data.Rows[i].Holding.Code)
		if tc == "" {
			data.Rows[i].QuoteErr = true
			data.PartialQuoteErr = true
			continue
		}
		quote, ok, err := a.CachedStockQuote(tc)
		if err != nil {
			return DetailData{}, false, err
		}
		if ok {
			data.Rows[i].Quote = quote
			data.Rows[i].QuoteErr = false
		} else {
			data.Rows[i].QuoteErr = true
			data.PartialQuoteErr = true
		}
	}
	return data, true, nil
}

func LoadDetail(ctx context.Context, a *fundapp.App, fund Position) (DetailData, error) {
	return RefreshDetail(ctx, a, fund)
}

func RefreshDetail(ctx context.Context, a *fundapp.App, fund Position) (DetailData, error) {
	client := newStockQuoteFetcher()
	holdings, err := a.FundStockHoldings(ctx, fund.Code)
	if err != nil {
		return DetailData{}, err
	}
	data := DetailData{
		ReportDate:        holdings.ReportDate,
		IsRecent:          holdings.IsRecent,
		HoldingsAvailable: len(holdings.Holdings) > 0,
	}
	if len(holdings.Holdings) == 0 {
		return data, nil
	}
	codes := make([]string, 0, len(holdings.Holdings))
	for _, holding := range holdings.Holdings {
		codes = append(codes, holding.Code)
	}
	quotes, quoteErr := client.FetchTencentStockQuotes(ctx, codes)
	if quoteErr != nil {
		data.PartialQuoteErr = true
	}
	for _, holding := range holdings.Holdings {
		tc := valuation.NormalizeTencentCode(holding.Code)
		quote, ok := quotes[tc]
		row := StockHoldingRow{Holding: holding, Quote: quote}
		if quoteErr != nil || tc == "" || !ok || (!quote.HasChangePercent && !quote.HasPrice) {
			row.QuoteErr = true
			data.PartialQuoteErr = true
		} else {
			_ = a.SetStockQuote(tc, quote)
		}
		data.Rows = append(data.Rows, row)
	}
	return data, nil
}

func buildDetailData(holdings valuation.FundStockHoldings) DetailData {
	data := DetailData{
		ReportDate:        holdings.ReportDate,
		IsRecent:          holdings.IsRecent,
		HoldingsAvailable: len(holdings.Holdings) > 0,
	}
	for _, holding := range holdings.Holdings {
		data.Rows = append(data.Rows, StockHoldingRow{Holding: holding, QuoteErr: true})
	}
	if len(data.Rows) > 0 {
		data.PartialQuoteErr = true
	}
	return data
}

func firstErrText(errs map[string]error) string {
	for code, err := range errs {
		if err != nil {
			return fmt.Sprintf("%s: %v", code, err)
		}
	}
	return ""
}

func joinStatusText(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" || left == right {
		return left
	}
	return left + "; " + right
}

func sortRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if left.Quote.HasGSZZL != right.Quote.HasGSZZL {
			return left.Quote.HasGSZZL
		}
		if left.Quote.HasGSZZL && left.Quote.GSZZL != right.Quote.GSZZL {
			return left.Quote.GSZZL > right.Quote.GSZZL
		}
		return left.Code < right.Code
	})
}

func renderTable(rows []Row) string {
	return renderTableWithCursor(rows, -1, 0)
}

func renderTableWithCursor(rows []Row, cursor int, width int) string {
	return renderTableWithCursorAt(rows, cursor, width, time.Now())
}

func renderTableWithCursorAt(rows []Row, cursor int, width int, now time.Time) string {
	const (
		selectorWidth = 2
		estWidth      = 12
		profitWidth   = 14
		latestWidth   = 12
	)
	fundWidth := fundListNameWidth(width)
	tableWidth := selectorWidth + fundWidth + estWidth + profitWidth + latestWidth
	var b strings.Builder
	b.WriteString(tuiHeaderStyle.Render(
		strings.Repeat(" ", selectorWidth) +
			cell("基金名称/代码", fundWidth, lipgloss.Left) +
			cell("估值涨幅↓", estWidth, lipgloss.Right) +
			cell("最新涨幅", latestWidth, lipgloss.Right) +
			cell("估算收益", profitWidth, lipgloss.Right),
	))
	b.WriteString("\n")
	b.WriteString(tuiHelpStyle.Render(strings.Repeat("─", tableWidth)))
	b.WriteString("\n")
	for i, row := range rows {
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		var rowText strings.Builder
		rowText.WriteString(prefix)
		rowText.WriteString(cell(fundLabel(row, fundWidth), fundWidth, lipgloss.Left))
		rowText.WriteString(cell(formatPercent(row.Quote.GSZZL, row.Quote.HasGSZZL), estWidth, lipgloss.Right))
		rowText.WriteString(cell(formatLatestPercent(row.Quote, now), latestWidth, lipgloss.Right))
		rowText.WriteString(cell(formatMoney(row.EstimatedTodayProfit, row.HasEstimatedTodayProfit), profitWidth, lipgloss.Right))
		if row.QuoteErr != nil {
			rowText.WriteString(" !")
		}
		b.WriteString(tuiTextStyle.Render(rowText.String()))
		b.WriteString("\n")
	}
	total := summarizeRows(rows)
	b.WriteString(tuiHelpStyle.Render(strings.Repeat("─", tableWidth)))
	b.WriteString("\n")
	var totalText strings.Builder
	totalText.WriteString("  ")
	totalText.WriteString(cell("汇总", fundWidth, lipgloss.Left))
	totalText.WriteString(cell(formatPercent(total.EstimatedChange, total.HasEstimatedChange), estWidth, lipgloss.Right))
	totalText.WriteString(cell(formatPercent(total.LatestChange, total.HasLatestChange), latestWidth, lipgloss.Right))
	totalText.WriteString(cell(formatMoney(total.EstimatedTodayProfit, total.HasEstimatedTodayProfit), profitWidth, lipgloss.Right))
	b.WriteString(tuiTextStyle.Render(totalText.String()))
	b.WriteString("\n")
	return b.String()
}

func formatLatestPercent(quote valuation.Quote, now time.Time) string {
	if !quote.HasZZL {
		return formatPercent(quote.ZZL, quote.HasZZL)
	}
	text := formatPercent(quote.ZZL, quote.HasZZL)
	if quote.JZRQ == now.Format("2006-01-02") {
		return "✓ " + text
	}
	return text
}

func fundListNameWidth(windowWidth int) int {
	const (
		minFundWidth      = 34
		maxFundWidth      = 58
		defaultTableWidth = 74
	)
	if windowWidth <= defaultTableWidth {
		return minFundWidth
	}
	fundWidth := minFundWidth + windowWidth - defaultTableWidth
	if fundWidth > maxFundWidth {
		return maxFundWidth
	}
	return fundWidth
}

func renderDetail(state detailState) string {
	return renderDetailWithSpinner(state, newStatusSpinner().View())
}

func renderDetailWithSpinner(state detailState, spinnerView string) string {
	const (
		stockWidth  = 34
		chgWidth    = 12
		priceWidth  = 12
		weightWidth = 12
		sharesWidth = 14
		valueWidth  = 14
	)
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render(fundPositionLabel(state.Fund)))
	b.WriteString("\n")

	help := "Esc back  r refresh"
	if state.Data.ReportDate != "" {
		help = "report " + state.Data.ReportDate + "  " + help
	}
	b.WriteString(tuiHelpStyle.Render(renderStatusBar(
		state.Loading,
		state.ErrText != "",
		state.LastRefresh,
		help,
		spinnerView,
	)))
	b.WriteString("\n\n")

	if state.ErrText != "" {
		b.WriteString(tuiErrStyle.Render(state.ErrText))
		b.WriteString("\n\n")
	}
	if state.Data.PartialQuoteErr {
		b.WriteString(tuiErrStyle.Render("行情不完整，失败项显示 --"))
		b.WriteString("\n\n")
	}
	if len(state.Data.Rows) == 0 {
		if state.Loading {
			b.WriteString(tuiTextStyle.Render("正在加载持仓明细和实时行情..."))
			b.WriteString("\n")
		} else if state.Data.ReportDate != "" && !state.Data.IsRecent {
			b.WriteString(tuiTextStyle.Render("最新持仓报告期已超过 6 个月，未展示过期持仓。"))
			b.WriteString("\n")
		} else {
			b.WriteString(tuiTextStyle.Render("没有找到可展示的股票持仓。"))
			b.WriteString("\n")
		}
		return b.String()
	}

	b.WriteString(tuiHeaderStyle.Render(
		cell("股票名称/代码", stockWidth, lipgloss.Left) +
			cell("涨跌幅", chgWidth, lipgloss.Right) +
			cell("最新价", priceWidth, lipgloss.Right) +
			cell("占净值", weightWidth, lipgloss.Right) +
			cell("持股数", sharesWidth, lipgloss.Right) +
			cell("持仓市值", valueWidth, lipgloss.Right),
	))
	b.WriteString("\n")
	b.WriteString(tuiHelpStyle.Render(strings.Repeat("─", stockWidth+chgWidth+priceWidth+weightWidth+sharesWidth+valueWidth)))
	b.WriteString("\n")
	for _, row := range state.Data.Rows {
		var rowText strings.Builder
		rowText.WriteString(cell(stockLabel(row), stockWidth, lipgloss.Left))
		rowText.WriteString(cell(formatPercent(row.Quote.ChangePercent, row.Quote.HasChangePercent), chgWidth, lipgloss.Right))
		rowText.WriteString(cell(formatNumber(row.Quote.Price, row.Quote.HasPrice), priceWidth, lipgloss.Right))
		rowText.WriteString(cell(formatUnsignedPercent(row.Holding.Weight, row.Holding.HasWeight), weightWidth, lipgloss.Right))
		rowText.WriteString(cell(formatNumber(row.Holding.Shares, row.Holding.HasShares), sharesWidth, lipgloss.Right))
		rowText.WriteString(cell(formatNumber(row.Holding.MarketValue, row.Holding.HasMarketValue), valueWidth, lipgloss.Right))
		if row.QuoteErr {
			rowText.WriteString(" !")
		}
		b.WriteString(tuiTextStyle.Render(rowText.String()))
		b.WriteString("\n")
	}
	return b.String()
}

const statusLeftWidth = 19

func newStatusSpinner() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.MiniDot))
}

func renderStatusBar(loading bool, hasError bool, lastRefresh time.Time, help string, spinnerView string) string {
	timestamp := "--:--:--"
	if !lastRefresh.IsZero() {
		timestamp = lastRefresh.Format("15:04:05")
	}

	symbol := "✓"
	label := "updated "
	if loading {
		symbol = spinnerView
		if symbol == "" {
			symbol = spinner.MiniDot.Frames[0]
		}
		label = "updating"
	} else if hasError {
		symbol = "!"
	}

	left := fmt.Sprintf("%s %s %s", symbol, label, timestamp)
	left = padRight(left, statusLeftWidth)
	return left + "  " + help
}

func padRight(text string, width int) string {
	padding := width - lipgloss.Width(text)
	if padding <= 0 {
		return text
	}
	return text + strings.Repeat(" ", padding)
}

func summarizeRows(rows []Row) summary {
	var total summary
	var estimatedProfit float64
	var previousValue float64
	var latestProfit float64
	var latestPreviousValue float64
	for _, row := range rows {
		if row.HasEstimatedTodayProfit {
			total.EstimatedTodayProfit += row.EstimatedTodayProfit
			total.HasEstimatedTodayProfit = true
		}
		if row.Quote.HasGSZ && row.Quote.HasGSZZL && row.Quote.GSZZL > -100 {
			currentValue := row.Share * row.Quote.GSZ
			rowPreviousValue := currentValue / (1 + row.Quote.GSZZL/100)
			estimatedProfit += currentValue - rowPreviousValue
			previousValue += rowPreviousValue
		}
		if row.Quote.HasDWJZ && row.Quote.HasZZL && row.Quote.ZZL > -100 {
			currentValue := row.Share * row.Quote.DWJZ
			rowPreviousValue := currentValue / (1 + row.Quote.ZZL/100)
			latestProfit += currentValue - rowPreviousValue
			latestPreviousValue += rowPreviousValue
		}
	}
	if previousValue > 0 {
		total.EstimatedChange = estimatedProfit / previousValue * 100
		total.HasEstimatedChange = true
	}
	if latestPreviousValue > 0 {
		total.LatestChange = latestProfit / latestPreviousValue * 100
		total.HasLatestChange = true
	}
	return total
}

func cell(text string, width int, align lipgloss.Position) string {
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Align(align).Render(text)
}

func fundLabel(row Row, width int) string {
	name := row.Name
	if strings.TrimSpace(name) == "" {
		name = row.Quote.Name
	}
	if strings.TrimSpace(name) == "" {
		name = "未知基金"
	}
	suffix := fmt.Sprintf(" #%s", row.Code)
	nameWidth := width - lipgloss.Width(suffix)
	if nameWidth < 1 {
		return suffix
	}
	return fmt.Sprintf("%s%s", truncateDisplayWidth(name, nameWidth), suffix)
}

func fundPositionLabel(pos Position) string {
	name := strings.TrimSpace(pos.Name)
	if name == "" {
		name = "未知基金"
	}
	return fmt.Sprintf("%s #%s", name, pos.Code)
}

func stockLabel(row StockHoldingRow) string {
	name := strings.TrimSpace(row.Holding.Name)
	if name == "" {
		name = strings.TrimSpace(row.Quote.Name)
	}
	if name == "" {
		name = "未知股票"
	}
	return fmt.Sprintf("%s #%s", name, row.Holding.Code)
}

func truncateDisplayWidth(text string, width int) string {
	text = strings.TrimSpace(text)
	if width <= 0 || lipgloss.Width(text) <= width {
		return text
	}
	const marker = "..."
	markerWidth := lipgloss.Width(marker)
	if width <= markerWidth {
		return marker[:width]
	}
	limit := width - markerWidth
	var b strings.Builder
	for _, r := range text {
		next := b.String() + string(r)
		if lipgloss.Width(next) > limit {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + marker
}

func formatPercent(value float64, ok bool) string {
	if !ok {
		return "--"
	}
	return fmt.Sprintf("%+.2f%%", value)
}

func formatUnsignedPercent(value float64, ok bool) string {
	if !ok {
		return "--"
	}
	return fmt.Sprintf("%.2f%%", value)
}

func formatMoney(value float64, ok bool) string {
	if !ok {
		return "--"
	}
	return fmt.Sprintf("%+.2f", value)
}

func formatNumber(value float64, ok bool) string {
	if !ok {
		return "--"
	}
	return fmt.Sprintf("%.2f", value)
}
