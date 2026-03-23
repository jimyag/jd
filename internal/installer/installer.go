package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	getter "github.com/hashicorp/go-getter/v2"

	"github.com/jimyag/jd/internal/registry"
	"github.com/jimyag/jd/internal/versioner"
)

const defaultBinDir = ".local/bin"

type InstallOptions struct {
	Method string
}

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
	return InstallWithOptions(ctx, entry, version, InstallOptions{})
}

func InstallWithOptions(ctx context.Context, entry *registry.PackageEntry, version string, opts InstallOptions) error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	methods, err := selectMethods(entry, opts.Method, goos, goarch)
	if err != nil {
		return err
	}

	var errs []error
	for _, method := range methods {
		if err := installMethod(ctx, entry, &method, version, goos, goarch); err != nil {
			errs = append(errs, formatMethodError(method, err))
			continue
		}
		return nil
	}

	if len(errs) == 0 {
		return fmt.Errorf("%s does not support %s/%s", entry.Name, goos, goarch)
	}
	return errors.Join(errs...)
}

func formatMethodError(method registry.InstallMethod, err error) error {
	if method.DocURL == "" {
		return fmt.Errorf("%s: %w", method.Type, err)
	}
	return fmt.Errorf("%s: %w (docs: %s)", method.Type, err, method.DocURL)
}

func installMethod(ctx context.Context, entry *registry.PackageEntry, method *registry.InstallMethod, version, goos, goarch string) error {
	if isCommandMethod(method.Type) {
		return runCommand(ctx, entry, method, version, goos, goarch)
	}

	version, err := resolveVersion(method, version)
	if err != nil {
		return err
	}
	fmt.Printf("  resolving version %s... ok\n", version)

	url, err := method.RenderURL(entry.Name, version, goos, goarch)
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
	if method.Mode == "file" {
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

	if method.InstallDir != "" {
		return installDir(dst, entry, method, version, goos, goarch)
	}
	return installBinary(dst, entry, method, version, goos, goarch)
}

// installBinary locates the binary inside dst and copies it to ~/.local/bin.
func installBinary(dst string, entry *registry.PackageEntry, method *registry.InstallMethod, version, goos, goarch string) error {
	binaryPath, err := locateBinary(dst, entry, method, version, goos, goarch)
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

// installDir moves the inner_path directory from dst to entry.InstallDir,
// then creates/updates entry.Symlink pointing to the versioned install dir.
func installDir(dst string, entry *registry.PackageEntry, method *registry.InstallMethod, version, goos, goarch string) error {
	innerPath, err := method.RenderInnerPath(entry.Name, version, goos, goarch)
	if err != nil {
		return err
	}
	srcDir := filepath.Join(dst, innerPath)
	if _, err := os.Stat(srcDir); err != nil {
		return fmt.Errorf("inner_path %q not found in archive (expected at %s)", method.InnerPath, srcDir)
	}

	rawInstallDir, err := method.RenderInstallDir(entry.Name, version, goos, goarch)
	if err != nil {
		return err
	}
	installDir, err := expandHome(rawInstallDir)
	if err != nil {
		return err
	}

	// Remove existing versioned directory.
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

	// Create/update symlink if configured.
	if method.Symlink != "" {
		link, err := expandHome(method.Symlink)
		if err != nil {
			return err
		}
		// Remove existing symlink or directory.
		if err := os.RemoveAll(link); err != nil {
			return fmt.Errorf("remove existing %s: %w", link, err)
		}
		if err := os.Symlink(installDir, link); err != nil {
			return fmt.Errorf("create symlink %s -> %s: %w", link, installDir, err)
		}
		fmt.Printf("  symlinked %s -> %s\n", link, installDir)
		warnIfNotInPATH(filepath.Join(link, "bin"))
	} else {
		warnIfNotInPATH(filepath.Join(installDir, "bin"))
	}

	fmt.Printf("  done. %s %s\n", entry.Name, version)
	return nil
}

func locateBinary(dst string, entry *registry.PackageEntry, method *registry.InstallMethod, version, goos, goarch string) (string, error) {
	info, err := os.Stat(dst)
	if err != nil {
		return "", fmt.Errorf("stat download: %w", err)
	}

	if !info.IsDir() {
		return dst, nil
	}

	if method.InnerPath != "" {
		innerPath, err := method.RenderInnerPath(entry.Name, version, goos, goarch)
		if err != nil {
			return "", err
		}
		p := filepath.Join(dst, innerPath)
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("inner_path %q not found in archive (expected at %s)", method.InnerPath, p)
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

func selectMethods(entry *registry.PackageEntry, requestedType, goos, goarch string) ([]registry.InstallMethod, error) {
	var filtered []registry.InstallMethod
	for _, method := range entry.SortedMethods() {
		if requestedType != "" && method.Type != requestedType {
			continue
		}
		if !method.SupportsPlatform(goos, goarch) {
			continue
		}
		filtered = append(filtered, method)
	}
	if len(filtered) > 0 {
		return filtered, nil
	}
	if requestedType != "" {
		return nil, fmt.Errorf("%s has no supported method %q for %s/%s", entry.Name, requestedType, goos, goarch)
	}
	return nil, nil
}

func resolveVersion(method *registry.InstallMethod, version string) (string, error) {
	if version == "" {
		if method.VersionFrom.Type == "" {
			return "", fmt.Errorf("method %s does not support automatic version resolution", method.Type)
		}
		v, err := versioner.Latest(method.VersionFrom)
		if err != nil {
			return "", err
		}
		return v, nil
	}
	if method.VersionPrefix != "" && !strings.HasPrefix(version, method.VersionPrefix) {
		return method.VersionPrefix + version, nil
	}
	return version, nil
}

func isCommandMethod(methodType string) bool {
	switch methodType {
	case "command", "apt", "dnf", "yum", "pacman", "brew", "go", "npm":
		return true
	default:
		return false
	}
}

func defaultCommandForMethod(method *registry.InstallMethod) (string, error) {
	switch method.Type {
	case "apt":
		return "apt install -y " + method.Package, nil
	case "dnf":
		return "dnf install -y " + method.Package, nil
	case "yum":
		return "yum install -y " + method.Package, nil
	case "pacman":
		return "pacman -S --noconfirm " + method.Package, nil
	case "brew":
		return "brew install " + method.Package, nil
	case "go":
		return "go install " + method.Package, nil
	case "npm":
		return "npm install -g " + method.Package, nil
	default:
		if method.Command == "" {
			return "", fmt.Errorf("method %s requires command or package", method.Type)
		}
		return method.Command, nil
	}
}

func useSudo(method *registry.InstallMethod) bool {
	if method.UseSudo != nil {
		return *method.UseSudo
	}
	switch method.Type {
	case "apt", "dnf", "yum", "pacman":
		return true
	default:
		return false
	}
}

func prependSudo(command string) string {
	if strings.HasPrefix(strings.TrimSpace(command), "sudo ") {
		return command
	}
	return "sudo " + command
}

func ensureBinDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func moveBinary(src, dst string) error {
	// Try rename first (fastest, atomic, handles busy files if on same filesystem)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// If rename fails (likely cross-device), we must copy.
	// To avoid "text file busy" when updating the running binary, remove the destination first.
	// Unlinking a running binary is allowed on Unix.
	_ = os.Remove(dst)

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

// runCommand renders the method command and executes it via sh -c.
func runCommand(ctx context.Context, entry *registry.PackageEntry, method *registry.InstallMethod, version, goos, goarch string) error {
	for _, command := range method.PreCommands {
		if err := executeShellCommand(ctx, entry, method, command, version, goos, goarch); err != nil {
			return fmt.Errorf("pre command failed: %w", err)
		}
	}

	cmd, err := defaultCommandForMethod(method)
	if err != nil {
		return err
	}
	if err := executeShellCommand(ctx, entry, method, cmd, version, goos, goarch); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}

	for _, command := range method.PostCommands {
		if err := executeShellCommand(ctx, entry, method, command, version, goos, goarch); err != nil {
			return fmt.Errorf("post command failed: %w", err)
		}
	}

	fmt.Printf("  done. %s\n", entry.Name)
	return nil
}

func executeShellCommand(ctx context.Context, entry *registry.PackageEntry, method *registry.InstallMethod, command, version, goos, goarch string) error {
	cmd, err := renderTemplateIfNeeded(entry, method, command, version, goos, goarch)
	if err != nil {
		return fmt.Errorf("render command: %w", err)
	}
	if useSudo(method) {
		cmd = prependSudo(cmd)
	}

	fmt.Printf("  running: %s\n", cmd)

	extra, err := method.RenderEnv(entry.Name, version, goos, goarch)
	if err != nil {
		return err
	}
	env := append(os.Environ(), extra...)
	for _, kv := range extra {
		fmt.Printf("  env: %s\n", kv)
	}

	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Env = env
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin

	if err := c.Run(); err != nil {
		return err
	}
	return nil
}

func renderTemplateIfNeeded(entry *registry.PackageEntry, method *registry.InstallMethod, value, version, goos, goarch string) (string, error) {
	if !strings.Contains(value, "{{") {
		return value, nil
	}
	renderMethod := *method
	renderMethod.Command = value
	return renderMethod.RenderCommand(entry.Name, version, goos, goarch)
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
