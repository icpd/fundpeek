package valuation

import (
	"context"
	"strings"
	"time"

	fundcache "github.com/icpd/fundpeek/internal/cache"
)

const FundStockHoldingsTTL = 7 * 24 * time.Hour

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
	err := c.cache.GetOrFetch("fund_holdings/"+code, FundStockHoldingsTTL, &out, func() (any, error) {
		return c.next.FetchFundStockHoldings(ctx, code)
	})
	return out, err
}
