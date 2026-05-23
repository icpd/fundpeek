package jsonexport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	fundapp "github.com/icpd/fundpeek/internal/app"
	"github.com/icpd/fundpeek/internal/tui"
)

var refreshFundQuotes = tui.RefreshFundQuotes

type Document struct {
	GeneratedAt string      `json:"generated_at"`
	Summary     Summary     `json:"summary"`
	Funds       []Fund      `json:"funds"`
	Errors      []FundError `json:"errors,omitempty"`
}

type Summary struct {
	FundCount                  int        `json:"fund_count"`
	EstimatedChangePercent     JSONNumber `json:"estimated_change_percent"`
	EstimatedTodayProfitAmount JSONNumber `json:"estimated_today_profit_amount"`
	TotalHoldingAmount         JSONNumber `json:"total_holding_amount"`
	TotalCostAmount            JSONNumber `json:"total_cost_amount"`
	TotalEstimatedAmount       JSONNumber `json:"total_estimated_amount"`
	TotalLatestNAVAmount       JSONNumber `json:"total_latest_nav_amount"`
}

type Fund struct {
	Code                       string     `json:"code"`
	Name                       string     `json:"name"`
	Share                      float64    `json:"share"`
	HoldingAmount              JSONNumber `json:"holding_amount"`
	CostAmount                 JSONNumber `json:"cost_amount"`
	CostNAV                    JSONNumber `json:"cost_nav"`
	EstimatedAmount            JSONNumber `json:"estimated_amount"`
	LatestNAVAmount            JSONNumber `json:"latest_nav_amount"`
	EstimatedChangePercent     JSONNumber `json:"estimated_change_percent"`
	EstimatedTodayProfitAmount JSONNumber `json:"estimated_today_profit_amount"`
	LatestNAVChangePercent     JSONNumber `json:"latest_nav_change_percent"`
	QuoteTime                  string     `json:"quote_time,omitempty"`
	NAVDate                    string     `json:"nav_date,omitempty"`
	QuoteAvailable             bool       `json:"quote_available"`
}

type FundError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type JSONNumber struct {
	Available bool    `json:"available"`
	Value     float64 `json:"value"`
}

func Write(ctx context.Context, a *fundapp.App, out io.Writer) error {
	data, err := a.PortfolioData(ctx)
	if err != nil {
		return err
	}
	positions := tui.BuildPositions(data)
	rows, errs := refreshFundQuotes(ctx, a, positions)
	doc := BuildDocument(rows, errs, time.Now())
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func BuildDocument(rows []tui.Row, errs map[string]error, generatedAt time.Time) Document {
	doc := Document{
		GeneratedAt: generatedAt.UTC().Format(time.RFC3339),
		Summary: Summary{
			FundCount: len(rows),
		},
		Funds:  make([]Fund, 0, len(rows)),
		Errors: buildErrors(errs),
	}

	var todayProfit float64
	var hasTodayProfit bool
	var estimatedProfit float64
	var previousValue float64
	var totalHoldingAmount float64
	var hasHoldingAmount bool
	var totalCostAmount float64
	var hasCostAmount bool
	var totalEstimatedAmount float64
	var hasEstimatedAmount bool
	var totalLatestNAVAmount float64
	var hasLatestNAVAmount bool

	for _, row := range rows {
		if row.HasProfit {
			todayProfit += row.TodayProfit
			hasTodayProfit = true
		}
		if row.HasHoldingAmount {
			totalHoldingAmount += row.HoldingAmount
			hasHoldingAmount = true
		}
		if row.HasCostAmount {
			totalCostAmount += row.CostAmount
			hasCostAmount = true
		}
		if row.Quote.HasGSZ && row.Quote.HasGSZZL && row.Quote.GSZZL > -100 {
			currentValue := row.Share * row.Quote.GSZ
			rowPreviousValue := currentValue / (1 + row.Quote.GSZZL/100)
			estimatedProfit += currentValue - rowPreviousValue
			previousValue += rowPreviousValue
		}
		if row.Quote.HasGSZ {
			totalEstimatedAmount += row.Share * row.Quote.GSZ
			hasEstimatedAmount = true
		}
		if row.Quote.HasDWJZ {
			totalLatestNAVAmount += row.Share * row.Quote.DWJZ
			hasLatestNAVAmount = true
		}
		doc.Funds = append(doc.Funds, buildFund(row))
	}
	if hasTodayProfit {
		doc.Summary.EstimatedTodayProfitAmount = available(todayProfit)
	}
	if previousValue > 0 {
		doc.Summary.EstimatedChangePercent = available(estimatedProfit / previousValue * 100)
	}
	if hasHoldingAmount {
		doc.Summary.TotalHoldingAmount = available(totalHoldingAmount)
	}
	if hasCostAmount {
		doc.Summary.TotalCostAmount = available(totalCostAmount)
	}
	if hasEstimatedAmount {
		doc.Summary.TotalEstimatedAmount = available(totalEstimatedAmount)
	}
	if hasLatestNAVAmount {
		doc.Summary.TotalLatestNAVAmount = available(totalLatestNAVAmount)
	}
	return doc
}

func buildFund(row tui.Row) Fund {
	name := row.Name
	if name == "" {
		name = row.Quote.Name
	}
	fund := Fund{
		Code:           row.Code,
		Name:           name,
		Share:          row.Share,
		QuoteTime:      row.Quote.GZTime,
		NAVDate:        row.Quote.JZRQ,
		QuoteAvailable: quoteAvailable(row),
	}
	if row.HasHoldingAmount {
		fund.HoldingAmount = available(row.HoldingAmount)
	}
	if row.HasCostAmount {
		fund.CostAmount = available(row.CostAmount)
	}
	if row.HasCostNAV {
		fund.CostNAV = available(row.CostNAV)
	}
	if row.Quote.HasGSZ {
		fund.EstimatedAmount = available(row.Share * row.Quote.GSZ)
	}
	if row.Quote.HasDWJZ {
		fund.LatestNAVAmount = available(row.Share * row.Quote.DWJZ)
	}
	if row.Quote.HasGSZZL {
		fund.EstimatedChangePercent = available(row.Quote.GSZZL)
	}
	if row.HasProfit {
		fund.EstimatedTodayProfitAmount = available(row.TodayProfit)
	}
	if row.Quote.HasZZL {
		fund.LatestNAVChangePercent = available(row.Quote.ZZL)
	}
	return fund
}

func quoteAvailable(row tui.Row) bool {
	if row.QuoteErr != nil {
		return false
	}
	return row.Quote.HasGSZZL || row.Quote.HasGSZ || row.Quote.HasZZL || row.Quote.HasDWJZ
}

func buildErrors(errs map[string]error) []FundError {
	if len(errs) == 0 {
		return nil
	}
	codes := make([]string, 0, len(errs))
	for code, err := range errs {
		if err != nil {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	out := make([]FundError, 0, len(codes))
	for _, code := range codes {
		out = append(out, FundError{Code: code, Message: fmt.Sprint(errs[code])})
	}
	return out
}

func available(value float64) JSONNumber {
	return JSONNumber{Available: true, Value: value}
}
