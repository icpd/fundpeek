package xiaobei

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/lewis/fundsync/internal/httpclient"
)

// Account is the XiaoBei account shape consumed by later normalization.
type Account struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Raw  map[string]any `json:"raw,omitempty"`
}

// Holding is the XiaoBei holding shape consumed by later normalization.
type Holding struct {
	AccountID      string         `json:"accountId"`
	AccountName    string         `json:"accountName,omitempty"`
	FundCode       string         `json:"fundCode"`
	FundName       string         `json:"fundName"`
	Share          float64        `json:"share"`
	CostNAV        float64        `json:"costNav"`
	Amount         float64        `json:"amount"`
	Earnings       float64        `json:"earnings"`
	OperationDate  string         `json:"operationDate"`
	ShareEstimated bool           `json:"shareEstimated"`
	Raw            map[string]any `json:"raw,omitempty"`
}

type apiResponse struct {
	Code    int             `json:"code"`
	Msg     string          `json:"msg"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type loginData struct {
	AccessToken string `json:"accessToken"`
	UnionID     string `json:"unionId"`
	User        struct {
		UnionID string `json:"unionId"`
	} `json:"user"`
}

type accountListData struct {
	AccountList []map[string]any `json:"accountList"`
}

type holdListData struct {
	List []map[string]any `json:"list"`
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		switch x := v.(type) {
		case string:
			if strings.TrimSpace(x) != "" {
				return strings.TrimSpace(x)
			}
		case json.Number:
			return x.String()
		case float64:
			return formatFloatID(x)
		case float32:
			return formatFloatID(float64(x))
		case int:
			return strconv.Itoa(x)
		case int64:
			return strconv.FormatInt(x, 10)
		case uint64:
			return strconv.FormatUint(x, 10)
		default:
			s := strings.TrimSpace(fmt.Sprint(x))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func numberFromAny(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case uint64:
		return float64(x)
	case json.Number:
		n, _ := x.Float64()
		return n
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return n
	default:
		n, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(x)), 64)
		return n
	}
}

func firstNumber(m map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return numberFromAny(v)
		}
	}
	return 0
}

func moneyIsPositive(v any) bool {
	return numberFromAny(v) > 0
}

func normalizeAccountID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || id == "0" || strings.EqualFold(id, "<nil>") {
		return "default"
	}
	return id
}

func fundName(item map[string]any) string {
	if name := firstString(item, "name", "fundName", "fund_name"); name != "" {
		return name
	}
	rawData, ok := item["data"].(map[string]any)
	if !ok {
		return ""
	}
	return firstString(rawData, "name", "fundName", "fund_name")
}

func accountName(item map[string]any, accountID string) string {
	if name := firstString(item, "accountName", "account_name"); name != "" {
		return name
	}
	if accountID == "default" {
		return "默认账户"
	}
	return ""
}

func formatFloatID(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func trimBody(body []byte) string {
	return httpclient.SafeBody(body)
}
