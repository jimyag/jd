package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

type Installation struct {
	Name             string `json:"name"`
	RequestedVersion string `json:"requested_version,omitempty"`
	Version          string `json:"version,omitempty"`
	Method           string `json:"method"`
	Package          string `json:"package,omitempty"`
	Path             string `json:"path,omitempty"`
	GoVersion        string `json:"go_version,omitempty"`
}

func Dir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "jd"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "jd"), nil
}

// Lock covers the whole command, including downloads and registry/state writes.
// Keep the lock file in place so all processes lock the same inode.
func Lock() (*flock.Flock, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "operation.lock")
	lock := flock.New(path)
	ok, err := lock.TryLock()
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	if !ok {
		pid, _ := os.ReadFile(path)
		_ = lock.Close()
		return nil, fmt.Errorf("another jd operation is running (holder PID: %s); retry after it finishes", string(pid))
	}
	if err := os.WriteFile(path, fmt.Appendf(nil, "%d", os.Getpid()), 0o600); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return lock, nil
}

func Load() (map[string]Installation, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "installed.json"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Installation{}, nil
	}
	if err != nil {
		return nil, err
	}
	var installed map[string]Installation
	if err := json.Unmarshal(data, &installed); err != nil {
		return nil, fmt.Errorf("read jd installation records: %w", err)
	}
	if installed == nil {
		return nil, fmt.Errorf("invalid jd installation records: expected an object")
	}
	for name, record := range installed {
		if name == "" || name != record.Name || record.Method == "" {
			return nil, fmt.Errorf("invalid jd installation record %q", name)
		}
	}
	return installed, nil
}

// Save must be called while holding Lock.
func Save(installed map[string]Installation) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(installed, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "installed-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "installed.json"))
}
