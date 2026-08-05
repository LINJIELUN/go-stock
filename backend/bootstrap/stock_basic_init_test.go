package bootstrap

import (
	"fmt"
	"go-stock/backend/data"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInitializeStockBasicsSQLite(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "stock.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&data.StockBasic{}))

	const fixture = `{
  "data": {
    "fields": ["ts_code", "symbol", "name", "industry", "exchange", "list_status"],
    "items": [
      ["000001.SZ", "000001", "平安银行", "银行", "SZSE", "L"],
      ["600000.SH", "600000", "浦发银行", "银行", "SSE", "L"],
      ["920001.BJ", "920001", "北交测试", "测试", "BSE", "L"]
    ]
  }
}`

	// A batch size of two forces multiple INSERT statements against real SQLite.
	require.NoError(t, InitializeStockBasics(database, []byte(fixture), 2))

	var stocks []data.StockBasic
	require.NoError(t, database.Order("ts_code").Find(&stocks).Error)
	require.Len(t, stocks, 3)
	require.Equal(t, "平安银行", findStock(t, stocks, "000001.SZ").Name)
	require.Equal(t, "SSE", findStock(t, stocks, "600000.SH").Exchange)

	// A second startup must neither duplicate nor overwrite existing data.
	require.NoError(t, database.Model(&data.StockBasic{}).
		Where("ts_code = ?", "000001.SZ").Update("name", "用户更新名称").Error)
	require.NoError(t, InitializeStockBasics(database, []byte(fixture), 2))

	var count int64
	require.NoError(t, database.Model(&data.StockBasic{}).Count(&count).Error)
	require.EqualValues(t, 3, count)
	var updated data.StockBasic
	require.NoError(t, database.Where("ts_code = ?", "000001.SZ").First(&updated).Error)
	require.Equal(t, "用户更新名称", updated.Name)
}

func TestInitializeStockBasicsRollsBackInvalidEmbeddedData(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "stock.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&data.StockBasic{}))

	invalid := []byte(`{"data":{"fields":["ts_code","name"],"items":[["000001.SZ","平安银行"],["600000.SH"]]}}`)
	err = InitializeStockBasics(database, invalid, 1)
	require.ErrorContains(t, err, `has no value for "name"`)

	var count int64
	require.NoError(t, database.Model(&data.StockBasic{}).Count(&count).Error)
	require.Zero(t, count, fmt.Sprintf("invalid seed must not leave partial rows; got %d", count))
}

func findStock(t *testing.T, stocks []data.StockBasic, tsCode string) data.StockBasic {
	t.Helper()
	for _, stock := range stocks {
		if stock.TsCode == tsCode {
			return stock
		}
	}
	t.Fatalf("stock %s not found", tsCode)
	return data.StockBasic{}
}
