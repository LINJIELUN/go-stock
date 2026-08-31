package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GitHubRepository struct {
	FullName       string    `json:"fullName"`
	URL            string    `json:"url"`
	Description    string    `json:"description"`
	Stars          int       `json:"stars"`
	Forks          int       `json:"forks"`
	OpenIssues     int       `json:"openIssues"`
	Archived       bool      `json:"archived"`
	LicenseSPDX    string    `json:"licenseSpdx"`
	DefaultBranch  string    `json:"defaultBranch"`
	PushedAt       time.Time `json:"pushedAt"`
	FetchedAt      time.Time `json:"fetchedAt"`
	PopularityOnly bool      `json:"popularityOnly"`
}

type GitHubClient struct {
	baseURL *url.URL
	http    *http.Client
	token   string
	now     func() time.Time
}

func NewGitHubClient(client *http.Client, token string) (*GitHubClient, error) {
	base, _ := url.Parse("https://api.github.com/")
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &GitHubClient{baseURL: base, http: client, token: token, now: time.Now}, nil
}

// FetchRepository collects maintenance/popularity metadata only. It cannot verify
// upstream data accuracy, licensing, display rights, pricing, or a real-time SLA.
func (c *GitHubClient) FetchRepository(ctx context.Context, fullName string) (GitHubRepository, error) {
	parts := strings.Split(fullName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(fullName, "?#") {
		return GitHubRepository{}, errors.New("GitHub repository must use owner/name form")
	}
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: "repos/" + parts[0] + "/" + parts[1]})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return GitHubRepository{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return GitHubRepository{}, fmt.Errorf("fetch GitHub repository %s: %w", fullName, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return GitHubRepository{}, fmt.Errorf("fetch GitHub repository %s: HTTP %d", fullName, response.StatusCode)
	}
	var payload struct {
		FullName      string `json:"full_name"`
		HTMLURL       string `json:"html_url"`
		Description   string `json:"description"`
		Stars         int    `json:"stargazers_count"`
		Forks         int    `json:"forks_count"`
		OpenIssues    int    `json:"open_issues_count"`
		Archived      bool   `json:"archived"`
		DefaultBranch string `json:"default_branch"`
		PushedAt      string `json:"pushed_at"`
		License       *struct {
			SPDX string `json:"spdx_id"`
		} `json:"license"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return GitHubRepository{}, fmt.Errorf("decode GitHub repository %s: %w", fullName, err)
	}
	pushedAt, err := time.Parse(time.RFC3339, payload.PushedAt)
	if err != nil {
		return GitHubRepository{}, fmt.Errorf("decode GitHub pushed_at for %s: %w", fullName, err)
	}
	license := ""
	if payload.License != nil {
		license = payload.License.SPDX
	}
	return GitHubRepository{
		FullName: payload.FullName, URL: payload.HTMLURL, Description: payload.Description,
		Stars: payload.Stars, Forks: payload.Forks, OpenIssues: payload.OpenIssues,
		Archived: payload.Archived, LicenseSPDX: license, DefaultBranch: payload.DefaultBranch,
		PushedAt: pushedAt, FetchedAt: c.now(), PopularityOnly: true,
	}, nil
}
