package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jimyag/jd/internal/registry"
)

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
