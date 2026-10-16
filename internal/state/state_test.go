package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOperationLockAndRecords(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	lock, err := Lock()
	if err != nil {
		t.Fatal(err)
	}
	if other, err := Lock(); err == nil {
		_ = other.Close()
		t.Fatal("second writer acquired the lock")
	}
	installed := map[string]Installation{"demo": {Name: "demo", Version: "1", Method: "binary"}}
	if err := Save(installed); err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err = Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	loaded, err := Load()
	if err != nil || loaded["demo"] != installed["demo"] {
		t.Fatalf("records: %v, %v", loaded, err)
	}
	dir, _ := Dir()
	if err := os.WriteFile(filepath.Join(dir, "installed.json"), []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("accepted malformed records")
	}
}
