package versioner

import (
	"fmt"

	"github.com/jimyag/jd/internal/registry"
)

// Latest returns the latest version for the given version source.
func Latest(src registry.VersionSource) (string, error) {
	switch src.Type {
	case "godev":
		return GoDevLatestVersion()
	case "github":
		return LatestVersion(src.Repo)
	default:
		return "", fmt.Errorf("unknown version source type: %q", src.Type)
	}
}

// List returns available versions for the given version source.
func List(src registry.VersionSource) ([]string, error) {
	switch src.Type {
	case "godev":
		return GoDevListVersions()
	case "github":
		return ListVersions(src.Repo)
	default:
		return nil, fmt.Errorf("unknown version source type: %q", src.Type)
	}
}
