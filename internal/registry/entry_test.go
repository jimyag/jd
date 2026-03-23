package registry

import (
	"testing"
)

func TestRenderURL(t *testing.T) {
	entry := PackageEntry{
		Name:        "kubectl",
		URLTemplate: "https://dl.k8s.io/release/{{.Version}}/bin/{{.OS}}/{{.Arch}}/kubectl",
		OSMap:       map[string]string{"darwin": "darwin", "linux": "linux"},
		ArchMap:     map[string]string{"amd64": "amd64", "arm64": "arm64"},
	}

	got, err := entry.RenderURL("v1.32.0", "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://dl.k8s.io/release/v1.32.0/bin/darwin/arm64/kubectl"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderURL_OSMapping(t *testing.T) {
	entry := PackageEntry{
		Name:        "mytool",
		URLTemplate: "https://example.com/{{.Version}}/{{.OS}}/{{.Arch}}/mytool.tar.gz",
		OSMap:       map[string]string{"darwin": "macOS", "linux": "linux"},
		ArchMap:     map[string]string{"amd64": "x86_64", "arm64": "arm64"},
	}

	got, err := entry.RenderURL("v1.0.0", "darwin", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://example.com/v1.0.0/macOS/x86_64/mytool.tar.gz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderInnerPath(t *testing.T) {
	entry := PackageEntry{
		Name:      "nexttrace",
		InnerPath: "nexttrace_{{.OS}}_{{.Arch}}/nexttrace",
		OSMap:     map[string]string{"linux": "linux"},
		ArchMap:   map[string]string{"amd64": "amd64"},
	}

	got, err := entry.RenderInnerPath("v1.0.0", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	want := "nexttrace_linux_amd64/nexttrace"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBinaryName_Default(t *testing.T) {
	entry := PackageEntry{Name: "kubectl"}
	if entry.GetBinaryName() != "kubectl" {
		t.Errorf("expected kubectl, got %s", entry.GetBinaryName())
	}
}

func TestBinaryName_Override(t *testing.T) {
	entry := PackageEntry{Name: "kubectl", BinaryName: "kube"}
	if entry.GetBinaryName() != "kube" {
		t.Errorf("expected kube, got %s", entry.GetBinaryName())
	}
}
