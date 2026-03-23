package registry

import (
	"os"
	"path/filepath"
	"testing"

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

// silence unused import warning until builtin is created
var _ = builtin.BuiltinYAML
