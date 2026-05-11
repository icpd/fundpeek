package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	fundapp "github.com/icpd/fundsync/internal/app"
	"github.com/icpd/fundsync/internal/valuation"
)

const refreshEvery = 30 * time.Second

type model struct {
	ctx context.Context
	app *fundapp.App

	rows        []Row
	loading     bool
	errText     string
	lastRefresh time.Time
	width       int
	height      int
}

type loadedMsg struct {
	rows []Row
	err  error
}

type tickMsg time.Time

type summary struct {
	TodayProfit        float64
	HasProfit          bool
	EstimatedChange    float64
	HasEstimatedChange bool
}

var (
	tuiTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("244"))
	tuiHelpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	tuiErrStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("95"))
	tuiHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("242"))
	tuiUpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	tuiDownStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)

func Run(ctx context.Context, a *fundapp.App) error {
	m := model{ctx: ctx, app: a, loading: true}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.load(), tick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading = true
				m.errText = ""
				return m, m.load()
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		if !m.loading {
			m.loading = true
			m.errText = ""
			return m, tea.Batch(tick(), m.load())
		}
		return m, tick()
	case loadedMsg:
		m.loading = false
		if msg.err != nil {
			m.errText = msg.err.Error()
			break
		}
		m.errText = ""
		m.rows = msg.rows
		m.lastRefresh = time.Now()
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("fundsync tui"))
	b.WriteString("\n")

	status := "ready"
	if m.loading {
		status = "refreshing..."
	}
	if !m.lastRefresh.IsZero() {
		status += "  updated " + m.lastRefresh.Format("15:04:05")
	}
	b.WriteString(tuiHelpStyle.Render(status + "  r refresh  q quit"))
	b.WriteString("\n\n")

	if m.errText != "" {
		b.WriteString(tuiErrStyle.Render(m.errText))
		b.WriteString("\n\n")
	}
	if len(m.rows) == 0 {
		if m.loading {
			b.WriteString("正在读取 real 同步数据并刷新估值...\n")
		} else {
			b.WriteString("没有找到 fundsync 导入分组下的基金持仓。\n")
			b.WriteString(tuiHelpStyle.Render("先执行 fundsync sync yjb / fundsync sync xb / fundsync sync all。"))
			b.WriteString("\n")
		}
		return b.String()
	}

	b.WriteString(renderTable(m.rows))
	b.WriteString("\n")
	return b.String()
}

func (m model) load() tea.Cmd {
	return func() tea.Msg {
		rows, err := LoadRows(m.ctx, m.app)
		return loadedMsg{rows: rows, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func LoadRows(ctx context.Context, a *fundapp.App) ([]Row, error) {
	data, err := a.RealData(ctx)
	if err != nil {
		return nil, err
	}
	positions := BuildPositions(data)
	if len(positions) == 0 {
		return nil, nil
	}

	client := valuation.NewClient()
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
			mu.Lock()
			if err != nil {
				errs[pos.Code] = err
			}
			if q.Code != "" {
				quotes[pos.Code] = q
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	rows := BuildRows(positions, quotes, errs)
	sortRows(rows)
	return rows, nil
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
	const (
		fundWidth   = 34
		estWidth    = 12
		profitWidth = 14
		latestWidth = 12
	)
	var b strings.Builder
	b.WriteString(tuiHeaderStyle.Render(
		cell("基金名称/代码", fundWidth, lipgloss.Left) +
			cell("估值涨幅↓", estWidth, lipgloss.Right) +
			cell("当日收益", profitWidth, lipgloss.Right) +
			cell("最新涨幅", latestWidth, lipgloss.Right),
	))
	b.WriteString("\n")
	b.WriteString(tuiHelpStyle.Render(strings.Repeat("─", fundWidth+estWidth+profitWidth+latestWidth)))
	b.WriteString("\n")
	for _, row := range rows {
		b.WriteString(cell(fundLabel(row), fundWidth, lipgloss.Left))
		b.WriteString(cell(formatPercent(row.Quote.GSZZL, row.Quote.HasGSZZL), estWidth, lipgloss.Right))
		b.WriteString(cell(formatMoney(row.TodayProfit, row.HasProfit), profitWidth, lipgloss.Right))
		b.WriteString(cell(formatPercent(row.Quote.ZZL, row.Quote.HasZZL), latestWidth, lipgloss.Right))
		if row.QuoteErr != nil {
			b.WriteString(" ")
			b.WriteString(tuiErrStyle.Render("!"))
		}
		b.WriteString("\n")
	}
	total := summarizeRows(rows)
	b.WriteString(tuiHelpStyle.Render(strings.Repeat("─", fundWidth+estWidth+profitWidth+latestWidth)))
	b.WriteString("\n")
	b.WriteString(cell("汇总", fundWidth, lipgloss.Left))
	b.WriteString(cell(formatPercent(total.EstimatedChange, total.HasEstimatedChange), estWidth, lipgloss.Right))
	b.WriteString(cell(formatMoney(total.TodayProfit, total.HasProfit), profitWidth, lipgloss.Right))
	b.WriteString(cell("", latestWidth, lipgloss.Right))
	b.WriteString("\n")
	return b.String()
}

func summarizeRows(rows []Row) summary {
	var total summary
	var estimatedProfit float64
	var previousValue float64
	for _, row := range rows {
		if row.HasProfit {
			total.TodayProfit += row.TodayProfit
			total.HasProfit = true
		}
		if !row.Quote.HasGSZ || !row.Quote.HasGSZZL || row.Quote.GSZZL <= -100 {
			continue
		}
		currentValue := row.Share * row.Quote.GSZ
		rowPreviousValue := currentValue / (1 + row.Quote.GSZZL/100)
		estimatedProfit += currentValue - rowPreviousValue
		previousValue += rowPreviousValue
	}
	if previousValue > 0 {
		total.EstimatedChange = estimatedProfit / previousValue * 100
		total.HasEstimatedChange = true
	}
	return total
}

func cell(text string, width int, align lipgloss.Position) string {
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Align(align).Render(text)
}

func fundLabel(row Row) string {
	name := row.Name
	if strings.TrimSpace(name) == "" {
		name = row.Quote.Name
	}
	if strings.TrimSpace(name) == "" {
		name = "未知基金"
	}
	return fmt.Sprintf("%s #%s", name, row.Code)
}

func formatPercent(value float64, ok bool) string {
	if !ok {
		return "--"
	}
	text := fmt.Sprintf("%+.2f%%", value)
	if value > 0 {
		return tuiUpStyle.Render(text)
	}
	if value < 0 {
		return tuiDownStyle.Render(text)
	}
	return text
}

func formatMoney(value float64, ok bool) string {
	if !ok {
		return "--"
	}
	text := fmt.Sprintf("%+.2f", value)
	if value > 0 {
		return tuiUpStyle.Render(text)
	}
	if value < 0 {
		return tuiDownStyle.Render(text)
	}
	return text
}
