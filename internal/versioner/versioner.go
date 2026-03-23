package versioner

import (
	"fmt"

	"github.com/jimyag/jd/internal/registry"
)

// Versioner can resolve versions for a package.
type Versioner interface {
	Latest() (string, error)
	List() ([]string, error)
}

// New returns the Versioner implementation for the given version source.
func New(src registry.VersionSource) (Versioner, error) {
	switch src.Type {
	case "github":
		return &GitHub{repo: src.Repo}, nil
	case "godev":
		return &GoDev{}, nil
	default:
		return nil, fmt.Errorf("unknown version source type: %q", src.Type)
	}
}
