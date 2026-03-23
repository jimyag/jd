package installer

import (
	"os"
	"path/filepath"
	"testing"
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
