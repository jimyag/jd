package cnb

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloadURL(t *testing.T) {
	original := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = original })
	status, body := http.StatusOK, "v1.2.3\n"
	http.DefaultClient = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != AssetURL("owner/tool", "upstream-version.txt") {
			t.Fatalf("unexpected request: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("upstream credentials must not be sent to CNB")
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	upstream := "https://github.com/owner/tool/releases/download/v1.2.3/tool_linux_amd64.tar.gz"
	got, err := DownloadURL(t.Context(), upstream, "v1.2.3")
	if err != nil || got != AssetURL("owner/tool", "tool_linux_amd64.tar.gz") {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := DownloadURL(t.Context(), upstream, "v1.0.0"); err == nil {
		t.Fatal("must reject old versions")
	}
	if _, err := DownloadURL(t.Context(), "https://example.com/tool", "v1.2.3"); err == nil {
		t.Fatal("must reject unmirrored sources")
	}
	status = http.StatusNotFound
	if _, err := Latest(t.Context(), "owner/tool"); err == nil {
		t.Fatal("must fail when CNB release is absent")
	}
	status, body = http.StatusOK, "<html>login</html>"
	if _, err := Latest(t.Context(), "owner/tool"); err == nil {
		t.Fatal("must reject non-version responses")
	}
}

func TestDirectDownloadURL(t *testing.T) {
	for _, tc := range []struct{ upstream, version, tag, asset string }{
		{"https://dl.k8s.io/release/v1.37.1/bin/linux/amd64/kubectl", "v1.37.1", "dl.k8s.io/kubectl/latest", "kubectl-linux-amd64"},
		{"https://dl.k8s.io/release/v1.37.1/bin/darwin/arm64/kubectl", "v1.37.1", "dl.k8s.io/kubectl/latest", "kubectl-darwin-arm64"},
		{"https://go.dev/dl/go1.27.3.linux-arm64.tar.gz", "go1.27.3", "go.dev/go/latest", "go1.27.3.linux-arm64.tar.gz"},
		{"https://get.helm.sh/helm-v4.3.1-darwin-amd64.tar.gz", "v4.3.1", "get.helm.sh/helm/latest", "helm-v4.3.1-darwin-amd64.tar.gz"},
	} {
		t.Run(tc.asset, func(t *testing.T) {
			original := http.DefaultClient
			t.Cleanup(func() { http.DefaultClient = original })
			http.DefaultClient = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/jimyag/release/-/releases/download/"+tc.tag+"/upstream-version.txt" {
					t.Fatalf("unexpected version source: %s", r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.version)), Header: make(http.Header)}, nil
			})}
			got, err := DownloadURL(t.Context(), tc.upstream, tc.version)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(got)
			if err != nil || u.Path != "/jimyag/release/-/releases/download/"+tc.tag+"/"+tc.asset {
				t.Fatalf("wrong asset: %s, %v", got, err)
			}
			if _, err := DownloadURL(t.Context(), tc.upstream, "v0.1.0"); err == nil {
				t.Fatal("must reject a version unavailable in the mirror")
			}
		})
	}
}
