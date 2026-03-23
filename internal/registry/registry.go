package registry

import (
	"fmt"

	"github.com/jimyag/jd/internal/registry/builtin"
	"gopkg.in/yaml.v3"
)

type Registry struct {
	packages map[string]*PackageEntry
}

type yamlRoot struct {
	Packages []*PackageEntry `yaml:"packages"`
}

// LoadBuiltin loads the embedded built-in package registry.
func LoadBuiltin() (*Registry, error) {
	return loadFromYAML(builtin.BuiltinYAML)
}

func loadFromYAML(data []byte) (*Registry, error) {
	var root yamlRoot
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse registry YAML: %w", err)
	}

	r := &Registry{packages: make(map[string]*PackageEntry, len(root.Packages))}
	for _, p := range root.Packages {
		r.packages[p.Name] = p
	}
	return r, nil
}

// Find returns the package entry for the given name.
func (r *Registry) Find(name string) (*PackageEntry, bool) {
	p, ok := r.packages[name]
	return p, ok
}

// List returns all packages.
func (r *Registry) List() []*PackageEntry {
	pkgs := make([]*PackageEntry, 0, len(r.packages))
	for _, p := range r.packages {
		pkgs = append(pkgs, p)
	}
	return pkgs
}
