package registry

import (
	"bytes"
	"fmt"
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
	VersionFrom        VersionSource     `yaml:"version_from"`
	URLTemplate        string            `yaml:"url_template"`
	Mode               string            `yaml:"mode"`                // "dir" (default), "file", or "command"
	Command            string            `yaml:"command"`             // shell command for mode: command (supports templates)
	InnerPath          string            `yaml:"inner_path"`          // path inside archive, supports template
	InstallDir         string            `yaml:"install_dir"`         // install whole directory here instead of single binary (supports ~ and templates)
	Symlink            string            `yaml:"symlink"`             // create/update this symlink pointing to install_dir after install (supports ~)
	VersionPrefix      string            `yaml:"version_prefix"`      // prepended to user-supplied version if not already present (e.g. "go" for Go)
	Env                map[string]string `yaml:"env"`                 // env vars injected when executing install script; values support templates (mode: script only)
	SupportedPlatforms []string          `yaml:"supported_platforms"` // "os/arch" pairs; empty means all supported
	OSMap              map[string]string `yaml:"os_map"`
	ArchMap            map[string]string `yaml:"arch_map"`
}

// SupportsPlatform returns true if the package supports the given GOOS/GOARCH.
// An empty SupportedPlatforms list means all platforms are supported.
// Entries can be:
//   - "os"       — matches all arches for that OS (e.g. "linux")
//   - "os/arch"  — exact match (e.g. "darwin/arm64")
func (e *PackageEntry) SupportsPlatform(goos, goarch string) bool {
	if len(e.SupportedPlatforms) == 0 {
		return true
	}
	for _, p := range e.SupportedPlatforms {
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

// RenderEnv renders each env value as a template and returns "KEY=VALUE" pairs,
// skipping entries whose rendered value is empty.
func (e *PackageEntry) RenderEnv(version, os, arch string) ([]string, error) {
	result := make([]string, 0, len(e.Env))
	for k, v := range e.Env {
		rendered, err := e.render(v, version, os, arch)
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
	return e.render(e.InstallDir, version, os, arch)
}

// RenderURL renders the URL template with the given version, OS, and arch.
func (e *PackageEntry) RenderURL(version, os, arch string) (string, error) {
	return e.render(e.URLTemplate, version, os, arch)
}

// RenderInnerPath renders the InnerPath template.
func (e *PackageEntry) RenderInnerPath(version, os, arch string) (string, error) {
	return e.render(e.InnerPath, version, os, arch)
}

// RenderCommand renders the Command template.
func (e *PackageEntry) RenderCommand(version, os, arch string) (string, error) {
	return e.render(e.Command, version, os, arch)
}

func (e *PackageEntry) render(tmpl, version, os, arch string) (string, error) {
	mappedOS := e.mapOS(os)
	mappedArch := e.mapArch(arch)

	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	vars := templateVars{
		Version:    version,
		VersionNoV: strings.TrimPrefix(version, "v"),
		OS:         mappedOS,
		Arch:       mappedArch,
		Name:       e.Name,
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

func (e *PackageEntry) mapOS(os string) string {
	if e.OSMap != nil {
		if mapped, ok := e.OSMap[os]; ok {
			return mapped
		}
	}
	return os
}

func (e *PackageEntry) mapArch(arch string) string {
	if e.ArchMap != nil {
		if mapped, ok := e.ArchMap[arch]; ok {
			return mapped
		}
	}
	return arch
}
