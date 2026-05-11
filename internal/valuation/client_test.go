package valuation

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseFundGZ(t *testing.T) {
	got, err := ParseFundGZ(`jsonpgz({"fundcode":"000001","name":"华夏成长混合","jzrq":"2026-05-08","dwjz":"1.1960","gsz":"1.2343","gszzl":"3.20","gztime":"2026-05-11 14:12"});`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "000001" || got.Name != "华夏成长混合" {
		t.Fatalf("unexpected identity: %#v", got)
	}
	if !got.HasGSZZL || got.GSZZL != 3.20 {
		t.Fatalf("GSZZL = %v/%f, want true/3.20", got.HasGSZZL, got.GSZZL)
	}
	if !got.HasGSZ || got.GSZ != 1.2343 {
		t.Fatalf("GSZ = %v/%f, want true/1.2343", got.HasGSZ, got.GSZ)
	}
}

func TestParseNetValues(t *testing.T) {
	body := `var apidata={ content:"<table><tbody><tr><td>2026-05-08</td><td class='tor bold'>1.1960</td><td>3.7690</td><td>-1.48%</td></tr><tr><td>2026-05-07</td><td>1.2140</td><td>3.7870</td><td>1.51%</td></tr></tbody></table>",records:2,pages:1,curpage:1};`
	got := ParseNetValues(body)
	if len(got) != 2 {
		t.Fatalf("len(ParseNetValues) = %d, want 2: %#v", len(got), got)
	}
	if got[0].Date != "2026-05-07" || got[1].Date != "2026-05-08" {
		t.Fatalf("dates not sorted ascending: %#v", got)
	}
	if !got[1].HasGrowth || got[1].Growth != -1.48 {
		t.Fatalf("latest growth = %v/%f, want true/-1.48", got[1].HasGrowth, got[1].Growth)
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
