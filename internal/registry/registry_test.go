package registry

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jimyag/jd/internal/registry/builtin"
)

func TestLoadBuiltin(t *testing.T) {
	r, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.packages) == 0 {
		t.Fatal("expected at least one package")
	}
}

func TestFind_Exists(t *testing.T) {
	r, _ := LoadBuiltin()
	pkg, ok := r.Find("kubectl")
	if !ok {
		t.Fatal("kubectl not found")
	}
	if pkg.Name != "kubectl" {
		t.Errorf("got %q", pkg.Name)
	}
}

func TestFind_NotExists(t *testing.T) {
	r, _ := LoadBuiltin()
	_, ok := r.Find("nonexistent-tool-xyz")
	if ok {
		t.Error("expected not found")
	}
}

func TestList(t *testing.T) {
	r, _ := LoadBuiltin()
	pkgs := r.List()
	if len(pkgs) == 0 {
		t.Fatal("expected packages")
	}
}

func TestLoadFromYAMLRejectsEmptyPackageName(t *testing.T) {
	_, err := loadFromYAML([]byte(`
packages:
  - description: missing name
`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromYAMLRejectsDuplicatePackageName(t *testing.T) {
	_, err := loadFromYAML([]byte(`
packages:
  - name: demo
    description: first
  - name: demo
    description: second
`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuiltinPackagesHaveDocURLs(t *testing.T) {
	r, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}

	for _, pkg := range r.List() {
		methods := pkg.SortedMethods()
		if len(methods) == 0 {
			t.Fatalf("package %s has no install methods", pkg.Name)
		}
		for _, method := range methods {
			if method.DocURL == "" {
				t.Fatalf("package %s method %s is missing doc_url", pkg.Name, method.Type)
			}
		}
	}
}

func TestLoadMergesLocalRegistryOverBuiltin(t *testing.T) {
	t.Setenv("JD_DISABLE_REMOTE_REGISTRY", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".config", "jd")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "packages.yaml"), []byte(`
packages:
  - name: kubectl
    description: local kubectl
    doc_url: "https://example.com/kubectl"
    command: "echo local"
    mode: command
  - name: custom-tool
    description: custom tool
    doc_url: "https://example.com/custom-tool"
    command: "echo custom"
    mode: command
`), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	kubectl, ok := r.Find("kubectl")
	if !ok {
		t.Fatal("kubectl not found")
	}
	if kubectl.Description != "local kubectl" {
		t.Fatalf("got %q", kubectl.Description)
	}

	custom, ok := r.Find("custom-tool")
	if !ok {
		t.Fatal("custom-tool not found")
	}
	if custom.Description != "custom tool" {
		t.Fatalf("got %q", custom.Description)
	}
}

func TestLoadMergesPackagesDirectoryInLexicalOrder(t *testing.T) {
	t.Setenv("JD_DISABLE_REMOTE_REGISTRY", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".config", "jd", "packages.d")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "10-base.yaml"), []byte(`
packages:
  - name: custom-tool
    description: base
    doc_url: "https://example.com/base"
    command: "echo base"
    mode: command
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "20-override.yaml"), []byte(`
packages:
  - name: custom-tool
    description: override
    doc_url: "https://example.com/override"
    command: "echo override"
    mode: command
`), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	custom, ok := r.Find("custom-tool")
	if !ok {
		t.Fatal("custom-tool not found")
	}
	if custom.Description != "override" {
		t.Fatalf("got %q", custom.Description)
	}
}

func TestLoadMergesRemoteBetweenBuiltinAndLocal(t *testing.T) {
	configTestHome(t)

	server := registryServer(t, `
packages:
  - name: kubectl
    description: remote kubectl
    doc_url: "https://example.com/remote-kubectl"
    command: "echo remote"
    mode: command
  - name: remote-tool
    description: remote tool
    doc_url: "https://example.com/remote-tool"
    command: "echo remote"
    mode: command
`)
	t.Setenv("JD_REGISTRY_URL", server.URL)

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "jd")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "packages.yaml"), []byte(`
packages:
  - name: kubectl
    description: local kubectl
    doc_url: "https://example.com/local-kubectl"
    command: "echo local"
    mode: command
`), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	kubectl, ok := r.Find("kubectl")
	if !ok {
		t.Fatal("kubectl not found")
	}
	if kubectl.Description != "local kubectl" {
		t.Fatalf("got %q", kubectl.Description)
	}

	remote, ok := r.Find("remote-tool")
	if !ok {
		t.Fatal("remote-tool not found")
	}
	if remote.Description != "remote tool" {
		t.Fatalf("got %q", remote.Description)
	}
}

func TestLoadUsesFreshRemoteRegistryCache(t *testing.T) {
	configTestHome(t)

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`
packages:
  - name: remote-cache-tool
    description: cached remote tool
    doc_url: "https://example.com/remote-cache-tool"
    command: "echo remote"
    mode: command
`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("JD_REGISTRY_URL", server.URL)

	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if requests != 1 {
		t.Fatalf("got %d requests, want 1", requests)
	}
	if _, ok := r.Find("remote-cache-tool"); !ok {
		t.Fatal("remote-cache-tool not found")
	}
}

func TestLoadWithRefreshRemoteBypassesCache(t *testing.T) {
	configTestHome(t)

	body := `
packages:
  - name: refresh-tool
    description: first
    doc_url: "https://example.com/refresh-tool"
    command: "echo first"
    mode: command
`
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	t.Setenv("JD_REGISTRY_URL", server.URL)

	if _, err := Load(); err != nil {
		t.Fatal(err)
	}

	body = `
packages:
  - name: refresh-tool
    description: second
    doc_url: "https://example.com/refresh-tool"
    command: "echo second"
    mode: command
`
	r, err := LoadWithOptions(LoadOptions{RefreshRemote: true})
	if err != nil {
		t.Fatal(err)
	}

	if requests != 2 {
		t.Fatalf("got %d requests, want 2", requests)
	}
	pkg, ok := r.Find("refresh-tool")
	if !ok {
		t.Fatal("refresh-tool not found")
	}
	if pkg.Description != "second" {
		t.Fatalf("got %q", pkg.Description)
	}
}

func TestLoadFallsBackToBuiltinWhenCacheExpiredAndRemoteFails(t *testing.T) {
	configTestHome(t)

	server := registryServer(t, `
packages:
  - name: expired-cache-tool
    description: expired cache tool
    doc_url: "https://example.com/expired-cache-tool"
    command: "echo expired"
    mode: command
`)
	t.Setenv("JD_REGISTRY_URL", server.URL)

	if _, err := Load(); err != nil {
		t.Fatal(err)
	}

	cachePath, err := remoteRegistryCachePath()
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-remoteRegistryCacheTTL - time.Minute)
	if err := os.Chtimes(cachePath, old, old); err != nil {
		t.Fatal(err)
	}
	server.Close()

	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Find("expired-cache-tool"); ok {
		t.Fatal("expired-cache-tool should not be loaded from expired cache")
	}
	if _, ok := r.Find("kubectl"); !ok {
		t.Fatal("builtin registry was not loaded")
	}
}

func configTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
}

func registryServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// silence unused import warning until builtin is created
var _ = builtin.BuiltinYAML
