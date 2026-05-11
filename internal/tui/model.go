package tui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/icpd/fundpeek/internal/valuation"
)

type Position struct {
	Code  string
	Name  string
	Share float64
}

type Row struct {
	Position
	Quote       valuation.Quote
	QuoteErr    error
	TodayProfit float64
	HasProfit   bool
}

func BuildPositions(data map[string]any) []Position {
	if data == nil {
		return nil
	}
	names := fundNames(data["funds"])
	byCode := map[string]*Position{}
	for groupID, bucket := range toMap(data["groupHoldings"]) {
		if !isImportGroup(groupID) {
			continue
		}
		for code, rawHolding := range toMap(bucket) {
			code = strings.TrimSpace(code)
			if code == "" {
				continue
			}
			share, ok := numberFromAny(toMap(rawHolding)["share"])
			if !ok || share <= 0 {
				continue
			}
			pos := byCode[code]
			if pos == nil {
				pos = &Position{Code: code, Name: names[code]}
				byCode[code] = pos
			}
			pos.Share += share
		}
	}
	out := make([]Position, 0, len(byCode))
	for _, pos := range byCode {
		out = append(out, *pos)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Code < out[j].Code
	})
	return out
}

func TodayProfit(pos Position, quote valuation.Quote) (float64, bool) {
	if pos.Share <= 0 {
		return 0, false
	}
	if quote.HasGSZ && quote.HasGSZZL && quote.GSZZL > -100 {
		amount := pos.Share * quote.GSZ
		return amount - amount/(1+quote.GSZZL/100), true
	}
	if quote.HasDWJZ && quote.HasLastNAV {
		return (quote.DWJZ - quote.LastNAV) * pos.Share, true
	}
	if quote.HasDWJZ && quote.HasZZL && quote.ZZL > -100 {
		amount := pos.Share * quote.DWJZ
		return amount - amount/(1+quote.ZZL/100), true
	}
	return 0, false
}

func BuildRows(positions []Position, quotes map[string]valuation.Quote, errs map[string]error) []Row {
	rows := make([]Row, 0, len(positions))
	for _, pos := range positions {
		q := quotes[pos.Code]
		if pos.Name == "" {
			pos.Name = q.Name
		}
		profit, ok := TodayProfit(pos, q)
		rows = append(rows, Row{
			Position:    pos,
			Quote:       q,
			QuoteErr:    errs[pos.Code],
			TodayProfit: profit,
			HasProfit:   ok,
		})
	}
	return rows
}

func isImportGroup(id string) bool {
	return strings.HasPrefix(id, "import_yangjibao_") || strings.HasPrefix(id, "import_xiaobei_")
}

func fundNames(value any) map[string]string {
	out := map[string]string{}
	for _, item := range toSlice(value) {
		m := toMap(item)
		code, _ := m["code"].(string)
		name, _ := m["name"].(string)
		code = strings.TrimSpace(code)
		if code != "" && strings.TrimSpace(name) != "" {
			out[code] = strings.TrimSpace(name)
		}
	}
	return out
}

func toSlice(value any) []any {
	if value == nil {
		return nil
	}
	if out, ok := value.([]any); ok {
		return out
	}
	return nil
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

func numberFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case jsonNumber:
		n, err := v.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

type jsonNumber interface {
	Float64() (float64, error)
}
