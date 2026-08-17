package marketdata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTushareDailyBarsMapsStockAndETFAndNormalizesUnits(t *testing.T) {
	var apiNames []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			APIName string         `json:"api_name"`
			Token   string         `json:"token"`
			Params  map[string]any `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Token != "secret-token" || payload.Params["start_date"] != "20260813" || payload.Params["end_date"] != "20260814" {
			t.Fatalf("unexpected request payload: %+v", payload)
		}
		apiNames = append(apiNames, payload.APIName)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"request_id":"req-1","code":0,"msg":null,
			"data":{"fields":["ts_code","trade_date","open","high","low","close","vol","amount"],
			"items":[["` + payload.Params["ts_code"].(string) + `","20260814",10,11,9,10.5,123,456.7]]}
		}`))
	}))
	defer server.Close()
	client, err := NewTushareClient(server.URL, "secret-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }
	start := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	stock, _ := NormalizeInstrument("600519.SH", SecurityStock)
	bars, err := client.DailyBars(context.Background(), stock, start, end, AdjustmentNone)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 1 || bars[0].Volume != 12300 || bars[0].Turnover != 456700 || bars[0].FetchedAt != now {
		t.Fatalf("unexpected normalized stock bar: %+v", bars)
	}
	etf, _ := NormalizeInstrument("159915.SZ", SecurityETF)
	if _, err := client.DailyBars(context.Background(), etf, start, end, AdjustmentNone); err != nil {
		t.Fatal(err)
	}
	if len(apiNames) != 2 || apiNames[0] != "daily" || apiNames[1] != "fund_daily" {
		t.Fatalf("unexpected TuShare APIs: %+v", apiNames)
	}
}

func TestTushareDailyBarsFailsClosedOnAPIAndSchemaErrors(t *testing.T) {
	responses := []string{
		`{"request_id":"r","code":-2001,"msg":"permission denied","data":{"fields":[],"items":[]}}`,
		`{"request_id":"r","code":0,"msg":null,"data":{"fields":["ts_code","trade_date","close"],"items":[]}}`,
	}
	call := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(responses[call]))
		call++
	}))
	defer server.Close()
	client, _ := NewTushareClient(server.URL, "token", server.Client())
	instrument, _ := NormalizeInstrument("830799.BJ", SecurityStock)
	start := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	if _, err := client.DailyBars(context.Background(), instrument, start, start, AdjustmentNone); err == nil {
		t.Fatal("expected TuShare API error")
	}
	if _, err := client.DailyBars(context.Background(), instrument, start, start, AdjustmentNone); err == nil {
		t.Fatal("expected missing schema field error")
	}
}

func TestTushareClientRejectsUnsafeConfigurationAndUnsupportedAdjustment(t *testing.T) {
	if _, err := NewTushareClient("http://api.tushare.pro", "token", nil); err == nil {
		t.Fatal("expected non-HTTPS endpoint rejection")
	}
	if _, err := NewTushareClient("https://api.tushare.pro", "", nil); err == nil {
		t.Fatal("expected empty token rejection")
	}
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	client, _ := NewTushareClient(server.URL, "token", server.Client())
	instrument, _ := NormalizeInstrument("600519.SH", SecurityStock)
	date := time.Now()
	if _, err := client.DailyBars(context.Background(), instrument, date, date, AdjustmentForward); err == nil {
		t.Fatal("expected unsupported adjustment rejection")
	}
}

func TestTushareTradingDatesRequiresCompleteNaturalDateCoverage(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			APIName string         `json:"api_name"`
			Params  map[string]any `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.APIName != "trade_cal" || payload.Params["exchange"] != "SSE" {
			t.Fatalf("unexpected calendar request: %+v", payload)
		}
		_, _ = writer.Write([]byte(`{
			"code":0,"msg":null,"data":{
			"fields":["exchange","cal_date","is_open","pretrade_date"],
			"items":[["SSE","20260814",1,"20260813"],["SSE","20260815",0,"20260814"],["SSE","20260816",0,"20260814"]]}}
		`))
	}))
	defer server.Close()
	client, _ := NewTushareClient(server.URL, "token", server.Client())
	start := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	dates, err := client.TradingDates(context.Background(), ExchangeShanghai, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(dates) != 1 || dates[0].Format(time.DateOnly) != "2026-08-14" {
		t.Fatalf("unexpected open dates: %+v", dates)
	}
}

func TestTushareTradingDatesRejectsIncompleteResponseAndUnconfirmedBSE(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = writer.Write([]byte(`{"code":0,"data":{"fields":["exchange","cal_date","is_open"],"items":[["SZSE","20260814",1]]}}`))
	}))
	defer server.Close()
	client, _ := NewTushareClient(server.URL, "token", server.Client())
	start := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	if _, err := client.TradingDates(context.Background(), ExchangeShenzhen, start, end); err == nil {
		t.Fatal("expected incomplete natural-date response rejection")
	}
	if _, err := client.TradingDates(context.Background(), ExchangeBeijing, start, end); err == nil {
		t.Fatal("expected undocumented BSE calendar support rejection")
	}
	if calls != 1 {
		t.Fatalf("BSE rejection unexpectedly called provider: %d", calls)
	}
}
