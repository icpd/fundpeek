package stockexport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	fundapp "github.com/icpd/fundpeek/internal/app"
	"github.com/icpd/fundpeek/internal/valuation"
	"github.com/icpd/fundpeek/internal/watchlist"
)

var (
	fetchStockQuotes = func(ctx context.Context, codes []string) (map[string]valuation.StockQuote, error) {
		return valuation.NewClient().FetchTencentStockQuotes(ctx, codes)
	}
	fetchStockMinute = func(ctx context.Context, code string) (valuation.StockMinute, error) {
		return valuation.NewClient().FetchStockMinute(ctx, code)
	}
	searchStocks = func(ctx context.Context, query string) ([]valuation.StockSearchResult, error) {
		return valuation.NewClient().SearchAStocks(ctx, query)
	}
)

type QuoteDocument struct {
	GeneratedAt string       `json:"generated_at"`
	Stocks      []Stock      `json:"stocks"`
	Errors      []StockError `json:"errors,omitempty"`
}

type SearchDocument struct {
	GeneratedAt string         `json:"generated_at"`
	Query       string         `json:"query"`
	Results     []SearchResult `json:"results"`
	Errors      []StockError   `json:"errors,omitempty"`
}

type MinuteDocument struct {
	GeneratedAt string       `json:"generated_at"`
	Stocks      []Stock      `json:"stocks"`
	Errors      []StockError `json:"errors,omitempty"`
}

type Stock struct {
	Market          string     `json:"market"`
	Code            string     `json:"code"`
	TencentCode     string     `json:"tencent_code,omitempty"`
	Name            string     `json:"name,omitempty"`
	QuoteAvailable  bool       `json:"quote_available,omitempty"`
	Price           JSONNumber `json:"price,omitempty"`
	ChangePercent   JSONNumber `json:"change_percent,omitempty"`
	MinuteAvailable bool       `json:"minute_available,omitempty"`
	Minute          Minute     `json:"minute,omitempty"`
}

type Minute struct {
	Date   string        `json:"date,omitempty"`
	Points []MinutePoint `json:"points,omitempty"`
}

type SearchResult struct {
	Market string `json:"market"`
	Code   string `json:"code"`
	Name   string `json:"name,omitempty"`
}

type MinutePoint struct {
	Time   string  `json:"time"`
	Price  float64 `json:"price"`
	Volume float64 `json:"volume,omitempty"`
	Amount float64 `json:"amount,omitempty"`
}

type StockError struct {
	Code    string `json:"code,omitempty"`
	Scope   string `json:"scope"`
	Message string `json:"message"`
}

type JSONNumber struct {
	Available bool    `json:"available"`
	Value     float64 `json:"value"`
}

type StockRow struct {
	Item      watchlist.Item
	Quote     valuation.StockQuote
	Minute    valuation.StockMinute
	QuoteErr  error
	MinuteErr error
}

func WriteSearch(ctx context.Context, query string, out io.Writer) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("stock query is required")
	}
	results, err := searchStocks(ctx, query)
	docErrs := map[string]error{}
	if err != nil {
		docErrs["search"] = err
		results = nil
	}
	doc := BuildSearchDocument(query, results, docErrs, time.Now())
	return encode(out, doc)
}

func WriteQuote(ctx context.Context, a *fundapp.App, code string, out io.Writer) error {
	item, err := itemFromAStockCode(code)
	if err != nil {
		return err
	}
	rows, errs := refreshQuoteRows(ctx, a, []watchlist.Item{item})
	doc := BuildQuoteDocument(rows, errs, time.Now())
	return encode(out, doc)
}

func WriteMinute(ctx context.Context, a *fundapp.App, code string, out io.Writer) error {
	item, err := itemFromAStockCode(code)
	if err != nil {
		return err
	}
	rows, errs := refreshMinuteRows(ctx, a, []watchlist.Item{item})
	doc := BuildMinuteDocument(rows, errs, time.Now())
	return encode(out, doc)
}

func WriteList(ctx context.Context, a *fundapp.App, out io.Writer) error {
	items, err := a.Watchlist()
	if err != nil {
		return err
	}
	rows, errs := refreshQuoteRows(ctx, a, items)
	doc := BuildQuoteDocument(rows, errs, time.Now())
	return encode(out, doc)
}

func BuildSearchDocument(query string, results []valuation.StockSearchResult, errs map[string]error, generatedAt time.Time) SearchDocument {
	docResults := make([]SearchResult, 0, len(results))
	for _, result := range results {
		docResults = append(docResults, SearchResult{
			Market: strings.TrimSpace(result.Market),
			Code:   strings.TrimSpace(result.Code),
			Name:   strings.TrimSpace(result.Name),
		})
	}
	return SearchDocument{
		GeneratedAt: generatedAt.UTC().Format(time.RFC3339),
		Query:       strings.TrimSpace(query),
		Results:     docResults,
		Errors:      buildErrors("search", errs),
	}
}

func BuildQuoteDocument(rows []StockRow, errs map[string]error, generatedAt time.Time) QuoteDocument {
	doc := QuoteDocument{
		GeneratedAt: generatedAt.UTC().Format(time.RFC3339),
		Stocks:      make([]Stock, 0, len(rows)),
		Errors:      buildErrors("quote", errs),
	}
	for _, row := range rows {
		doc.Stocks = append(doc.Stocks, buildQuoteStock(row))
	}
	return doc
}

func BuildMinuteDocument(rows []StockRow, errs map[string]error, generatedAt time.Time) MinuteDocument {
	doc := MinuteDocument{
		GeneratedAt: generatedAt.UTC().Format(time.RFC3339),
		Stocks:      make([]Stock, 0, len(rows)),
		Errors:      buildErrors("minute", errs),
	}
	for _, row := range rows {
		doc.Stocks = append(doc.Stocks, buildMinuteStock(row))
	}
	return doc
}

func refreshQuoteRows(ctx context.Context, a *fundapp.App, items []watchlist.Item) ([]StockRow, map[string]error) {
	errs := map[string]error{}
	keys := make([]string, 0, len(items))
	keyByID := map[string]string{}
	for _, item := range items {
		item = watchlist.Normalize(item)
		id := stockID(item)
		key := quoteKey(item)
		if key == "" {
			errs[id] = fmt.Errorf("unsupported stock code %s", item.Code)
			continue
		}
		keys = append(keys, key)
		keyByID[id] = key
	}
	quotes, err := fetchStockQuotes(ctx, keys)
	if err != nil {
		errs["quotes"] = err
		quotes = map[string]valuation.StockQuote{}
	}
	for key, quote := range quotes {
		_ = a.SetStockQuote(key, quote)
	}
	rows := make([]StockRow, 0, len(items))
	for _, item := range items {
		item = watchlist.Normalize(item)
		id := stockID(item)
		key := keyByID[id]
		row := StockRow{Item: item}
		if key == "" {
			row.QuoteErr = errs[id]
			rows = append(rows, row)
			continue
		}
		quote, ok := quotes[key]
		row.Quote = quote
		if err != nil {
			row.QuoteErr = err
		} else if !ok || (!quote.HasPrice && !quote.HasChangePercent) {
			row.QuoteErr = fmt.Errorf("missing quote")
			errs[id] = row.QuoteErr
		}
		rows = append(rows, row)
	}
	return rows, errs
}

func refreshMinuteRows(ctx context.Context, a *fundapp.App, items []watchlist.Item) ([]StockRow, map[string]error) {
	errs := map[string]error{}
	rows := make([]StockRow, 0, len(items))
	for _, item := range items {
		item = watchlist.Normalize(item)
		id := stockID(item)
		key := minuteKey(item)
		row := StockRow{Item: item}
		if key == "" {
			row.MinuteErr = fmt.Errorf("unsupported stock code %s", item.Code)
			errs[id] = row.MinuteErr
			rows = append(rows, row)
			continue
		}
		minute, err := fetchStockMinute(ctx, key)
		if err != nil {
			row.MinuteErr = err
			errs[id] = err
			rows = append(rows, row)
			continue
		}
		row.Minute = minute
		_ = a.SetStockMinute(key, minute)
		rows = append(rows, row)
	}
	return rows, errs
}

func buildQuoteStock(row StockRow) Stock {
	item := watchlist.Normalize(row.Item)
	stock := Stock{
		Market:      item.Market,
		Code:        item.Code,
		TencentCode: quoteKey(item),
		Name:        item.Name,
	}
	if row.Quote.Name != "" {
		stock.Name = row.Quote.Name
	}
	if row.QuoteErr == nil && (row.Quote.HasPrice || row.Quote.HasChangePercent) {
		stock.QuoteAvailable = true
	}
	if row.Quote.HasPrice {
		stock.Price = available(row.Quote.Price)
	}
	if row.Quote.HasChangePercent {
		stock.ChangePercent = available(row.Quote.ChangePercent)
	}
	return stock
}

func buildMinuteStock(row StockRow) Stock {
	item := watchlist.Normalize(row.Item)
	stock := Stock{
		Market:          item.Market,
		Code:            item.Code,
		TencentCode:     quoteKey(item),
		Name:            item.Name,
		MinuteAvailable: row.MinuteErr == nil && len(row.Minute.Points) > 0,
		Minute: Minute{
			Date:   row.Minute.Date,
			Points: buildMinutePoints(row.Minute.Points),
		},
	}
	return stock
}

func buildMinutePoints(points []valuation.StockMinutePoint) []MinutePoint {
	out := make([]MinutePoint, 0, len(points))
	for _, point := range points {
		out = append(out, MinutePoint{
			Time:   point.Time,
			Price:  point.Price,
			Volume: point.Volume,
			Amount: point.Amount,
		})
	}
	return out
}

func itemFromAStockCode(code string) (watchlist.Item, error) {
	market, normalized := valuation.NormalizeAStock(code)
	if market == "" || normalized == "" {
		return watchlist.Item{}, fmt.Errorf("unsupported A-share stock code %q", code)
	}
	return watchlist.Item{Market: market, Code: normalized}, nil
}

func quoteKey(item watchlist.Item) string {
	item = watchlist.Normalize(item)
	return valuation.NormalizeTencentCode(item.Code)
}

func minuteKey(item watchlist.Item) string {
	item = watchlist.Normalize(item)
	if item.Market == "" || item.Code == "" {
		return ""
	}
	return item.Market + item.Code
}

func stockID(item watchlist.Item) string {
	item = watchlist.Normalize(item)
	if item.Market == "" {
		return item.Code
	}
	return item.Market + item.Code
}

func buildErrors(scope string, errs map[string]error) []StockError {
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
	out := make([]StockError, 0, len(codes))
	for _, code := range codes {
		out = append(out, StockError{Code: code, Scope: scope, Message: fmt.Sprint(errs[code])})
	}
	return out
}

func available(value float64) JSONNumber {
	return JSONNumber{Available: true, Value: value}
}

func encode(out io.Writer, doc any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
