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
	"github.com/icpd/fundsync/internal/httpclient"
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

func parseNumber(value string) (float64, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	if value == "" || value == "--" || value == "—" {
		return 0, false
	}
	n, err := strconv.ParseFloat(value, 64)
	return n, err == nil
}
