package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go-stock/backend/bootstrap"
	"os"
)

func main() {
	path := flag.String("file", "build/stock_basic.json", "path to the embedded A-share stock pool")
	flag.Parse()

	contents, err := os.ReadFile(*path)
	if err != nil {
		fail(fmt.Errorf("read %s: %w", *path, err))
	}
	report, err := bootstrap.AuditStockBasics(contents)
	if err != nil {
		fail(err)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fail(fmt.Errorf("encode report: %w", err))
	}
	fmt.Println(string(encoded))
	if len(report.Issues) > 0 {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "stock pool audit:", err)
	os.Exit(1)
}
