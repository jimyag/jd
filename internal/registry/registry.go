package registry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jimyag/jd/internal/registry/builtin"
	"gopkg.in/yaml.v3"
)

const (
	defaultRemoteRegistryURL = "https://raw.githubusercontent.com/jimyag/jd/main/internal/registry/builtin/packages.yaml"
	remoteRegistryCacheTTL   = 30 * time.Minute
	remoteRegistryTimeout    = 5 * time.Second
)

type LoadOptions struct {
	RefreshRemote bool
}

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

// Load loads the builtin registry, overlays the remote registry when available,
// and then overlays local package definitions from:
//   - ~/.config/jd/packages.yaml
//   - ~/.config/jd/packages.d/*.yaml
//
// Later sources override earlier sources by package name.
func Load() (*Registry, error) {
	return LoadWithOptions(LoadOptions{
		RefreshRemote: envBool("JD_REFRESH_REMOTE_REGISTRY"),
	})
}

func LoadWithOptions(opts LoadOptions) (*Registry, error) {
	r, err := LoadBuiltin()
	if err != nil {
		return nil, err
	}

	if !envBool("JD_DISABLE_REMOTE_REGISTRY") {
		remote, err := loadRemoteRegistry(opts.RefreshRemote)
		if err != nil {
			if envBool("JD_REGISTRY_STRICT") {
				return nil, err
			}
			fmt.Fprintf(os.Stderr, "warning: remote registry unavailable, using bundled registry: %v\n", err)
		} else {
			r.merge(remote)
		}
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

	if err := r.validateUniqueBinaryNames(); err != nil {
		return nil, err
	}
	return r, nil
}

func loadRemoteRegistry(refresh bool) (*Registry, error) {
	cachePath, err := remoteRegistryCachePath()
	if err != nil {
		return nil, err
	}

	if !refresh {
		if cached, err := loadFreshCachedRegistry(cachePath); err == nil {
			return cached, nil
		}
	}

	url := remoteRegistryURL()
	data, err := fetchRemoteRegistry(url)
	if err != nil {
		return nil, err
	}

	r, err := loadFromYAML(data)
	if err != nil {
		return nil, fmt.Errorf("load remote registry %s: %w", url, err)
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
		_ = os.WriteFile(cachePath, data, 0o644)
	}
	return r, nil
}

func loadFreshCachedRegistry(path string) (*Registry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if time.Since(info.ModTime()) > remoteRegistryCacheTTL {
		return nil, fmt.Errorf("remote registry cache expired")
	}
	return loadFromFile(path)
}

func fetchRemoteRegistry(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteRegistryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch remote registry %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch remote registry %s: status %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read remote registry %s: %w", url, err)
	}
	return data, nil
}

func loadFromYAML(data []byte) (*Registry, error) {
	var root yamlRoot
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse registry YAML: %w", err)
	}

	r := &Registry{packages: make(map[string]*PackageEntry, len(root.Packages))}
	for _, p := range root.Packages {
		if p.Name == "" {
			return nil, fmt.Errorf("registry package name cannot be empty")
		}
		if _, exists := r.packages[p.Name]; exists {
			return nil, fmt.Errorf("duplicate registry package %q", p.Name)
		}
		r.packages[p.Name] = p
	}
	if err := r.validateUniqueBinaryNames(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) validateUniqueBinaryNames() error {
	packages := r.List()
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Name < packages[j].Name
	})

	owners := make(map[string]string, len(packages))
	for _, pkg := range packages {
		binaryName := pkg.GetBinaryName()
		if owner, exists := owners[binaryName]; exists {
			return fmt.Errorf("duplicate registry binary name %q used by packages %q and %q", binaryName, owner, pkg.Name)
		}
		owners[binaryName] = pkg.Name
	}
	return nil
}

func loadFromFile(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadFromYAML(data)
}

func remoteRegistryURL() string {
	if url := os.Getenv("JD_REGISTRY_URL"); url != "" {
		return url
	}
	return defaultRemoteRegistryURL
}

func remoteRegistryCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache dir: %w", err)
	}
	return filepath.Join(dir, "jd", "packages.yaml"), nil
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
