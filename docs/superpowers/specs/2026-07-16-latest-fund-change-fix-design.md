# Latest Fund Change Fix Design

## Problem

The fund list's latest-change column depends on Eastmoney's historical NAV
endpoint. The legacy `F10DataApi.aspx?type=lsjz` endpoint no longer returns a
usable response: without a fund-page `Referer` it returns HTTP 404, and with a
`Referer` it returns only the incomplete prefix `var apidata=`. The current
Eastmoney fund page instead loads JSON from `api.fund.eastmoney.com/f10/lsjz`.

`FetchQuote` currently suppresses a historical-NAV error whenever the separate
fund-estimate request succeeds. As a result, the TUI silently renders `--` in
the latest-change column instead of indicating that one part of the quote
refresh failed.

## Design

Add a dedicated Resty client for `https://api.fund.eastmoney.com`; keep the
existing `fundf10.eastmoney.com` client for fund-holdings requests. Fetch the
latest NAV records from `/f10/lsjz` with `fundCode`, `pageIndex`, `pageSize`,
`startDate`, and `endDate` query parameters and the matching fund NAV page as
the request `Referer`.

Decode the JSON response fields `FSRQ`, `DWJZ`, and `JZZZL` into the existing
internal `netValue` shape. Continue sorting records by date so callers retain
the existing oldest-to-newest contract. Treat a non-zero upstream `ErrCode`, a
missing data object, an empty NAV list, an invalid date/NAV value, or malformed
JSON as a clear fetch or decode error. An individual invalid record may be
skipped when other valid records remain.

Keep the public `Quote` shape, cache keys, TUI rendering, and JSON field names
unchanged. Change `FetchQuote` so any failed source returns both the fields
obtained from the successful source and a non-nil error. `RefreshFundQuotes`
already retains and caches a non-empty partial quote while associating the
error with the row, so the TUI will continue showing available values and add
its existing `!` marker. If both sources fail, combine both errors as today.

## Alternatives Considered

- Trying the new endpoint and then the retired endpoint would add latency and
  maintenance without a viable fallback response.
- Computing the latest percentage from adjacent NAV values could disagree with
  the source around distributions or NAV adjustments. The upstream `JZZZL`
  value remains authoritative.

## Verification

Use test-driven development for three regression surfaces:

1. Parse a representative new JSON response into sorted NAV records with the
   latest `JZZZL` value.
2. Assert the new request host, path, query parameters, and `Referer` through a
   transport-level test.
3. Assert that `FetchQuote` preserves successful estimate fields while
   returning a historical-NAV error, and likewise reports an estimate error
   while preserving valid NAV fields.

Run the focused valuation tests after each red/green cycle, then run
`make verify`. Finally, call the public new endpoint for a non-personal sample
fund and confirm that the parser produces the current NAV date and latest
change percentage.
