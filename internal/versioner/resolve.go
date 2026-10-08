package versioner

import (
	"context"
	"errors"
	"fmt"

	"github.com/jimyag/jd/internal/cnb"
	"github.com/jimyag/jd/internal/registry"
)

// Latest returns the latest version for the given version source.
func Latest(src registry.VersionSource) (string, error) {
	if src.Type == "github" && cnb.Enabled() {
		return cnb.Latest(context.Background(), src.Repo)
	}
	v, err := New(src)
	if err != nil {
		return "", err
	}
	latest, err := v.Latest()
	if err != nil && src.Type == "github" {
		mirrored, mirrorErr := cnb.Latest(context.Background(), src.Repo)
		if mirrorErr != nil {
			return "", errors.Join(err, mirrorErr)
		}
		return mirrored, nil
	}
	return latest, err
}

// List returns available versions for the given version source.
func List(src registry.VersionSource) ([]string, error) {
	if cnb.Enabled() && src.Type != "github" {
		return nil, fmt.Errorf("CNB does not mirror version source %q", src.Type)
	}
	if src.Type == "github" && cnb.Enabled() {
		latest, err := cnb.Latest(context.Background(), src.Repo)
		if err != nil {
			return nil, err
		}
		return []string{latest}, nil
	}
	v, err := New(src)
	if err != nil {
		return nil, err
	}
	return v.List()
}
