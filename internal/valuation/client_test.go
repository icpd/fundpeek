package valuation

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
)

func TestFetchFundEstimateUsesCurrentJSONAPI(t *testing.T) {
	var gotURL *url.URL
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL
		body := `{"data":[{"NAV":0.55,"GZTIME":"2026-07-22 10:09","SHORTNAME":"招商中证白酒指数(LOF)A","FCODE":"161725","PDATE":"2026-07-21","GSZZL":-0.36,"GSZ":0.548}],"errorCode":0,"success":true}`
		return restyResponse(r, http.StatusOK, "application/json", body), nil
	})
	client := &Client{estimate: resty.New().SetBaseURL("https://fundcomapi.tiantianfunds.com").SetTransport(transport)}

	got, err := client.fetchFundEstimate(context.Background(), "161725")
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "161725" || got.Name != "招商中证白酒指数(LOF)A" || got.JZRQ != "2026-07-21" {
		t.Fatalf("unexpected identity: %#v", got)
	}
	if !got.HasGSZZL || got.GSZZL != -0.36 {
		t.Fatalf("GSZZL = %v/%f, want true/-0.36", got.HasGSZZL, got.GSZZL)
	}
	if !got.HasGSZ || got.GSZ != 0.548 || !got.HasDWJZ || got.DWJZ != 0.55 {
		t.Fatalf("estimate/NAV = %#v, want 0.548/0.55", got)
	}
	if gotURL.Scheme != "https" || gotURL.Host != "fundcomapi.tiantianfunds.com" || gotURL.Path != "/mm/newCore/FundValuationLast" {
		t.Fatalf("request URL = %s, want current fund valuation API", gotURL)
	}
	query := gotURL.Query()
	if query.Get("FCODES") != "161725" || query.Get("FIELDS") != "FCODE,SHORTNAME,GSZZL,GZTIME,GSZ,NAV,PDATE" {
		t.Fatalf("request query = %v, want fund code and estimate fields", query)
	}
}

func TestParseSinaFundEstimatePrefersSecondMethod(t *testing.T) {
	body := `/*<script>location.href='//sina.com';</script>*/
fundpeek({"result":{"status":{"code":0},"data":{"networth":[{"symbol":"000001","min_time":"10:12:00","pre_date":"2026-07-22","pre_nav":"1.4460","growthrate":0.00069204152249127,"pre_nav2":"1.4411","growthrate2":"-0.002699"}]}}})`

	got, err := ParseSinaFundEstimate(body, "000001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "000001" || !got.HasGSZ || got.GSZ != 1.4411 {
		t.Fatalf("quote = %#v, want Sina method 2 NAV", got)
	}
	if !got.HasGSZZL || math.Abs(got.GSZZL-(-0.2699)) > 0.0000001 {
		t.Fatalf("GSZZL = %v/%f, want true/-0.2699", got.HasGSZZL, got.GSZZL)
	}
	if got.GZTime != "2026-07-22 10:12" {
		t.Fatalf("GZTime = %q, want minute precision", got.GZTime)
	}
}

func TestParseSinaFundEstimateFallsBackToFirstMethod(t *testing.T) {
	body := `fundpeek({"result":{"status":{"code":0},"data":{"networth":[{"symbol":"000001","min_time":"10:12:00","pre_date":"2026-07-22","pre_nav":"1.4460","growthrate":0.00069204152249127,"pre_nav2":null,"growthrate2":null}]}}})`

	got, err := ParseSinaFundEstimate(body, "000001")
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasGSZ || got.GSZ != 1.446 || !got.HasGSZZL || math.Abs(got.GSZZL-0.069204152249127) > 0.0000001 {
		t.Fatalf("quote = %#v, want Sina method 1 fallback", got)
	}
}

func TestParseSinaFundEstimateRejectsEmptyCurve(t *testing.T) {
	body := `fundpeek({"result":{"status":{"code":0},"data":{"networth":null}}})`

	_, err := ParseSinaFundEstimate(body, "999999")
	if err == nil || !strings.Contains(err.Error(), "contains no estimate") {
		t.Fatalf("error = %v, want empty-estimate error", err)
	}
}

func TestParseSinaFundEstimateRejectsScaledOverflow(t *testing.T) {
	body := `fundpeek({"result":{"status":{"code":0},"data":{"networth":[{"symbol":"000001","min_time":"10:12:00","pre_date":"2026-07-22","pre_nav":null,"growthrate":null,"pre_nav2":"1.4411","growthrate2":"1e308"}]}}})`

	_, err := ParseSinaFundEstimate(body, "000001")
	if err == nil || !strings.Contains(err.Error(), "contains no estimate") {
		t.Fatalf("error = %v, want overflow rejected", err)
	}
}

func TestParseNetValues(t *testing.T) {
	body := `{"Data":{"LSJZList":[{"FSRQ":"2026-07-15","DWJZ":"1.4600","JZZZL":"-3.31"},{"FSRQ":"2026-07-14","DWJZ":"1.5100","JZZZL":"1.27"}]},"ErrCode":0,"ErrMsg":null,"PageSize":2,"PageIndex":1}`

	got, err := ParseNetValues(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len(ParseNetValues) = %d, want 2: %#v", len(got), got)
	}
	if got[0].Date != "2026-07-14" || got[1].Date != "2026-07-15" {
		t.Fatalf("dates not sorted ascending: %#v", got)
	}
	if got[1].NAV != 1.46 || !got[1].HasGrowth || got[1].Growth != -3.31 {
		t.Fatalf("latest NAV/growth = %#v, want 1.46/-3.31", got[1])
	}
}

func TestFetchLatestNetValuesUsesCurrentJSONAPI(t *testing.T) {
	var gotURL *url.URL
	var gotReferer string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL
		gotReferer = r.Header.Get("Referer")
		body := `{"Data":{"LSJZList":[{"FSRQ":"2026-07-15","DWJZ":"1.4600","JZZZL":"-3.31"}]},"ErrCode":0}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
	client := &Client{fundAPI: resty.New().SetBaseURL("https://api.fund.eastmoney.com").SetTransport(transport)}

	values, err := client.fetchLatestNetValues(context.Background(), "000001", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Growth != -3.31 {
		t.Fatalf("values = %#v, want parsed JSON value", values)
	}
	if gotURL.Scheme != "https" || gotURL.Host != "api.fund.eastmoney.com" || gotURL.Path != "/f10/lsjz" {
		t.Fatalf("request URL = %s, want current NAV API", gotURL)
	}
	query := gotURL.Query()
	if query.Get("fundCode") != "000001" || query.Get("pageIndex") != "1" || query.Get("pageSize") != "3" || query.Get("startDate") != "" || query.Get("endDate") != "" {
		t.Fatalf("request query = %v, want fund code, page, size, and empty date range", query)
	}
	if want := "https://fundf10.eastmoney.com/jjjz_000001.html"; gotReferer != want {
		t.Fatalf("Referer = %q, want %q", gotReferer, want)
	}
}

func restyResponse(r *http.Request, status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

func TestFetchQuoteReturnsEstimateWithNetValueError(t *testing.T) {
	estimate := resty.New().SetBaseURL("https://fundcomapi.tiantianfunds.com").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return restyResponse(r, http.StatusOK, "application/json", `{"data":[{"FCODE":"000001","SHORTNAME":"华夏成长混合","PDATE":"2026-07-15","NAV":1.46,"GSZ":1.4366,"GSZZL":-1.60,"GZTIME":"2026-07-16 11:30"}],"errorCode":0,"success":true}`), nil
	}))
	fundAPI := resty.New().SetBaseURL("https://api.fund.eastmoney.com").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return restyResponse(r, http.StatusBadGateway, "application/json", `{"message":"upstream unavailable"}`), nil
	}))

	quote, err := (&Client{estimate: estimate, fundAPI: fundAPI}).FetchQuote(context.Background(), "000001")
	if err == nil || !strings.Contains(err.Error(), "fetch net values 000001: http 502") {
		t.Fatalf("error = %v, want net-value HTTP error", err)
	}
	if !quote.HasGSZ || quote.GSZ != 1.4366 || !quote.HasGSZZL || quote.GSZZL != -1.60 {
		t.Fatalf("quote = %#v, want preserved estimate fields", quote)
	}
}

func TestFetchQuoteReturnsNetValueWithEstimateError(t *testing.T) {
	estimate := resty.New().SetBaseURL("https://fundcomapi.tiantianfunds.com").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return restyResponse(r, http.StatusBadGateway, "application/json", "unavailable"), nil
	}))
	sina := resty.New().SetBaseURL("https://stock.finance.sina.com.cn").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return restyResponse(r, http.StatusBadGateway, "application/javascript", "unavailable"), nil
	}))
	fundAPI := resty.New().SetBaseURL("https://api.fund.eastmoney.com").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return restyResponse(r, http.StatusOK, "application/json", `{"Data":{"LSJZList":[{"FSRQ":"2026-07-15","DWJZ":"1.4600","JZZZL":"-3.31"},{"FSRQ":"2026-07-14","DWJZ":"1.5100","JZZZL":"1.27"}]},"ErrCode":0}`), nil
	}))

	quote, err := (&Client{estimate: estimate, sina: sina, fundAPI: fundAPI}).FetchQuote(context.Background(), "000001")
	if err == nil || !strings.Contains(err.Error(), "fetch fund estimate 000001: http 502") || !strings.Contains(err.Error(), "fetch sina fund estimate 000001: http 502") {
		t.Fatalf("error = %v, want estimate HTTP error", err)
	}
	if !quote.HasDWJZ || quote.DWJZ != 1.46 || !quote.HasZZL || quote.ZZL != -3.31 || !quote.HasLastNAV || quote.LastNAV != 1.51 {
		t.Fatalf("quote = %#v, want preserved NAV fields", quote)
	}
}

func TestFetchQuoteFallsBackToSinaWhenCurrentEstimateMissing(t *testing.T) {
	estimate := resty.New().SetBaseURL("https://fundcomapi.tiantianfunds.com").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"data":[{"FCODE":"000001","SHORTNAME":"华夏成长混合","PDATE":"2026-07-21","NAV":1.445,"GSZ":null,"GSZZL":null,"GZTIME":null}],"errorCode":0,"success":true}`
		return restyResponse(r, http.StatusOK, "application/json", body), nil
	}))
	var gotSinaURL *url.URL
	sina := resty.New().SetBaseURL("https://stock.finance.sina.com.cn").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotSinaURL = r.URL
		body := `/*<script>location.href='//sina.com';</script>*/fundpeek({"result":{"status":{"code":0},"data":{"networth":[{"symbol":"000001","min_time":"10:12:00","pre_date":"2026-07-22","pre_nav":"1.4460","growthrate":0.00069204152249127,"pre_nav2":"1.4411","growthrate2":"-0.002699"}]}}})`
		return restyResponse(r, http.StatusOK, "application/javascript", body), nil
	}))
	fundAPI := resty.New().SetBaseURL("https://api.fund.eastmoney.com").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"Data":{"LSJZList":[{"FSRQ":"2026-07-21","DWJZ":"1.4450","JZZZL":"10.64"},{"FSRQ":"2026-07-20","DWJZ":"1.3060","JZZZL":"-0.99"}]},"ErrCode":0}`
		return restyResponse(r, http.StatusOK, "application/json", body), nil
	}))

	quote, err := (&Client{estimate: estimate, sina: sina, fundAPI: fundAPI}).FetchQuote(context.Background(), "000001")
	if err != nil {
		t.Fatal(err)
	}
	if quote.Name != "华夏成长混合" || !quote.HasGSZ || quote.GSZ != 1.4411 || !quote.HasGSZZL || math.Abs(quote.GSZZL-(-0.2699)) > 0.0000001 {
		t.Fatalf("quote = %#v, want identity plus Sina estimate", quote)
	}
	if !quote.HasDWJZ || quote.DWJZ != 1.445 || !quote.HasLastNAV || quote.LastNAV != 1.306 || quote.GZTime != "2026-07-22 10:12" {
		t.Fatalf("quote = %#v, want current NAV plus minute estimate time", quote)
	}
	if gotSinaURL.Path != "/fundInfo/api/openapi.php/FdFundService.getEstimateNetworthPic" || gotSinaURL.Query().Get("symbol") != "000001" || gotSinaURL.Query().Get("callback") != "fundpeek" {
		t.Fatalf("Sina request URL = %s, want symbol and callback", gotSinaURL)
	}
}

func TestParseFundStockHoldingsFindsReportDateAndColumns(t *testing.T) {
	body := `var apidata={ content:"<div>报告期：2026-03-31</div><table><thead><tr><th>序号</th><th>股票代码</th><th>股票名称</th><th>持股数</th><th>持仓市值</th><th>占净值比例</th></tr></thead><tbody><tr><td>1</td><td><a>600519</a></td><td>贵州茅台</td><td>12,300</td><td>1820.50</td><td>9.87%</td></tr><tr><td>2</td><td>00700.HK</td><td>腾讯控股</td><td>45,000</td><td>1500</td><td>8.01%</td></tr></tbody></table>",records:2};`

	got := ParseFundStockHoldings(body, time.Date(2026, 5, 11, 12, 0, 0, 0, time.Local))

	if got.ReportDate != "2026-03-31" {
		t.Fatalf("ReportDate = %q, want 2026-03-31", got.ReportDate)
	}
	if !got.IsRecent {
		t.Fatal("expected recent report")
	}
	if len(got.Holdings) != 2 {
		t.Fatalf("len(Holdings) = %d, want 2: %#v", len(got.Holdings), got.Holdings)
	}
	if got.Holdings[0].Code != "600519" || got.Holdings[0].Name != "贵州茅台" {
		t.Fatalf("unexpected first holding identity: %#v", got.Holdings[0])
	}
	if !got.Holdings[0].HasWeight || math.Abs(got.Holdings[0].Weight-9.87) > 0.000001 {
		t.Fatalf("weight = %v/%f, want true/9.87", got.Holdings[0].HasWeight, got.Holdings[0].Weight)
	}
	if !got.Holdings[0].HasShares || got.Holdings[0].Shares != 12300 {
		t.Fatalf("shares = %v/%f, want true/12300", got.Holdings[0].HasShares, got.Holdings[0].Shares)
	}
	if !got.Holdings[0].HasMarketValue || got.Holdings[0].MarketValue != 1820.50 {
		t.Fatalf("market value = %v/%f, want true/1820.50", got.Holdings[0].HasMarketValue, got.Holdings[0].MarketValue)
	}
	if got.Holdings[1].Code != "00700.HK" {
		t.Fatalf("second code = %q, want 00700.HK", got.Holdings[1].Code)
	}
}

func TestParseFundStockHoldingsFallsBackToFirstDateAndHidesStaleHoldings(t *testing.T) {
	body := `var apidata={ content:"<p>2025-06-30</p><table><tbody><tr><td>600519</td><td>贵州茅台</td><td>9.87%</td></tr></tbody></table>",records:1};`

	got := ParseFundStockHoldings(body, time.Date(2026, 5, 11, 12, 0, 0, 0, time.Local))

	if got.ReportDate != "2025-06-30" {
		t.Fatalf("ReportDate = %q, want 2025-06-30", got.ReportDate)
	}
	if got.IsRecent {
		t.Fatal("expected stale report")
	}
	if len(got.Holdings) != 0 {
		t.Fatalf("stale holdings should be hidden: %#v", got.Holdings)
	}
}

func TestFetchFundStockHoldingsSetsFundPageReferer(t *testing.T) {
	var gotReferer string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotReferer = r.Header.Get("Referer")
		body := `var apidata={ content:"<div>报告期：2026-03-31</div><table><thead><tr><th>股票代码</th><th>股票名称</th></tr></thead><tbody><tr><td>600519</td><td>贵州茅台</td></tr></tbody></table>",records:1};`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
	client := &Client{f10: resty.New().SetBaseURL("https://fundf10.eastmoney.com").SetTransport(transport)}

	got, err := client.FetchFundStockHoldings(context.Background(), "006503")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Holdings) != 1 || got.Holdings[0].Code != "600519" {
		t.Fatalf("holdings = %#v, want parsed fixture", got.Holdings)
	}
	want := "https://fundf10.eastmoney.com/ccmx_006503.html"
	if gotReferer != want {
		t.Fatalf("holdings Referer = %q, want %q", gotReferer, want)
	}
}

func TestFetchFundStockHoldingsHidesHTMLErrorBody(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader("<html>not found</html>")),
			Request:    r,
		}, nil
	})
	client := &Client{f10: resty.New().SetBaseURL("https://fundf10.eastmoney.com").SetTransport(transport)}

	_, err := client.FetchFundStockHoldings(context.Background(), "006503")
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "fetch fund holdings 006503: http 404") {
		t.Fatalf("error = %q, want operation, code, and status", err)
	}
	if strings.Contains(err.Error(), "<html>") || strings.Contains(err.Error(), "not found") {
		t.Fatalf("error should hide HTML response body: %q", err)
	}
}

func TestNormalizeTencentCode(t *testing.T) {
	tests := map[string]string{
		"600519":     "s_sh600519",
		"000001":     "s_sz000001",
		"430047":     "s_bj430047",
		"00700":      "s_hk00700",
		"0700.HK":    "s_hk00700",
		"AAPL":       "usAAPL",
		"tsla.us":    "usTSLA",
		"s_sz000001": "s_sz000001",
		"usmsft":     "usMSFT",
	}
	for input, want := range tests {
		if got := NormalizeTencentCode(input); got != want {
			t.Fatalf("NormalizeTencentCode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeAStock(t *testing.T) {
	tests := map[string]struct {
		market string
		code   string
	}{
		"600519":     {"sh", "600519"},
		"000001":     {"sz", "000001"},
		"430047":     {"bj", "430047"},
		"sh600519":   {"sh", "600519"},
		"s_sz000001": {"sz", "000001"},
		"AAPL":       {"", ""},
	}
	for input, want := range tests {
		market, code := NormalizeAStock(input)
		if market != want.market || code != want.code {
			t.Fatalf("NormalizeAStock(%q) = %q/%q, want %q/%q", input, market, code, want.market, want.code)
		}
	}
}

func TestParseEastmoneyStockSearchFiltersAStocks(t *testing.T) {
	body := `({"QuotationCodeTable":{"Data":[{"Code":"000001","Name":"平安银行","Classify":"AStock"},{"Code":"01833","Name":"平安好医生","Classify":"HK"},{"Code":"601318","Name":"中国平安","Classify":"AStock"}]}})`

	got, err := ParseEastmoneyStockSearch(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len(results) = %d, want 2: %#v", len(got), got)
	}
	if got[0].Market != "sz" || got[0].Code != "000001" || got[0].Name != "平安银行" {
		t.Fatalf("first result = %#v, want sz/000001/平安银行", got[0])
	}
	if got[1].Market != "sh" || got[1].Code != "601318" {
		t.Fatalf("second result = %#v, want sh/601318", got[1])
	}
}

func TestParseTencentStockQuotes(t *testing.T) {
	body := strings.Join([]string{
		`v_s_sh600519="1~贵州茅台~600519~1820.50~0~+1.23";`,
		`v_s_hk00700="1~腾讯控股~00700~365.80~0~-2.34";`,
		`v_usAAPL="51~苹果~AAPL.OQ~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~+0.56~192.10";`,
	}, "\n")

	got := ParseTencentStockQuotes(body)

	if len(got) != 3 {
		t.Fatalf("len(ParseTencentStockQuotes) = %d, want 3: %#v", len(got), got)
	}
	if q := got["s_sh600519"]; q.Name != "贵州茅台" || !q.HasChangePercent || q.ChangePercent != 1.23 || !q.HasPrice || q.Price != 1820.50 {
		t.Fatalf("unexpected A-share quote: %#v", q)
	}
	if q := got["s_hk00700"]; q.Name != "腾讯控股" || !q.HasChangePercent || q.ChangePercent != -2.34 || !q.HasPrice || q.Price != 365.80 {
		t.Fatalf("unexpected HK quote: %#v", q)
	}
	if q := got["usAAPL"]; q.Name != "苹果" || !q.HasChangePercent || q.ChangePercent != 0.56 || !q.HasPrice || q.Price != 192.10 {
		t.Fatalf("unexpected US quote: %#v", q)
	}
}

func TestParseTencentStockMinute(t *testing.T) {
	body := `{"code":0,"msg":"","data":{"sh600519":{"data":{"data":["0930 1187.00 294 34897800.00","0931 1189.00 1050 124702396.33"],"date":"20260630"}}}}`

	got, err := ParseTencentStockMinute(body, "sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if got.Market != "sh" || got.Code != "600519" || got.Date != "20260630" {
		t.Fatalf("minute identity = %#v, want sh/600519/20260630", got)
	}
	if len(got.Points) != 2 || got.Points[1].Time != "0931" || got.Points[1].Price != 1189.00 {
		t.Fatalf("minute points = %#v, want parsed prices", got.Points)
	}
}

func TestFetchStockMinuteUsesTencentProxyEndpoint(t *testing.T) {
	var gotPath string
	var gotCode string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotCode = r.URL.Query().Get("code")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"","data":{"sh600519":{"data":{"data":["0930 1187.00 294 34897800.00"],"date":"20260630"}}}}`)),
			Request:    r,
		}, nil
	})

	client := &Client{minute: resty.New().SetBaseURL("https://proxy.finance.qq.com").SetTransport(transport)}
	got, err := client.FetchStockMinute(context.Background(), "600519")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/ifzqgtimg/appstock/app/minute/query" {
		t.Fatalf("minute request path = %q, want proxy path", gotPath)
	}
	if gotCode != "sh600519" {
		t.Fatalf("minute request code = %q, want sh600519", gotCode)
	}
	if got.Market != "sh" || got.Code != "600519" || len(got.Points) != 1 {
		t.Fatalf("minute response = %#v, want parsed sh600519 point", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
