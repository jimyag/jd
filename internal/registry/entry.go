package registry

import (
	"bytes"
	"fmt"
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
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	BinaryName  string            `yaml:"binary_name"` // defaults to Name
	VersionFrom VersionSource     `yaml:"version_from"`
	URLTemplate string            `yaml:"url_template"`
	IsArchive   bool              `yaml:"is_archive"`
	InnerPath   string            `yaml:"inner_path"` // path inside archive, supports template
	OSMap       map[string]string `yaml:"os_map"`
	ArchMap     map[string]string `yaml:"arch_map"`
}

type templateVars struct {
	Version string
	OS      string
	Arch    string
	Name    string
}

// GetBinaryName returns the binary name, defaulting to Name.
func (e *PackageEntry) GetBinaryName() string {
	if e.BinaryName != "" {
		return e.BinaryName
	}
	return e.Name
}

// RenderURL renders the URL template with the given version, OS, and arch.
func (e *PackageEntry) RenderURL(version, os, arch string) (string, error) {
	return e.render(e.URLTemplate, version, os, arch)
}

// RenderInnerPath renders the InnerPath template.
func (e *PackageEntry) RenderInnerPath(version, os, arch string) (string, error) {
	return e.render(e.InnerPath, version, os, arch)
}

func (e *PackageEntry) render(tmpl, version, os, arch string) (string, error) {
	mappedOS := e.mapOS(os)
	mappedArch := e.mapArch(arch)

	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	vars := templateVars{
		Version: version,
		OS:      mappedOS,
		Arch:    mappedArch,
		Name:    e.Name,
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
