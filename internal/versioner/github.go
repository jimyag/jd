package versioner

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const githubAPIBase = "https://api.github.com"

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

// ListVersions returns up to 30 stable release versions for the given repo.
func ListVersions(repo string) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=30", githubAPIBase, repo)
	releases, err := fetchReleases(url)
	if err != nil {
		return nil, err
	}

	versions := make([]string, 0, len(releases))
	for _, r := range releases {
		if r.Prerelease || r.Draft {
			continue
		}
		versions = append(versions, r.TagName)
	}
	return versions, nil
}

// LatestVersion returns the latest stable release tag for the given repo.
func LatestVersion(repo string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", githubAPIBase, repo)

	req, err := newRequest(url)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("GitHub API rate limit exceeded — set GITHUB_TOKEN env var to increase limit")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d for %s", resp.StatusCode, url)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return release.TagName, nil
}

func fetchReleases(url string) ([]githubRelease, error) {
	req, err := newRequest(url)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("GitHub API rate limit exceeded — set GITHUB_TOKEN env var to increase limit")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return releases, nil
}

func newRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

// parseVersionFromTag returns the tag as-is (the tag IS the version string).
// TagPrefix is informational only — kept for future filtering use.
func parseVersionFromTag(tag, _ string) string {
	_ = strings.TrimPrefix // imported for future use
	return tag
}
