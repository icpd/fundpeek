package valuation

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/icpd/fundpeek/internal/httpclient"
)

type Client struct {
	fundgz *resty.Client
	f10    *resty.Client
}

type Quote struct {
	Code string
	Name string

	DWJZ       float64
	HasDWJZ    bool
	GSZ        float64
	HasGSZ     bool
	GSZZL      float64
	HasGSZZL   bool
	ZZL        float64
	HasZZL     bool
	LastNAV    float64
	HasLastNAV bool

	JZRQ   string
	GZTime string
}

type StockHolding struct {
	Code string
	Name string

	Weight    float64
	HasWeight bool

	Shares    float64
	HasShares bool

	MarketValue    float64
	HasMarketValue bool
}

type FundStockHoldings struct {
	ReportDate string
	IsRecent   bool
	Holdings   []StockHolding
}

type StockQuote struct {
	Code string
	Name string

	ChangePercent    float64
	HasChangePercent bool

	Price    float64
	HasPrice bool
}

type netValue struct {
	Date      string
	NAV       float64
	Growth    float64
	HasGrowth bool
}

func NewClient() *Client {
	return &Client{
		fundgz: httpclient.New("https://fundgz.1234567.com.cn"),
		f10:    httpclient.New("https://fundf10.eastmoney.com"),
	}
}

func (c *Client) FetchQuote(ctx context.Context, code string) (Quote, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Quote{}, fmt.Errorf("fund code is required")
	}

	quote, gzErr := c.fetchFundGZ(ctx, code)
	values, navErr := c.fetchLatestNetValues(ctx, code, 3)
	if len(values) > 0 {
		latest := values[len(values)-1]
		if !quote.HasDWJZ || quote.JZRQ == "" || latest.Date >= quote.JZRQ {
			quote.Code = code
			quote.DWJZ = latest.NAV
			quote.HasDWJZ = true
			quote.JZRQ = latest.Date
			if latest.HasGrowth {
				quote.ZZL = latest.Growth
				quote.HasZZL = true
			}
			if len(values) > 1 {
				quote.LastNAV = values[len(values)-2].NAV
				quote.HasLastNAV = true
			}
		}
	}
	if quote.Code == "" {
		quote.Code = code
	}
	if gzErr != nil && navErr != nil {
		return quote, fmt.Errorf("%v; %v", gzErr, navErr)
	}
	if !quote.HasGSZ && !quote.HasDWJZ {
		if gzErr != nil {
			return quote, gzErr
		}
		if navErr != nil {
			return quote, navErr
		}
	}
	return quote, nil
}

func (c *Client) FetchFundStockHoldings(ctx context.Context, code string) (FundStockHoldings, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return FundStockHoldings{}, fmt.Errorf("fund code is required")
	}
	path := fmt.Sprintf("/FundArchivesDatas.aspx?type=jjcc&code=%s&topline=1000&year=&month=&_=%d", code, time.Now().UnixMilli())
	resp, err := c.f10.R().
		SetContext(ctx).
		Get(path)
	if err != nil {
		return FundStockHoldings{}, fmt.Errorf("fetch fund holdings %s: %w", code, err)
	}
	if resp.IsError() {
		return FundStockHoldings{}, fmt.Errorf("fetch fund holdings %s: http %d: %s", code, resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	return ParseFundStockHoldings(resp.String(), time.Now()), nil
}

func (c *Client) FetchTencentStockQuotes(ctx context.Context, codes []string) (map[string]StockQuote, error) {
	var normalized []string
	seen := map[string]bool{}
	for _, code := range codes {
		tc := NormalizeTencentCode(code)
		if tc == "" || seen[tc] {
			continue
		}
		seen[tc] = true
		normalized = append(normalized, tc)
	}
	if len(normalized) == 0 {
		return map[string]StockQuote{}, nil
	}
	client := httpclient.New("https://qt.gtimg.cn")
	resp, err := client.R().
		SetContext(ctx).
		SetQueryParam("q", strings.Join(normalized, ",")).
		Get("/")
	if err != nil {
		return nil, fmt.Errorf("fetch tencent stock quotes: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("fetch tencent stock quotes: http %d: %s", resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	return ParseTencentStockQuotes(resp.String()), nil
}

func (c *Client) fetchFundGZ(ctx context.Context, code string) (Quote, error) {
	path := fmt.Sprintf("/js/%s.js?rt=%d", code, time.Now().UnixMilli())
	resp, err := c.fundgz.R().
		SetContext(ctx).
		Get(path)
	if err != nil {
		return Quote{}, fmt.Errorf("fetch fundgz %s: %w", code, err)
	}
	if resp.IsError() {
		return Quote{}, fmt.Errorf("fetch fundgz %s: http %d: %s", code, resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	return ParseFundGZ(resp.String())
}

func (c *Client) fetchLatestNetValues(ctx context.Context, code string, count int) ([]netValue, error) {
	path := fmt.Sprintf("/F10DataApi.aspx?type=lsjz&code=%s&page=1&per=%d&sdate=&edate=", code, count)
	resp, err := c.f10.R().
		SetContext(ctx).
		Get(path)
	if err != nil {
		return nil, fmt.Errorf("fetch net values %s: %w", code, err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("fetch net values %s: http %d: %s", code, resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	values := ParseNetValues(resp.String())
	if len(values) == 0 {
		return nil, fmt.Errorf("fetch net values %s: empty response", code)
	}
	return values, nil
}

func ParseFundGZ(body string) (Quote, error) {
	start := strings.Index(body, "(")
	end := strings.LastIndex(body, ")")
	if start < 0 || end <= start {
		return Quote{}, fmt.Errorf("invalid fundgz jsonp")
	}
	var raw struct {
		FundCode string `json:"fundcode"`
		Name     string `json:"name"`
		JZRQ     string `json:"jzrq"`
		DWJZ     string `json:"dwjz"`
		GSZ      string `json:"gsz"`
		GSZZL    string `json:"gszzl"`
		GZTime   string `json:"gztime"`
	}
	if err := json.Unmarshal([]byte(body[start+1:end]), &raw); err != nil {
		return Quote{}, fmt.Errorf("decode fundgz json: %w", err)
	}
	q := Quote{
		Code:   strings.TrimSpace(raw.FundCode),
		Name:   strings.TrimSpace(raw.Name),
		JZRQ:   strings.TrimSpace(raw.JZRQ),
		GZTime: strings.TrimSpace(raw.GZTime),
	}
	if q.Code == "" {
		return Quote{}, fmt.Errorf("fundgz response missing code")
	}
	if v, ok := parseNumber(raw.DWJZ); ok {
		q.DWJZ = v
		q.HasDWJZ = true
	}
	if v, ok := parseNumber(raw.GSZ); ok {
		q.GSZ = v
		q.HasGSZ = true
	}
	if v, ok := parseNumber(raw.GSZZL); ok {
		q.GSZZL = v
		q.HasGSZZL = true
	}
	return q, nil
}

func ParseNetValues(body string) []netValue {
	content := extractF10Content(body)
	if content == "" || strings.Contains(content, "暂无数据") {
		return nil
	}
	rowRe := regexp.MustCompile(`(?is)<tr[\s\S]*?</tr>`)
	cellRe := regexp.MustCompile(`(?is)<td[^>]*>([\s\S]*?)</td>`)
	tagRe := regexp.MustCompile(`(?is)<[^>]+>`)

	var values []netValue
	for _, row := range rowRe.FindAllString(content, -1) {
		cells := cellRe.FindAllStringSubmatch(row, -1)
		if len(cells) < 2 {
			continue
		}
		text := func(i int) string {
			if i < 0 || i >= len(cells) {
				return ""
			}
			return strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(cells[i][1], "")))
		}
		date := text(0)
		if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(date) {
			continue
		}
		nav, ok := parseNumber(text(1))
		if !ok {
			continue
		}
		item := netValue{Date: date, NAV: nav}
		for i := 2; i < len(cells); i++ {
			t := strings.TrimSuffix(text(i), "%")
			if growth, ok := parseNumber(t); ok && strings.Contains(text(i), "%") {
				item.Growth = growth
				item.HasGrowth = true
				break
			}
		}
		values = append(values, item)
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].Date < values[j].Date
	})
	return values
}

func ParseFundStockHoldings(body string, now time.Time) FundStockHoldings {
	content := extractF10Content(body)
	if content == "" {
		content = body
	}
	result := FundStockHoldings{ReportDate: extractHoldingsReportDate(content)}
	result.IsRecent = isRecentHoldingsReport(result.ReportDate, now)
	if !result.IsRecent {
		return result
	}

	headerCells := parseHeaderCells(content)
	idxCode, idxName, idxWeight, idxShares, idxMarketValue := -1, -1, -1, -1, -1
	for i, h := range headerCells {
		t := strings.ReplaceAll(strings.TrimSpace(h), " ", "")
		if idxCode < 0 && (strings.Contains(t, "股票代码") || strings.Contains(t, "证券代码")) {
			idxCode = i
		}
		if idxName < 0 && (strings.Contains(t, "股票名称") || strings.Contains(t, "证券名称")) {
			idxName = i
		}
		if idxWeight < 0 && (strings.Contains(t, "占净值比例") || strings.Contains(t, "占比")) {
			idxWeight = i
		}
		if idxShares < 0 && (strings.Contains(t, "持股数") || strings.Contains(t, "持有数量")) {
			idxShares = i
		}
		if idxMarketValue < 0 && (strings.Contains(t, "持仓市值") || strings.Contains(t, "持有市值")) {
			idxMarketValue = i
		}
	}

	for _, cells := range parseBodyRows(content) {
		if len(cells) == 0 {
			continue
		}
		holding := StockHolding{}
		if idxCode >= 0 && idxCode < len(cells) {
			holding.Code = extractStockCode(cells[idxCode])
		}
		if holding.Code == "" {
			for _, cell := range cells {
				if code := extractStockCode(cell); code != "" {
					holding.Code = code
					break
				}
			}
		}
		if idxName >= 0 && idxName < len(cells) {
			holding.Name = strings.TrimSpace(cells[idxName])
		}
		if holding.Name == "" && holding.Code != "" {
			for _, cell := range cells {
				t := strings.TrimSpace(cell)
				if t != "" && t != holding.Code && !strings.Contains(t, "%") && extractStockCode(t) == "" && !looksNumeric(t) {
					holding.Name = t
					break
				}
			}
		}
		if idxWeight >= 0 && idxWeight < len(cells) {
			holding.Weight, holding.HasWeight = parsePercent(cells[idxWeight])
		}
		if !holding.HasWeight {
			for _, cell := range cells {
				if v, ok := parsePercent(cell); ok {
					holding.Weight, holding.HasWeight = v, true
					break
				}
			}
		}
		if idxShares >= 0 && idxShares < len(cells) {
			holding.Shares, holding.HasShares = parseNumber(cells[idxShares])
		}
		if idxMarketValue >= 0 && idxMarketValue < len(cells) {
			holding.MarketValue, holding.HasMarketValue = parseNumber(cells[idxMarketValue])
		}
		if holding.Code != "" || holding.Name != "" || holding.HasWeight {
			result.Holdings = append(result.Holdings, holding)
		}
	}
	return result
}

func NormalizeTencentCode(input string) string {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return ""
	}
	if m := regexp.MustCompile(`(?i)^(us|hk|sh|sz|bj)(.+)$`).FindStringSubmatch(raw); len(m) == 3 {
		prefix := strings.ToLower(m[1])
		rest := strings.TrimSpace(m[2])
		if rest == "" {
			return ""
		}
		if regexp.MustCompile(`^\d+$`).MatchString(rest) {
			return prefix + rest
		}
		return prefix + strings.ToUpper(rest)
	}
	if m := regexp.MustCompile(`(?i)^s_(sh|sz|bj|hk)(.+)$`).FindStringSubmatch(raw); len(m) == 3 {
		prefix := strings.ToLower(m[1])
		rest := strings.TrimSpace(m[2])
		if rest == "" {
			return ""
		}
		if regexp.MustCompile(`^\d+$`).MatchString(rest) {
			return "s_" + prefix + rest
		}
		return "s_" + prefix + strings.ToUpper(rest)
	}
	if regexp.MustCompile(`^\d{6}$`).MatchString(raw) {
		prefix := "sz"
		if strings.HasPrefix(raw, "6") || strings.HasPrefix(raw, "9") {
			prefix = "sh"
		} else if strings.HasPrefix(raw, "4") || strings.HasPrefix(raw, "8") {
			prefix = "bj"
		}
		return "s_" + prefix + raw
	}
	if regexp.MustCompile(`^\d{5}$`).MatchString(raw) {
		return "s_hk" + raw
	}
	if m := regexp.MustCompile(`(?i)^(\d{4,5})\.HK$`).FindStringSubmatch(raw); len(m) == 2 {
		return "s_hk" + leftPad(m[1], 5, "0")
	}
	if m := regexp.MustCompile(`^([A-Za-z]{1,10})(?:\.[A-Za-z]{1,6})$`).FindStringSubmatch(raw); len(m) == 2 {
		return "us" + strings.ToUpper(m[1])
	}
	if regexp.MustCompile(`^[A-Za-z]{1,10}$`).MatchString(raw) {
		return "us" + strings.ToUpper(raw)
	}
	return ""
}

func ParseTencentStockQuotes(body string) map[string]StockQuote {
	out := map[string]StockQuote{}
	re := regexp.MustCompile(`(?s)v_([A-Za-z0-9_]+)="([^"]*)"`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if len(m) != 3 {
			continue
		}
		code := m[1]
		parts := strings.Split(m[2], "~")
		q := StockQuote{Code: code}
		if len(parts) > 1 {
			q.Name = strings.TrimSpace(parts[1])
		}
		changeIdx := 5
		priceIdx := 3
		if strings.HasPrefix(strings.ToLower(code), "us") {
			changeIdx = 32
			priceIdx = 33
		}
		if changeIdx < len(parts) {
			q.ChangePercent, q.HasChangePercent = parseNumber(parts[changeIdx])
		}
		if priceIdx < len(parts) {
			q.Price, q.HasPrice = parseNumber(parts[priceIdx])
		}
		out[code] = q
	}
	return out
}

func extractF10Content(body string) string {
	re := regexp.MustCompile(`(?s)content:"(.*?)",\s*records:`)
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	s := strings.ReplaceAll(m[1], `\"`, `"`)
	s = strings.ReplaceAll(s, `\/`, `/`)
	return s
}

func extractHoldingsReportDate(value string) string {
	if m := regexp.MustCompile(`(报告期|截止日期)[^0-9]{0,20}(\d{4}-\d{2}-\d{2})`).FindStringSubmatch(value); len(m) == 3 {
		return m[2]
	}
	if m := regexp.MustCompile(`\d{4}-\d{2}-\d{2}`).FindString(value); m != "" {
		return m
	}
	return ""
}

func isRecentHoldingsReport(reportDate string, now time.Time) bool {
	if reportDate == "" {
		return false
	}
	report, err := time.ParseInLocation("2006-01-02", reportDate, now.Location())
	if err != nil {
		return false
	}
	sixMonthsAgo := now.AddDate(0, -6, 0)
	sevenDaysLater := now.AddDate(0, 0, 7)
	return report.After(sixMonthsAgo) && report.Before(sevenDaysLater)
}

func parseHeaderCells(content string) []string {
	headerRow := ""
	if m := regexp.MustCompile(`(?is)<thead[\s\S]*?<tr[\s\S]*?</tr>[\s\S]*?</thead>`).FindString(content); m != "" {
		headerRow = m
	}
	if headerRow == "" {
		if m := regexp.MustCompile(`(?is)<tr[\s\S]*?</tr>`).FindString(content); m != "" && strings.Contains(strings.ToLower(m), "<th") {
			headerRow = m
		}
	}
	return extractCells(headerRow, "th")
}

func parseBodyRows(content string) [][]string {
	body := content
	if m := regexp.MustCompile(`(?is)<tbody[\s\S]*?</tbody>`).FindString(content); m != "" {
		body = m
	}
	rowRe := regexp.MustCompile(`(?is)<tr[\s\S]*?</tr>`)
	var rows [][]string
	for _, row := range rowRe.FindAllString(body, -1) {
		if strings.Contains(strings.ToLower(row), "<th") {
			continue
		}
		cells := extractCells(row, "td")
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	return rows
}

func extractCells(row, tag string) []string {
	if row == "" {
		return nil
	}
	cellRe := regexp.MustCompile(`(?is)<` + tag + `[^>]*>([\s\S]*?)</` + tag + `>`)
	tagRe := regexp.MustCompile(`(?is)<[^>]+>`)
	var cells []string
	for _, m := range cellRe.FindAllStringSubmatch(row, -1) {
		text := tagRe.ReplaceAllString(m[1], "")
		cells = append(cells, normalizeSpace(html.UnescapeString(text)))
	}
	return cells
}

func extractStockCode(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ""
	}
	if m := regexp.MustCompile(`(?i)\b\d{4,5}\.HK\b`).FindString(raw); m != "" {
		return strings.ToUpper(m)
	}
	if m := regexp.MustCompile(`\d{6}`).FindString(raw); m != "" {
		return m
	}
	if m := regexp.MustCompile(`\d{5}`).FindString(raw); m != "" {
		return m
	}
	if m := regexp.MustCompile(`(?i)\b[A-Z]{1,10}(?:\.[A-Z]{1,6})?\b`).FindString(raw); m != "" {
		return strings.ToUpper(m)
	}
	return raw
}

func parsePercent(value string) (float64, bool) {
	m := regexp.MustCompile(`([-+]?\d+(?:\.\d+)?)\s*%`).FindStringSubmatch(value)
	if len(m) != 2 {
		return 0, false
	}
	return parseNumber(m[1])
}

func looksNumeric(value string) bool {
	_, ok := parseNumber(value)
	return ok
}

func normalizeSpace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func leftPad(value string, width int, pad string) string {
	for len(value) < width {
		value = pad + value
	}
	return value
}

func parseNumber(value string) (float64, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	if value == "" || value == "--" || value == "—" {
		return 0, false
	}
	n, err := strconv.ParseFloat(value, 64)
	return n, err == nil
}
