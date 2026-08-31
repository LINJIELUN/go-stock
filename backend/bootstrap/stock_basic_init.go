package bootstrap

import (
	"encoding/json"
	"fmt"
	"go-stock/backend/data"

	"gorm.io/gorm"
)

const StockBasicInsertBatchSize = 500

// initializeStockBasics seeds a newly-created StockBasic table from the
// embedded Tushare response. Existing databases are left untouched, making
// startup initialization idempotent and preserving later data updates.
func InitializeStockBasics(database *gorm.DB, embeddedJSON []byte, batchSize int) error {
	if database == nil {
		return fmt.Errorf("database is nil")
	}
	if batchSize <= 0 {
		return fmt.Errorf("batch size must be positive")
	}

	var count int64
	if err := database.Model(&data.StockBasic{}).Count(&count).Error; err != nil {
		return fmt.Errorf("count stock basics: %w", err)
	}
	if count > 0 {
		return nil
	}

	var response data.TushareStockBasicResponse
	if err := json.Unmarshal(embeddedJSON, &response); err != nil {
		return fmt.Errorf("decode embedded stock basics: %w", err)
	}

	fieldIndexes := make(map[string]int, len(response.Data.Fields))
	for index, field := range response.Data.Fields {
		fieldIndexes[field] = index
	}

	stocks := make([]data.StockBasic, 0, len(response.Data.Items))
	for rowIndex, item := range response.Data.Items {
		stockData := make(map[string]any, len(fieldIndexes))
		for field, index := range fieldIndexes {
			if index >= len(item) {
				return fmt.Errorf("decode embedded stock basics: row %d has no value for %q", rowIndex, field)
			}
			stockData[field] = item[index]
		}
		encoded, err := json.Marshal(stockData)
		if err != nil {
			return fmt.Errorf("encode embedded stock basic row %d: %w", rowIndex, err)
		}
		var stock data.StockBasic
		if err := json.Unmarshal(encoded, &stock); err != nil {
			return fmt.Errorf("decode embedded stock basic row %d: %w", rowIndex, err)
		}
		if stock.TsCode == "" {
			return fmt.Errorf("decode embedded stock basics: row %d has empty ts_code", rowIndex)
		}
		stocks = append(stocks, stock)
	}
	if len(stocks) == 0 {
		return fmt.Errorf("embedded stock basics contain no rows")
	}

	if err := database.Transaction(func(tx *gorm.DB) error {
		return tx.CreateInBatches(&stocks, batchSize).Error
	}); err != nil {
		return fmt.Errorf("insert embedded stock basics: %w", err)
	}
	return nil
}
