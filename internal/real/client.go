package real

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/lewis/fundsync/internal/httpclient"
	"github.com/lewis/fundsync/internal/model"
)

var ErrUserConfigConflict = errors.New("real user config changed remotely")

type Client struct {
	http   *resty.Client
	anon   string
	device string
}

type UserConfig struct {
	UserID    string                 `json:"user_id,omitempty"`
	Data      map[string]any         `json:"data"`
	UpdatedAt string                 `json:"updated_at,omitempty"`
	Exists    bool                   `json:"-"`
	Raw       map[string]interface{} `json:"-"`
}

type otpResponse struct{}

type verifyResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	User         struct {
		ID string `json:"id"`
	} `json:"user"`
}

func NewClient(baseURL, anonKey, deviceID string) *Client {
	c := httpclient.New(baseURL).
		SetHeader("apikey", anonKey)
	return &Client{http: c, anon: anonKey, device: deviceID}
}

func (c *Client) SendOTP(ctx context.Context, email string) error {
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]any{
			"email":                 email,
			"data":                  map[string]any{},
			"create_user":           true,
			"gotrue_meta_security":  map[string]any{},
			"code_challenge":        nil,
			"code_challenge_method": nil,
		}).
		SetResult(&otpResponse{}).
		Post("/auth/v1/otp")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return fmt.Errorf("send real otp failed: http %d: %s", resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	return nil
}

func (c *Client) VerifyOTP(ctx context.Context, email, token string) (model.RealCredential, error) {
	var out verifyResponse
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]any{
			"email":                email,
			"token":                token,
			"type":                 "email",
			"gotrue_meta_security": map[string]any{},
		}).
		SetResult(&out).
		Post("/auth/v1/verify")
	if err != nil {
		return model.RealCredential{}, err
	}
	if resp.IsError() {
		return model.RealCredential{}, fmt.Errorf("verify real otp failed: http %d: %s", resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	if out.AccessToken == "" || out.User.ID == "" {
		return model.RealCredential{}, fmt.Errorf("verify real otp failed: missing token or user id")
	}
	return model.RealCredential{
		UserID:       out.User.ID,
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresAt:    out.ExpiresAt,
	}, nil
}

func (c *Client) Refresh(ctx context.Context, cred model.RealCredential) (model.RealCredential, error) {
	if cred.RefreshToken == "" {
		return cred, fmt.Errorf("real refresh token is empty")
	}
	var out verifyResponse
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"refresh_token": cred.RefreshToken}).
		SetResult(&out).
		Post("/auth/v1/token?grant_type=refresh_token")
	if err != nil {
		return cred, err
	}
	if resp.IsError() {
		return cred, fmt.Errorf("refresh real token failed: http %d: %s", resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	if out.AccessToken == "" {
		return cred, fmt.Errorf("refresh real token failed: missing access token")
	}
	if out.User.ID == "" {
		out.User.ID = cred.UserID
	}
	return model.RealCredential{
		UserID:       out.User.ID,
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresAt:    out.ExpiresAt,
	}, nil
}

func (c *Client) FetchUserConfig(ctx context.Context, cred model.RealCredential) (UserConfig, error) {
	var rows []UserConfig
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(cred.AccessToken).
		SetHeader("Accept", "application/json").
		SetResult(&rows).
		Get("/rest/v1/user_configs?select=user_id,data,updated_at&user_id=eq." + url.QueryEscape(cred.UserID))
	if err != nil {
		return UserConfig{}, err
	}
	if resp.IsError() {
		return UserConfig{}, fmt.Errorf("fetch real user config failed: http %d: %s", resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	if len(rows) == 0 {
		return UserConfig{UserID: cred.UserID, Data: map[string]any{}}, nil
	}
	if rows[0].Data == nil {
		rows[0].Data = map[string]any{}
	}
	rows[0].Exists = true
	return rows[0], nil
}

func (c *Client) UpsertUserConfig(ctx context.Context, cred model.RealCredential, data map[string]any) error {
	body := c.userConfigBody(cred, data)
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(cred.AccessToken).
		SetHeader("Content-Type", "application/json").
		SetHeader("prefer", "resolution=merge-duplicates").
		SetBody(body).
		Post("/rest/v1/user_configs?on_conflict=user_id")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return fmt.Errorf("upsert real user config failed: http %d: %s", resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	return nil
}

func (c *Client) UpdateUserConfigIfUnchanged(ctx context.Context, cred model.RealCredential, current UserConfig, data map[string]any) error {
	if !current.Exists {
		return c.UpsertUserConfig(ctx, cred, data)
	}
	path := "/rest/v1/user_configs?user_id=eq." + url.QueryEscape(cred.UserID)
	if current.UpdatedAt == "" {
		path += "&updated_at=is.null"
	} else {
		path += "&updated_at=eq." + url.QueryEscape(current.UpdatedAt)
	}

	var rows []UserConfig
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(cred.AccessToken).
		SetHeader("Content-Type", "application/json").
		SetHeader("prefer", "return=representation").
		SetBody(c.userConfigBody(cred, data)).
		SetResult(&rows).
		Patch(path)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return fmt.Errorf("update real user config failed: http %d: %s", resp.StatusCode(), httpclient.SafeBody(resp.Body()))
	}
	if len(rows) == 0 {
		return ErrUserConfigConflict
	}
	return nil
}

func CloneData(data map[string]any) (map[string]any, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func (c *Client) userConfigBody(cred model.RealCredential, data map[string]any) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	data["_syncMeta"] = map[string]any{
		"deviceId": c.device,
		"at":       now,
	}
	return map[string]any{
		"user_id":        cred.UserID,
		"data":           data,
		"updated_at":     now,
		"last_device_id": c.device,
	}
}
