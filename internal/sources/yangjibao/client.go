package yangjibao

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/icpd/fundpeek/internal/httpclient"
)

const (
	baseURL = "https://browser-plug-api.yangjibao.com"
	secret  = "YxmKSrQR4uoJ5lOoWIhcbd7SlUEh9OOc"
)

// Client calls the YangJiBao browser plugin API.
type Client struct {
	token string
	http  *resty.Client
	now   func() time.Time
}

// NewClient creates a YangJiBao API client. Pass an empty token for QR login.
func NewClient(token string) *Client {
	c := &Client{
		token: token,
		http: resty.New().
			SetBaseURL(baseURL).
			SetLogger(httpclient.DiscardLogger()).
			SetTimeout(30*time.Second).
			SetRetryCount(2).
			SetHeader("User-Agent", "fundpeek/1.0"),
		now: time.Now,
	}
	return c
}

// GetQRCode starts QR-code login and returns the QR id plus URL to display.
func (c *Client) GetQRCode(ctx context.Context) (QRCode, error) {
	var data qrCodeData
	if err := c.request(ctx, resty.MethodGet, "/qr_code", &data); err != nil {
		return QRCode{}, err
	}

	if data.ID == "" || data.URL == "" {
		return QRCode{}, errors.New("yangjibao: invalid qr_code response: missing id or url")
	}

	return QRCode{
		QRID:  data.ID,
		QRURL: data.URL,
	}, nil
}

// CheckQRCodeState polls QR-code login state. Token is set only when State is confirmed.
func (c *Client) CheckQRCodeState(ctx context.Context, qrID string) (QRCodeState, error) {
	if strings.TrimSpace(qrID) == "" {
		return QRCodeState{}, errors.New("yangjibao: qr id is required")
	}

	var data qrCodeStateData
	path := "/qr_code_state/" + url.PathEscape(qrID)
	if err := c.request(ctx, resty.MethodGet, path, &data); err != nil {
		return QRCodeState{}, err
	}

	state := mapQRCodeState(data.State)
	token := ""
	if state == StateConfirmed {
		token = data.Token
	}

	return QRCodeState{
		State: state,
		Token: token,
	}, nil
}

// FetchAccounts returns the YangJiBao accounts visible to the current token.
func (c *Client) FetchAccounts(ctx context.Context) ([]Account, error) {
	if err := c.requireToken(); err != nil {
		return nil, err
	}

	var data accountListData
	if err := c.request(ctx, resty.MethodGet, "/user_account", &data); err != nil {
		return nil, err
	}

	accounts := make([]Account, 0, len(data.List))
	for _, raw := range data.List {
		id := firstString(raw, "id", "account_id", "accountId")
		name := firstString(raw, "title", "name", "account_name", "accountName")
		if id == "" || name == "" {
			continue
		}
		accounts = append(accounts, Account{
			ID:   id,
			Name: name,
			Raw:  raw,
		})
	}

	return accounts, nil
}

// FetchHoldings returns normalized-enough holdings for a single YangJiBao account.
func (c *Client) FetchHoldings(ctx context.Context, accountID string) ([]Holding, error) {
	if err := c.requireToken(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(accountID) == "" {
		return nil, errors.New("yangjibao: account id is required")
	}

	var data []map[string]any
	path := "/fund_hold?account_id=" + url.QueryEscape(accountID)
	if err := c.request(ctx, resty.MethodGet, path, &data); err != nil {
		return nil, err
	}

	holdings := make([]Holding, 0, len(data))
	for _, raw := range data {
		fundCode := firstString(raw, "fund_code", "code")
		share, okShare := decimalString(raw, "hold_share", "share")
		costNav, okCostNav := decimalString(raw, "hold_cost", "cost_nav", "nav")
		if fundCode == "" || !okShare || !okCostNav {
			continue
		}

		amount, okAmount := decimalString(raw, "money", "amount")
		if !okAmount {
			amount = ""
		}

		holdings = append(holdings, Holding{
			AccountID:     accountID,
			AccountName:   firstString(raw, "account_name", "accountName", "account_title", "accountTitle"),
			FundCode:      fundCode,
			FundName:      firstString(raw, "fund_name", "short_name", "name"),
			Share:         share,
			CostNav:       costNav,
			Amount:        amount,
			OperationDate: firstString(raw, "hold_day", "operation_date", "operationDate"),
			Raw:           raw,
		})
	}

	return holdings, nil
}

func (c *Client) request(ctx context.Context, method, path string, out any) error {
	timestamp := c.now().Unix()
	headers := map[string]string{
		"Request-Time": strconv.FormatInt(timestamp, 10),
		"Request-Sign": c.sign(path, timestamp),
		"Content-Type": "application/json",
	}
	if c.token != "" {
		headers["Authorization"] = c.token
	}

	var envelope apiResponse
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeaders(headers).
		Execute(method, path)
	if err != nil {
		return fmt.Errorf("yangjibao: request %s %s failed: %w", method, stripQuery(path), err)
	}
	if resp.IsError() {
		return fmt.Errorf("yangjibao: request %s %s failed: http %d: %s", method, stripQuery(path), resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	if err := json.Unmarshal(resp.Body(), &envelope); err != nil {
		return fmt.Errorf("yangjibao: decode %s response envelope: %w", stripQuery(path), err)
	}
	if envelope.Code != 200 {
		message := strings.TrimSpace(envelope.Message)
		if message == "" {
			message = strings.TrimSpace(envelope.Msg)
		}
		if message == "" {
			message = "unknown business error"
		}
		return fmt.Errorf("yangjibao: request %s %s failed: code %d: %s", method, stripQuery(path), envelope.Code, message)
	}
	if out == nil {
		return nil
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("yangjibao: decode %s response data: %w", stripQuery(path), err)
	}
	return nil
}

func (c *Client) sign(path string, timestamp int64) string {
	sum := md5.Sum([]byte(stripQuery(path) + c.token + strconv.FormatInt(timestamp, 10) + secret))
	return hex.EncodeToString(sum[:])
}

func (c *Client) requireToken() error {
	if strings.TrimSpace(c.token) == "" {
		return errors.New("yangjibao: token is required")
	}
	return nil
}

func stripQuery(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i]
	}
	return path
}

func mapQRCodeState(v any) string {
	switch fmt.Sprint(v) {
	case "1":
		return StateWaiting
	case "2":
		return StateConfirmed
	case "3":
		return StateExpired
	default:
		return StateUnknown
	}
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return v
			}
		case json.Number:
			return v.String()
		default:
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func decimalString(raw map[string]any, keys ...string) (string, bool) {
	s := firstString(raw, keys...)
	if s == "" {
		return "", false
	}
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return "", false
	}
	return s, true
}
