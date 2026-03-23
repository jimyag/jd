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

func TestMethodsSortedByPriority(t *testing.T) {
	entry := PackageEntry{
		Name: "gh",
		Methods: []InstallMethod{
			{Type: "apt", Priority: 20},
			{Type: "binary", Priority: 100},
			{Type: "brew", Priority: 50},
		},
	}

	methods := entry.SortedMethods()
	if len(methods) != 3 {
		t.Fatalf("got %d methods", len(methods))
	}
	if methods[0].Type != "binary" {
		t.Fatalf("got first method %q", methods[0].Type)
	}
	if methods[1].Type != "brew" {
		t.Fatalf("got second method %q", methods[1].Type)
	}
	if methods[2].Type != "apt" {
		t.Fatalf("got third method %q", methods[2].Type)
	}
}
