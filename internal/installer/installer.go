package installer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	getter "github.com/hashicorp/go-getter/v2"

	"github.com/jimyag/jd/internal/registry"
	"github.com/jimyag/jd/internal/versioner"
)

const defaultBinDir = ".local/bin"

// BinDir returns the directory where binaries are installed.
func BinDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, defaultBinDir), nil
}

// Install downloads and installs a package to ~/.local/bin.
// If version is empty, the latest version is used.
func Install(ctx context.Context, entry *registry.PackageEntry, version string) error {
	if version == "" {
		v, err := versioner.LatestVersion(entry.VersionFrom.Repo)
		if err != nil {
			return err
		}
		version = v
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH

	if !entry.SupportsPlatform(goos, goarch) {
		return fmt.Errorf("%s does not support %s/%s", entry.Name, goos, goarch)
	}

	fmt.Printf("  resolving version %s... ok\n", version)

	url, err := entry.RenderURL(version, goos, goarch)
	if err != nil {
		return fmt.Errorf("render URL: %w", err)
	}

	fmt.Printf("  downloading %s\n", url)

	tmpDir, err := os.MkdirTemp("", "jd-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dst := filepath.Join(tmpDir, "download")

	// Use ModeFile for direct binaries so go-getter saves to dst as a file.
	// Use ModeDir (default) for archives so go-getter extracts into dst as a directory.
	getMode := getter.ModeDir
	if entry.Mode == "file" {
		getMode = getter.ModeFile
	}

	req := &getter.Request{
		Src:     url,
		Dst:     dst,
		GetMode: getMode,
	}

	client := &getter.Client{}
	if _, err := client.Get(ctx, req); err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}

	// Locate the binary inside the downloaded content.
	binaryPath, err := locateBinary(dst, entry, version, goos, goarch)
	if err != nil {
		return err
	}

	binDir, err := BinDir()
	if err != nil {
		return err
	}

	if err := ensureBinDir(binDir); err != nil {
		return err
	}

	target := filepath.Join(binDir, entry.GetBinaryName())
	if err := moveBinary(binaryPath, target); err != nil {
		return err
	}

	fmt.Printf("  installed to %s\n", target)
	fmt.Printf("  done. %s %s\n", entry.Name, version)

	warnIfNotInPATH(binDir)
	return nil
}

func locateBinary(dst string, entry *registry.PackageEntry, version, goos, goarch string) (string, error) {
	// Check if dst is already a file (direct binary download).
	info, err := os.Stat(dst)
	if err != nil {
		return "", fmt.Errorf("stat download: %w", err)
	}

	if !info.IsDir() {
		return dst, nil
	}

	// Archive was extracted into dst directory.
	if entry.InnerPath != "" {
		innerPath, err := entry.RenderInnerPath(version, goos, goarch)
		if err != nil {
			return "", err
		}
		p := filepath.Join(dst, innerPath)
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("inner_path %q not found in archive (expected at %s)", entry.InnerPath, p)
		}
		return p, nil
	}

	// No inner_path: look for a file matching the binary name.
	target := filepath.Join(dst, entry.GetBinaryName())
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}

	// Walk and find any executable file.
	var found string
	err = filepath.Walk(dst, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		name := strings.ToLower(fi.Name())
		if name == strings.ToLower(entry.GetBinaryName()) {
			found = path
			return io.EOF // stop walking
		}
		return nil
	})
	if err != nil && err != io.EOF {
		return "", err
	}
	if found != "" {
		return found, nil
	}

	return "", fmt.Errorf("could not locate binary %q in downloaded content; set inner_path in registry", entry.GetBinaryName())
}

func ensureBinDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func moveBinary(src, dst string) error {
	// Copy then delete, works across filesystems.
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source binary: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}
	return nil
}

func warnIfNotInPATH(binDir string) {
	path := os.Getenv("PATH")
	for _, p := range filepath.SplitList(path) {
		if p == binDir {
			return
		}
	}
	fmt.Printf("\n  warning: %s is not in your PATH\n", binDir)
	fmt.Printf("  add this to your shell profile:\n")
	fmt.Printf("    export PATH=\"%s:$PATH\"\n", binDir)
}
