package versioner

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jimyag/jd/internal/registry"
)

type mirrorTransport func(*http.Request) (*http.Response, error)

func (f mirrorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCNBLatestAndGitHubRetry(t *testing.T) {
	for _, source := range []string{"", "cnb"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("JD_DOWNLOAD_SOURCE", source)
			original, originalGitHub := http.DefaultClient, httpClient
			t.Cleanup(func() { http.DefaultClient, httpClient = original, originalGitHub })
			client := &http.Client{Transport: mirrorTransport(func(r *http.Request) (*http.Response, error) {
				status, body := http.StatusOK, "v1.2.3\n"
				if r.URL.Host == "api.github.com" {
					if source == "cnb" {
						t.Fatal("CNB mode contacted GitHub")
					}
					status = http.StatusServiceUnavailable
				} else if r.URL.Host != "cnb.cool" {
					t.Fatalf("unexpected host: %s", r.URL.Host)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			http.DefaultClient, httpClient = client, client
			src := registry.VersionSource{Type: "github", Repo: "owner/tool"}
			got, err := Latest(src)
			if err != nil || got != "v1.2.3" {
				t.Fatalf("got %q, %v", got, err)
			}
			if source == "cnb" {
				versions, err := List(src)
				if err != nil || len(versions) != 1 || versions[0] != "v1.2.3" {
					t.Fatalf("got %v, %v", versions, err)
				}
			}
		})
	}
}
