package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	fundcache "github.com/icpd/fundpeek/internal/cache"
	"github.com/icpd/fundpeek/internal/config"
	"github.com/icpd/fundpeek/internal/console"
	"github.com/icpd/fundpeek/internal/credential"
	"github.com/icpd/fundpeek/internal/merge"
	"github.com/icpd/fundpeek/internal/model"
	"github.com/icpd/fundpeek/internal/real"
	"github.com/icpd/fundpeek/internal/sources/xiaobei"
	"github.com/icpd/fundpeek/internal/sources/yangjibao"
	"github.com/icpd/fundpeek/internal/valuation"
)

type realClient interface {
	SendOTP(context.Context, string) error
	VerifyOTP(context.Context, string, string) (model.RealCredential, error)
	Refresh(context.Context, model.RealCredential) (model.RealCredential, error)
	FetchUserConfig(context.Context, model.RealCredential) (real.UserConfig, error)
	UpsertUserConfig(context.Context, model.RealCredential, map[string]any) error
	UpdateUserConfigIfUnchanged(context.Context, model.RealCredential, real.UserConfig, map[string]any) error
}

type App struct {
	cfg   config.Config
	store *credential.FileStore
	real  realClient
	cache *fundcache.FileCache

	fetchYangJiBaoInput func(context.Context) (model.SyncInput, error)
	fetchXiaoBeiInput   func(context.Context) (model.SyncInput, error)
}

type PartialSyncError struct {
	Err error
}

func (e PartialSyncError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e PartialSyncError) Unwrap() error {
	return e.Err
}

func New(cfg config.Config, store *credential.FileStore) *App {
	a := &App{
		cfg:   cfg,
		store: store,
		real:  real.NewClient(cfg.SupabaseURL, cfg.SupabaseAnon, cfg.DeviceID),
		cache: fundcache.NewFileCache(cfg.CacheDir, time.Now),
	}
	a.fetchYangJiBaoInput = a.fetchYangJiBao
	a.fetchXiaoBeiInput = a.fetchXiaoBei
	return a
}

func (a *App) AuthRealStart(ctx context.Context, email string) error {
	if err := a.SendRealOTP(ctx, email); err != nil {
		return err
	}
	fmt.Println("real OTP sent")
	return nil
}

func (a *App) SendRealOTP(ctx context.Context, email string) error {
	if strings.TrimSpace(email) == "" {
		return fmt.Errorf("email is required")
	}
	if err := a.real.SendOTP(ctx, email); err != nil {
		return err
	}
	return nil
}

func (a *App) AuthRealVerify(ctx context.Context, email, token string) error {
	if err := a.VerifyRealOTP(ctx, email, token); err != nil {
		return err
	}
	fmt.Println("real authenticated")
	return nil
}

func (a *App) VerifyRealOTP(ctx context.Context, email, token string) error {
	if strings.TrimSpace(email) == "" || strings.TrimSpace(token) == "" {
		return fmt.Errorf("email and token are required")
	}
	cred, err := a.real.VerifyOTP(ctx, email, token)
	if err != nil {
		return err
	}
	if err := a.store.SaveReal(cred); err != nil {
		return err
	}
	return nil
}

func (a *App) AuthYangJiBao(ctx context.Context) error {
	if !console.IsTerminal(os.Stdout) {
		return fmt.Errorf("yangjibao qr login requires an interactive terminal")
	}

	qr, err := a.NewYangJiBaoQRCode(ctx)
	if err != nil {
		return err
	}

	fmt.Println("养基宝扫码登录")
	fmt.Println()
	console.PrintQR(os.Stdout, qr.QRURL)
	fmt.Println()

	authCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()
	statusTicker := time.NewTicker(time.Second)
	defer statusTicker.Stop()

	spinner := console.NewSpinner(os.Stdout, true)
	spinner.Update("等待扫码")
	for {
		select {
		case <-authCtx.Done():
			spinner.Done("二维码已超时，请重新执行：fundpeek auth yjb")
			return fmt.Errorf("yangjibao qr timed out")
		case <-statusTicker.C:
			spinner.Update("等待扫码")
		case <-pollTicker.C:
			state, err := a.CheckYangJiBaoQRCode(authCtx, qr.QRID)
			if err != nil {
				spinner.Clear()
				return err
			}
			switch state.State {
			case yangjibao.StateConfirmed:
				if state.Token == "" {
					spinner.Clear()
					return fmt.Errorf("yangjibao confirmed but token is empty")
				}
				if err := a.SaveYangJiBaoToken(state.Token); err != nil {
					spinner.Clear()
					return err
				}
				spinner.Done("养基宝已授权")
				return nil
			case yangjibao.StateExpired:
				spinner.Done("二维码已超时，请重新执行：fundpeek auth yjb")
				return fmt.Errorf("yangjibao qr timed out")
			}
		}
	}
}

func (a *App) NewYangJiBaoQRCode(ctx context.Context) (yangjibao.QRCode, error) {
	return yangjibao.NewClient("").GetQRCode(ctx)
}

func (a *App) CheckYangJiBaoQRCode(ctx context.Context, qrID string) (yangjibao.QRCodeState, error) {
	return yangjibao.NewClient("").CheckQRCodeState(ctx, qrID)
}

func (a *App) SaveYangJiBaoToken(token string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("yangjibao token is required")
	}
	return a.store.SaveYangJiBao(model.YangJiBaoCredential{Token: token})
}

func (a *App) AuthXiaoBeiStart(ctx context.Context, phone string) error {
	if err := a.SendXiaoBeiSMS(ctx, phone); err != nil {
		return err
	}
	fmt.Println("xiaobei sms sent")
	return nil
}

func (a *App) SendXiaoBeiSMS(ctx context.Context, phone string) error {
	if strings.TrimSpace(phone) == "" {
		return fmt.Errorf("phone is required")
	}
	client := xiaobei.NewClient("", "")
	if err := client.SendSMS(ctx, phone); err != nil {
		return err
	}
	return nil
}

func (a *App) AuthXiaoBeiVerify(ctx context.Context, phone, code string) error {
	if err := a.VerifyXiaoBeiSMS(ctx, phone, code); err != nil {
		return err
	}
	fmt.Println("xiaobei authenticated")
	return nil
}

func (a *App) VerifyXiaoBeiSMS(ctx context.Context, phone, code string) error {
	if strings.TrimSpace(phone) == "" || strings.TrimSpace(code) == "" {
		return fmt.Errorf("phone and code are required")
	}
	client := xiaobei.NewClient("", "")
	token, unionID, err := client.VerifyPhone(ctx, phone, code)
	if err != nil {
		return err
	}
	if err := a.store.SaveXiaoBei(model.XiaoBeiCredential{AccessToken: token, UnionID: unionID}); err != nil {
		return err
	}
	return nil
}

func (a *App) Status(ctx context.Context) error {
	_ = ctx
	printCredentialStatus("real", func() error {
		_, err := a.store.GetReal()
		return err
	})
	printCredentialStatus("yangjibao", func() error {
		_, err := a.store.GetYangJiBao()
		return err
	})
	printCredentialStatus("xiaobei", func() error {
		_, err := a.store.GetXiaoBei()
		return err
	})
	return nil
}

func (a *App) Sync(ctx context.Context, source string) error {
	if source == "" {
		source = "all"
	}
	switch source {
	case model.SourceYangJiBao:
		input, err := a.fetchYangJiBaoInput(ctx)
		if err != nil {
			return err
		}
		return a.applyLocalSync([]model.SyncInput{input}, nil)
	case model.SourceXiaoBei:
		input, err := a.fetchXiaoBeiInput(ctx)
		if err != nil {
			return err
		}
		return a.applyLocalSync([]model.SyncInput{input}, nil)
	case "all":
		var inputs []model.SyncInput
		var errs []error
		if yjb, err := a.fetchYangJiBaoInput(ctx); err != nil {
			errs = append(errs, err)
		} else {
			inputs = append(inputs, yjb)
		}
		if xb, err := a.fetchXiaoBeiInput(ctx); err != nil {
			errs = append(errs, err)
		} else {
			inputs = append(inputs, xb)
		}
		err := a.applyLocalSync(inputs, errs)
		var partial PartialSyncError
		if errors.As(err, &partial) {
			fmt.Println("warning:", partial.Error())
			return nil
		}
		return err
	default:
		return fmt.Errorf("unknown sync source %q", source)
	}
}

func (a *App) RealData(ctx context.Context) (map[string]any, error) {
	cred, err := a.realCred(ctx)
	if err != nil {
		return nil, err
	}
	if a.cache == nil {
		cfg, err := a.real.FetchUserConfig(ctx, cred)
		if err != nil {
			return nil, err
		}
		return real.CloneData(cfg.Data)
	}
	var data map[string]any
	if _, ok, err := a.cache.Get("real_data", &data); err != nil {
		return nil, err
	} else if ok {
		return real.CloneData(data)
	}
	cfg, err := a.real.FetchUserConfig(ctx, cred)
	if err != nil {
		return nil, err
	}
	data, err = real.CloneData(cfg.Data)
	if err != nil {
		return nil, err
	}
	a.setRealDataCache(data)
	return real.CloneData(data)
}

func (a *App) CachedRealData() (map[string]any, bool, error) {
	if a.cache == nil {
		return nil, false, nil
	}
	var data map[string]any
	if _, ok, err := a.cache.Get("real_data", &data); err != nil {
		return nil, false, err
	} else if ok {
		cloned, err := real.CloneData(data)
		if err != nil {
			return nil, false, err
		}
		return cloned, true, nil
	}
	return nil, false, nil
}

func (a *App) PortfolioData(ctx context.Context) (map[string]any, error) {
	if data, ok, err := a.CachedPortfolioData(); err != nil {
		return nil, err
	} else if ok {
		return data, nil
	}
	return nil, fmt.Errorf("no local portfolio data; run fundpeek sync")
}

func (a *App) CachedPortfolioData() (map[string]any, bool, error) {
	if a.cache == nil {
		return nil, false, nil
	}
	var data map[string]any
	if _, ok, err := a.cache.Get("portfolio_data", &data); err != nil {
		return nil, false, err
	} else if ok {
		cloned, err := real.CloneData(data)
		if err != nil {
			return nil, false, err
		}
		return cloned, true, nil
	}
	return nil, false, nil
}

func (a *App) FundStockHoldings(ctx context.Context, code string) (valuation.FundStockHoldings, error) {
	client := valuation.NewClient()
	cached := valuation.NewCachedClient(a.cache, client)
	return cached.FetchFundStockHoldings(ctx, code)
}

func (a *App) CachedFundStockHoldings(code string) (valuation.FundStockHoldings, bool, error) {
	if a.cache == nil {
		return valuation.FundStockHoldings{}, false, nil
	}
	var holdings valuation.FundStockHoldings
	if _, ok, err := a.cache.Get("fund_holdings/"+strings.TrimSpace(code), &holdings); err != nil {
		return valuation.FundStockHoldings{}, false, err
	} else if ok {
		return holdings, true, nil
	}
	return valuation.FundStockHoldings{}, false, nil
}

func (a *App) CachedFundQuote(code string) (valuation.Quote, bool, error) {
	if a.cache == nil {
		return valuation.Quote{}, false, nil
	}
	var quote valuation.Quote
	if _, ok, err := a.cache.Get("fund_quote/"+strings.TrimSpace(code), &quote); err != nil {
		return valuation.Quote{}, false, err
	} else if ok {
		return quote, true, nil
	}
	return valuation.Quote{}, false, nil
}

func (a *App) SetFundQuote(code string, quote valuation.Quote) error {
	if a.cache == nil {
		return nil
	}
	return a.cache.Set("fund_quote/"+strings.TrimSpace(code), quote)
}

func (a *App) CachedStockQuote(code string) (valuation.StockQuote, bool, error) {
	if a.cache == nil {
		return valuation.StockQuote{}, false, nil
	}
	var quote valuation.StockQuote
	if _, ok, err := a.cache.Get("stock_quote/"+strings.TrimSpace(code), &quote); err != nil {
		return valuation.StockQuote{}, false, err
	} else if ok {
		return quote, true, nil
	}
	return valuation.StockQuote{}, false, nil
}

func (a *App) SetStockQuote(code string, quote valuation.StockQuote) error {
	if a.cache == nil {
		return nil
	}
	return a.cache.Set("stock_quote/"+strings.TrimSpace(code), quote)
}

func (a *App) PushReal(ctx context.Context) error {
	cred, err := a.realCred(ctx)
	if err != nil {
		return err
	}
	data, err := a.PortfolioData(ctx)
	if err != nil {
		return err
	}
	if !hasImportedHoldings(data) {
		return fmt.Errorf("no local portfolio data; run fundpeek sync")
	}
	current, err := a.real.FetchUserConfig(ctx, cred)
	if err != nil {
		return err
	}
	merged, err := mergePortfolioIntoRemote(current.Data, data)
	if err != nil {
		return err
	}
	if err := a.real.UpdateUserConfigIfUnchanged(ctx, cred, current, merged); err != nil {
		return err
	}
	a.setRealDataCache(merged)
	fmt.Println("real push: ok")
	return nil
}

func (a *App) Logout(source string) error {
	if err := a.store.Delete(source); err != nil {
		return err
	}
	if source == model.SourceReal {
		a.InvalidateRealData()
	}
	return nil
}

func (a *App) RefreshPortfolio(ctx context.Context) error {
	var inputs []model.SyncInput
	var errs []error
	if yjb, err := a.fetchYangJiBaoInput(ctx); err != nil {
		errs = append(errs, err)
	} else {
		inputs = append(inputs, yjb)
	}
	if xb, err := a.fetchXiaoBeiInput(ctx); err != nil {
		errs = append(errs, err)
	} else {
		inputs = append(inputs, xb)
	}
	return a.applyLocalSync(inputs, errs)
}

func (a *App) applyLocalSync(inputs []model.SyncInput, errs []error) error {
	if len(inputs) == 0 {
		if len(errs) > 0 {
			return joinErrors(errs)
		}
		return fmt.Errorf("no source data synced")
	}
	data, _, err := a.CachedPortfolioData()
	if err != nil {
		return err
	}
	if data == nil {
		data = map[string]any{}
	}
	for _, input := range inputs {
		report, err := merge.Apply(data, input)
		if err != nil {
			return err
		}
		printReport(report)
	}
	if err := a.setPortfolioDataCache(data); err != nil {
		return err
	}
	fmt.Println("portfolio sync: ok")
	if len(errs) > 0 {
		return PartialSyncError{Err: joinErrors(errs)}
	}
	return nil
}

func (a *App) setRealDataCache(data map[string]any) {
	if a.cache != nil {
		_ = a.cache.Set("real_data", data)
	}
}

func (a *App) setPortfolioDataCache(data map[string]any) error {
	if a.cache == nil {
		return nil
	}
	return a.cache.Set("portfolio_data", data)
}

func (a *App) InvalidatePortfolioData() {
	if a.cache != nil {
		_ = a.cache.Invalidate("portfolio_data")
	}
}

func (a *App) InvalidateRealData() {
	if a.cache != nil {
		_ = a.cache.Invalidate("real_data")
	}
}

func (a *App) InvalidateFundStockHoldings(code string) {
	if a.cache != nil {
		_ = a.cache.Invalidate("fund_holdings/" + strings.TrimSpace(code))
	}
}

func (a *App) InvalidateFundQuote(code string) {
	if a.cache != nil {
		_ = a.cache.Invalidate("fund_quote/" + strings.TrimSpace(code))
	}
}

func (a *App) InvalidateStockQuote(code string) {
	if a.cache != nil {
		_ = a.cache.Invalidate("stock_quote/" + strings.TrimSpace(code))
	}
}

func (a *App) realCred(ctx context.Context) (model.RealCredential, error) {
	cred, err := a.store.GetReal()
	if err != nil {
		return model.RealCredential{}, err
	}
	if cred.ExpiresAt > time.Now().Add(2*time.Minute).Unix() {
		return *cred, nil
	}
	refreshed, err := a.real.Refresh(ctx, *cred)
	if err != nil {
		return model.RealCredential{}, err
	}
	if err := a.store.SaveReal(refreshed); err != nil {
		return model.RealCredential{}, err
	}
	return refreshed, nil
}

func (a *App) fetchYangJiBao(ctx context.Context) (model.SyncInput, error) {
	cred, err := a.store.GetYangJiBao()
	if err != nil {
		return model.SyncInput{}, err
	}
	client := yangjibao.NewClient(cred.Token)
	accounts, err := client.FetchAccounts(ctx)
	if err != nil {
		return model.SyncInput{}, err
	}
	out := model.SyncInput{Source: model.SourceYangJiBao}
	accountNames := map[string]string{}
	for _, account := range accounts {
		out.Accounts = append(out.Accounts, model.NormalizedAccount{
			Source:            model.SourceYangJiBao,
			ExternalAccountID: account.ID,
			Name:              account.Name,
		})
		accountNames[account.ID] = account.Name
	}
	for _, account := range accounts {
		holdings, err := client.FetchHoldings(ctx, account.ID)
		if err != nil {
			return model.SyncInput{}, err
		}
		for _, holding := range holdings {
			out.Holdings = append(out.Holdings, model.NormalizedHolding{
				Source:            model.SourceYangJiBao,
				ExternalAccountID: holding.AccountID,
				FundCode:          holding.FundCode,
				FundName:          holding.FundName,
				Share:             parseFloat(holding.Share),
				CostNav:           parseFloat(holding.CostNav),
				Amount:            parseFloat(holding.Amount),
				OperationDate:     holding.OperationDate,
			})
			if holding.AccountName != "" && accountNames[holding.AccountID] == "" {
				accountNames[holding.AccountID] = holding.AccountName
			}
		}
	}
	return out, nil
}

func (a *App) fetchXiaoBei(ctx context.Context) (model.SyncInput, error) {
	cred, err := a.store.GetXiaoBei()
	if err != nil {
		return model.SyncInput{}, err
	}
	client := xiaobei.NewClient(cred.AccessToken, cred.UnionID)
	accounts, err := client.FetchAccounts(ctx)
	if err != nil {
		return model.SyncInput{}, err
	}
	holdings, err := client.FetchHoldings(ctx, "")
	if err != nil {
		return model.SyncInput{}, err
	}
	out := model.SyncInput{Source: model.SourceXiaoBei}
	for _, account := range accounts {
		out.Accounts = append(out.Accounts, model.NormalizedAccount{
			Source:            model.SourceXiaoBei,
			ExternalAccountID: normalizeDefault(account.ID),
			Name:              account.Name,
		})
	}
	for _, holding := range holdings {
		out.Holdings = append(out.Holdings, model.NormalizedHolding{
			Source:            model.SourceXiaoBei,
			ExternalAccountID: normalizeDefault(holding.AccountID),
			FundCode:          holding.FundCode,
			FundName:          holding.FundName,
			Share:             holding.Share,
			CostNav:           holding.CostNAV,
			Amount:            holding.Amount,
			OperationDate:     holding.OperationDate,
			EstimatedShare:    holding.ShareEstimated,
		})
	}
	return out, nil
}

func printReport(report merge.Report) {
	fmt.Printf("source: %s\n", report.Source)
	fmt.Printf("accounts: %d\n", report.AccountCount)
	fmt.Printf("funds: %d, added: %d\n", report.FundCount, report.AddedFunds)
	fmt.Printf("groups: %d\n", report.UpdatedGroups)
	fmt.Printf("holdings: %d\n", report.UpdatedHoldings)
	if report.EstimatedShares > 0 {
		fmt.Printf("estimated shares: %d\n", report.EstimatedShares)
	}
}

func printCredentialStatus(name string, read func() error) {
	if err := read(); err != nil {
		if errors.Is(err, credential.ErrNotAuthenticated) {
			fmt.Printf("%s: not authenticated\n", name)
			return
		}
		fmt.Printf("%s: credential error: %v\n", name, err)
		return
	}
	fmt.Printf("%s: authenticated\n", name)
}

func hasImportedHoldings(data map[string]any) bool {
	for key, value := range toMap(data["groupHoldings"]) {
		if !strings.HasPrefix(key, "import_"+model.SourceYangJiBao+"_") && !strings.HasPrefix(key, "import_"+model.SourceXiaoBei+"_") {
			continue
		}
		if len(toMap(value)) > 0 {
			return true
		}
	}
	return false
}

func mergePortfolioIntoRemote(remote, local map[string]any) (map[string]any, error) {
	out, err := real.CloneData(remote)
	if err != nil {
		return nil, err
	}
	localClone, err := real.CloneData(local)
	if err != nil {
		return nil, err
	}
	out["funds"] = mergePortfolioFunds(out["funds"], localClone["funds"])
	out["groups"] = replaceImportedGroups(out["groups"], localClone["groups"])
	out["groupHoldings"] = replaceImportedGroupHoldings(out["groupHoldings"], localClone["groupHoldings"])
	return out, nil
}

func mergePortfolioFunds(remote, local any) []any {
	out := toSlice(remote)
	seen := map[string]bool{}
	for _, item := range out {
		code, _ := toMap(item)["code"].(string)
		if code != "" {
			seen[code] = true
		}
	}
	for _, item := range toSlice(local) {
		code, _ := toMap(item)["code"].(string)
		if code == "" || seen[code] {
			continue
		}
		out = append(out, item)
		seen[code] = true
	}
	return out
}

func replaceImportedGroups(remote, local any) []any {
	out := make([]any, 0)
	for _, item := range toSlice(remote) {
		id, _ := toMap(item)["id"].(string)
		if !isFundpeekImportGroup(id) {
			out = append(out, item)
		}
	}
	for _, item := range toSlice(local) {
		id, _ := toMap(item)["id"].(string)
		if isFundpeekImportGroup(id) {
			out = append(out, item)
		}
	}
	return out
}

func replaceImportedGroupHoldings(remote, local any) map[string]any {
	out := toMap(remote)
	for key := range out {
		if isFundpeekImportGroup(key) {
			delete(out, key)
		}
	}
	for key, value := range toMap(local) {
		if isFundpeekImportGroup(key) {
			out[key] = value
		}
	}
	return out
}

func isFundpeekImportGroup(id string) bool {
	return strings.HasPrefix(id, "import_"+model.SourceYangJiBao+"_") || strings.HasPrefix(id, "import_"+model.SourceXiaoBei+"_")
}

func joinErrors(errs []error) error {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return errors.New(strings.Join(parts, "; "))
}

func toMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if out, ok := value.(map[string]any); ok {
		return out
	}
	return map[string]any{}
}

func toSlice(value any) []any {
	if value == nil {
		return []any{}
	}
	if out, ok := value.([]any); ok {
		return out
	}
	return []any{}
}

func parseFloat(value string) float64 {
	n, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return n
}

func normalizeDefault(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return "default"
	}
	return value
}
