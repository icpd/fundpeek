package valuation

import (
	"context"
	"strings"
	"time"

	fundcache "github.com/icpd/fundpeek/internal/cache"
)

const (
	fundStockHoldingsRetryTTL        = 24 * time.Hour
	fundStockHoldingsWindowRetryTTL  = 24 * time.Hour
	fundStockHoldingsMissedWindowTTL = 7 * 24 * time.Hour
	disclosureWindowDays             = 25
)

type fundStockHoldingsFetcher interface {
	FetchFundStockHoldings(context.Context, string) (FundStockHoldings, error)
}

type ClientFuncs struct {
	FetchFundStockHoldingsFunc func(context.Context, string) (FundStockHoldings, error)
}

func (f ClientFuncs) FetchFundStockHoldings(ctx context.Context, code string) (FundStockHoldings, error) {
	return f.FetchFundStockHoldingsFunc(ctx, code)
}

type CachedClient struct {
	cache *fundcache.FileCache
	next  fundStockHoldingsFetcher
}

func NewCachedClient(cache *fundcache.FileCache, next fundStockHoldingsFetcher) *CachedClient {
	return &CachedClient{cache: cache, next: next}
}

func (c *CachedClient) FetchFundStockHoldings(ctx context.Context, code string) (FundStockHoldings, error) {
	code = strings.TrimSpace(code)
	var out FundStockHoldings
	if c.cache == nil {
		return c.next.FetchFundStockHoldings(ctx, code)
	}
	err := c.cache.GetFreshOrFetch("fund_holdings/"+code, func(entry fundcache.Entry) bool {
		return fundStockHoldingsCacheFresh(out, entry.FetchedAt, c.cache.Now())
	}, &out, func() (any, error) {
		return c.next.FetchFundStockHoldings(ctx, code)
	})
	return out, err
}

func fundStockHoldingsCacheFresh(value FundStockHoldings, fetchedAt, now time.Time) bool {
	report, ok := parseReportDate(value.ReportDate, now.Location())
	if !ok {
		return now.Sub(fetchedAt) <= fundStockHoldingsRetryTTL
	}
	windowStart, windowEnd := nextDisclosureWindow(report)
	nowDay := startOfDay(now)
	if nowDay.Before(windowStart) {
		return true
	}
	if !nowDay.After(windowEnd) {
		return now.Sub(fetchedAt) < fundStockHoldingsWindowRetryTTL
	}
	return now.Sub(fetchedAt) < fundStockHoldingsMissedWindowTTL
}

func parseReportDate(value string, loc *time.Location) (time.Time, bool) {
	report, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), loc)
	if err != nil {
		return time.Time{}, false
	}
	return startOfDay(report), true
}

func nextDisclosureWindow(report time.Time) (time.Time, time.Time) {
	next := nextQuarterEnd(report)
	start := next.AddDate(0, 0, 1)
	end := next.AddDate(0, 0, disclosureWindowDays)
	return startOfDay(start), startOfDay(end)
}

func nextQuarterEnd(report time.Time) time.Time {
	year, month, _ := report.Date()
	loc := report.Location()
	switch {
	case month <= time.March:
		return time.Date(year, time.June, 30, 0, 0, 0, 0, loc)
	case month <= time.June:
		return time.Date(year, time.September, 30, 0, 0, 0, 0, loc)
	case month <= time.September:
		return time.Date(year, time.December, 31, 0, 0, 0, 0, loc)
	default:
		return time.Date(year+1, time.March, 31, 0, 0, 0, 0, loc)
	}
}

func startOfDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}
