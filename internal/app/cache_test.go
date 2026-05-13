package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	fundcache "github.com/icpd/fundpeek/internal/cache"
	"github.com/icpd/fundpeek/internal/config"
	"github.com/icpd/fundpeek/internal/credential"
	"github.com/icpd/fundpeek/internal/model"
	"github.com/icpd/fundpeek/internal/real"
	"github.com/icpd/fundpeek/internal/valuation"
)

type fakeRealClient struct {
	fetches     int
	data        map[string]any
	updates     int
	updatedData map[string]any
}

func (f *fakeRealClient) SendOTP(context.Context, string) error { return nil }
func (f *fakeRealClient) VerifyOTP(context.Context, string, string) (model.RealCredential, error) {
	return model.RealCredential{}, nil
}
func (f *fakeRealClient) Refresh(_ context.Context, cred model.RealCredential) (model.RealCredential, error) {
	return cred, nil
}
func (f *fakeRealClient) FetchUserConfig(context.Context, model.RealCredential) (real.UserConfig, error) {
	f.fetches++
	return real.UserConfig{UserID: "u1", Data: f.data, Exists: true, UpdatedAt: "remote-1"}, nil
}
func (f *fakeRealClient) UpsertUserConfig(context.Context, model.RealCredential, map[string]any) error {
	return nil
}
func (f *fakeRealClient) UpdateUserConfigIfUnchanged(_ context.Context, _ model.RealCredential, _ real.UserConfig, data map[string]any) error {
	f.updates++
	cloned, err := real.CloneData(data)
	if err != nil {
		return err
	}
	f.updatedData = cloned
	return nil
}

func TestRealDataUsesFreshCache(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, fake := newCacheTestApp(t, now)
	a.cache.Set("real_data", map[string]any{"funds": []any{map[string]any{"code": "000001"}}})

	got, err := a.RealData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fake.fetches != 0 {
		t.Fatalf("FetchUserConfig calls = %d, want 0", fake.fetches)
	}
	if len(got["funds"].([]any)) != 1 {
		t.Fatalf("unexpected cached data: %#v", got)
	}
}

func TestRealDataFetchesRemoteWhenCacheMissingAndStoresResult(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, fake := newCacheTestApp(t, now)

	got, err := a.RealData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fake.fetches != 1 {
		t.Fatalf("FetchUserConfig calls = %d, want 1", fake.fetches)
	}
	if got["funds"].([]any)[0].(map[string]any)["code"] != "fresh" {
		t.Fatalf("unexpected remote data: %#v", got)
	}

	fake.data = map[string]any{"funds": []any{map[string]any{"code": "new-remote"}}}
	got, err = a.RealData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fake.fetches != 1 {
		t.Fatalf("FetchUserConfig calls after cached read = %d, want 1", fake.fetches)
	}
	if got["funds"].([]any)[0].(map[string]any)["code"] != "fresh" {
		t.Fatalf("RealData should return stored cache after first fetch: %#v", got)
	}
}

func TestPushRealUpdatesRealDataCache(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, fake := newCacheTestApp(t, now)
	if err := a.cache.Set("real_data", map[string]any{"funds": []any{map[string]any{"code": "stale"}}}); err != nil {
		t.Fatal(err)
	}
	if err := a.setPortfolioDataCache(testPortfolioData("000001", model.SourceYangJiBao)); err != nil {
		t.Fatal(err)
	}

	if err := a.PushReal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.updates != 1 {
		t.Fatalf("updates = %d, want 1", fake.updates)
	}

	got, err := a.RealData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fake.fetches != 1 {
		t.Fatalf("FetchUserConfig calls = %d, want 1", fake.fetches)
	}
	if !fundsContainCode(got["funds"], "000001") {
		t.Fatalf("cache was not updated from synced data: %#v", got)
	}
}

func TestSyncUpdatesPortfolioDataWithoutUpdatingReal(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, fake := newCacheTestApp(t, now)
	a.fetchYangJiBaoInput = func(context.Context) (model.SyncInput, error) {
		return model.SyncInput{
			Source: model.SourceYangJiBao,
			Accounts: []model.NormalizedAccount{{
				Source:            model.SourceYangJiBao,
				ExternalAccountID: "acc1",
				Name:              "账户",
			}},
			Holdings: []model.NormalizedHolding{{
				Source:            model.SourceYangJiBao,
				ExternalAccountID: "acc1",
				FundCode:          "000001",
				FundName:          "华夏成长",
				Share:             100,
			}},
		}, nil
	}

	if err := a.Sync(context.Background(), model.SourceYangJiBao); err != nil {
		t.Fatal(err)
	}
	if fake.updates != 0 {
		t.Fatalf("real updates = %d, want 0", fake.updates)
	}
	got, ok, err := a.CachedPortfolioData()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !fundsContainCode(got["funds"], "000001") {
		t.Fatalf("portfolio data = %#v ok=%v, want synced fund", got, ok)
	}
}

func TestPushRealUsesPortfolioData(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, fake := newCacheTestApp(t, now)
	if err := a.setPortfolioDataCache(testPortfolioData("000001", model.SourceYangJiBao)); err != nil {
		t.Fatal(err)
	}

	if err := a.PushReal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.updates != 1 {
		t.Fatalf("real updates = %d, want 1", fake.updates)
	}
}

func TestPushRealPreservesRemoteManualGroups(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, fake := newCacheTestApp(t, now)
	fake.data = map[string]any{
		"funds": []any{map[string]any{"code": "000999", "name": "手动基金"}},
		"groups": []any{
			map[string]any{"id": "manual", "name": "手动", "codes": []any{"000999"}},
			map[string]any{"id": "import_yangjibao_old", "name": "旧来源", "codes": []any{"000888"}},
		},
		"groupHoldings": map[string]any{
			"manual":               map[string]any{"000999": map[string]any{"share": 10, "cost": 1}},
			"import_yangjibao_old": map[string]any{"000888": map[string]any{"share": 1, "cost": 1}},
		},
	}
	if err := a.setPortfolioDataCache(testPortfolioData("000001", model.SourceYangJiBao)); err != nil {
		t.Fatal(err)
	}

	if err := a.PushReal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !hasGroup(fake.updatedData["groups"], "manual") {
		t.Fatalf("updated real data should preserve manual groups: %#v", fake.updatedData)
	}
	if !hasGroup(fake.updatedData["groups"], "import_yangjibao_default") {
		t.Fatalf("updated real data should include local import group: %#v", fake.updatedData)
	}
	if hasGroup(fake.updatedData["groups"], "import_yangjibao_old") {
		t.Fatalf("updated real data should replace old import groups: %#v", fake.updatedData)
	}
}

func TestPushRealRejectsEmptyPortfolio(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, fake := newCacheTestApp(t, now)
	if err := a.setPortfolioDataCache(map[string]any{}); err != nil {
		t.Fatal(err)
	}

	err := a.PushReal(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no local portfolio data") {
		t.Fatalf("PushReal err = %v, want empty portfolio error", err)
	}
	if fake.updates != 0 {
		t.Fatalf("real updates = %d, want 0", fake.updates)
	}
}

func TestSyncAllKeepsFailedSourcePortfolioData(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, _ := newCacheTestApp(t, now)
	if err := a.setPortfolioDataCache(testPortfolioData("000002", model.SourceXiaoBei)); err != nil {
		t.Fatal(err)
	}
	a.fetchYangJiBaoInput = func(context.Context) (model.SyncInput, error) {
		return model.SyncInput{
			Source: model.SourceYangJiBao,
			Accounts: []model.NormalizedAccount{{
				Source:            model.SourceYangJiBao,
				ExternalAccountID: "acc1",
				Name:              "账户",
			}},
			Holdings: []model.NormalizedHolding{{
				Source:            model.SourceYangJiBao,
				ExternalAccountID: "acc1",
				FundCode:          "000001",
				FundName:          "华夏成长",
				Share:             100,
			}},
		}, nil
	}
	a.fetchXiaoBeiInput = func(context.Context) (model.SyncInput, error) {
		return model.SyncInput{}, errors.New("xiaobei unavailable")
	}

	err := a.Sync(context.Background(), "all")
	if err != nil {
		t.Fatalf("Sync err = %v, want best-effort success", err)
	}
	got, ok, err := a.CachedPortfolioData()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !fundsContainCode(got["funds"], "000001") || !fundsContainCode(got["funds"], "000002") {
		t.Fatalf("portfolio data after partial sync = %#v ok=%v, want successful and retained failed source", got, ok)
	}
}

func TestRefreshPortfolioReturnsPartialSyncWarning(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, _ := newCacheTestApp(t, now)
	a.fetchYangJiBaoInput = func(context.Context) (model.SyncInput, error) {
		return model.SyncInput{
			Source: model.SourceYangJiBao,
			Accounts: []model.NormalizedAccount{{
				Source:            model.SourceYangJiBao,
				ExternalAccountID: "acc1",
				Name:              "账户",
			}},
			Holdings: []model.NormalizedHolding{{
				Source:            model.SourceYangJiBao,
				ExternalAccountID: "acc1",
				FundCode:          "000001",
				FundName:          "华夏成长",
				Share:             100,
			}},
		}, nil
	}
	a.fetchXiaoBeiInput = func(context.Context) (model.SyncInput, error) {
		return model.SyncInput{}, errors.New("xiaobei unavailable")
	}

	err := a.RefreshPortfolio(context.Background())
	if err == nil || !strings.Contains(err.Error(), "xiaobei unavailable") {
		t.Fatalf("RefreshPortfolio err = %v, want partial warning", err)
	}
	var partial PartialSyncError
	if !errors.As(err, &partial) {
		t.Fatalf("RefreshPortfolio err = %T, want PartialSyncError", err)
	}
}

func TestPortfolioDataDoesNotReadLegacyRealData(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, _ := newCacheTestApp(t, now)
	if err := a.cache.Set("real_data", testPortfolioData("000001", model.SourceYangJiBao)); err != nil {
		t.Fatal(err)
	}

	got, ok, err := a.CachedPortfolioData()
	if err != nil {
		t.Fatal(err)
	}
	if ok || got != nil {
		t.Fatalf("portfolio data = %#v ok=%v, want no legacy fallback", got, ok)
	}
}

func fundsContainCode(value any, code string) bool {
	for _, item := range value.([]any) {
		if item.(map[string]any)["code"] == code {
			return true
		}
	}
	return false
}

func TestInvalidateFundStockHoldingsCache(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, _ := newCacheTestApp(t, now)
	if err := a.cache.Set("fund_holdings/000001", map[string]any{"report": "stale"}); err != nil {
		t.Fatal(err)
	}

	a.InvalidateFundStockHoldings("000001")

	var got map[string]any
	err := a.cache.GetOrFetch("fund_holdings/000001", time.Hour, &got, func() (any, error) {
		return map[string]any{"report": "fresh"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["report"] != "fresh" {
		t.Fatalf("report = %v, want fresh", got["report"])
	}
}

func TestQuoteCacheAccessors(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, _ := newCacheTestApp(t, now)

	if err := a.SetFundQuote("000001", valuation.Quote{Code: "000001", GSZZL: 1.23, HasGSZZL: true}); err != nil {
		t.Fatal(err)
	}
	gotFund, ok, err := a.CachedFundQuote("000001")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || gotFund.GSZZL != 1.23 {
		t.Fatalf("cached fund quote = %#v ok=%v, want stored quote", gotFund, ok)
	}

	if err := a.SetStockQuote("sh600519", valuation.StockQuote{Code: "sh600519", Price: 1800, HasPrice: true}); err != nil {
		t.Fatal(err)
	}
	gotStock, ok, err := a.CachedStockQuote("sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || gotStock.Price != 1800 {
		t.Fatalf("cached stock quote = %#v ok=%v, want stored quote", gotStock, ok)
	}
}

func TestCachedRealDataDoesNotRequireCredentials(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, _ := newCacheTestApp(t, now)
	if err := a.cache.Set("real_data", map[string]any{"funds": []any{map[string]any{"code": "000001"}}}); err != nil {
		t.Fatal(err)
	}
	a.store = nil

	got, ok, err := a.CachedRealData()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got["funds"].([]any)[0].(map[string]any)["code"] != "000001" {
		t.Fatalf("cached real data = %#v ok=%v, want cached data", got, ok)
	}
}

func TestInvalidateFundQuoteCache(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, _ := newCacheTestApp(t, now)
	if err := a.SetFundQuote("000001", valuation.Quote{Code: "000001", GSZZL: 1.23, HasGSZZL: true}); err != nil {
		t.Fatal(err)
	}

	a.InvalidateFundQuote("000001")

	_, ok, err := a.CachedFundQuote("000001")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("force refresh should invalidate fund quote cache")
	}
}

func TestInvalidateStockQuoteCache(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, _ := newCacheTestApp(t, now)
	if err := a.SetStockQuote("sh600519", valuation.StockQuote{Code: "sh600519", Price: 1800, HasPrice: true}); err != nil {
		t.Fatal(err)
	}

	a.InvalidateStockQuote("sh600519")

	_, ok, err := a.CachedStockQuote("sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("force refresh should invalidate stock quote cache")
	}
}

func newCacheTestApp(t *testing.T, now time.Time) (*App, *fakeRealClient) {
	t.Helper()
	dir := t.TempDir()
	store, err := credential.NewFileStore(dir + "/credentials.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReal(model.RealCredential{
		UserID:      "u1",
		AccessToken: "token",
		ExpiresAt:   now.Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRealClient{data: map[string]any{"funds": []any{map[string]any{"code": "fresh"}}}}
	a := New(config.Config{
		SupabaseURL:    "https://example.com",
		SupabaseAnon:   "anon",
		DeviceID:       "device",
		ConfigDir:      dir,
		CredentialPath: dir + "/credentials.json",
		CacheDir:       dir + "/cache",
	}, store)
	a.real = fake
	a.cache = fundcache.NewFileCache(dir+"/cache", func() time.Time { return now })
	return a, fake
}

func testPortfolioData(code, source string) map[string]any {
	return map[string]any{
		"funds": []any{
			map[string]any{"code": code, "name": "测试基金"},
		},
		"groups": []any{
			map[string]any{"id": "import_" + source + "_default", "name": "默认账户", "codes": []any{code}},
		},
		"groupHoldings": map[string]any{
			"import_" + source + "_default": map[string]any{
				code: map[string]any{"share": 100, "cost": 1.23},
			},
		},
	}
}

func hasGroup(value any, id string) bool {
	for _, item := range value.([]any) {
		if item.(map[string]any)["id"] == id {
			return true
		}
	}
	return false
}
