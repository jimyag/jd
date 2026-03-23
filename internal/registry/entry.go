package registry

import (
	"bytes"
	"fmt"
	"slices"
	"sort"
	"strings"
	"text/template"
)

// VersionSource describes where to fetch version information.
type VersionSource struct {
	Type      string `yaml:"type"`       // "github"
	Repo      string `yaml:"repo"`       // "owner/repo"
	TagPrefix string `yaml:"tag_prefix"` // "v" — strip from tag to get version, or keep
}

// PackageEntry describes a single installable package.
type PackageEntry struct {
	Name               string            `yaml:"name"`
	Description        string            `yaml:"description"`
	BinaryName         string            `yaml:"binary_name"` // defaults to Name
	DocURL             string            `yaml:"doc_url"`
	Methods            []InstallMethod   `yaml:"methods"`
	VersionFrom        VersionSource     `yaml:"version_from"`
	URLTemplate        string            `yaml:"url_template"`
	Mode               string            `yaml:"mode"`                // "dir" (default), "file", or "command"
	Command            string            `yaml:"command"`             // shell command for mode: command (supports templates)
	InnerPath          string            `yaml:"inner_path"`          // path inside archive, supports template
	InstallDir         string            `yaml:"install_dir"`         // install whole directory here instead of single binary (supports ~ and templates)
	Symlink            string            `yaml:"symlink"`             // create/update this symlink pointing to install_dir after install (supports ~)
	VersionPrefix      string            `yaml:"version_prefix"`      // prepended to user-supplied version if not already present (e.g. "go" for Go)
	Env                map[string]string `yaml:"env"`                 // env vars injected when executing install script; values support templates (mode: script only)
	FallbackCommands   map[string]string `yaml:"fallback_commands"`   // map of OS to fallback install command (e.g. darwin: "brew install ...")
	SupportedPlatforms []string          `yaml:"supported_platforms"` // "os/arch" pairs; empty means all supported
	OSMap              map[string]string `yaml:"os_map"`
	ArchMap            map[string]string `yaml:"arch_map"`
}

type InstallMethod struct {
	Type               string            `yaml:"type"`
	Priority           int               `yaml:"priority"`
	VersionFrom        VersionSource     `yaml:"version_from"`
	URLTemplate        string            `yaml:"url_template"`
	Mode               string            `yaml:"mode"`
	Command            string            `yaml:"command"`
	Package            string            `yaml:"package"`
	DocURL             string            `yaml:"doc_url"`
	PreCommands        []string          `yaml:"pre_commands"`
	PostCommands       []string          `yaml:"post_commands"`
	InnerPath          string            `yaml:"inner_path"`
	InstallDir         string            `yaml:"install_dir"`
	Symlink            string            `yaml:"symlink"`
	VersionPrefix      string            `yaml:"version_prefix"`
	Env                map[string]string `yaml:"env"`
	UseSudo            *bool             `yaml:"use_sudo"`
	SupportedPlatforms []string          `yaml:"supported_platforms"`
	OSMap              map[string]string `yaml:"os_map"`
	ArchMap            map[string]string `yaml:"arch_map"`
}

// SupportsPlatform returns true if the package supports the given GOOS/GOARCH.
// An empty SupportedPlatforms list means all platforms are supported.
// Entries can be:
//   - "os"       — matches all arches for that OS (e.g. "linux")
//   - "os/arch"  — exact match (e.g. "darwin/arm64")
func (e *PackageEntry) SupportsPlatform(goos, goarch string) bool {
	if len(e.Methods) > 0 {
		for _, method := range e.Methods {
			if method.SupportsPlatform(goos, goarch) {
				return true
			}
		}
		return false
	}
	return supportsPlatform(e.SupportedPlatforms, goos, goarch)
}

func (m InstallMethod) SupportsPlatform(goos, goarch string) bool {
	return supportsPlatform(m.SupportedPlatforms, goos, goarch)
}

func supportsPlatform(platforms []string, goos, goarch string) bool {
	if len(platforms) == 0 {
		return true
	}
	for _, p := range platforms {
		if strings.Contains(p, "/") {
			if p == goos+"/"+goarch {
				return true
			}
		} else {
			if p == goos {
				return true
			}
		}
	}
	return false
}

type templateVars struct {
	Version    string
	VersionNoV string // Version with leading "v" stripped, e.g. "2.88.1"
	OS         string
	Arch       string
	Name       string
}

// GetBinaryName returns the binary name, defaulting to Name.
func (e *PackageEntry) GetBinaryName() string {
	if e.BinaryName != "" {
		return e.BinaryName
	}
	return e.Name
}

func (e *PackageEntry) SortedMethods() []InstallMethod {
	methods := e.Methods
	if len(methods) == 0 {
		methods = []InstallMethod{e.legacyMethod()}
	}

	sorted := slices.Clone(methods)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})
	return sorted
}

func (e *PackageEntry) VersionSourceForMethod(methodType string) (VersionSource, bool) {
	for _, method := range e.SortedMethods() {
		if methodType != "" && method.Type != methodType {
			continue
		}
		if method.VersionFrom.Type != "" {
			return method.VersionFrom, true
		}
	}
	return VersionSource{}, false
}

// RenderEnv renders each env value as a template and returns "KEY=VALUE" pairs,
// skipping entries whose rendered value is empty.
func (e *PackageEntry) RenderEnv(version, os, arch string) ([]string, error) {
	result := make([]string, 0, len(e.Env))
	for k, v := range e.Env {
		rendered, err := renderTemplate(v, version, os, arch, e.Name, e.OSMap, e.ArchMap)
		if err != nil {
			return nil, fmt.Errorf("render env %s: %w", k, err)
		}
		if rendered != "" {
			result = append(result, k+"="+rendered)
		}
	}
	return result, nil
}

// RenderInstallDir renders the InstallDir template.
func (e *PackageEntry) RenderInstallDir(version, os, arch string) (string, error) {
	return renderTemplate(e.InstallDir, version, os, arch, e.Name, e.OSMap, e.ArchMap)
}

// RenderURL renders the URL template with the given version, OS, and arch.
func (e *PackageEntry) RenderURL(version, os, arch string) (string, error) {
	return renderTemplate(e.URLTemplate, version, os, arch, e.Name, e.OSMap, e.ArchMap)
}

// RenderInnerPath renders the InnerPath template.
func (e *PackageEntry) RenderInnerPath(version, os, arch string) (string, error) {
	return renderTemplate(e.InnerPath, version, os, arch, e.Name, e.OSMap, e.ArchMap)
}

// RenderCommand renders the Command template.
func (e *PackageEntry) RenderCommand(version, os, arch string) (string, error) {
	return renderTemplate(e.Command, version, os, arch, e.Name, e.OSMap, e.ArchMap)
}

func (m *InstallMethod) RenderEnv(name, version, os, arch string) ([]string, error) {
	result := make([]string, 0, len(m.Env))
	for k, v := range m.Env {
		rendered, err := renderTemplate(v, version, os, arch, name, m.OSMap, m.ArchMap)
		if err != nil {
			return nil, fmt.Errorf("render env %s: %w", k, err)
		}
		if rendered != "" {
			result = append(result, k+"="+rendered)
		}
	}
	return result, nil
}

func (m *InstallMethod) RenderInstallDir(name, version, os, arch string) (string, error) {
	return renderTemplate(m.InstallDir, version, os, arch, name, m.OSMap, m.ArchMap)
}

func (m *InstallMethod) RenderURL(name, version, os, arch string) (string, error) {
	return renderTemplate(m.URLTemplate, version, os, arch, name, m.OSMap, m.ArchMap)
}

func (m *InstallMethod) RenderInnerPath(name, version, os, arch string) (string, error) {
	return renderTemplate(m.InnerPath, version, os, arch, name, m.OSMap, m.ArchMap)
}

func (m *InstallMethod) RenderCommand(name, version, os, arch string) (string, error) {
	return renderTemplate(m.Command, version, os, arch, name, m.OSMap, m.ArchMap)
}

func renderTemplate(tmpl, version, os, arch, name string, osMap, archMap map[string]string) (string, error) {
	mappedOS := mapValue(osMap, os)
	mappedArch := mapValue(archMap, arch)

	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	vars := templateVars{
		Version:    version,
		VersionNoV: strings.TrimPrefix(version, "v"),
		OS:         mappedOS,
		Arch:       mappedArch,
		Name:       name,
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

func mapValue(values map[string]string, key string) string {
	if values != nil {
		if mapped, ok := values[key]; ok {
			return mapped
		}
	}
	return key
}

func (e *PackageEntry) legacyMethod() InstallMethod {
	return InstallMethod{
		Type:               legacyMethodType(e.Mode),
		Priority:           100,
		VersionFrom:        e.VersionFrom,
		URLTemplate:        e.URLTemplate,
		Mode:               e.Mode,
		Command:            e.Command,
		DocURL:             e.DocURL,
		InnerPath:          e.InnerPath,
		InstallDir:         e.InstallDir,
		Symlink:            e.Symlink,
		VersionPrefix:      e.VersionPrefix,
		Env:                e.Env,
		SupportedPlatforms: e.SupportedPlatforms,
		OSMap:              e.OSMap,
		ArchMap:            e.ArchMap,
	}
}

func legacyMethodType(mode string) string {
	if mode == "command" {
		return "command"
	}
	return "binary"
}
