# Latest Fund Change Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore the fund list's latest-change data from Eastmoney's current JSON API and visibly report partial quote-refresh failures without discarding successful fields.

**Architecture:** Add a dedicated Resty client for Eastmoney's current fund JSON API while retaining the existing F10 client for holdings. Normalize the new `FSRQ`, `DWJZ`, and `JZZZL` fields into the existing `netValue` and `Quote` models, then make `FetchQuote` return partial data together with any single-source error so existing TUI error rendering can show `!`.

**Tech Stack:** Go 1.25, Resty, `encoding/json`, `net/http`, Go `testing`

## Global Constraints

- Preserve the `fund_quote/<code>` cache key and existing cache behavior.
- Preserve the public `Quote` structure and JSON export field names.
- Keep fund holdings on `fundf10.eastmoney.com`; only historical NAV moves to `api.fund.eastmoney.com`.
- Keep successfully fetched quote fields even when the other upstream source fails.
- Do not add dependencies or change TUI navigation and refresh key semantics.

---

### Task 1: Parse and fetch the current Eastmoney NAV JSON

**Files:**
- Modify: `internal/valuation/client.go:18-24,101-110,268-368`
- Test: `internal/valuation/client_test.go:13-42`

**Interfaces:**
- Consumes: `(*Client).fetchLatestNetValues(context.Context, string, int) ([]netValue, error)`
- Produces: `ParseNetValues(string) ([]netValue, error)` and a `Client.fundAPI *resty.Client` initialized by `NewClient`

- [ ] **Step 1: Replace the old HTML parser test with a failing JSON parser test**

Update `TestParseNetValues` to use the current response shape and require sorted values plus the latest source-provided change percentage:

```go
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
```

- [ ] **Step 2: Run the parser test and verify RED**

Run:

```bash
GOCACHE=$PWD/.gocache go test ./internal/valuation -run '^TestParseNetValues$' -count=1
```

Expected: build failure because the existing `ParseNetValues` returns only one value and parses the retired HTML response shape.

- [ ] **Step 3: Implement the JSON parser**

Change `ParseNetValues` to return `([]netValue, error)`, decode the response, reject explicit API failures, skip malformed records, and retain the existing ascending-date contract:

```go
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
```

Remove the now-unused `extractF10Content` call from this code path; keep the helper itself because fund-holdings parsing still uses it.

- [ ] **Step 4: Run the parser test and verify GREEN**

Run the command from Step 2.

Expected: `PASS`.

- [ ] **Step 5: Write a failing transport-level request test**

Add a test that captures the current JSON request and returns a valid response:

```go
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
```

Add `net/url` to the test imports.

- [ ] **Step 6: Run the request test and verify RED**

Run:

```bash
GOCACHE=$PWD/.gocache go test ./internal/valuation -run '^TestFetchLatestNetValuesUsesCurrentJSONAPI$' -count=1
```

Expected: `FAIL` because `Client` has no `fundAPI` field and the implementation still requests `F10DataApi.aspx` from the holdings host.

- [ ] **Step 7: Add the dedicated client and current request**

Extend `Client` and `NewClient`:

```go
type Client struct {
	fundgz *resty.Client
	f10    *resty.Client
	fundAPI *resty.Client
	minute *resty.Client
}

func NewClient() *Client {
	return &Client{
		fundgz:  httpclient.New("https://fundgz.1234567.com.cn"),
		f10:     httpclient.New("https://fundf10.eastmoney.com"),
		fundAPI: httpclient.New("https://api.fund.eastmoney.com"),
		minute:  httpclient.New("https://proxy.finance.qq.com"),
	}
}
```

Replace `fetchLatestNetValues` with the JSON request and parser call:

```go
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
```

- [ ] **Step 8: Format and verify Task 1**

Run:

```bash
gofmt -w internal/valuation/client.go internal/valuation/client_test.go
GOCACHE=$PWD/.gocache go test ./internal/valuation -run 'TestParseNetValues|TestFetchLatestNetValuesUsesCurrentJSONAPI' -count=1
```

Expected: both tests `PASS`.

- [ ] **Step 9: Commit Task 1**

```bash
git add internal/valuation/client.go internal/valuation/client_test.go
git commit -m "Use current fund NAV API"
```

---

### Task 2: Surface partial quote failures without discarding data

**Files:**
- Modify: `internal/valuation/client.go:112-152`
- Test: `internal/valuation/client_test.go`

**Interfaces:**
- Consumes: `(*Client).FetchQuote(context.Context, string) (Quote, error)` and the two Resty clients implemented in Task 1
- Produces: the same `FetchQuote` signature with partial-data-plus-error semantics

- [ ] **Step 1: Write failing tests for both partial-failure directions**

Add a small response helper and two tests:

```go
func restyResponse(r *http.Request, status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

func TestFetchQuoteReturnsEstimateWithNetValueError(t *testing.T) {
	gz := resty.New().SetBaseURL("https://fundgz.1234567.com.cn").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return restyResponse(r, http.StatusOK, "application/javascript", `jsonpgz({"fundcode":"000001","name":"华夏成长混合","jzrq":"2026-07-15","dwjz":"1.4600","gsz":"1.4366","gszzl":"-1.60","gztime":"2026-07-16 11:30"});`), nil
	}))
	fundAPI := resty.New().SetBaseURL("https://api.fund.eastmoney.com").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return restyResponse(r, http.StatusBadGateway, "application/json", `{"message":"upstream unavailable"}`), nil
	}))

	quote, err := (&Client{fundgz: gz, fundAPI: fundAPI}).FetchQuote(context.Background(), "000001")
	if err == nil || !strings.Contains(err.Error(), "fetch net values 000001: http 502") {
		t.Fatalf("error = %v, want net-value HTTP error", err)
	}
	if !quote.HasGSZ || quote.GSZ != 1.4366 || !quote.HasGSZZL || quote.GSZZL != -1.60 {
		t.Fatalf("quote = %#v, want preserved estimate fields", quote)
	}
}

func TestFetchQuoteReturnsNetValueWithEstimateError(t *testing.T) {
	gz := resty.New().SetBaseURL("https://fundgz.1234567.com.cn").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return restyResponse(r, http.StatusBadGateway, "application/javascript", "unavailable"), nil
	}))
	fundAPI := resty.New().SetBaseURL("https://api.fund.eastmoney.com").SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return restyResponse(r, http.StatusOK, "application/json", `{"Data":{"LSJZList":[{"FSRQ":"2026-07-15","DWJZ":"1.4600","JZZZL":"-3.31"},{"FSRQ":"2026-07-14","DWJZ":"1.5100","JZZZL":"1.27"}]},"ErrCode":0}`), nil
	}))

	quote, err := (&Client{fundgz: gz, fundAPI: fundAPI}).FetchQuote(context.Background(), "000001")
	if err == nil || !strings.Contains(err.Error(), "fetch fundgz 000001: http 502") {
		t.Fatalf("error = %v, want estimate HTTP error", err)
	}
	if !quote.HasDWJZ || quote.DWJZ != 1.46 || !quote.HasZZL || quote.ZZL != -3.31 || !quote.HasLastNAV || quote.LastNAV != 1.51 {
		t.Fatalf("quote = %#v, want preserved NAV fields", quote)
	}
}
```

- [ ] **Step 2: Run the partial-failure tests and verify RED**

Run:

```bash
GOCACHE=$PWD/.gocache go test ./internal/valuation -run '^TestFetchQuoteReturns(EstimateWithNetValueError|NetValueWithEstimateError)$' -count=1
```

Expected: both tests `FAIL` because the current `FetchQuote` returns `nil` when either source succeeds.

- [ ] **Step 3: Return each single-source error after merging successful fields**

Replace the final error selection in `FetchQuote` with:

```go
	if gzErr != nil && navErr != nil {
		return quote, fmt.Errorf("%v; %v", gzErr, navErr)
	}
	if gzErr != nil {
		return quote, gzErr
	}
	if navErr != nil {
		return quote, navErr
	}
	return quote, nil
```

Remove the old `!quote.HasGSZ && !quote.HasDWJZ` conditional because source errors are now reported independently of which partial fields exist.

- [ ] **Step 4: Format and verify Task 2**

Run:

```bash
gofmt -w internal/valuation/client.go internal/valuation/client_test.go
GOCACHE=$PWD/.gocache go test ./internal/valuation -run 'TestFetchQuote|TestParseNetValues|TestFetchLatestNetValues' -count=1
```

Expected: all matching tests `PASS`.

- [ ] **Step 5: Verify the existing TUI partial-error path**

Run:

```bash
GOCACHE=$PWD/.gocache go test ./internal/tui -run 'TestRefreshFundQuotesStoresCache|TestRenderTable' -count=1
```

Expected: all matching tests `PASS`, confirming partial quotes are still cached and rows with errors retain the existing marker behavior.

- [ ] **Step 6: Commit Task 2**

```bash
git add internal/valuation/client.go internal/valuation/client_test.go
git commit -m "Report partial fund quote failures"
```

---

### Task 3: Sweep sibling paths and verify the completed fix

**Files:**
- Verify: `internal/valuation/client.go`
- Verify: `internal/valuation/client_test.go`
- Verify: `internal/tui/app.go`
- Verify: `internal/jsonexport/export.go`

**Interfaces:**
- Consumes: the completed current NAV client and partial-error semantics from Tasks 1 and 2
- Produces: verified repository behavior with no additional interface changes

- [ ] **Step 1: Sweep for retired endpoint and partial-error patterns**

Run:

```bash
rg -n 'F10DataApi\.aspx\?type=lsjz|fetchLatestNetValues|ParseNetValues|gzErr|navErr' --glob '*.go' .
```

Expected: no retired `type=lsjz` URL remains; every parser/fetch caller matches the new signature; the only combined estimate/NAV error selection is the updated `FetchQuote` path.

- [ ] **Step 2: Run the complete repository verification**

Run:

```bash
make verify
```

Expected: `go test ./...`, `go vet ./...`, and the `fundpeek` build all succeed.

- [ ] **Step 3: Confirm the live upstream contract with a public sample fund**

Run:

```bash
curl -fsS --max-time 15 \
  -A 'Mozilla/5.0' \
  -e 'https://fundf10.eastmoney.com/jjjz_000001.html' \
  'https://api.fund.eastmoney.com/f10/lsjz?fundCode=000001&pageIndex=1&pageSize=3&startDate=&endDate=' \
  -o /tmp/fundpeek-lsjz.json
wc -c /tmp/fundpeek-lsjz.json
rg -o '"ErrCode":0|"FSRQ":"[^"]+"|"DWJZ":"[^"]+"|"JZZZL":"[^"]+"' /tmp/fundpeek-lsjz.json
```

Expected: the response is non-empty, `ErrCode` is `0`, and the first record
contains non-empty `FSRQ`, `DWJZ`, and `JZZZL`. Do not print or inspect
personal portfolio data.

- [ ] **Step 4: Review the final diff**

Run:

```bash
git diff HEAD~2 --check
git diff HEAD~2 -- internal/valuation/client.go internal/valuation/client_test.go
git status --short --branch -uall
```

Expected: only the scoped valuation implementation and tests changed after the plan commit; no credentials, caches, generated state, or unrelated churn are present.
