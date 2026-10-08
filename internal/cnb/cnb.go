package cnb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const BaseURL = "https://cnb.cool/jimyag/release/-/releases/download/"

var ErrUnavailable = errors.New("CNB mirror unavailable")

func Enabled() bool { return os.Getenv("JD_DOWNLOAD_SOURCE") == "cnb" }

func AssetURL(repo, name string) string {
	return BaseURL + url.PathEscape("github.com/"+repo+"/latest") + "/" + url.PathEscape(name)
}

func GitHubRepo(rawURL string) string {
	path, ok := strings.CutPrefix(rawURL, "https://github.com/")
	if !ok {
		return ""
	}
	repo, _, ok := strings.Cut(path, "/releases/download/")
	if !ok || strings.Count(repo, "/") != 1 {
		return ""
	}
	return repo
}

func Latest(ctx context.Context, repo string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, AssetURL(repo, "upstream-version.txt"), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: fetch version for %s: %w", ErrUnavailable, repo, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: latest release for %s: HTTP %d", ErrUnavailable, repo, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1025))
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(data))
	if len(data) > 1024 || version == "" || strings.ContainsAny(version, " /\r\n\t<>") {
		return "", fmt.Errorf("invalid CNB version for %s", repo)
	}
	return version, nil
}

func DownloadURL(ctx context.Context, upstream, version string) (string, error) {
	repo := GitHubRepo(upstream)
	if repo == "" {
		return "", fmt.Errorf("CNB does not mirror this binary source: %s", upstream)
	}
	latest, err := Latest(ctx, repo)
	if err != nil {
		return "", err
	}
	if version != latest {
		return "", fmt.Errorf("%w: only mirrors %s@%s; requested %s", ErrUnavailable, repo, latest, version)
	}
	u, err := url.Parse(upstream)
	if err != nil {
		return "", err
	}
	_, asset, ok := strings.Cut(u.Path, "/releases/download/"+version+"/")
	if !ok || asset == "" || strings.Contains(asset, "/") {
		return "", fmt.Errorf("unsupported GitHub release URL: %s", upstream)
	}
	return AssetURL(repo, asset), nil
}
