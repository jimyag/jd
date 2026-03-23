package registry

import (
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

// silence unused import warning until builtin is created
var _ = builtin.BuiltinYAML
