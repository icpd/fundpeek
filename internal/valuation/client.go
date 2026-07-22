package valuation

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/icpd/fundpeek/internal/httpclient"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

type Client struct {
	estimate *resty.Client
	sina     *resty.Client
	f10      *resty.Client
	fundAPI  *resty.Client
	minute   *resty.Client
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

type StockSearchResult struct {
	Code   string
	Name   string
	Market string
}

type StockMinutePoint struct {
	Time   string
	Price  float64
	Volume float64
	Amount float64
}

type StockMinute struct {
	Code   string
	Market string
	Date   string
	Points []StockMinutePoint
}

type netValue struct {
	Date      string
	NAV       float64
	Growth    float64
	HasGrowth bool
}

func NewClient() *Client {
	return &Client{
		estimate: httpclient.New("https://fundcomapi.tiantianfunds.com"),
		sina:     httpclient.New("https://stock.finance.sina.com.cn"),
		f10:      httpclient.New("https://fundf10.eastmoney.com"),
		fundAPI:  httpclient.New("https://api.fund.eastmoney.com"),
		minute:   httpclient.New("https://proxy.finance.qq.com"),
	}
}

func (c *Client) FetchQuote(ctx context.Context, code string) (Quote, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Quote{}, fmt.Errorf("fund code is required")
	}

	quote, estimateErr := c.fetchRealtimeFundEstimate(ctx, code)
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
	if estimateErr != nil && navErr != nil {
		return quote, fmt.Errorf("%v; %v", estimateErr, navErr)
	}
	if estimateErr != nil {
		return quote, estimateErr
	}
	if navErr != nil {
		return quote, navErr
	}
	return quote, nil
}

func (c *Client) FetchFundStockHoldings(ctx context.Context, code string) (FundStockHoldings, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return FundStockHoldings{}, fmt.Errorf("fund code is required")
	}
	path := fmt.Sprintf("/FundArchivesDatas.aspx?type=jjcc&code=%s&topline=1000&year=&month=&_=%d", code, time.Now().UnixMilli())
	referer := fmt.Sprintf("https://fundf10.eastmoney.com/ccmx_%s.html", code)
	resp, err := c.f10.R().
		SetContext(ctx).
		SetHeader("Referer", referer).
		Get(path)
	if err != nil {
		return FundStockHoldings{}, fmt.Errorf("fetch fund holdings %s: %w", code, err)
	}
	if resp.IsError() {
		if strings.Contains(strings.ToLower(resp.Header().Get("Content-Type")), "text/html") {
			return FundStockHoldings{}, fmt.Errorf("fetch fund holdings %s: http %d", code, resp.StatusCode())
		}
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
	body := decodeGB18030(resp.Body())
	return ParseTencentStockQuotes(body), nil
}

func (c *Client) SearchAStocks(ctx context.Context, query string) ([]StockSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("stock query is required")
	}
	client := httpclient.New("https://searchapi.eastmoney.com")
	resp, err := client.R().
		SetContext(ctx).
		SetQueryParam("input", query).
		SetQueryParam("type", "14").
		SetQueryParam("count", "10").
		Get("/api/suggest/get")
	if err != nil {
		return nil, fmt.Errorf("search stocks %q: %w", query, err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("search stocks %q: http %d: %s", query, resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	return ParseEastmoneyStockSearch(resp.String())
}

func (c *Client) FetchStockMinute(ctx context.Context, code string) (StockMinute, error) {
	market, normalized := NormalizeAStock(code)
	if market == "" || normalized == "" {
		return StockMinute{}, fmt.Errorf("unsupported A-share stock code %q", code)
	}
	tencentCode := market + normalized
	client := c.minute
	if client == nil {
		client = httpclient.New("https://proxy.finance.qq.com")
	}
	resp, err := client.R().
		SetContext(ctx).
		SetQueryParam("code", tencentCode).
		Get("/ifzqgtimg/appstock/app/minute/query")
	if err != nil {
		return StockMinute{}, fmt.Errorf("fetch stock minute %s: %w", tencentCode, err)
	}
	if resp.IsError() {
		return StockMinute{}, fmt.Errorf("fetch stock minute %s: http %d: %s", tencentCode, resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	minute, err := ParseTencentStockMinute(resp.String(), tencentCode)
	if err != nil {
		return StockMinute{}, err
	}
	return minute, nil
}

func (c *Client) fetchRealtimeFundEstimate(ctx context.Context, code string) (Quote, error) {
	quote, estimateErr := c.fetchFundEstimate(ctx, code)
	if estimateErr == nil && quote.HasGSZ && quote.HasGSZZL {
		return quote, nil
	}

	fallback, sinaErr := c.fetchSinaFundEstimate(ctx, code)
	if sinaErr == nil {
		return mergeFundEstimate(quote, fallback), nil
	}
	if estimateErr != nil {
		return quote, fmt.Errorf("%v; %v", estimateErr, sinaErr)
	}
	return quote, sinaErr
}

func (c *Client) fetchFundEstimate(ctx context.Context, code string) (Quote, error) {
	resp, err := c.estimate.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"FCODES": code,
			"FIELDS": "FCODE,SHORTNAME,GSZZL,GZTIME,GSZ,NAV,PDATE",
		}).
		Get("/mm/newCore/FundValuationLast")
	if err != nil {
		return Quote{}, fmt.Errorf("fetch fund estimate %s: %w", code, err)
	}
	if resp.IsError() {
		return Quote{}, fmt.Errorf("fetch fund estimate %s: http %d: %s", code, resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	quote, err := ParseFundEstimate(resp.String(), code)
	if err != nil {
		return Quote{}, fmt.Errorf("fetch fund estimate %s: %w", code, err)
	}
	return quote, nil
}

func (c *Client) fetchSinaFundEstimate(ctx context.Context, code string) (Quote, error) {
	resp, err := c.sina.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"symbol":   code,
			"callback": "fundpeek",
		}).
		Get("/fundInfo/api/openapi.php/FdFundService.getEstimateNetworthPic")
	if err != nil {
		return Quote{}, fmt.Errorf("fetch sina fund estimate %s: %w", code, err)
	}
	if resp.IsError() {
		return Quote{}, fmt.Errorf("fetch sina fund estimate %s: http %d: %s", code, resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	quote, err := ParseSinaFundEstimate(resp.String(), code)
	if err != nil {
		return Quote{}, fmt.Errorf("fetch sina fund estimate %s: %w", code, err)
	}
	return quote, nil
}

func (c *Client) fetchLatestNetValues(ctx context.Context, code string, count int) ([]netValue, error) {
	referer := fmt.Sprintf("https://fundf10.eastmoney.com/jjjz_%s.html", code)
	resp, err := c.fundAPI.R().
		SetContext(ctx).
		SetHeader("Referer", referer).
		SetQueryParams(map[string]string{
			"fundCode":  code,
			"pageIndex": "1",
			"pageSize":  strconv.Itoa(count),
			"startDate": "",
			"endDate":   "",
		}).
		Get("/f10/lsjz")
	if err != nil {
		return nil, fmt.Errorf("fetch net values %s: %w", code, err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("fetch net values %s: http %d: %s", code, resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	values, err := ParseNetValues(resp.String())
	if err != nil {
		return nil, fmt.Errorf("fetch net values %s: %w", code, err)
	}
	return values, nil
}

func ParseFundEstimate(body string, code string) (Quote, error) {
	var raw struct {
		Data []struct {
			Code   string `json:"FCODE"`
			Name   string `json:"SHORTNAME"`
			JZRQ   string `json:"PDATE"`
			DWJZ   any    `json:"NAV"`
			GSZ    any    `json:"GSZ"`
			GSZZL  any    `json:"GSZZL"`
			GZTime string `json:"GZTIME"`
		} `json:"data"`
		ErrorCode int  `json:"errorCode"`
		Success   bool `json:"success"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return Quote{}, fmt.Errorf("decode fund estimate: %w", err)
	}
	if !raw.Success {
		return Quote{}, fmt.Errorf("fund estimate api error %d", raw.ErrorCode)
	}
	code = strings.TrimSpace(code)
	for _, item := range raw.Data {
		if strings.TrimSpace(item.Code) != code {
			continue
		}
		quote := Quote{
			Code:   code,
			Name:   strings.TrimSpace(item.Name),
			JZRQ:   strings.TrimSpace(item.JZRQ),
			GZTime: strings.TrimSpace(item.GZTime),
		}
		quote.DWJZ, quote.HasDWJZ = parseJSONNumber(item.DWJZ)
		quote.GSZ, quote.HasGSZ = parseJSONNumber(item.GSZ)
		quote.GSZZL, quote.HasGSZZL = parseJSONNumber(item.GSZZL)
		return quote, nil
	}
	return Quote{}, fmt.Errorf("fund estimate response missing code %s", code)
}

func ParseSinaFundEstimate(body string, code string) (Quote, error) {
	start := strings.Index(body, "{")
	end := strings.LastIndex(body, "}")
	if start < 0 || end <= start {
		return Quote{}, fmt.Errorf("invalid sina fund estimate jsonp")
	}
	var raw struct {
		Result struct {
			Status struct {
				Code int `json:"code"`
			} `json:"status"`
			Data *struct {
				NetWorth []struct {
					Code         string `json:"symbol"`
					Minute       string `json:"min_time"`
					Date         string `json:"pre_date"`
					NAVFirst     any    `json:"pre_nav"`
					GrowthFirst  any    `json:"growthrate"`
					NAVSecond    any    `json:"pre_nav2"`
					GrowthSecond any    `json:"growthrate2"`
				} `json:"networth"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body[start:end+1]), &raw); err != nil {
		return Quote{}, fmt.Errorf("decode sina fund estimate: %w", err)
	}
	if raw.Result.Status.Code != 0 {
		return Quote{}, fmt.Errorf("sina fund estimate api error %d", raw.Result.Status.Code)
	}
	if raw.Result.Data == nil || len(raw.Result.Data.NetWorth) == 0 {
		return Quote{}, fmt.Errorf("sina fund estimate response contains no estimate")
	}
	item := raw.Result.Data.NetWorth[len(raw.Result.Data.NetWorth)-1]
	code = strings.TrimSpace(code)
	if strings.TrimSpace(item.Code) != code {
		return Quote{}, fmt.Errorf("sina fund estimate response missing code %s", code)
	}

	gsz, change, ok := parseSinaEstimatePair(item.NAVSecond, item.GrowthSecond)
	if !ok {
		gsz, change, ok = parseSinaEstimatePair(item.NAVFirst, item.GrowthFirst)
	}
	if !ok {
		return Quote{}, fmt.Errorf("sina fund estimate response contains no estimate")
	}
	return Quote{
		Code:     code,
		GSZ:      gsz,
		HasGSZ:   true,
		GSZZL:    change,
		HasGSZZL: true,
		GZTime:   estimateMinute(item.Date, item.Minute),
	}, nil
}

func parseSinaEstimatePair(nav any, growth any) (float64, float64, bool) {
	gsz, hasGSZ := parseJSONNumber(nav)
	rate, hasRate := parseJSONNumber(growth)
	change := rate * 100
	if !hasGSZ || !hasRate || math.IsNaN(change) || math.IsInf(change, 0) {
		return 0, 0, false
	}
	return gsz, change, true
}

func mergeFundEstimate(quote Quote, estimate Quote) Quote {
	if quote.Code == "" {
		quote.Code = estimate.Code
	}
	if quote.Name == "" {
		quote.Name = estimate.Name
	}
	quote.GSZ = estimate.GSZ
	quote.HasGSZ = estimate.HasGSZ
	quote.GSZZL = estimate.GSZZL
	quote.HasGSZZL = estimate.HasGSZZL
	quote.GZTime = estimate.GZTime
	return quote
}

func estimateMinute(date string, clock string) string {
	date = strings.TrimSpace(date)
	parts := strings.Split(strings.TrimSpace(clock), ":")
	clock = ""
	if len(parts) >= 2 {
		clock = parts[0] + ":" + parts[1]
	}
	if date == "" {
		return clock
	}
	if clock == "" {
		return date
	}
	return date + " " + clock
}

func parseJSONNumber(value any) (float64, bool) {
	var number float64
	var ok bool
	switch value := value.(type) {
	case float64:
		number, ok = value, true
	case json.Number:
		number, ok = parseNumber(value.String())
	case string:
		number, ok = parseNumber(value)
	}
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func ParseNetValues(body string) ([]netValue, error) {
	var raw struct {
		Data *struct {
			List []struct {
				Date   string `json:"FSRQ"`
				NAV    string `json:"DWJZ"`
				Growth string `json:"JZZZL"`
			} `json:"LSJZList"`
		} `json:"Data"`
		ErrCode int    `json:"ErrCode"`
		ErrMsg  string `json:"ErrMsg"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil, fmt.Errorf("decode net values: %w", err)
	}
	if raw.ErrCode != 0 {
		return nil, fmt.Errorf("net values api error %d: %s", raw.ErrCode, strings.TrimSpace(raw.ErrMsg))
	}
	if raw.Data == nil {
		return nil, fmt.Errorf("net values response missing data")
	}

	dateRE := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	values := make([]netValue, 0, len(raw.Data.List))
	for _, item := range raw.Data.List {
		date := strings.TrimSpace(item.Date)
		nav, ok := parseNumber(item.NAV)
		if !dateRE.MatchString(date) || !ok {
			continue
		}
		value := netValue{Date: date, NAV: nav}
		if growth, ok := parseNumber(item.Growth); ok {
			value.Growth = growth
			value.HasGrowth = true
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("net values response contains no valid records")
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].Date < values[j].Date
	})
	return values, nil
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

func NormalizeAStock(input string) (string, string) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return "", ""
	}
	raw = strings.TrimPrefix(strings.ToLower(raw), "s_")
	if m := regexp.MustCompile(`^(sh|sz|bj)(\d{6})$`).FindStringSubmatch(raw); len(m) == 3 {
		return m[1], m[2]
	}
	if !regexp.MustCompile(`^\d{6}$`).MatchString(raw) {
		return "", ""
	}
	market := "sz"
	if strings.HasPrefix(raw, "6") || strings.HasPrefix(raw, "9") {
		market = "sh"
	} else if strings.HasPrefix(raw, "4") || strings.HasPrefix(raw, "8") {
		market = "bj"
	}
	return market, raw
}

func ParseEastmoneyStockSearch(body string) ([]StockSearchResult, error) {
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "(") && strings.HasSuffix(body, ")") {
		body = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(body, "("), ")"))
	}
	var raw struct {
		QuotationCodeTable struct {
			Data []struct {
				Code     string `json:"Code"`
				Name     string `json:"Name"`
				Classify string `json:"Classify"`
			} `json:"Data"`
		} `json:"QuotationCodeTable"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil, fmt.Errorf("decode stock search: %w", err)
	}
	var out []StockSearchResult
	seen := map[string]bool{}
	for _, item := range raw.QuotationCodeTable.Data {
		if item.Classify != "AStock" {
			continue
		}
		market, code := NormalizeAStock(item.Code)
		if market == "" || code == "" {
			continue
		}
		key := market + code
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, StockSearchResult{
			Code:   code,
			Name:   strings.TrimSpace(item.Name),
			Market: market,
		})
	}
	return out, nil
}

func ParseTencentStockMinute(body string, code string) (StockMinute, error) {
	code = strings.TrimSpace(code)
	var raw struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data map[string]struct {
			Data struct {
				Data []string `json:"data"`
				Date string   `json:"date"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return StockMinute{}, fmt.Errorf("decode stock minute: %w", err)
	}
	if raw.Code != 0 {
		return StockMinute{}, fmt.Errorf("fetch stock minute %s: %s", code, raw.Msg)
	}
	entry, ok := raw.Data[code]
	if !ok {
		return StockMinute{}, fmt.Errorf("stock minute %s: missing data", code)
	}
	market, short := NormalizeAStock(code)
	out := StockMinute{Code: short, Market: market, Date: strings.TrimSpace(entry.Data.Date)}
	for _, line := range entry.Data.Data {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		price, ok := parseNumber(fields[1])
		if !ok {
			continue
		}
		point := StockMinutePoint{Time: fields[0], Price: price}
		if len(fields) > 2 {
			point.Volume, _ = parseNumber(fields[2])
		}
		if len(fields) > 3 {
			point.Amount, _ = parseNumber(fields[3])
		}
		out.Points = append(out.Points, point)
	}
	return out, nil
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

func decodeGB18030(body []byte) string {
	reader := transform.NewReader(strings.NewReader(string(body)), simplifiedchinese.GB18030.NewDecoder())
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return string(body)
	}
	return string(decoded)
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
