package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jimyag/jd/internal/installer"
	"github.com/jimyag/jd/internal/registry"
)

func TestInstallMethodFlagPassesThrough(t *testing.T) {
	originalInstaller := installPackage
	originalRegistryLoader := loadRegistry
	originalMethod := rootMethod
	t.Cleanup(func() {
		installPackage = originalInstaller
		loadRegistry = originalRegistryLoader
		rootMethod = originalMethod
	})

	var gotMethod string
	installPackage = func(_ context.Context, _ *registry.PackageEntry, _ string, opts installer.InstallOptions) error {
		gotMethod = opts.Method
		return nil
	}
	loadRegistry = registry.LoadBuiltin

	rootMethod = "apt"
	if err := rootCmd.RunE(rootCmd, []string{"gh"}); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "apt" {
		t.Fatalf("got method %q", gotMethod)
	}
}

func TestRootCommandLoadsLocalRegistryFromHomeConfig(t *testing.T) {
	originalInstaller := installPackage
	originalRegistryLoader := loadRegistry
	t.Cleanup(func() {
		installPackage = originalInstaller
		loadRegistry = originalRegistryLoader
	})

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
	installPackage = func(_ context.Context, entry *registry.PackageEntry, _ string, _ installer.InstallOptions) error {
		gotName = entry.Name
		return nil
	}
	loadRegistry = registry.Load

	if err := rootCmd.RunE(rootCmd, []string{"local-tool"}); err != nil {
		t.Fatal(err)
	}
	if gotName != "local-tool" {
		t.Fatalf("got package %q", gotName)
	}
}
