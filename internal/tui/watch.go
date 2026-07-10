package tui

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	fundapp "github.com/icpd/fundpeek/internal/app"
	"github.com/icpd/fundpeek/internal/valuation"
	"github.com/icpd/fundpeek/internal/watchlist"
)

type WatchRow struct {
	Item      watchlist.Item
	Quote     valuation.StockQuote
	Minute    valuation.StockMinute
	QuoteErr  error
	MinuteErr error
}

func newWatchState() watchState {
	input := textinput.New()
	input.Width = 24
	input.Prompt = "> "
	input.Placeholder = "股票代码或名称"
	return watchState{Input: input}
}

func LoadWatchRows(ctx context.Context, a *fundapp.App) ([]WatchRow, map[string]error, error) {
	items, err := a.Watchlist()
	if err != nil {
		return nil, nil, err
	}
	rows, errs, err := LoadWatchRowsSnapshot(a, items)
	if err != nil {
		return nil, nil, err
	}
	refreshed, refreshErrs := RefreshWatchRows(ctx, a, items)
	for key, err := range refreshErrs {
		errs[key] = err
	}
	if len(refreshed) > 0 || len(items) == 0 {
		return refreshed, errs, nil
	}
	return rows, errs, nil
}

func LoadWatchRowsSnapshot(a *fundapp.App, items []watchlist.Item) ([]WatchRow, map[string]error, error) {
	errs := map[string]error{}
	rows := make([]WatchRow, 0, len(items))
	for _, item := range items {
		row := WatchRow{Item: watchlist.Normalize(item)}
		quoteKey := watchQuoteKey(row.Item)
		if quoteKey != "" {
			quote, ok, err := a.CachedStockQuote(quoteKey)
			if err != nil {
				errs[row.Item.Code] = err
				row.QuoteErr = err
			} else if ok {
				row.Quote = quote
			}
		} else {
			row.QuoteErr = fmt.Errorf("unsupported stock code %s", row.Item.Code)
		}
		minuteKey := watchMinuteKey(row.Item)
		if minuteKey != "" {
			minute, ok, err := a.CachedStockMinute(minuteKey)
			if err != nil {
				errs[row.Item.Code+"/minute"] = err
				row.MinuteErr = err
			} else if ok {
				row.Minute = minute
			}
		} else {
			row.MinuteErr = fmt.Errorf("unsupported stock code %s", row.Item.Code)
		}
		rows = append(rows, row)
	}
	return rows, errs, nil
}

func RefreshWatchRows(ctx context.Context, a *fundapp.App, items []watchlist.Item) ([]WatchRow, map[string]error) {
	client := newStockQuoteFetcher()
	minuteClient := newStockMinuteFetcher()
	errs := map[string]error{}
	quotes := map[string]valuation.StockQuote{}
	codes := make([]string, 0, len(items))
	for _, item := range items {
		if key := watchQuoteKey(item); key != "" {
			codes = append(codes, key)
		}
	}
	fetchedQuotes, quoteErr := client.FetchTencentStockQuotes(ctx, codes)
	if quoteErr != nil {
		errs["quotes"] = quoteErr
	} else {
		for key, quote := range fetchedQuotes {
			quotes[key] = quote
			_ = a.SetStockQuote(key, quote)
		}
	}

	minutes := map[string]valuation.StockMinute{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	for _, item := range items {
		item := watchlist.Normalize(item)
		key := watchMinuteKey(item)
		if key == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				errs[key] = ctx.Err()
				mu.Unlock()
				return
			}
			minute, err := minuteClient.FetchStockMinute(ctx, key)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[key] = err
				return
			}
			minutes[key] = minute
			_ = a.SetStockMinute(key, minute)
		}()
	}
	wg.Wait()

	rows := make([]WatchRow, 0, len(items))
	for _, item := range items {
		item = watchlist.Normalize(item)
		quoteKey := watchQuoteKey(item)
		minuteKey := watchMinuteKey(item)
		row := WatchRow{Item: item, Quote: quotes[quoteKey], Minute: minutes[minuteKey]}
		if quoteErr != nil || (!row.Quote.HasChangePercent && !row.Quote.HasPrice) {
			row.QuoteErr = quoteErr
			if row.QuoteErr == nil {
				row.QuoteErr = fmt.Errorf("missing quote")
			}
		}
		if err := errs[minuteKey]; err != nil {
			row.MinuteErr = err
		}
		rows = append(rows, row)
	}
	return rows, errs
}

func renderWatchWithSpinner(state watchState, width int, spinnerView string) string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("fundpeek watch"))
	b.WriteString("\n")
	b.WriteString(tuiHelpStyle.Render(renderStatusBar(
		state.Loading,
		state.ErrText != "",
		state.LastRefresh,
		"Tab funds  ↑/↓ select  Enter detail  a add  d delete  r refresh",
		spinnerView,
	)))
	b.WriteString("\n\n")
	if state.ErrText != "" {
		b.WriteString(tuiErrStyle.Render(state.ErrText))
		b.WriteString("\n\n")
	}
	if state.Adding {
		b.WriteString(renderWatchAdd(state))
		b.WriteString("\n\n")
	}
	if len(state.Rows) == 0 {
		if state.Loading {
			b.WriteString(tuiTextStyle.Render("正在加载自选股行情和分时..."))
		} else {
			b.WriteString(tuiTextStyle.Render("还没有自选股票。按 a 添加。"))
		}
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString(renderWatchTable(state.Rows, state.Cursor, width))
	return b.String()
}

func renderWatchAdd(state watchState) string {
	var b strings.Builder
	b.WriteString(tuiHeaderStyle.Render("添加自选股"))
	b.WriteString("\n")
	if len(state.Candidates) == 0 {
		b.WriteString(state.Input.View())
		b.WriteString("\n")
		b.WriteString(tuiHelpStyle.Render("Enter search  Esc cancel"))
		return b.String()
	}
	for i, item := range state.Candidates {
		prefix := "  "
		if i == state.CandidateCursor {
			prefix = "> "
		}
		b.WriteString(tuiTextStyle.Render(prefix + watchItemLabel(item, 32)))
		b.WriteString("\n")
	}
	b.WriteString(tuiHelpStyle.Render("Enter add  ↑/↓ choose  Esc cancel"))
	return b.String()
}

func renderWatchTable(rows []WatchRow, cursor int, width int) string {
	const (
		selectorWidth = 2
		chgWidth      = 12
		priceWidth    = 12
	)
	nameWidth := watchNameWidth(width)
	tableWidth := selectorWidth + nameWidth + chgWidth + priceWidth
	var b strings.Builder
	b.WriteString(tuiHeaderStyle.Render(
		strings.Repeat(" ", selectorWidth) +
			cell("股票名称/代码", nameWidth, lipgloss.Left) +
			cell("当日涨幅", chgWidth, lipgloss.Right) +
			cell("最新价", priceWidth, lipgloss.Right),
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
		rowText.WriteString(cell(watchRowLabel(row, nameWidth), nameWidth, lipgloss.Left))
		rowText.WriteString(cell(formatPercent(row.Quote.ChangePercent, row.Quote.HasChangePercent), chgWidth, lipgloss.Right))
		rowText.WriteString(cell(formatNumber(row.Quote.Price, row.Quote.HasPrice), priceWidth, lipgloss.Right))
		if row.QuoteErr != nil || row.MinuteErr != nil {
			rowText.WriteString(" !")
		}
		b.WriteString(tuiTextStyle.Render(rowText.String()))
		b.WriteString("\n")
	}
	return b.String()
}

func renderWatchDetail(row WatchRow, width int) string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render(watchRowLabel(row, watchDetailTitleWidth(width))))
	b.WriteString("\n")
	b.WriteString(tuiHelpStyle.Render("Esc back  r refresh"))
	b.WriteString("\n\n")
	if row.QuoteErr != nil {
		b.WriteString(tuiErrStyle.Render(row.QuoteErr.Error()))
		b.WriteString("\n\n")
	}
	var quoteLine strings.Builder
	quoteLine.WriteString("涨幅 ")
	quoteLine.WriteString(formatPercent(row.Quote.ChangePercent, row.Quote.HasChangePercent))
	quoteLine.WriteString("  最新价 ")
	quoteLine.WriteString(formatNumber(row.Quote.Price, row.Quote.HasPrice))
	if row.Minute.Date != "" {
		quoteLine.WriteString("  日期 ")
		quoteLine.WriteString(formatMinuteDate(row.Minute.Date))
	}
	b.WriteString(tuiTextStyle.Render(quoteLine.String()))
	b.WriteString("\n\n")
	if row.MinuteErr != nil {
		b.WriteString(tuiErrStyle.Render(row.MinuteErr.Error()))
		b.WriteString("\n\n")
	}
	baseline, hasBaseline := previousCloseFromQuote(row.Quote)
	chart := MinuteChartWithBaseline(row.Minute.Points, baseline, hasBaseline, watchChartWidth(width), 8)
	if chart == "" {
		b.WriteString(tuiTextStyle.Render("暂无分时数据。"))
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString(tuiTextStyle.Render(chart))
	b.WriteString("\n")
	return b.String()
}

func MinuteChart(points []valuation.StockMinutePoint, width int, height int) string {
	return MinuteChartWithBaseline(points, 0, false, width, height)
}

func MinuteChartWithBaseline(points []valuation.StockMinutePoint, baseline float64, hasBaseline bool, width int, height int) string {
	if width < 12 || height < 3 {
		return ""
	}
	chartPoints := make([]valuation.StockMinutePoint, 0, len(points))
	values := make([]float64, 0, len(points))
	for _, point := range points {
		if point.Price > 0 {
			chartPoints = append(chartPoints, point)
			values = append(values, point.Price)
		}
	}
	if len(values) == 0 {
		return ""
	}
	minV, maxV := values[0], values[0]
	if !hasBaseline || baseline <= 0 {
		baseline = values[0]
		hasBaseline = true
	}
	for _, value := range values {
		if value < minV {
			minV = value
		}
		if value > maxV {
			maxV = value
		}
	}
	if hasBaseline {
		if baseline < minV {
			minV = baseline
		}
		if baseline > maxV {
			maxV = baseline
		}
	}
	if maxV <= minV {
		maxV = minV + 1
	}
	labelWidth := chartLabelWidth(maxV, minV, baseline)
	plotWidth := width - labelWidth - 2
	if plotWidth < 4 {
		return ""
	}
	waterlineY := chartY(baseline, minV, maxV, height)
	plot := brailleLineChart(chartPoints, plotWidth, height, minV, maxV, baseline)
	var b strings.Builder
	for y := 0; y < height; y++ {
		value := maxV - (maxV-minV)*float64(y)/float64(height-1)
		if y == waterlineY {
			b.WriteString(chartLabel(baseline, labelWidth))
		} else if y == 0 || y == height-1 {
			b.WriteString(chartLabel(value, labelWidth))
		} else {
			b.WriteString(strings.Repeat(" ", labelWidth))
		}
		b.WriteString(" │")
		b.WriteString(plot[y])
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat(" ", labelWidth+1))
	b.WriteString("└")
	b.WriteString(strings.Repeat("─", plotWidth))
	b.WriteString("\n")
	b.WriteString(strings.Repeat(" ", labelWidth+2))
	b.WriteString(minuteChartXLabels(plotWidth))
	return b.String()
}

func previousCloseFromQuote(quote valuation.StockQuote) (float64, bool) {
	if !quote.HasPrice || !quote.HasChangePercent || quote.ChangePercent <= -100 {
		return 0, false
	}
	previousClose := quote.Price / (1 + quote.ChangePercent/100)
	return previousClose, previousClose > 0
}

func chartLabelWidth(values ...float64) int {
	width := len("0000.00")
	for _, value := range values {
		if n := lipgloss.Width(formatNumber(value, true)); n > width {
			width = n
		}
	}
	return width
}

func chartLabel(value float64, width int) string {
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Right).Render(formatNumber(value, true))
}

func chartY(value float64, minV float64, maxV float64, height int) int {
	if height <= 1 || maxV <= minV {
		return 0
	}
	y := int(math.Round((maxV - value) / (maxV - minV) * float64(height-1)))
	if y < 0 {
		return 0
	}
	if y >= height {
		return height - 1
	}
	return y
}

func watchChartWidth(windowWidth int) int {
	if windowWidth <= 0 {
		return 74
	}
	if windowWidth < 48 {
		return 48
	}
	if windowWidth > 88 {
		return 88
	}
	return windowWidth
}

func watchDetailTitleWidth(windowWidth int) int {
	if windowWidth <= 0 {
		return 72
	}
	return windowWidth
}

func formatMinuteDate(value string) string {
	if len(value) == 8 {
		return value[:4] + "-" + value[4:6] + "-" + value[6:]
	}
	return value
}

func brailleLineChart(points []valuation.StockMinutePoint, width int, height int, minV float64, maxV float64, baseline float64) []string {
	pixelWidth := width * 2
	pixelHeight := height * 4
	canvas := make([][]byte, height)
	for y := range canvas {
		canvas[y] = make([]byte, width)
	}
	drawBrailleHorizontal(canvas, chartPixelY(baseline, minV, maxV, pixelHeight), pixelWidth)
	points = chartPointsForPlot(points, pixelWidth)
	useTradingTime := pointsUseAShareTradingTime(points)
	var prevX, prevY int
	for i, point := range points {
		x := 0
		if useTradingTime {
			offset, _ := aShareTradingMinuteOffset(point.Time)
			x = int(math.Round(float64(offset) / 240 * float64(pixelWidth-1)))
		} else if len(points) > 1 {
			x = int(math.Round(float64(i) / float64(len(points)-1) * float64(pixelWidth-1)))
		}
		y := chartPixelY(point.Price, minV, maxV, pixelHeight)
		if i > 0 {
			drawBrailleLine(canvas, prevX, prevY, x, y)
		}
		setBraillePixel(canvas, x, y)
		prevX, prevY = x, y
	}
	out := make([]string, height)
	for y := range canvas {
		var b strings.Builder
		for x := range canvas[y] {
			mask := canvas[y][x]
			if mask == 0 {
				b.WriteRune(' ')
			} else {
				b.WriteRune(rune(0x2800 + int(mask)))
			}
		}
		out[y] = b.String()
	}
	return out
}

func chartPointsForPlot(points []valuation.StockMinutePoint, maxPoints int) []valuation.StockMinutePoint {
	if maxPoints <= 0 || len(points) <= maxPoints || pointsUseAShareTradingTime(points) {
		return points
	}
	out := make([]valuation.StockMinutePoint, 0, maxPoints)
	step := float64(len(points)) / float64(maxPoints)
	for i := 0; i < maxPoints; i++ {
		start := int(math.Floor(float64(i) * step))
		end := int(math.Floor(float64(i+1) * step))
		if end <= start {
			end = start + 1
		}
		if end > len(points) {
			end = len(points)
		}
		point := points[start]
		point.Time = ""
		point.Price = 0
		for _, sample := range points[start:end] {
			point.Price += sample.Price
		}
		point.Price /= float64(end - start)
		out = append(out, point)
	}
	return out
}

func pointsUseAShareTradingTime(points []valuation.StockMinutePoint) bool {
	if len(points) == 0 {
		return false
	}
	for _, point := range points {
		if _, ok := aShareTradingMinuteOffset(point.Time); !ok {
			return false
		}
	}
	return true
}

func aShareTradingMinuteOffset(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if len(value) != 4 {
		return 0, false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	hour, err := strconv.Atoi(value[:2])
	if err != nil {
		return 0, false
	}
	minute, err := strconv.Atoi(value[2:])
	if err != nil || minute >= 60 {
		return 0, false
	}
	total := hour*60 + minute
	switch {
	case total >= 9*60+30 && total <= 11*60+30:
		return total - (9*60 + 30), true
	case total >= 13*60 && total <= 15*60:
		return 120 + total - 13*60, true
	default:
		return 0, false
	}
}

func chartPixelY(value float64, minV float64, maxV float64, pixelHeight int) int {
	if pixelHeight <= 1 || maxV <= minV {
		return 0
	}
	y := int(math.Round((maxV - value) / (maxV - minV) * float64(pixelHeight-1)))
	if y < 0 {
		return 0
	}
	if y >= pixelHeight {
		return pixelHeight - 1
	}
	return y
}

func drawBrailleHorizontal(canvas [][]byte, y int, pixelWidth int) {
	for x := 0; x < pixelWidth; x++ {
		setBraillePixel(canvas, x, y)
	}
}

func drawBrailleLine(canvas [][]byte, x0 int, y0 int, x1 int, y1 int) {
	dx := absInt(x1 - x0)
	dy := -absInt(y1 - y0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		setBraillePixel(canvas, x0, y0)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func setBraillePixel(canvas [][]byte, px int, py int) {
	if len(canvas) == 0 || len(canvas[0]) == 0 || px < 0 || py < 0 {
		return
	}
	cellY := py / 4
	cellX := px / 2
	if cellY >= len(canvas) || cellX >= len(canvas[cellY]) {
		return
	}
	dotY := py % 4
	dotX := px % 2
	canvas[cellY][cellX] |= brailleDot(dotX, dotY)
}

func brailleDot(x int, y int) byte {
	if x == 0 {
		switch y {
		case 0:
			return 0x01
		case 1:
			return 0x02
		case 2:
			return 0x04
		case 3:
			return 0x40
		}
	}
	switch y {
	case 0:
		return 0x08
	case 1:
		return 0x10
	case 2:
		return 0x20
	case 3:
		return 0x80
	}
	return 0
}

func minuteChartXLabels(width int) string {
	if width <= 0 {
		return ""
	}
	const start = "09:30"
	const end = "15:00"
	if lipgloss.Width(start)+lipgloss.Width(end)+1 > width {
		return ""
	}
	gap := width - lipgloss.Width(start) - lipgloss.Width(end)
	return start + strings.Repeat(" ", gap) + end
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func watchNameWidth(windowWidth int) int {
	const (
		minWidth          = 30
		maxWidth          = 54
		defaultTableWidth = 74
	)
	if windowWidth <= defaultTableWidth {
		return minWidth
	}
	nameWidth := minWidth + windowWidth - defaultTableWidth
	if nameWidth > maxWidth {
		return maxWidth
	}
	return nameWidth
}

func watchRowLabel(row WatchRow, width int) string {
	item := row.Item
	if item.Name == "" {
		item.Name = row.Quote.Name
	}
	return watchItemLabel(item, width)
}

func watchItemLabel(item watchlist.Item, width int) string {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		name = "未知股票"
	}
	suffix := fmt.Sprintf(" #%s%s", item.Market, item.Code)
	nameWidth := width - lipgloss.Width(suffix)
	if nameWidth < 1 {
		return suffix
	}
	return truncateDisplayWidth(name, nameWidth) + suffix
}

func watchQuoteKey(item watchlist.Item) string {
	item = watchlist.Normalize(item)
	if item.Code == "" {
		return ""
	}
	return valuation.NormalizeTencentCode(item.Code)
}

func watchMinuteKey(item watchlist.Item) string {
	item = watchlist.Normalize(item)
	if item.Code == "" || item.Market == "" {
		return ""
	}
	return item.Market + item.Code
}
