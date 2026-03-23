package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

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

// Load loads the builtin registry and overlays local package definitions from:
//   - ~/.config/jd/packages.yaml
//   - ~/.config/jd/packages.d/*.yaml
//
// Later sources override earlier sources by package name.
func Load() (*Registry, error) {
	r, err := LoadBuiltin()
	if err != nil {
		return nil, err
	}

	paths, err := localRegistryPaths()
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		local, err := loadFromFile(path)
		if err != nil {
			return nil, fmt.Errorf("load local registry %s: %w", path, err)
		}
		r.merge(local)
	}

	return r, nil
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

func loadFromFile(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadFromYAML(data)
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

func (r *Registry) merge(other *Registry) {
	for name, pkg := range other.packages {
		r.packages[name] = pkg
	}
}

func localRegistryPaths() ([]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	baseDir := filepath.Join(homeDir, ".config", "jd")
	var paths []string

	mainFile := filepath.Join(baseDir, "packages.yaml")
	if _, err := os.Stat(mainFile); err == nil {
		paths = append(paths, mainFile)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	matches, err := filepath.Glob(filepath.Join(baseDir, "packages.d", "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("glob local registry files: %w", err)
	}
	sort.Strings(matches)
	paths = append(paths, matches...)

	return paths, nil
}
