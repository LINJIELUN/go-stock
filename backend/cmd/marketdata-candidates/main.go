package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"go-stock/backend/marketdata"
)

var candidates = []string{
	"akfamily/akshare",
	"Micro-sheep/efinance",
	"mootdx/mootdx",
	"rainx/pytdx",
	"waditu/tushare",
}

func main() {
	client, err := marketdata.NewGitHubClient(nil, os.Getenv("GH_TOKEN"))
	if err != nil {
		panic(err)
	}
	repositories := make([]marketdata.GitHubRepository, 0, len(candidates))
	for _, candidate := range candidates {
		repository, err := client.FetchRepository(context.Background(), candidate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", candidate, err)
			continue
		}
		repositories = append(repositories, repository)
	}
	sort.Slice(repositories, func(i, j int) bool { return repositories[i].Stars > repositories[j].Stars })
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(repositories); err != nil {
		panic(err)
	}
}
