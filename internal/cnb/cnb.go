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
	source := "github.com/" + repo
	switch repo {
	case "kubernetes/kubernetes":
		source = "dl.k8s.io/kubectl"
	case "helm/helm":
		source = "get.helm.sh/helm"
	case "go.dev/go":
		source = repo
	}
	return BaseURL + url.PathEscape(source+"/latest") + "/" + url.PathEscape(name)
}

func Repo(rawURL string) string {
	switch {
	case strings.HasPrefix(rawURL, "https://dl.k8s.io/release/"):
		return "kubernetes/kubernetes"
	case strings.HasPrefix(rawURL, "https://get.helm.sh/helm-"):
		return "helm/helm"
	case strings.HasPrefix(rawURL, "https://go.dev/dl/"):
		return "go.dev/go"
	}
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
	repo := Repo(upstream)
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
	switch u.Host {
	case "dl.k8s.io":
		var platform string
		platform, ok = strings.CutPrefix(u.Path, "/release/"+version+"/bin/")
		parts := strings.Split(platform, "/")
		if !ok || len(parts) != 3 || parts[2] != "kubectl" ||
			(parts[0] != "linux" && parts[0] != "darwin") ||
			(parts[1] != "amd64" && parts[1] != "arm64") {
			return "", fmt.Errorf("unsupported kubectl URL: %s", upstream)
		}
		asset = "kubectl-" + parts[0] + "-" + parts[1]
	case "go.dev":
		asset, ok = strings.CutPrefix(u.Path, "/dl/")
		ok = ok && strings.HasPrefix(asset, version+".") && strings.HasSuffix(asset, ".tar.gz")
	case "get.helm.sh":
		asset, ok = strings.CutPrefix(u.Path, "/")
		ok = ok && strings.HasPrefix(asset, "helm-"+version+"-") && strings.HasSuffix(asset, ".tar.gz")
	}
	if !ok || asset == "" || strings.Contains(asset, "/") {
		return "", fmt.Errorf("unsupported binary URL: %s", upstream)
	}
	return AssetURL(repo, asset), nil
}
