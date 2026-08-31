package bootstrap

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuditStockBasics(t *testing.T) {
	fixture := []byte(`{"data":{"fields":["ts_code","symbol","name","exchange","market","list_status"],"items":[["000001.SZ","000001","平安银行","SZSE","主板","L"],["600000.SH","600000","浦发银行","SSE","主板","L"],["920001.BJ","920001","北交测试","BSE","北交所","P"]]}}`)
	report, err := AuditStockBasics(fixture)
	require.NoError(t, err)
	require.Empty(t, report.Issues)
	require.Equal(t, 3, report.Total)
	require.Equal(t, map[string]int{"BSE": 1, "SSE": 1, "SZSE": 1}, report.ByExchange)
}

func TestAuditStockBasicsReportsInvalidAndDuplicateRows(t *testing.T) {
	fixture := []byte(`{"data":{"fields":["ts_code","symbol","name","exchange","market","list_status"],"items":[["000001.SZ","000001","","SZSE","主板","L"],["000001.SZ","000001","重复","SSE","主板","D"]]}}`)
	report, err := AuditStockBasics(fixture)
	require.NoError(t, err)
	require.Len(t, report.Issues, 4)
	require.Contains(t, report.Issues, `row 0: empty name`)
	require.Contains(t, report.Issues, `row 1: duplicate ts_code "000001.SZ" (first seen at row 0)`)
}

func TestAuditStockBasicsRequiresExpectedFields(t *testing.T) {
	fixture := []byte(`{"data":{"fields":["ts_code"],"items":[]}}`)
	_, err := AuditStockBasics(fixture)
	require.EqualError(t, err, `stock basics missing required field "symbol"`)
}

func TestEmbeddedStockBasicsPassAudit(t *testing.T) {
	contents, err := os.ReadFile("../../build/stock_basic.json")
	require.NoError(t, err)
	report, err := AuditStockBasics(contents)
	require.NoError(t, err)
	require.Empty(t, report.Issues)
	require.Greater(t, report.Total, 5000)
	require.NotZero(t, report.ByExchange["SSE"])
	require.NotZero(t, report.ByExchange["SZSE"])
	require.NotZero(t, report.ByExchange["BSE"])
}
