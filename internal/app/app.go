package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/icpd/fundpeek/internal/backup"
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

const realDataCacheTTL = 24 * time.Hour

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
}

func New(cfg config.Config, store *credential.FileStore) *App {
	return &App{
		cfg:   cfg,
		store: store,
		real:  real.NewClient(cfg.SupabaseURL, cfg.SupabaseAnon, cfg.DeviceID),
		cache: fundcache.NewFileCache(cfg.CacheDir, time.Now),
	}
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
	switch source {
	case model.SourceYangJiBao:
		input, err := a.fetchYangJiBao(ctx)
		if err != nil {
			return err
		}
		return a.applySync(ctx, []model.SyncInput{input})
	case model.SourceXiaoBei:
		input, err := a.fetchXiaoBei(ctx)
		if err != nil {
			return err
		}
		return a.applySync(ctx, []model.SyncInput{input})
	case "all":
		yjb, err := a.fetchYangJiBao(ctx)
		if err != nil {
			return err
		}
		xb, err := a.fetchXiaoBei(ctx)
		if err != nil {
			return err
		}
		return a.applySync(ctx, []model.SyncInput{yjb, xb})
	default:
		return fmt.Errorf("unknown sync source %q", source)
	}
}

func (a *App) Backup(ctx context.Context) (string, error) {
	cred, err := a.realCred(ctx)
	if err != nil {
		return "", err
	}
	cfg, err := a.real.FetchUserConfig(ctx, cred)
	if err != nil {
		return "", err
	}
	data, err := real.CloneData(cfg.Data)
	if err != nil {
		return "", err
	}
	return backup.Save(a.cfg.BackupDir, cred.UserID, data)
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
	err = a.cache.GetOrFetch("real_data", realDataCacheTTL, &data, func() (any, error) {
		cfg, err := a.real.FetchUserConfig(ctx, cred)
		if err != nil {
			return nil, err
		}
		return real.CloneData(cfg.Data)
	})
	if err != nil {
		return nil, err
	}
	return real.CloneData(data)
}

func (a *App) FundStockHoldings(ctx context.Context, code string) (valuation.FundStockHoldings, error) {
	client := valuation.NewClient()
	cached := valuation.NewCachedClient(a.cache, client)
	return cached.FetchFundStockHoldings(ctx, code)
}

func (a *App) Restore(ctx context.Context, path string) error {
	cred, err := a.realCred(ctx)
	if err != nil {
		return err
	}
	current, err := a.real.FetchUserConfig(ctx, cred)
	if err != nil {
		return err
	}
	currentData, err := real.CloneData(current.Data)
	if err != nil {
		return err
	}
	backupPath, err := backup.Save(a.cfg.BackupDir, cred.UserID, currentData)
	if err != nil {
		return err
	}
	snapshot, err := backup.Load(path)
	if err != nil {
		return err
	}
	if snapshot.UserID != cred.UserID {
		return fmt.Errorf("backup belongs to user %s, current user is %s", snapshot.UserID, cred.UserID)
	}
	if err := a.real.UpdateUserConfigIfUnchanged(ctx, cred, current, snapshot.Data); err != nil {
		return fmt.Errorf("%w; restore pre-backup saved at %s", err, backupPath)
	}
	a.InvalidateRealData()
	fmt.Println("restore pre-backup:", backupPath)
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

func (a *App) applySync(ctx context.Context, inputs []model.SyncInput) error {
	cred, err := a.realCred(ctx)
	if err != nil {
		return err
	}
	current, err := a.real.FetchUserConfig(ctx, cred)
	if err != nil {
		return err
	}
	data, err := real.CloneData(current.Data)
	if err != nil {
		return err
	}
	backupPath, err := backup.Save(a.cfg.BackupDir, cred.UserID, data)
	if err != nil {
		return err
	}

	for _, input := range inputs {
		report, err := merge.Apply(data, input)
		if err != nil {
			return err
		}
		printReport(report)
	}
	if err := a.real.UpdateUserConfigIfUnchanged(ctx, cred, current, data); err != nil {
		return fmt.Errorf("%w; backup saved at %s", err, backupPath)
	}
	a.InvalidateRealData()
	fmt.Println("backup:", backupPath)
	fmt.Println("real upsert: ok")
	return nil
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
