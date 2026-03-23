package cmd

import (
	"context"
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
