package cnb

import (
	"io"
	"net/http"
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
