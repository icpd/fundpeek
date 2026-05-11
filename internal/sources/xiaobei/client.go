package xiaobei

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/icpd/fundpeek/internal/httpclient"
)

const (
	defaultBaseURL = "https://api.xiaobeiyangji.com"
	version        = "3.5.7.0"
	clientType     = "APP"
)

// Client talks to the XiaoBei YangJi API.
type Client struct {
	http    *resty.Client
	token   string
	unionID string
}

// NewClient creates a XiaoBei client. unionID may be empty when token is a JWT
// containing unionId in its payload.
func NewClient(token, unionID string) *Client {
	if unionID == "" && token != "" {
		unionID = unionIDFromJWT(token)
	}

	c := resty.New().
		SetBaseURL(defaultBaseURL).
		SetLogger(httpclient.DiscardLogger()).
		SetTimeout(30*time.Second).
		SetRetryCount(2).
		SetRetryWaitTime(500*time.Millisecond).
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", "fundpeek/xiaobei")

	return &Client{
		http:    c,
		token:   token,
		unionID: unionID,
	}
}

// SetBaseURL overrides the API endpoint. It is mainly useful for tests.
func (c *Client) SetBaseURL(baseURL string) {
	c.http.SetBaseURL(strings.TrimRight(baseURL, "/"))
}

// SendSMS sends a phone verification code. XiaoBei accepts this before login,
// so the Authorization header is intentionally "Bearer " when no token exists.
func (c *Client) SendSMS(ctx context.Context, phone string) error {
	body := map[string]any{
		"phoneNumber": phone,
		"isBind":      false,
		"version":     version,
		"clientType":  clientType,
	}

	var data json.RawMessage
	if err := c.post(ctx, "/yangji-api/api/send-sms", body, "", &data); err != nil {
		return fmt.Errorf("send xiaobei sms: %w", err)
	}
	return nil
}

// VerifyPhone logs in with a phone verification code and returns accessToken
// and unionId. When the response omits unionId, the token JWT payload is used as
// fallback.
func (c *Client) VerifyPhone(ctx context.Context, phone, code string) (string, string, error) {
	body := map[string]any{
		"phone":      phone,
		"code":       code,
		"clientType": "PHONE",
		"version":    version,
	}

	var data loginData
	if err := c.post(ctx, "/yangji-api/api/login/phone", body, "", &data); err != nil {
		return "", "", fmt.Errorf("verify xiaobei phone: %w", err)
	}

	accessToken := strings.TrimSpace(data.AccessToken)
	if accessToken == "" {
		return "", "", errors.New("verify xiaobei phone: missing accessToken in response")
	}

	unionID := strings.TrimSpace(data.User.UnionID)
	if unionID == "" {
		unionID = strings.TrimSpace(data.UnionID)
	}
	if unionID == "" {
		unionID = unionIDFromJWT(accessToken)
	}
	if unionID == "" {
		return "", "", errors.New("verify xiaobei phone: missing unionId in response and JWT payload")
	}

	c.token = accessToken
	c.unionID = unionID
	return accessToken, unionID, nil
}

// FetchAccounts returns XiaoBei accounts. The account fields are shaped so the
// normalize package can map source account id/name without knowing raw payloads.
func (c *Client) FetchAccounts(ctx context.Context) ([]Account, error) {
	if err := c.requireLogin(); err != nil {
		return nil, err
	}

	var data accountListData
	if err := c.post(ctx, "/yangji-api/api/get-account-list", c.commonBody(), c.token, &data); err != nil {
		return nil, fmt.Errorf("fetch xiaobei accounts: %w", err)
	}

	accounts := make([]Account, 0, len(data.AccountList))
	for _, raw := range data.AccountList {
		id := firstString(raw, "accountId", "id", "account_id")
		if id == "" {
			id = "default"
		}
		name := firstString(raw, "name", "accountName", "account_name")
		if name == "" {
			name = "默认账户"
		}
		accounts = append(accounts, Account{
			ID:   id,
			Name: name,
			Raw:  raw,
		})
	}
	return accounts, nil
}

// FetchHoldings returns holdings for accountID. Pass an empty accountID to
// return all accounts. XiaoBei does not return share reliably, so share is
// derived from money/nav when nav is available.
func (c *Client) FetchHoldings(ctx context.Context, accountID string) ([]Holding, error) {
	if err := c.requireLogin(); err != nil {
		return nil, err
	}

	var data holdListData
	if err := c.post(ctx, "/yangji-api/api/get-hold-list", c.commonBody(), c.token, &data); err != nil {
		return nil, fmt.Errorf("fetch xiaobei holdings: %w", err)
	}

	items := make([]map[string]any, 0, len(data.List))
	codes := make([]string, 0, len(data.List))
	seenCode := make(map[string]struct{})
	requestedAccountID := normalizeAccountID(accountID)
	for _, item := range data.List {
		if !moneyIsPositive(item["money"]) {
			continue
		}
		itemAccountID := normalizeAccountID(firstString(item, "accountId", "account_id"))
		if accountID != "" && requestedAccountID != itemAccountID {
			continue
		}
		code := firstString(item, "code", "fundCode", "fund_code")
		if code == "" {
			continue
		}
		items = append(items, item)
		if _, ok := seenCode[code]; !ok {
			codes = append(codes, code)
			seenCode[code] = struct{}{}
		}
	}
	if len(items) == 0 {
		return []Holding{}, nil
	}

	navs, err := c.fetchOptionalChangeNAV(ctx, codes)
	if err != nil {
		return nil, fmt.Errorf("fetch xiaobei holdings nav: %w", err)
	}

	holdings := make([]Holding, 0, len(items))
	for _, item := range items {
		code := firstString(item, "code", "fundCode", "fund_code")
		account := normalizeAccountID(firstString(item, "accountId", "account_id"))

		amount := numberFromAny(item["money"])
		earnings := firstNumber(item, "earnings", "holdingEarnings", "holdEarnings", "income", "profit")
		nav := navs[code]
		share := 0.0
		shareDerived := false
		if nav > 0 {
			share = amount / nav
			shareDerived = true
		}
		costNAV := costNAVFromAmountEarnings(amount, earnings, share, nav)

		holdings = append(holdings, Holding{
			AccountID:      account,
			AccountName:    accountName(item, account),
			FundCode:       code,
			FundName:       fundName(item),
			Share:          share,
			CostNAV:        costNAV,
			Amount:         amount,
			Earnings:       earnings,
			OperationDate:  firstString(item, "headDate", "operationDate", "operation_date"),
			ShareEstimated: shareDerived,
			Raw:            item,
		})
	}

	return holdings, nil
}

func costNAVFromAmountEarnings(amount, earnings, share, fallbackNAV float64) float64 {
	if share <= 0 {
		return fallbackNAV
	}
	principal := amount - earnings
	if principal < 0 {
		return fallbackNAV
	}
	return principal / share
}

func (c *Client) fetchOptionalChangeNAV(ctx context.Context, codes []string) (map[string]float64, error) {
	if len(codes) == 0 {
		return map[string]float64{}, nil
	}

	today := time.Now().Format(time.DateOnly)
	yesterday := time.Now().AddDate(0, 0, -1).Format(time.DateOnly)
	body := c.commonBody()
	body["dataResources"] = "4"
	body["dataSourceSwitch"] = true
	body["valuationDate"] = today
	body["navDate"] = yesterday
	body["isTD"] = true
	body["codeArr"] = codes

	var data []map[string]any
	if err := c.post(ctx, "/yangji-api/api/get-optional-change-nav", body, c.token, &data); err != nil {
		return nil, err
	}

	navs := make(map[string]float64, len(data))
	for _, item := range data {
		code := firstString(item, "code", "fundCode", "fund_code")
		if code == "" {
			continue
		}
		nav := numberFromAny(item["nav"])
		if nav <= 0 {
			nav = numberFromAny(item["valuation"])
		}
		if nav > 0 {
			navs[code] = nav
		}
	}
	return navs, nil
}

func (c *Client) post(ctx context.Context, path string, body any, token string, out any) error {
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+token).
		SetBody(body).
		Post(path)
	if err != nil {
		return err
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		return fmt.Errorf("%s: http %d: %s", path, resp.StatusCode(), trimBody(resp.Body()))
	}

	var envelope apiResponse
	if err := json.Unmarshal(resp.Body(), &envelope); err != nil {
		return fmt.Errorf("%s: decode response: %w", path, err)
	}
	if envelope.Code != 200 {
		msg := strings.TrimSpace(envelope.Msg)
		if msg == "" {
			msg = strings.TrimSpace(envelope.Message)
		}
		if msg == "" {
			msg = "unknown error"
		}
		return fmt.Errorf("%s: business code %d: %s", path, envelope.Code, msg)
	}
	if out == nil {
		return nil
	}
	if len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("%s: decode data: %w", path, err)
	}
	return nil
}

func (c *Client) commonBody() map[string]any {
	return map[string]any{
		"unionId":    c.unionID,
		"version":    version,
		"clientType": clientType,
	}
}

func (c *Client) requireLogin() error {
	if strings.TrimSpace(c.token) == "" {
		return errors.New("xiaobei: not logged in, missing access token")
	}
	if strings.TrimSpace(c.unionID) == "" {
		c.unionID = unionIDFromJWT(c.token)
	}
	if strings.TrimSpace(c.unionID) == "" {
		return errors.New("xiaobei: not logged in, missing unionId")
	}
	return nil
}

func unionIDFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return firstString(claims, "unionId", "union_id")
}
