package yangjibao

import "encoding/json"

const (
	StateWaiting   = "waiting"
	StateScanned   = "scanned"
	StateConfirmed = "confirmed"
	StateExpired   = "expired"
	StateUnknown   = "unknown"
)

type QRCode struct {
	QRID  string `json:"qr_id"`
	QRURL string `json:"qr_url"`
}

type QRCodeState struct {
	State string `json:"state"`
	Token string `json:"token,omitempty"`
}

type Account struct {
	ID   string         `json:"account_id"`
	Name string         `json:"name"`
	Raw  map[string]any `json:"raw,omitempty"`
}

type Holding struct {
	AccountID     string         `json:"account_id"`
	AccountName   string         `json:"account_name,omitempty"`
	FundCode      string         `json:"fund_code"`
	FundName      string         `json:"fund_name"`
	Share         string         `json:"share"`
	CostNav       string         `json:"cost_nav"`
	Amount        string         `json:"amount,omitempty"`
	OperationDate string         `json:"operation_date,omitempty"`
	Raw           map[string]any `json:"raw,omitempty"`
}

type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Msg     string          `json:"msg"`
	Data    json.RawMessage `json:"data"`
}

type qrCodeData struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type qrCodeStateData struct {
	State any    `json:"state"`
	Token string `json:"token"`
}

type accountListData struct {
	List []map[string]any `json:"list"`
}
