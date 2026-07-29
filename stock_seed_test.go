package main

import (
	"go-stock/backend/data"
	"go-stock/backend/db"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestParseEmbeddedStockBasics(t *testing.T) {
	stocks, err := parseStockBasics(stocksBin)
	if err != nil {
		t.Fatal(err)
	}
	if len(stocks) < 5000 {
		t.Fatalf("embedded stock pool unexpectedly small: %d", len(stocks))
	}
	want := map[string]bool{"SSE": false, "SZSE": false, "BSE": false}
	seen := make(map[string]struct{}, len(stocks))
	for _, stock := range stocks {
		if stock.TsCode == "" || stock.Name == "" {
			t.Fatalf("stock has empty code or name: %+v", stock)
		}
		if _, ok := seen[stock.TsCode]; ok {
			t.Fatalf("duplicate stock code: %s", stock.TsCode)
		}
		seen[stock.TsCode] = struct{}{}
		if _, ok := want[stock.Exchange]; ok {
			want[stock.Exchange] = true
		}
	}
	for exchange, found := range want {
		if !found {
			t.Errorf("embedded stock pool has no %s entries", exchange)
		}
	}
}

func TestSeedStockDataPopulatesEmptyDatabaseOnce(t *testing.T) {
	connection, err := gorm.Open(sqlite.Open(t.TempDir()+"/stock.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.AutoMigrate(&data.StockBasic{}); err != nil {
		t.Fatal(err)
	}
	previous := db.Dao
	db.Dao = connection
	t.Cleanup(func() { db.Dao = previous })

	added, err := seedStockData(stocksBin)
	if err != nil {
		t.Fatal(err)
	}
	if added < 5000 {
		t.Fatalf("seeded too few stocks: %d", added)
	}
	var count int64
	if err := connection.Model(&data.StockBasic{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != int64(added) {
		t.Fatalf("database count %d does not match added %d", count, added)
	}
	addedAgain, err := seedStockData(stocksBin)
	if err != nil {
		t.Fatal(err)
	}
	if addedAgain != 0 {
		t.Fatalf("second seed added %d duplicate stocks", addedAgain)
	}
}
