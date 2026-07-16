# Fund Holdings Referer Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore fund holdings refreshes and prevent upstream HTML error pages from being rendered in the TUI.

**Architecture:** Keep the existing Eastmoney client and cache/TUI data flow. Change only the holdings request metadata and its HTML-error formatting, with transport-level regression tests around the public `FetchFundStockHoldings` behavior.

**Tech Stack:** Go, Resty, `net/http`, Go `testing`

## Global Constraints

- Do not change cache keys or freshness semantics.
- Do not change list/detail navigation or refresh behavior.
- Do not add a preliminary network request or new dependency.
- Do not expose upstream HTML bodies in TUI errors.

---

### Task 1: Restore Eastmoney fund holdings requests

**Files:**
- Modify: `internal/valuation/client.go:154-169`
- Test: `internal/valuation/client_test.go`

**Interfaces:**
- Consumes: `(*Client).FetchFundStockHoldings(context.Context, string) (FundStockHoldings, error)`
- Produces: The same method and return types; only request metadata and HTML-error text change.

- [x] **Step 1: Write a failing Referer regression test**

Add a transport-level test whose `roundTripFunc` records
`r.Header.Get("Referer")`, returns a recent holdings fixture, calls
`FetchFundStockHoldings(context.Background(), "006503")`, and expects:

```go
want := "https://fundf10.eastmoney.com/ccmx_006503.html"
if gotReferer != want {
	t.Fatalf("holdings Referer = %q, want %q", gotReferer, want)
}
```

- [x] **Step 2: Run the test and verify RED**

Run:

```bash
GOCACHE=$PWD/.gocache go test ./internal/valuation -run TestFetchFundStockHoldingsSetsFundPageReferer -count=1
```

Expected: `FAIL` because the current request sends an empty `Referer`.

- [x] **Step 3: Add the request-scoped Referer**

Update the Resty request in `FetchFundStockHoldings`:

```go
referer := fmt.Sprintf("https://fundf10.eastmoney.com/ccmx_%s.html", code)
resp, err := c.f10.R().
	SetContext(ctx).
	SetHeader("Referer", referer).
	Get(path)
```

- [x] **Step 4: Run the focused test and verify GREEN**

Run the command from Step 2. Expected: `PASS`.

- [x] **Step 5: Write a failing HTML-error regression test**

Add a transport-level test returning status 404, content type `text/html`, and
body `<html>not found</html>`. Assert that the error contains
`fetch fund holdings 006503: http 404` but contains neither `<html>` nor
`not found`.

- [x] **Step 6: Run the HTML-error test and verify RED**

Run:

```bash
GOCACHE=$PWD/.gocache go test ./internal/valuation -run TestFetchFundStockHoldingsHidesHTMLErrorBody -count=1
```

Expected: `FAIL` because the current error embeds the response body.

- [x] **Step 7: Suppress only HTML error bodies**

Before the existing `SafeBody` error, check the response content type:

```go
if strings.Contains(strings.ToLower(resp.Header().Get("Content-Type")), "text/html") {
	return FundStockHoldings{}, fmt.Errorf("fetch fund holdings %s: http %d", code, resp.StatusCode())
}
```

Keep the existing body-bearing error for non-HTML responses.

- [x] **Step 8: Verify focused and full behavior**

Run:

```bash
GOCACHE=$PWD/.gocache go test ./internal/valuation -count=1
make verify
```

Expected: all tests, vet, and build succeed. Then run `./fundpeek tui`, enter a
fund detail, press `R`, and confirm current holdings load without an HTML 404.
