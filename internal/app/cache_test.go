package app

import (
	"context"
	"testing"
	"time"

	fundcache "github.com/icpd/fundpeek/internal/cache"
	"github.com/icpd/fundpeek/internal/config"
	"github.com/icpd/fundpeek/internal/credential"
	"github.com/icpd/fundpeek/internal/model"
	"github.com/icpd/fundpeek/internal/real"
)

type fakeRealClient struct {
	fetches int
	data    map[string]any
	updates int
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
func (f *fakeRealClient) UpdateUserConfigIfUnchanged(context.Context, model.RealCredential, real.UserConfig, map[string]any) error {
	f.updates++
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

func TestApplySyncInvalidatesRealDataCache(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	a, fake := newCacheTestApp(t, now)
	if err := a.cache.Set("real_data", map[string]any{"funds": []any{map[string]any{"code": "stale"}}}); err != nil {
		t.Fatal(err)
	}

	err := a.applySync(context.Background(), []model.SyncInput{{
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
	}})
	if err != nil {
		t.Fatal(err)
	}
	if fake.updates != 1 {
		t.Fatalf("updates = %d, want 1", fake.updates)
	}

	got, err := a.RealData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fake.fetches != 2 {
		t.Fatalf("FetchUserConfig calls = %d, want 2", fake.fetches)
	}
	if got["funds"].([]any)[0].(map[string]any)["code"] == "stale" {
		t.Fatalf("cache was not invalidated: %#v", got)
	}
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
		BackupDir:      dir + "/backups",
		CredentialPath: dir + "/credentials.json",
		CacheDir:       dir + "/cache",
	}, store)
	a.real = fake
	a.cache = fundcache.NewFileCache(dir+"/cache", func() time.Time { return now })
	return a, fake
}
