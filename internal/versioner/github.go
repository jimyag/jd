package versioner

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const githubAPIBase = "https://api.github.com"

// GitHub implements Versioner using the GitHub Releases API.
type GitHub struct {
	repo string
}

func (g *GitHub) Latest() (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", githubAPIBase, g.repo)
	req, err := newGitHubRequest(url)
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

func (g *GitHub) List() ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=30", githubAPIBase, g.repo)
	releases, err := fetchGitHubReleases(url)
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

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

func fetchGitHubReleases(url string) ([]githubRelease, error) {
	req, err := newGitHubRequest(url)
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

func newGitHubRequest(url string) (*http.Request, error) {
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
