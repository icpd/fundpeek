# Fund Holdings Referer Fix Design

## Problem

The Eastmoney `FundArchivesDatas.aspx` holdings endpoint now returns an HTML
404 response when a request does not include the fund holdings page as its
`Referer`. `fundpeek` sends only its shared user agent, so entering a fund
detail page shows cached rows and then replaces the status area with a
truncated HTML error when the background refresh fails.

## Design

Keep the existing endpoint, parser, caching policy, and stale-snapshot-first
TUI flow. Add a request-scoped `Referer` to `FetchFundStockHoldings`, using
`https://fundf10.eastmoney.com/ccmx_<fund-code>.html`. Do not add a preliminary
page request or cookie exchange because live probes show that `Referer` alone
is sufficient.

For non-success HTML responses from this endpoint, report the operation, fund
code, and HTTP status without embedding the HTML response body. Preserve the
existing redacted body detail for non-HTML responses so useful upstream error
messages remain available.

## Verification

Add transport-level tests that assert the exact `Referer` value and prove an
HTML 404 body is not exposed in the returned error. Run the focused valuation
tests, the repository-wide test suite, vet, and build. Finally, force-refresh a
real fund detail in the TUI and confirm that current holdings load without the
404 page.
