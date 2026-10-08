package installer

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	getter "github.com/hashicorp/go-getter/v2"

	"github.com/jimyag/jd/internal/registry"
)

type mirrorTransport func(*http.Request) (*http.Response, error)

func (f mirrorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCNBInstallAndAutomaticRetry(t *testing.T) {
	for _, mode := range []string{"automatic", "automatic-missing", "cnb", "missing", "old-version"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("JD_DOWNLOAD_SOURCE", "")
			if mode != "automatic" && mode != "automatic-missing" {
				t.Setenv("JD_DOWNLOAD_SOURCE", "cnb")
			}
			originalClient, originalGetters := http.DefaultClient, getter.Getters
			t.Cleanup(func() { http.DefaultClient, getter.Getters = originalClient, originalGetters })
			var requests []string
			client := &http.Client{Transport: mirrorTransport(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.String())
				status, body := http.StatusOK, "#!/bin/sh\nexit 0\n"
				if r.URL.Host == "github.com" {
					if mode != "automatic" && mode != "automatic-missing" {
						t.Fatal("strict CNB contacted GitHub")
					}
					status = http.StatusServiceUnavailable
				} else if r.URL.Host != "cnb.cool" {
					t.Fatalf("unexpected host: %s", r.URL.Host)
				}
				if strings.HasSuffix(r.URL.Path, "/upstream-version.txt") {
					body = "v1.2.3\n"
				}
				if mode == "missing" || mode == "automatic-missing" {
					status = http.StatusNotFound
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), ContentLength: int64(len(body)), Request: r}, nil
			})}
			http.DefaultClient = client
			getter.Getters = []getter.Getter{&getter.HttpGetter{Client: client}}
			entry := &registry.PackageEntry{Name: "demo", Mode: "file", URLTemplate: "https://github.com/owner/tool/releases/download/{{.Version}}/demo", Methods: []registry.InstallMethod{
				{Type: "binary", Priority: 100}, {Type: "command", Priority: 80, Command: "exit 0"},
			}}
			version := "v1.2.3"
			if mode == "old-version" {
				version = "v1.0.0"
			}
			err := Install(t.Context(), entry, version)
			if mode == "missing" || mode == "automatic-missing" || mode == "old-version" {
				if err == nil {
					t.Fatal("expected error without command fallback")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			binDir, err := BinDir()
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(binDir, "demo"))
			if err != nil || string(data) != "#!/bin/sh\nexit 0\n" {
				t.Fatalf("bad installed binary: %q, %v", data, err)
			}
			if len(requests) == 0 {
				t.Fatal("no requests")
			}
		})
	}
}

func TestEnsureBinDir(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "bin")

	err := ensureBinDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Error("expected a directory")
	}
}

func TestMoveBinary(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "mybinary")
	dst := filepath.Join(tmp, "bin", "mybinary")

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("fake binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := moveBinary(src, dst)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal("binary not at destination:", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("binary not executable")
	}
}

func TestMoveBinarySetsExecutableMode(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "mybinary")
	dst := filepath.Join(tmp, "bin", "mybinary")

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("fake binary"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := moveBinary(src, dst); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("got mode %o, want 755", got)
	}
}

func TestDecompressGzipFileIfNeeded(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "download")

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte("binary data")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := decompressGzipFileIfNeeded(path); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "binary data" {
		t.Fatalf("got %q", string(data))
	}
}

func TestSelectMethodsSortsAndFilters(t *testing.T) {
	entry := &registry.PackageEntry{
		Name: "gh",
		Methods: []registry.InstallMethod{
			{Type: "apt", Priority: 20, SupportedPlatforms: []string{"linux"}},
			{Type: "binary", Priority: 100, SupportedPlatforms: []string{"darwin", "linux"}},
			{Type: "brew", Priority: 50, SupportedPlatforms: []string{"darwin"}},
		},
	}

	methods, err := selectMethods(entry, "apt", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 {
		t.Fatalf("got %d methods", len(methods))
	}
	if methods[0].Type != "apt" {
		t.Fatalf("got method %q", methods[0].Type)
	}

	methods, err = selectMethods(entry, "", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 2 {
		t.Fatalf("got %d methods", len(methods))
	}
	if methods[0].Type != "binary" {
		t.Fatalf("got first method %q", methods[0].Type)
	}
	if methods[1].Type != "apt" {
		t.Fatalf("got second method %q", methods[1].Type)
	}
}

func TestSelectMethodsErrorsForMissingRequestedMethod(t *testing.T) {
	entry := &registry.PackageEntry{
		Name: "gh",
		Methods: []registry.InstallMethod{
			{Type: "binary", Priority: 100, SupportedPlatforms: []string{"darwin"}},
		},
	}

	_, err := selectMethods(entry, "apt", "linux", "amd64")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultCommandForGoMethodUsesRequestedVersion(t *testing.T) {
	method := &registry.InstallMethod{
		Type:    "go",
		Package: "golang.org/x/tools/gopls@latest",
	}

	got, err := defaultCommandForMethod(&registry.PackageEntry{Name: "gopls"}, method, "v0.19.0", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	want := "go install golang.org/x/tools/gopls@v0.19.0"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDefaultCommandForScopedNPMPackageUsesRequestedVersion(t *testing.T) {
	method := &registry.InstallMethod{
		Type:    "npm",
		Package: "@openai/codex@latest",
	}

	got, err := defaultCommandForMethod(&registry.PackageEntry{Name: "codex"}, method, "1.2.3", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	want := "npm install -g @openai/codex@1.2.3"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveCommandVersionSkipsUnusedInheritedVersionSource(t *testing.T) {
	method := &registry.InstallMethod{
		Type:    "brew",
		Package: "jq",
		VersionFrom: registry.VersionSource{
			Type: "github",
			Repo: "stedolan/jq",
		},
	}

	got, err := resolveCommandVersion(method, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty version", got)
	}
}

func TestResolveCommandVersionDefaultsGoPackageToLatest(t *testing.T) {
	method := &registry.InstallMethod{
		Type:    "go",
		Package: "golang.org/x/tools/gopls",
	}

	got, err := resolveCommandVersion(method, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "latest" {
		t.Fatalf("got %q, want latest", got)
	}
}

func TestRunCommandMethodExecutesPreAndPostCommands(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "steps.log")
	method := &registry.InstallMethod{
		Type:        "command",
		Command:     "printf 'main\\n' >> " + logPath,
		PreCommands: []string{"printf 'pre\\n' >> " + logPath},
		PostCommands: []string{
			"printf 'post\\n' >> " + logPath,
		},
	}

	if err := runCommand(context.Background(), &registry.PackageEntry{Name: "demo"}, method, "", "linux", "amd64"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "pre\nmain\npost\n" {
		t.Fatalf("got %q", string(data))
	}
}

func TestRunCommandMethodSkipsPostCommandsAfterFailure(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "steps.log")
	method := &registry.InstallMethod{
		Type:        "command",
		Command:     "printf 'main\\n' >> " + logPath + " && exit 1",
		PreCommands: []string{"printf 'pre\\n' >> " + logPath},
		PostCommands: []string{
			"printf 'post\\n' >> " + logPath,
		},
	}

	if err := runCommand(context.Background(), &registry.PackageEntry{Name: "demo"}, method, "", "linux", "amd64"); err == nil {
		t.Fatal("expected error")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "pre\nmain\n" {
		t.Fatalf("got %q", string(data))
	}
}

func TestInstallWithOptionsIncludesDocURLInErrors(t *testing.T) {
	entry := &registry.PackageEntry{
		Name: "gh",
		Methods: []registry.InstallMethod{
			{
				Type:     "command",
				Priority: 100,
				Command:  "exit 1",
				DocURL:   "https://example.com/install-gh",
			},
		},
	}

	err := InstallWithOptions(context.Background(), entry, "", InstallOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "docs: https://example.com/install-gh") {
		t.Fatalf("missing docs url in %q", got)
	}
}
