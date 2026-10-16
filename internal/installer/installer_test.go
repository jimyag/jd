package installer

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	getter "github.com/hashicorp/go-getter/v2"

	"github.com/jimyag/jd/internal/registry"
	"github.com/jimyag/jd/internal/state"
)

type mirrorTransport func(*http.Request) (*http.Response, error)

func TestOfficialScript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JD_DOWNLOAD_SOURCE", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/bin/sh\nset -eu\n[ \"$1\" = 'argument with spaces' ]\nmkdir -p \"$HOME/.local/bin\"\nprintf '#!/bin/sh\\necho script-ok\\n' > \"$HOME/.local/bin/fixture\"\nchmod +x \"$HOME/.local/bin/fixture\"\n")
	}))
	defer server.Close()
	t.Setenv("PATH", filepath.Join(home, ".local/bin")+":"+os.Getenv("PATH"))
	entry := &registry.PackageEntry{Name: "fixture", Methods: []registry.InstallMethod{{Type: "script", URLTemplate: server.URL + "/install.sh", Command: "sh", ScriptArgs: []string{"argument with spaces"}, VersionCommand: `"$HOME/.local/bin/fixture"`}}}
	result, err := InstallTracked(t.Context(), entry, "", InstallOptions{})
	if err != nil || result.Method != "script" || result.Version != "script-ok" || result.Path != filepath.Join(home, ".local/bin/fixture") {
		t.Fatalf("script result = %+v, err = %v", result, err)
	}
	if _, err := InstallTracked(t.Context(), entry, "1.0", InstallOptions{}); err == nil {
		t.Fatal("script silently ignored a version pin")
	}
	entry.Methods[0].URLTemplate = server.URL + "/missing"
	server.Close()
	if _, err := InstallTracked(t.Context(), entry, "", InstallOptions{}); err == nil {
		t.Fatal("failed download was accepted")
	}
}

func TestBinaryHooks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("JD_DOWNLOAD_SOURCE", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/bin/sh\nexit 0\n")
	}))
	defer server.Close()
	method := registry.InstallMethod{Type: "binary", URLTemplate: server.URL + "/fixture", Mode: "file", PreCommands: []string{`touch "$HOME/pre"`}, PostCommands: []string{`test -f "$HOME/pre" && test -x "$HOME/.local/bin/fixture"`}}
	entry := &registry.PackageEntry{Name: "fixture", Methods: []registry.InstallMethod{method}}
	if _, err := InstallTracked(t.Context(), entry, "v1", InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	entry.Methods[0].PostCommands = []string{"exit 9"}
	if _, err := InstallTracked(t.Context(), entry, "v1", InstallOptions{}); err == nil {
		t.Fatal("ignored failed post-install hook")
	}
}

func TestNPMInstallationMetadata(t *testing.T) {
	prefix := t.TempDir()
	root := filepath.Join(prefix, "lib/node_modules/@scope/tool")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"version":"1.2.3"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "bin/npm"), []byte("#!/bin/sh\necho '"+filepath.Join(prefix, "lib/node_modules")+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "bin/tool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	method := &registry.InstallMethod{Type: "npm", Package: "@scope/tool", Env: map[string]string{"PATH": filepath.Join(prefix, "bin")}}
	version, path, err := npmInstallation(t.Context(), &registry.PackageEntry{Name: "tool"}, method, "", "darwin", "arm64")
	if err != nil || version != "1.2.3" || path != filepath.Join(prefix, "bin/tool") {
		t.Fatalf("npm metadata = %s %s, %v", version, path, err)
	}
}

func TestNPMUsesStablePrefix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := t.TempDir()
	script := `#!/bin/sh
set -eu
if [ "$1" = root ]; then
    echo "$NPM_CONFIG_PREFIX/lib/node_modules"
else
    mkdir -p "$NPM_CONFIG_PREFIX/lib/node_modules/fixture" "$NPM_CONFIG_PREFIX/bin"
    echo '{"version":"1.2.3"}' > "$NPM_CONFIG_PREFIX/lib/node_modules/fixture/package.json"
    printf '#!/bin/sh\nexit 0\n' > "$NPM_CONFIG_PREFIX/bin/fixture"
    chmod +x "$NPM_CONFIG_PREFIX/bin/fixture"
fi
`
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"PATH": bin + ":/usr/bin:/bin"}
	entry := &registry.PackageEntry{Name: "fixture", Methods: []registry.InstallMethod{{Type: "npm", Package: "fixture", Env: env}}}
	for _, prefix := range []string{"", filepath.Join(home, "legacy-sdk")} {
		if prefix != "" {
			env["NPM_CONFIG_PREFIX"] = prefix
		}
		result, err := InstallTracked(t.Context(), entry, "", InstallOptions{})
		if prefix == "" {
			prefix = filepath.Join(home, ".local")
		}
		if err != nil || result.Path != filepath.Join(prefix, "bin/fixture") || result.Version != "1.2.3" {
			t.Fatalf("npm installation: %+v, %v", result, err)
		}
		if prefix == filepath.Join(home, ".local") && env["NPM_CONFIG_PREFIX"] != "" {
			t.Fatal("mutated registry environment")
		}
	}
}

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
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if err := os.WriteFile(filepath.Join(dir, "apt"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
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

func TestBinaryPreferredAndFailureDoesNotChangeSource(t *testing.T) {
	entry := &registry.PackageEntry{Name: "demo", Methods: []registry.InstallMethod{
		{Type: "command", Priority: 1000, Command: "exit 0"},
		{Type: "binary", Priority: 1, URLTemplate: "http://127.0.0.1:1/missing", Mode: "file"},
	}}
	methods, err := selectMethods(entry, "", "linux", "amd64")
	if err != nil || methods[0].Type != "binary" {
		t.Fatalf("source preference: %v, %v", methods, err)
	}
	t.Setenv("JD_DOWNLOAD_SOURCE", "")
	if err := Install(t.Context(), entry, "v1"); err == nil {
		t.Fatal("binary failure silently fell back to command")
	}
}

func TestAPKCommands(t *testing.T) {
	method := registry.InstallMethod{Type: "apk", Package: "jq"}
	command, err := defaultCommandForMethod(&registry.PackageEntry{Name: "jq"}, &method, "", "linux", "arm64")
	if err != nil || command != "apk add jq" || !useSudo(&method) {
		t.Fatalf("apk: %q, %v", command, err)
	}
	if _, err := resolveInstallVersion(t.Context(), &method, "1.0"); err == nil {
		t.Fatal("apk silently ignored version pin")
	}
}

func TestBrewVersionUsesActiveKeg(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	active := filepath.Join(dir, "Cellar", "demo", "2.0_1")
	if err := os.MkdirAll(filepath.Join(active, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(active, "bin/demo"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "selected")
	if err := os.Symlink(active, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brew"), []byte("#!/bin/sh\nprintf '%s\\n' '"+link+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	shadow := filepath.Join(dir, ".local/bin")
	if err := os.MkdirAll(shadow, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shadow, "demo"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shadow+string(os.PathListSeparator)+dir+string(os.PathListSeparator)+"/bin")
	result, err := InstallTracked(t.Context(), &registry.PackageEntry{Name: "demo", Methods: []registry.InstallMethod{{Type: "brew", Package: "demo"}}}, "", InstallOptions{Method: "brew"})
	expected, pathErr := filepath.EvalSymlinks(filepath.Join(active, "bin/demo"))
	if err != nil || pathErr != nil || result.Version != "2.0_1" || result.Path != expected {
		t.Fatalf("active installation: %+v, %v", result, err)
	}
}

func TestSkipRequiresRecordedMethodAndPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bin, err := BinDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureBinDir(bin); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bin, "demo")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := &registry.PackageEntry{Name: "demo", Mode: "file", URLTemplate: "http://127.0.0.1:1/missing"}
	for _, test := range []struct {
		method, path string
		skip         bool
	}{{"binary", path, true}, {"brew", path, false}, {"binary", path + "-other", false}} {
		_, err := InstallTracked(t.Context(), entry, "v1", InstallOptions{Current: &state.Installation{Name: "demo", Version: "v1", Method: test.method, Path: test.path}})
		if (err == nil) != test.skip {
			t.Fatalf("method=%s path=%s: %v", test.method, test.path, err)
		}
	}
}

func TestDirectoryInstallPreservesResourcesAndAllCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two"} {
		if err := os.WriteFile(filepath.Join(source, "bin", name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "resource"), []byte("required"), 0o644); err != nil {
		t.Fatal(err)
	}
	method := registry.InstallMethod{InnerPath: ".", InstallDir: "~/.local/share/jd/demo/v1", Binaries: map[string]string{"one": "bin/one", "two": "bin/two"}}
	entry := &registry.PackageEntry{Name: "demo"}
	if err := installDir(source, entry, &method, "v1", "linux", "arm64"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".local/share/jd/demo/v1")
	if !installationPresent(path, method) {
		t.Fatal("directory installation is incomplete")
	}
	if _, err := os.ReadFile(filepath.Join(path, "resource")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, ".local/bin/two")); err != nil {
		t.Fatal(err)
	}
	if installationPresent(path, method) {
		t.Fatal("missing command was accepted as installed")
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
	want := "go install 'golang.org/x/tools/gopls@v0.19.0'"
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
	want := "npm install -g '@openai/codex@1.2.3'"
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
