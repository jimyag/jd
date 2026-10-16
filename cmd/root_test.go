package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jimyag/jd/internal/installer"
	"github.com/jimyag/jd/internal/registry"
	"github.com/jimyag/jd/internal/state"
)

func TestInstallMethodFlagPassesThrough(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	originalInstaller := installPackage
	originalRegistryLoader := loadRegistry
	originalMethod := rootMethod
	originalRefreshRegistry := rootRefreshRegistry
	t.Cleanup(func() {
		installPackage = originalInstaller
		loadRegistry = originalRegistryLoader
		rootMethod = originalMethod
		rootRefreshRegistry = originalRefreshRegistry
	})

	var gotMethod string
	installPackage = func(_ context.Context, entry *registry.PackageEntry, _ string, opts installer.InstallOptions) (state.Installation, error) {
		gotMethod = opts.Method
		return state.Installation{Name: entry.Name, Method: opts.Method}, nil
	}
	loadRegistry = registry.LoadBuiltin

	rootMethod = "apt"
	rootRefreshRegistry = false
	if err := rootCmd.RunE(rootCmd, []string{"gh"}); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "apt" {
		t.Fatalf("got method %q", gotMethod)
	}
	rootMethod = ""
	gotMethod = ""
	if err := rootCmd.RunE(rootCmd, []string{"gh"}); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "apt" {
		t.Fatalf("reinstall lost recorded source: %q", gotMethod)
	}
}

func TestUpgradePreservesMethodsPinsAndFailedRecords(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	lock, err := state.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	installed := map[string]state.Installation{
		"actionlint": {Name: "actionlint", Method: "binary", Version: "v1"},
		"dive":       {Name: "dive", Method: "binary", Version: "v1", RequestedVersion: "v1"},
		"gopls":      {Name: "gopls", Method: "go", Version: "v1"},
	}
	if err := state.Save(installed); err != nil {
		t.Fatal(err)
	}
	original := installPackage
	t.Cleanup(func() { installPackage = original })
	var names []string
	installPackage = func(_ context.Context, entry *registry.PackageEntry, version string, opts installer.InstallOptions) (state.Installation, error) {
		names = append(names, entry.Name)
		if version != "" {
			t.Fatalf("latest must use automatic resolution, got %q", version)
		}
		if opts.Method != installed[entry.Name].Method || !opts.Upgrade {
			t.Fatal("lost recorded installation method")
		}
		if entry.Name == "gopls" {
			return state.Installation{}, fmt.Errorf("failed build")
		}
		return state.Installation{Name: entry.Name, Method: opts.Method, Version: "v2"}, nil
	}
	r, err := registry.LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	if err := upgradePackages(t.Context(), r, nil); err == nil {
		t.Fatal("upgrade hid failed build")
	}
	loaded, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || loaded["actionlint"].Version != "v2" || loaded["dive"] != installed["dive"] || loaded["gopls"] != installed["gopls"] {
		t.Fatalf("incorrect upgrade state: %v, %v", names, loaded)
	}
	if err := upgradePackages(t.Context(), r, []string{"dive@latest"}); err != nil {
		t.Fatal(err)
	}
	loaded, err = state.Load()
	if err != nil || loaded["dive"].RequestedVersion != "" || loaded["dive"].Version != "v2" {
		t.Fatalf("@latest did not clear the pin: %v, %v", loaded["dive"], err)
	}
}

func TestRootCommandLoadsLocalRegistryFromHomeConfig(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	originalInstaller := installPackage
	originalRegistryLoader := loadRegistry
	originalRefreshRegistry := rootRefreshRegistry
	t.Cleanup(func() {
		installPackage = originalInstaller
		loadRegistry = originalRegistryLoader
		rootRefreshRegistry = originalRefreshRegistry
	})
	t.Setenv("JD_DISABLE_REMOTE_REGISTRY", "1")

	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".config", "jd")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "packages.yaml"), []byte(`
packages:
  - name: local-tool
    description: local tool
    doc_url: "https://example.com/local-tool"
    command: "echo local"
    mode: command
`), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotName string
	installPackage = func(_ context.Context, entry *registry.PackageEntry, _ string, _ installer.InstallOptions) (state.Installation, error) {
		gotName = entry.Name
		return state.Installation{Name: entry.Name, Method: "command"}, nil
	}
	loadRegistry = registry.Load
	rootRefreshRegistry = false

	if err := rootCmd.RunE(rootCmd, []string{"local-tool"}); err != nil {
		t.Fatal(err)
	}
	if gotName != "local-tool" {
		t.Fatalf("got package %q", gotName)
	}
}

func TestCNBFlagAppliesToRegistryAndInstaller(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	originalInstaller, originalLoader, originalCNB := installPackage, loadRegistry, rootCNB
	t.Cleanup(func() {
		installPackage, loadRegistry, rootCNB = originalInstaller, originalLoader, originalCNB
	})
	t.Setenv("JD_DOWNLOAD_SOURCE", "github")
	rootCNB = true
	loadRegistry = func() (*registry.Registry, error) {
		if os.Getenv("JD_DOWNLOAD_SOURCE") != "cnb" {
			t.Fatal("registry must use CNB")
		}
		return registry.LoadBuiltin()
	}
	installPackage = func(_ context.Context, entry *registry.PackageEntry, _ string, _ installer.InstallOptions) (state.Installation, error) {
		if os.Getenv("JD_DOWNLOAD_SOURCE") != "cnb" {
			t.Fatal("installer must use CNB")
		}
		return state.Installation{Name: entry.Name, Method: "binary"}, nil
	}
	if err := rootCmd.RunE(rootCmd, []string{"zellij"}); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("JD_DOWNLOAD_SOURCE") != "github" {
		t.Fatal("source leaked after command")
	}
}
