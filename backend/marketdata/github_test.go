package marketdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestGitHubClientFetchesRepositoryWithoutClaimingQualification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/akfamily/akshare" || request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("unexpected request: %s auth=%q", request.URL.Path, request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"full_name":"akfamily/akshare","html_url":"https://github.com/akfamily/akshare",
			"description":"financial interface library","stargazers_count":22000,"forks_count":3400,
			"open_issues_count":10,"archived":false,"default_branch":"main",
			"pushed_at":"2026-08-13T18:01:15Z","license":{"spdx_id":"MIT"}
		}`))
	}))
	defer server.Close()
	client, _ := NewGitHubClient(server.Client(), "test-token")
	client.baseURL, _ = url.Parse(server.URL + "/")
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }
	repository, err := client.FetchRepository(context.Background(), "akfamily/akshare")
	if err != nil {
		t.Fatal(err)
	}
	if repository.Stars != 22000 || repository.LicenseSPDX != "MIT" || !repository.PopularityOnly || repository.FetchedAt != now {
		t.Fatalf("unexpected repository metadata: %+v", repository)
	}
}

func TestGitHubClientRejectsInvalidNamesBeforeNetwork(t *testing.T) {
	client, _ := NewGitHubClient(http.DefaultClient, "")
	for _, name := range []string{"", "owner", "owner/repo/extra", "owner/repo?x=1"} {
		if _, err := client.FetchRepository(context.Background(), name); err == nil {
			t.Fatalf("expected invalid repository %q to fail", name)
		}
	}
}
