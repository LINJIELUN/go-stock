package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const EastMoneyReferenceName = "eastmoney-public-reference"

// EastMoneyReferenceClient is an independent technical cross-check source. The
// public web endpoint has no SLA or verified redistribution grant in this project,
// so this adapter must not be promoted to a qualified production provider.
type EastMoneyReferenceClient struct {
	endpoint   *url.URL
	httpClient *http.Client
	now        func() time.Time
}

func NewEastMoneyReferenceClient(endpoint string, client *http.Client) (*EastMoneyReferenceClient, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("EastMoney endpoint must be an absolute HTTPS URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &EastMoneyReferenceClient{endpoint: parsed, httpClient: client, now: time.Now}, nil
}

func (c *EastMoneyReferenceClient) Name() string { return EastMoneyReferenceName }

func (c *EastMoneyReferenceClient) DailyBars(ctx context.Context, instrument Instrument, start, end time.Time, adjustment Adjustment) ([]DailyBar, error) {
	if adjustment != AdjustmentNone {
		return nil, errors.New("EastMoney reference adapter currently supports unadjusted bars only")
	}
	if start.IsZero() || end.IsZero() || start.After(end) {
		return nil, errors.New("daily bar date range is invalid")
	}
	secID, err := eastMoneySecurityID(instrument)
	if err != nil {
		return nil, err
	}
	query := c.endpoint.Query()
	query.Set("secid", secID)
	query.Set("klt", "101")
	query.Set("fqt", "0")
	query.Set("beg", start.Format("20060102"))
	query.Set("end", end.Format("20060102"))
	query.Set("lmt", "10000")
	query.Set("fields1", "f1,f2,f3,f4,f5,f6")
	query.Set("fields2", "f51,f52,f53,f54,f55,f56,f57")
	endpoint := *c.endpoint
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request EastMoney reference daily bars: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request EastMoney reference daily bars: HTTP %d", response.StatusCode)
	}
	var payload struct {
		RC      int    `json:"rc"`
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			Code   string   `json:"code"`
			Klines []string `json:"klines"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 10<<20))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode EastMoney reference response: %w", err)
	}
	if payload.RC != 0 || payload.Code != 0 || payload.Data == nil {
		return nil, fmt.Errorf("EastMoney reference error rc=%d code=%d message=%s", payload.RC, payload.Code, payload.Message)
	}
	if payload.Data.Code != instrument.Code {
		return nil, fmt.Errorf("EastMoney reference returned code %q, expected %q", payload.Data.Code, instrument.Code)
	}
	fetchedAt := c.now()
	bars := make([]DailyBar, 0, len(payload.Data.Klines))
	seen := make(map[string]struct{}, len(payload.Data.Klines))
	for index, line := range payload.Data.Klines {
		bar, err := parseEastMoneyDailyBar(line, instrument, fetchedAt)
		if err != nil {
			return nil, fmt.Errorf("parse EastMoney reference row %d: %w", index, err)
		}
		date := bar.TradeDate.Format(time.DateOnly)
		if _, duplicate := seen[date]; duplicate {
			return nil, fmt.Errorf("EastMoney reference returned duplicate date %s", date)
		}
		seen[date] = struct{}{}
		if bar.TradeDate.Before(dateOnly(start)) || bar.TradeDate.After(dateOnly(end)) {
			return nil, fmt.Errorf("EastMoney reference returned out-of-range date %s", date)
		}
		bars = append(bars, bar)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate.Before(bars[j].TradeDate) })
	return bars, nil
}

func eastMoneySecurityID(instrument Instrument) (string, error) {
	if instrument.SecurityType != SecurityStock && instrument.SecurityType != SecurityETF {
		return "", errors.New("EastMoney reference adapter supports stocks and ETFs only")
	}
	if len(instrument.Code) != 6 || strings.Trim(instrument.Code, "0123456789") != "" {
		return "", errors.New("EastMoney reference adapter requires a six-digit code")
	}
	switch instrument.Exchange {
	case ExchangeShanghai:
		return "1." + instrument.Code, nil
	case ExchangeShenzhen, ExchangeBeijing:
		return "0." + instrument.Code, nil
	default:
		return "", errors.New("EastMoney reference adapter received an unsupported exchange")
	}
}

func parseEastMoneyDailyBar(line string, instrument Instrument, fetchedAt time.Time) (DailyBar, error) {
	parts := strings.Split(line, ",")
	if len(parts) != 7 {
		return DailyBar{}, fmt.Errorf("expected 7 comma-separated fields, got %d", len(parts))
	}
	date, err := time.Parse(time.DateOnly, parts[0])
	if err != nil {
		return DailyBar{}, fmt.Errorf("invalid trade date %q", parts[0])
	}
	values := make([]float64, 6)
	for index := range values {
		values[index], err = strconv.ParseFloat(parts[index+1], 64)
		if err != nil || !finite(values[index]) {
			return DailyBar{}, fmt.Errorf("field %d is not a finite number", index+1)
		}
	}
	bar := DailyBar{
		Instrument: instrument, TradeDate: date, Open: values[0], Close: values[1],
		High: values[2], Low: values[3], Volume: values[4] * 100, Turnover: values[5],
		Adjustment: AdjustmentNone, Source: EastMoneyReferenceName, FetchedAt: fetchedAt,
	}
	if err := validateDailyBar(bar); err != nil {
		return DailyBar{}, err
	}
	return bar, nil
}
