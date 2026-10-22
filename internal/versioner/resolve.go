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
	repo := src.Repo
	if src.Type == "godev" {
		repo = "go.dev/go"
	}
	if cnb.Enabled() {
		if src.Type != "github" && src.Type != "godev" {
			return "", fmt.Errorf("CNB does not mirror version source %q", src.Type)
		}
		return cnb.Latest(context.Background(), repo)
	}
	v, err := New(src)
	if err != nil {
		return "", err
	}
	latest, err := v.Latest()
	if err != nil && (src.Type == "github" || src.Type == "godev") {
		mirrored, mirrorErr := cnb.Latest(context.Background(), repo)
		if mirrorErr != nil {
			return "", errors.Join(err, mirrorErr)
		}
		return mirrored, nil
	}
	return latest, err
}

// List returns available versions for the given version source.
func List(src registry.VersionSource) ([]string, error) {
	if cnb.Enabled() {
		latest, err := Latest(src)
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
