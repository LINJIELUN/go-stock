package marketdata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const TushareProviderName = "tushare-pro"

type DailyBarProvider interface {
	Name() string
	DailyBars(context.Context, Instrument, time.Time, time.Time, Adjustment) ([]DailyBar, error)
}

type TradeCalendarProvider interface {
	Name() string
	TradingDates(context.Context, Exchange, time.Time, time.Time) ([]time.Time, error)
}

type TushareClient struct {
	endpoint   *url.URL
	token      string
	httpClient *http.Client
	now        func() time.Time
}

func NewTushareClient(endpoint, token string, client *http.Client) (*TushareClient, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("TuShare token is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("TuShare endpoint must be an absolute HTTPS URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &TushareClient{endpoint: parsed, token: token, httpClient: client, now: time.Now}, nil
}

func (c *TushareClient) Name() string { return TushareProviderName }

func (c *TushareClient) TradingDates(ctx context.Context, exchange Exchange, start, end time.Time) ([]time.Time, error) {
	if start.IsZero() || end.IsZero() || start.After(end) {
		return nil, errors.New("trading-calendar date range is invalid")
	}
	exchangeCode := ""
	switch exchange {
	case ExchangeShanghai:
		exchangeCode = "SSE"
	case ExchangeShenzhen:
		exchangeCode = "SZSE"
	case ExchangeBeijing:
		return nil, errors.New("TuShare trade_cal documentation does not confirm BSE calendar support")
	default:
		return nil, errors.New("TuShare calendar adapter received an unsupported exchange")
	}
	payload := struct {
		APIName string         `json:"api_name"`
		Token   string         `json:"token"`
		Params  map[string]any `json:"params"`
		Fields  string         `json:"fields"`
	}{
		APIName: "trade_cal", Token: c.token,
		Params: map[string]any{"exchange": exchangeCode, "start_date": start.Format("20060102"), "end_date": end.Format("20060102")},
		Fields: "exchange,cal_date,is_open,pretrade_date",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request TuShare trade_cal: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request TuShare trade_cal: HTTP %d", response.StatusCode)
	}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Data    struct {
			Fields []string `json:"fields"`
			Items  [][]any  `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode TuShare trade_cal response: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("TuShare trade_cal error %d: %s", result.Code, result.Message)
	}
	columns, err := calendarColumns(result.Data.Fields)
	if err != nil {
		return nil, err
	}
	startDate, endDate := dateOnly(start), dateOnly(end)
	seen := make(map[string]struct{}, len(result.Data.Items))
	openDates := make([]time.Time, 0, len(result.Data.Items))
	for rowIndex, item := range result.Data.Items {
		if len(item) < len(result.Data.Fields) {
			return nil, fmt.Errorf("TuShare trade_cal row %d has missing values", rowIndex)
		}
		returnedExchange, ok := item[columns["exchange"]].(string)
		if !ok || returnedExchange != exchangeCode {
			return nil, fmt.Errorf("TuShare trade_cal row %d returned exchange %q, expected %q", rowIndex, returnedExchange, exchangeCode)
		}
		dateText, ok := item[columns["cal_date"]].(string)
		if !ok {
			return nil, fmt.Errorf("TuShare trade_cal row %d has invalid cal_date", rowIndex)
		}
		date, err := time.Parse("20060102", dateText)
		if err != nil || date.Before(startDate) || date.After(endDate) {
			return nil, fmt.Errorf("TuShare trade_cal row %d has invalid or out-of-range date %q", rowIndex, dateText)
		}
		if _, duplicate := seen[dateText]; duplicate {
			return nil, fmt.Errorf("TuShare trade_cal returned duplicate date %s", dateText)
		}
		seen[dateText] = struct{}{}
		isOpen, ok := item[columns["is_open"]].(float64)
		if !ok || (isOpen != 0 && isOpen != 1) {
			return nil, fmt.Errorf("TuShare trade_cal row %d has invalid is_open", rowIndex)
		}
		if isOpen == 1 {
			openDates = append(openDates, date)
		}
	}
	expectedNaturalDays := int(endDate.Sub(startDate).Hours()/24) + 1
	if len(seen) != expectedNaturalDays {
		return nil, fmt.Errorf("TuShare trade_cal returned %d calendar days, expected %d", len(seen), expectedNaturalDays)
	}
	sort.Slice(openDates, func(i, j int) bool { return openDates[i].Before(openDates[j]) })
	return openDates, nil
}

func (c *TushareClient) DailyBars(ctx context.Context, instrument Instrument, start, end time.Time, adjustment Adjustment) ([]DailyBar, error) {
	if adjustment != AdjustmentNone {
		return nil, errors.New("TuShare daily adapter currently supports unadjusted bars only")
	}
	if start.IsZero() || end.IsZero() || start.After(end) {
		return nil, errors.New("daily bar date range is invalid")
	}
	tsCode, err := tushareCode(instrument)
	if err != nil {
		return nil, err
	}
	apiName := "daily"
	if instrument.SecurityType == SecurityETF {
		apiName = "fund_daily"
	}
	payload := struct {
		APIName string         `json:"api_name"`
		Token   string         `json:"token"`
		Params  map[string]any `json:"params"`
		Fields  string         `json:"fields"`
	}{
		APIName: apiName, Token: c.token,
		Params: map[string]any{"ts_code": tsCode, "start_date": start.Format("20060102"), "end_date": end.Format("20060102")},
		Fields: "ts_code,trade_date,open,high,low,close,vol,amount",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request TuShare %s: %w", apiName, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request TuShare %s: HTTP %d", apiName, response.StatusCode)
	}
	var result struct {
		RequestID string `json:"request_id"`
		Code      int    `json:"code"`
		Message   string `json:"msg"`
		Data      struct {
			Fields []string `json:"fields"`
			Items  [][]any  `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode TuShare %s response: %w", apiName, err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("TuShare %s error %d: %s", apiName, result.Code, result.Message)
	}
	columns, err := requiredColumns(result.Data.Fields)
	if err != nil {
		return nil, err
	}
	fetchedAt := c.now()
	bars := make([]DailyBar, 0, len(result.Data.Items))
	seen := make(map[string]struct{}, len(result.Data.Items))
	for rowIndex, item := range result.Data.Items {
		bar, err := parseTushareDailyBar(item, columns, instrument, tsCode, fetchedAt)
		if err != nil {
			return nil, fmt.Errorf("parse TuShare %s row %d: %w", apiName, rowIndex, err)
		}
		date := bar.TradeDate.Format(time.DateOnly)
		if _, duplicate := seen[date]; duplicate {
			return nil, fmt.Errorf("TuShare %s returned duplicate trade date %s", apiName, date)
		}
		seen[date] = struct{}{}
		if bar.TradeDate.Before(dateOnly(start)) || bar.TradeDate.After(dateOnly(end)) {
			return nil, fmt.Errorf("TuShare %s returned date %s outside requested range", apiName, date)
		}
		bars = append(bars, bar)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate.Before(bars[j].TradeDate) })
	return bars, nil
}

func tushareCode(instrument Instrument) (string, error) {
	if instrument.SecurityType != SecurityStock && instrument.SecurityType != SecurityETF {
		return "", errors.New("TuShare adapter supports stocks and ETFs only")
	}
	suffix := ""
	switch instrument.Exchange {
	case ExchangeShanghai:
		suffix = "SH"
	case ExchangeShenzhen:
		suffix = "SZ"
	case ExchangeBeijing:
		suffix = "BJ"
	default:
		return "", errors.New("TuShare adapter received an unsupported exchange")
	}
	if len(instrument.Code) != 6 || strings.Trim(instrument.Code, "0123456789") != "" {
		return "", errors.New("TuShare adapter requires a six-digit security code")
	}
	return instrument.Code + "." + suffix, nil
}

func requiredColumns(fields []string) (map[string]int, error) {
	columns := make(map[string]int, len(fields))
	for index, field := range fields {
		columns[field] = index
	}
	for _, field := range []string{"ts_code", "trade_date", "open", "high", "low", "close", "vol", "amount"} {
		if _, exists := columns[field]; !exists {
			return nil, fmt.Errorf("TuShare response is missing required field %q", field)
		}
	}
	return columns, nil
}

func calendarColumns(fields []string) (map[string]int, error) {
	columns := make(map[string]int, len(fields))
	for index, field := range fields {
		columns[field] = index
	}
	for _, field := range []string{"exchange", "cal_date", "is_open"} {
		if _, exists := columns[field]; !exists {
			return nil, fmt.Errorf("TuShare trade_cal response is missing required field %q", field)
		}
	}
	return columns, nil
}

func parseTushareDailyBar(item []any, columns map[string]int, instrument Instrument, expectedCode string, fetchedAt time.Time) (DailyBar, error) {
	value := func(name string) (any, error) {
		index := columns[name]
		if index >= len(item) || item[index] == nil {
			return nil, fmt.Errorf("field %q is missing", name)
		}
		return item[index], nil
	}
	stringValue := func(name string) (string, error) {
		entry, err := value(name)
		if err != nil {
			return "", err
		}
		text, ok := entry.(string)
		if !ok || text == "" {
			return "", fmt.Errorf("field %q is not a string", name)
		}
		return text, nil
	}
	number := func(name string) (float64, error) {
		entry, err := value(name)
		if err != nil {
			return 0, err
		}
		numeric, ok := entry.(float64)
		if !ok || !finite(numeric) {
			return 0, fmt.Errorf("field %q is not a finite number", name)
		}
		return numeric, nil
	}
	code, err := stringValue("ts_code")
	if err != nil || code != expectedCode {
		return DailyBar{}, fmt.Errorf("response security code %q does not match %q", code, expectedCode)
	}
	tradeDate, err := stringValue("trade_date")
	if err != nil {
		return DailyBar{}, err
	}
	parsedDate, err := time.Parse("20060102", tradeDate)
	if err != nil {
		return DailyBar{}, fmt.Errorf("invalid trade_date %q", tradeDate)
	}
	open, err := number("open")
	if err != nil {
		return DailyBar{}, err
	}
	high, err := number("high")
	if err != nil {
		return DailyBar{}, err
	}
	low, err := number("low")
	if err != nil {
		return DailyBar{}, err
	}
	closePrice, err := number("close")
	if err != nil {
		return DailyBar{}, err
	}
	volumeLots, err := number("vol")
	if err != nil {
		return DailyBar{}, err
	}
	amountThousands, err := number("amount")
	if err != nil {
		return DailyBar{}, err
	}
	bar := DailyBar{
		Instrument: instrument, TradeDate: parsedDate, Open: open, High: high, Low: low, Close: closePrice,
		Volume: volumeLots * 100, Turnover: amountThousands * 1000, Adjustment: AdjustmentNone,
		Source: TushareProviderName, FetchedAt: fetchedAt,
	}
	if err := validateDailyBar(bar); err != nil {
		return DailyBar{}, err
	}
	return bar, nil
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
