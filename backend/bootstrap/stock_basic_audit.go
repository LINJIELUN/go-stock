package bootstrap

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var stockCodePattern = regexp.MustCompile(`^[0-9]{6}\.(SH|SZ|BJ)$`)

// StockBasicAuditReport summarizes the embedded A-share stock pool without
// requiring a database or network connection.
type StockBasicAuditReport struct {
	Total      int            `json:"total"`
	ByExchange map[string]int `json:"by_exchange"`
	ByMarket   map[string]int `json:"by_market"`
	Issues     []string       `json:"issues"`
}

// AuditStockBasics validates the Tushare-shaped embedded stock pool.
func AuditStockBasics(embeddedJSON []byte) (StockBasicAuditReport, error) {
	report := StockBasicAuditReport{
		ByExchange: map[string]int{},
		ByMarket:   map[string]int{},
		Issues:     []string{},
	}
	var payload struct {
		Data struct {
			Fields []string `json:"fields"`
			Items  [][]any  `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(embeddedJSON, &payload); err != nil {
		return report, fmt.Errorf("decode stock basics: %w", err)
	}

	indexes := make(map[string]int, len(payload.Data.Fields))
	for index, field := range payload.Data.Fields {
		indexes[field] = index
	}
	for _, required := range []string{"ts_code", "symbol", "name", "exchange", "market", "list_status"} {
		if _, ok := indexes[required]; !ok {
			return report, fmt.Errorf("stock basics missing required field %q", required)
		}
	}

	seen := make(map[string]int, len(payload.Data.Items))
	for rowIndex, row := range payload.Data.Items {
		value := func(field string) string {
			index := indexes[field]
			if index >= len(row) || row[index] == nil {
				return ""
			}
			return strings.TrimSpace(fmt.Sprint(row[index]))
		}
		tsCode, symbol, name := value("ts_code"), value("symbol"), value("name")
		exchange, market, listStatus := value("exchange"), value("market"), value("list_status")
		report.Total++
		report.ByExchange[exchange]++
		report.ByMarket[market]++

		if name == "" {
			report.Issues = append(report.Issues, fmt.Sprintf("row %d: empty name", rowIndex))
		}
		if !stockCodePattern.MatchString(tsCode) {
			report.Issues = append(report.Issues, fmt.Sprintf("row %d: invalid ts_code %q", rowIndex, tsCode))
		}
		if len(symbol) != 6 || tsCode != symbol+exchangeSuffix(exchange) {
			report.Issues = append(report.Issues, fmt.Sprintf("row %d: symbol %q does not match ts_code %q and exchange %q", rowIndex, symbol, tsCode, exchange))
		}
		if listStatus != "L" && listStatus != "P" {
			report.Issues = append(report.Issues, fmt.Sprintf("row %d: unsupported list_status %q", rowIndex, listStatus))
		}
		if previous, ok := seen[tsCode]; ok {
			report.Issues = append(report.Issues, fmt.Sprintf("row %d: duplicate ts_code %q (first seen at row %d)", rowIndex, tsCode, previous))
		} else {
			seen[tsCode] = rowIndex
		}
	}
	if report.Total == 0 {
		report.Issues = append(report.Issues, "stock pool is empty")
	}
	sort.Strings(report.Issues)
	return report, nil
}

func exchangeSuffix(exchange string) string {
	switch exchange {
	case "SSE":
		return ".SH"
	case "SZSE":
		return ".SZ"
	case "BSE":
		return ".BJ"
	default:
		return ""
	}
}
