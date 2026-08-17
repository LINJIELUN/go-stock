package marketdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEastMoneyReferenceDailyBarsMapsAndNormalizes(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("secid") != "1.600519" || query.Get("klt") != "101" || query.Get("fqt") != "0" ||
			query.Get("beg") != "20260813" || query.Get("end") != "20260814" {
			t.Fatalf("unexpected EastMoney query: %s", request.URL.RawQuery)
		}
		_, _ = writer.Write([]byte(`{"rc":0,"code":0,"data":{"code":"600519","klines":["2026-08-14,1400,1410,1420,1390,123,456700"]}}`))
	}))
	defer server.Close()
	client, err := NewEastMoneyReferenceClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }
	instrument, _ := NormalizeInstrument("600519.SH", SecurityStock)
	start := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	bars, err := client.DailyBars(context.Background(), instrument, start, end, AdjustmentNone)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 1 || bars[0].Volume != 12300 || bars[0].Turnover != 456700 || bars[0].Source != EastMoneyReferenceName {
		t.Fatalf("unexpected EastMoney bar: %+v", bars)
	}
}

func TestEastMoneyReferenceSupportsExplicitBSEMapping(t *testing.T) {
	instrument, _ := NormalizeInstrument("830799.BJ", SecurityStock)
	secID, err := eastMoneySecurityID(instrument)
	if err != nil || secID != "0.830799" {
		t.Fatalf("unexpected BSE secid %q, %v", secID, err)
	}
}

func TestEastMoneyReferenceFailsClosedOnWrongCodeAndMalformedRows(t *testing.T) {
	responses := []string{
		`{"rc":0,"code":0,"data":{"code":"000002","klines":[]}}`,
		`{"rc":0,"code":0,"data":{"code":"000001","klines":["2026-08-14,10,bad,11,9,1,2"]}}`,
	}
	call := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(responses[call]))
		call++
	}))
	defer server.Close()
	client, _ := NewEastMoneyReferenceClient(server.URL, server.Client())
	instrument, _ := NormalizeInstrument("000001.SZ", SecurityStock)
	date := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	if _, err := client.DailyBars(context.Background(), instrument, date, date, AdjustmentNone); err == nil {
		t.Fatal("expected response code mismatch rejection")
	}
	if _, err := client.DailyBars(context.Background(), instrument, date, date, AdjustmentNone); err == nil {
		t.Fatal("expected malformed numeric field rejection")
	}
}

func TestEastMoneyReferenceRejectsUnsafeEndpointAndAdjustment(t *testing.T) {
	if _, err := NewEastMoneyReferenceClient("http://push2his.eastmoney.com", nil); err == nil {
		t.Fatal("expected non-HTTPS endpoint rejection")
	}
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	client, _ := NewEastMoneyReferenceClient(server.URL, server.Client())
	instrument, _ := NormalizeInstrument("159915.SZ", SecurityETF)
	now := time.Now()
	if _, err := client.DailyBars(context.Background(), instrument, now, now, AdjustmentForward); err == nil {
		t.Fatal("expected adjusted request rejection")
	}
}
