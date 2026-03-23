package installer

import (
	"context"
	"fmt"
	"io"
	"io/fs"
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

// Install downloads and installs a package.
// Binaries go to ~/.local/bin; packages with install_dir get the whole directory installed there.
// If version is empty, the latest version is resolved automatically.
func Install(ctx context.Context, entry *registry.PackageEntry, version string) error {
	if version == "" {
		v, err := resolveLatest(entry)
		if err != nil {
			return err
		}
		version = v
	} else if entry.VersionPrefix != "" && !strings.HasPrefix(version, entry.VersionPrefix) {
		version = entry.VersionPrefix + version
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

	if entry.InstallDir != "" {
		return installDir(dst, entry, version, goos, goarch)
	}
	return installBinary(dst, entry, version, goos, goarch)
}

// installBinary locates the binary inside dst and copies it to ~/.local/bin.
func installBinary(dst string, entry *registry.PackageEntry, version, goos, goarch string) error {
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

// installDir moves the inner_path directory from dst to entry.InstallDir.
func installDir(dst string, entry *registry.PackageEntry, version, goos, goarch string) error {
	innerPath, err := entry.RenderInnerPath(version, goos, goarch)
	if err != nil {
		return err
	}
	srcDir := filepath.Join(dst, innerPath)
	if _, err := os.Stat(srcDir); err != nil {
		return fmt.Errorf("inner_path %q not found in archive (expected at %s)", entry.InnerPath, srcDir)
	}

	installDir, err := expandHome(entry.InstallDir)
	if err != nil {
		return err
	}

	// Remove existing installation.
	if err := os.RemoveAll(installDir); err != nil {
		return fmt.Errorf("remove existing %s: %w", installDir, err)
	}
	if err := os.MkdirAll(filepath.Dir(installDir), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	// Try rename first (fast, same filesystem); fall back to recursive copy.
	if err := os.Rename(srcDir, installDir); err != nil {
		if err := copyDirAll(srcDir, installDir); err != nil {
			return fmt.Errorf("install directory: %w", err)
		}
	}

	fmt.Printf("  installed to %s\n", installDir)
	fmt.Printf("  done. %s %s\n", entry.Name, version)

	warnIfNotInPATH(filepath.Join(installDir, "bin"))
	return nil
}

func resolveLatest(entry *registry.PackageEntry) (string, error) {
	return versioner.Latest(entry.VersionFrom)
}

func locateBinary(dst string, entry *registry.PackageEntry, version, goos, goarch string) (string, error) {
	info, err := os.Stat(dst)
	if err != nil {
		return "", fmt.Errorf("stat download: %w", err)
	}

	if !info.IsDir() {
		return dst, nil
	}

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

	target := filepath.Join(dst, entry.GetBinaryName())
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}

	var found string
	err = filepath.Walk(dst, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		if strings.ToLower(fi.Name()) == strings.ToLower(entry.GetBinaryName()) {
			found = path
			return io.EOF
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

func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, path[2:]), nil
}

// copyDirAll recursively copies src directory to dst.
func copyDirAll(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
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
