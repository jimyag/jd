package versioner

import (
	"github.com/jimyag/jd/internal/registry"
)

// Latest returns the latest version for the given version source.
func Latest(src registry.VersionSource) (string, error) {
	v, err := New(src)
	if err != nil {
		return "", err
	}
	return v.Latest()
}

// List returns available versions for the given version source.
func List(src registry.VersionSource) ([]string, error) {
	v, err := New(src)
	if err != nil {
		return nil, err
	}
	return v.List()
}
