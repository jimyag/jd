package installer

import (
	"compress/gzip"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	getter "github.com/hashicorp/go-getter/v2"
	goversion "github.com/hashicorp/go-version"

	"github.com/jimyag/jd/internal/cnb"
	"github.com/jimyag/jd/internal/registry"
	"github.com/jimyag/jd/internal/state"
	"github.com/jimyag/jd/internal/versioner"
)

const defaultBinDir = ".local/bin"

type InstallOptions struct {
	Method  string
	Upgrade bool
	Current *state.Installation
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
	_, err := InstallTracked(ctx, entry, version, opts)
	return err
}

// InstallTracked returns what was installed. The CLI holds the global operation lock.
func InstallTracked(ctx context.Context, entry *registry.PackageEntry, version string, opts InstallOptions) (state.Installation, error) {
	result := state.Installation{Name: entry.Name, RequestedVersion: version}
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	methods, err := selectMethods(entry, opts.Method, goos, goarch)
	if err != nil {
		return result, err
	}
	for _, method := range methods {
		if cnb.Enabled() && method.Type != "binary" {
			continue
		}
		resolved, err := resolveInstallVersion(ctx, &method, version)
		if err != nil {
			return result, formatMethodError(method, err)
		}
		if method.Type == "binary" && version == "" && opts.Current != nil && opts.Current.Version != "" {
			current, currentErr := goversion.NewVersion(strings.TrimPrefix(strings.TrimPrefix(opts.Current.Version, method.VersionPrefix), method.VersionFrom.TagPrefix))
			candidate, candidateErr := goversion.NewVersion(strings.TrimPrefix(strings.TrimPrefix(resolved, method.VersionPrefix), method.VersionFrom.TagPrefix))
			if currentErr == nil && candidateErr == nil && candidate.LessThan(current) {
				if opts.Current.Method != method.Type {
					return result, fmt.Errorf("%s binary %s is older than recorded %s from %s; specify an explicit version to allow a downgrade", entry.Name, resolved, opts.Current.Version, opts.Current.Method)
				}
				fmt.Printf("  keeping %s %s: source currently reports older %s\n", entry.Name, opts.Current.Version, resolved)
				resolved = opts.Current.Version
			}
		}
		result.Method, result.Version = method.Type, resolved
		result.Package = method.Package
		binDir, err := BinDir()
		if err != nil {
			return result, err
		}
		result.Path = filepath.Join(binDir, entry.GetBinaryName())
		if method.Type == "npm" {
			method.Env = maps.Clone(method.Env)
			if method.Env == nil {
				method.Env = make(map[string]string)
			}
			if _, configured := method.Env["NPM_CONFIG_PREFIX"]; !configured {
				method.Env["NPM_CONFIG_PREFIX"] = filepath.Dir(binDir)
			}
		}
		if method.InstallDir != "" {
			path, err := method.RenderInstallDir(entry.Name, resolved, goos, goarch)
			if err != nil {
				return result, err
			}
			result.Path, err = expandHome(path)
			if err != nil {
				return result, err
			}
		}
		if method.Type == "binary" && opts.Current != nil && opts.Current.Method == method.Type && opts.Current.Version == resolved && opts.Current.Path == result.Path {
			if installationPresent(result.Path, method) {
				if info, err := buildinfo.ReadFile(result.Path); err == nil {
					result.GoVersion = info.GoVersion
				}
				fmt.Printf("  %s %s is up to date\n", entry.Name, resolved)
				return result, nil
			}
		}
		if err := installMethod(ctx, entry, &method, resolved, goos, goarch, opts.Upgrade); err != nil {
			return result, formatMethodError(method, err)
		}
		if isCommandMethod(method.Type) {
			if method.Type == "brew" {
				prefix, err := exec.CommandContext(ctx, "brew", "--prefix", method.Package).Output()
				if err != nil {
					return result, fmt.Errorf("query installed brew prefix: %w", err)
				}
				keg, err := filepath.EvalSymlinks(strings.TrimSpace(string(prefix)))
				if err != nil {
					return result, fmt.Errorf("resolve installed brew keg: %w", err)
				}
				result.Version, result.Path = filepath.Base(keg), ""
				for _, subdir := range []string{"bin", "sbin", "libexec/bin"} {
					candidate := filepath.Join(keg, subdir, entry.GetBinaryName())
					if path, err := exec.LookPath(candidate); err == nil {
						result.Path = path
						break
					}
				}
			} else if method.Type == "npm" {
				result.Version, result.Path, err = npmInstallation(ctx, entry, &method, resolved, goos, goarch)
				if err != nil {
					return result, err
				}
			} else {
				if method.Type != "go" {
					result.Path, _ = exec.LookPath(entry.GetBinaryName())
				}
				result.Version = installedVersion(ctx, method, result.Path)
			}
		}
		if method.VersionCommand != "" {
			command := exec.CommandContext(ctx, "sh", "-c", method.VersionCommand)
			env, err := method.RenderEnv(entry.Name, resolved, goos, goarch)
			if err != nil {
				return result, err
			}
			command.Env = append(os.Environ(), env...)
			output, err := command.Output()
			if err != nil {
				return result, fmt.Errorf("query installed version: %w", err)
			}
			result.Version = strings.TrimSpace(string(output))
		}
		if info, err := buildinfo.ReadFile(result.Path); err == nil {
			result.GoVersion = info.GoVersion
		}
		return result, nil
	}
	if cnb.Enabled() {
		return result, fmt.Errorf("CNB has no binary installation method for %s on %s/%s", entry.Name, goos, goarch)
	}
	return result, fmt.Errorf("%s does not support %s/%s", entry.Name, goos, goarch)
}

func installationPresent(path string, method registry.InstallMethod) bool {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	if method.Symlink != "" {
		link, err := expandHome(method.Symlink)
		if err != nil {
			return false
		}
		target, err := filepath.EvalSymlinks(link)
		if err != nil || target != resolvedPath {
			return false
		}
	}
	binDir, err := BinDir()
	if err != nil {
		return false
	}
	for name, relative := range method.Binaries {
		target, err := filepath.EvalSymlinks(filepath.Join(binDir, name))
		if err != nil || target != filepath.Join(resolvedPath, relative) {
			return false
		}
	}
	return true
}

func formatMethodError(method registry.InstallMethod, err error) error {
	if method.DocURL == "" {
		return fmt.Errorf("%s: %w", method.Type, err)
	}
	return fmt.Errorf("%s: %w (docs: %s)", method.Type, err, method.DocURL)
}

func resolveInstallVersion(ctx context.Context, method *registry.InstallMethod, version string) (string, error) {
	if isCommandMethod(method.Type) {
		if version != "" && slices.Contains([]string{"brew", "apt", "apk", "dnf", "yum", "pacman"}, method.Type) {
			return "", fmt.Errorf("version pins are not supported by the %s method", method.Type)
		}
		return resolveCommandVersion(method, version)
	}
	if cnb.Enabled() {
		repo := cnb.GitHubRepo(method.URLTemplate)
		if repo == "" {
			return "", fmt.Errorf("CNB does not mirror binary source %s", method.URLTemplate)
		}
		latest, fetchErr := cnb.Latest(ctx, repo)
		if fetchErr != nil {
			return "", fetchErr
		}
		if version != "" && strings.TrimPrefix(version, "v") != strings.TrimPrefix(latest, "v") {
			return "", fmt.Errorf("CNB only mirrors %s; requested %s", latest, version)
		}
		return latest, nil
	}
	return resolveVersion(method, version)
}

func installMethod(ctx context.Context, entry *registry.PackageEntry, method *registry.InstallMethod, version, goos, goarch string, upgrade bool) error {
	if isCommandMethod(method.Type) && method.Type != "script" {
		return runCommandWithUpgrade(ctx, entry, method, version, goos, goarch, upgrade)
	}
	if method.Type == "binary" {
		for _, command := range method.PreCommands {
			if err := executeShellCommand(ctx, entry, method, command, version, goos, goarch); err != nil {
				return fmt.Errorf("pre command failed: %w", err)
			}
		}
	}
	fmt.Printf("  resolving version %s... ok\n", version)

	url, err := method.RenderURL(entry.Name, version, goos, goarch)
	if err != nil {
		return fmt.Errorf("render URL: %w", err)
	}
	if cnb.Enabled() {
		url, err = cnb.DownloadURL(ctx, url, version)
		if err != nil {
			return err
		}
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
	if method.Type == "script" || method.Mode == "file" || method.Mode == "pkg" || method.Mode == "tar" {
		getMode = getter.ModeFile
	}

	req := &getter.Request{
		Src:     url,
		Dst:     dst,
		GetMode: getMode,
	}

	getters := slices.Clone(getter.Getters)
	for i, backend := range getters {
		if httpGetter, ok := backend.(*getter.HttpGetter); ok {
			configured := *httpGetter
			// go-getter defaults to a 30-second read deadline even while data is arriving.
			configured.HeadFirstTimeout = 30 * time.Second
			configured.ReadTimeout = 10 * time.Minute
			getters[i] = &configured
		}
	}
	client := &getter.Client{Getters: getters}
	download := func() error {
		if method.Mode == "tar" {
			source, err := neturl.Parse(req.Src)
			if err != nil {
				return err
			}
			query := source.Query()
			query.Set("archive", "false")
			source.RawQuery = query.Encode()
			req.Src = source.String()
		}
		downloadCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		_, err := client.Get(downloadCtx, req)
		return err
	}
	if err := download(); err != nil {
		if cnb.Enabled() || cnb.GitHubRepo(url) == "" {
			return fmt.Errorf("download %s: %w", url, err)
		}
		mirrorURL, mirrorErr := cnb.DownloadURL(ctx, url, version)
		if mirrorErr != nil {
			return errors.Join(fmt.Errorf("download %s: %w", url, err), mirrorErr)
		}
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		fmt.Printf("  retrying download from CNB: %s\n", mirrorURL)
		req.Src = mirrorURL
		if mirrorErr := download(); mirrorErr != nil {
			return errors.Join(fmt.Errorf("download %s: %w", url, err), fmt.Errorf("%w: download %s: %w", cnb.ErrUnavailable, mirrorURL, mirrorErr))
		}
	}
	if method.Type == "script" {
		interpreter := method.Command
		if interpreter == "" {
			interpreter = "bash"
		}
		command := interpreter + " " + shellArg(dst)
		for _, arg := range method.ScriptArgs {
			value, err := renderTemplateIfNeeded(entry, method, arg, version, goos, goarch)
			if err != nil {
				return err
			}
			command += " " + shellArg(value)
		}
		scriptMethod := *method
		scriptMethod.Command = command
		return runCommandWithUpgrade(ctx, entry, &scriptMethod, version, goos, goarch, upgrade)
	}
	if method.Mode == "file" {
		if err := decompressGzipFileIfNeeded(dst); err != nil {
			return err
		}
	}
	if method.Mode == "pkg" || method.Mode == "tar" {
		extracted := filepath.Join(tmpDir, "expanded")
		command := exec.CommandContext(ctx, "pkgutil", "--expand-full", dst, extracted)
		if method.Mode == "tar" {
			if err := os.Mkdir(extracted, 0o755); err != nil {
				return err
			}
			command = exec.CommandContext(ctx, "tar", "-xf", dst, "-C", extracted)
		}
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("expand %s artifact: %w: %s", method.Mode, err, output)
		}
		dst = extracted
	}

	if method.InstallDir != "" {
		err = installDir(dst, entry, method, version, goos, goarch)
	} else {
		err = installBinary(dst, entry, method, version, goos, goarch)
	}
	if err != nil {
		return err
	}
	for _, command := range method.PostCommands {
		if err := executeShellCommand(ctx, entry, method, command, version, goos, goarch); err != nil {
			return fmt.Errorf("post command failed: %w", err)
		}
	}
	return nil
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
	for name, relative := range method.Binaries {
		if name == "." || filepath.Base(name) != name || !filepath.IsLocal(relative) {
			return fmt.Errorf("invalid executable mapping %q: %q", name, relative)
		}
		if info, err := os.Stat(filepath.Join(srcDir, relative)); err != nil || info.IsDir() {
			return fmt.Errorf("executable %s missing from archive", relative)
		}
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
		if err := replaceSymlink(installDir, link); err != nil {
			return fmt.Errorf("create symlink %s -> %s: %w", link, installDir, err)
		}
		fmt.Printf("  symlinked %s -> %s\n", link, installDir)
		warnIfNotInPATH(filepath.Join(link, "bin"))
	} else if len(method.Binaries) == 0 {
		warnIfNotInPATH(filepath.Join(installDir, "bin"))
	}
	if len(method.Binaries) > 0 {
		binDir, err := BinDir()
		if err != nil {
			return err
		}
		if err := ensureBinDir(binDir); err != nil {
			return err
		}
		for name, relative := range method.Binaries {
			target := filepath.Join(installDir, relative)
			link := filepath.Join(binDir, name)
			if err := replaceSymlink(target, link); err != nil {
				return err
			}
		}
		warnIfNotInPATH(binDir)
	}

	fmt.Printf("  done. %s %s\n", entry.Name, version)
	return nil
}

func replaceSymlink(target, link string) error {
	if info, err := os.Lstat(link); err == nil && info.IsDir() {
		return fmt.Errorf("refusing to replace directory %s with a symlink", link)
	}
	temporary, err := os.CreateTemp(filepath.Dir(link), ".jd-link-*")
	if err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporary.Name())
		return err
	}
	if err := os.Remove(temporary.Name()); err != nil {
		return err
	}
	if err := os.Symlink(target, temporary.Name()); err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	return os.Rename(temporary.Name(), link)
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
	if info, err := os.Lstat(target); err == nil && info.Mode().IsRegular() {
		return target, nil
	}

	var found string
	err = filepath.Walk(dst, func(path string, fi os.FileInfo, err error) error {
		if err != nil || !fi.Mode().IsRegular() {
			return err
		}
		if strings.EqualFold(fi.Name(), entry.GetBinaryName()) {
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
		if requestedType == "" && slices.Contains([]string{"brew", "apt", "apk", "dnf", "yum", "pacman"}, method.Type) {
			if _, err := exec.LookPath(method.Type); err != nil {
				continue
			}
		}
		filtered = append(filtered, method)
	}
	// Source preference takes precedence over numeric priorities within a source tier.
	slices.SortStableFunc(filtered, func(a, b registry.InstallMethod) int { return methodRank(a.Type) - methodRank(b.Type) })
	if len(filtered) > 0 {
		return filtered, nil
	}
	if requestedType != "" {
		return nil, fmt.Errorf("%s has no supported method %q for %s/%s", entry.Name, requestedType, goos, goarch)
	}
	return nil, nil
}

func methodRank(method string) int {
	switch method {
	case "binary":
		return 0
	case "script":
		return 1
	case "brew", "apt", "apk", "dnf", "yum", "pacman":
		return 2
	default:
		return 3
	}
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

func resolveCommandVersion(method *registry.InstallMethod, version string) (string, error) {
	if version != "" {
		if method.Type == "script" && !commandUsesVersion(method) {
			return "", fmt.Errorf("this script does not support version pins")
		}
		return resolveVersion(method, version)
	}
	if method.Type == "script" && method.VersionFrom.Type == "" {
		return "", nil
	}
	if commandUsesVersion(method) {
		return resolveVersion(method, version)
	}
	if method.Type == "go" && !packageHasVersionSuffix(method.Package) {
		return "latest", nil
	}
	return "", nil
}

func isCommandMethod(methodType string) bool {
	switch methodType {
	case "script", "command", "apt", "apk", "dnf", "yum", "pacman", "brew", "go", "npm":
		return true
	default:
		return false
	}
}

func defaultCommandForMethod(entry *registry.PackageEntry, method *registry.InstallMethod, version, goos, goarch string) (string, error) {
	pkg, err := renderPackage(entry, method, version, goos, goarch)
	if err != nil {
		return "", err
	}
	switch method.Type {
	case "apt":
		return "apt install -y " + pkg, nil
	case "apk":
		return "apk add " + pkg, nil
	case "dnf":
		return "dnf install -y " + pkg, nil
	case "yum":
		return "yum install -y " + pkg, nil
	case "pacman":
		return "pacman -S --noconfirm " + pkg, nil
	case "brew":
		return "brew install " + pkg, nil
	case "go":
		return "go install " + shellArg(versionedPackage(pkg, version)), nil
	case "npm":
		return "npm install -g " + shellArg(versionedPackage(pkg, version)), nil
	default:
		if method.Command == "" {
			return "", fmt.Errorf("method %s requires command or package", method.Type)
		}
		return method.Command, nil
	}
}

func shellArg(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func useSudo(method *registry.InstallMethod) bool {
	if method.UseSudo != nil {
		return *method.UseSudo
	}
	switch method.Type {
	case "apt", "apk", "dnf", "yum", "pacman":
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
		return os.Chmod(dst, 0o755)
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
	return os.Chmod(dst, 0o755)
}

func decompressGzipFileIfNeeded(path string) error {
	in, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open downloaded file: %w", err)
	}
	magic := make([]byte, 2)
	n, readErr := io.ReadFull(in, magic)
	if closeErr := in.Close(); closeErr != nil {
		return closeErr
	}
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		return fmt.Errorf("inspect downloaded file: %w", readErr)
	}
	if n < 2 || magic[0] != 0x1f || magic[1] != 0x8b {
		return nil
	}

	in, err = os.Open(path)
	if err != nil {
		return fmt.Errorf("open gzip file: %w", err)
	}
	defer in.Close()

	gz, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("read gzip file: %w", err)
	}
	defer gz.Close()

	tmp := path + ".ungz"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create decompressed file: %w", err)
	}
	if _, err := io.Copy(out, gz); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("decompress gzip file: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace gzip file: %w", err)
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
	return runCommandWithUpgrade(ctx, entry, method, version, goos, goarch, false)
}

func runCommandWithUpgrade(ctx context.Context, entry *registry.PackageEntry, method *registry.InstallMethod, version, goos, goarch string, upgrade bool) error {
	for _, command := range method.PreCommands {
		if err := executeShellCommand(ctx, entry, method, command, version, goos, goarch); err != nil {
			return fmt.Errorf("pre command failed: %w", err)
		}
	}

	cmd, err := defaultCommandForMethod(entry, method, version, goos, goarch)
	if err != nil {
		return err
	}
	if upgrade && method.Command == "" {
		pkg, err := renderPackage(entry, method, version, goos, goarch)
		if err != nil {
			return err
		}
		switch method.Type {
		case "brew":
			cmd = "brew upgrade " + pkg
		case "apk":
			cmd = "apk add --upgrade " + pkg
		case "dnf", "yum":
			cmd = method.Type + " upgrade -y " + pkg
		}
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
	if useSudo(method) && os.Geteuid() != 0 {
		cmd = prependSudo(cmd)
	}

	fmt.Printf("  running: %s\n", cmd)

	extra, err := method.RenderEnv(entry.Name, version, goos, goarch)
	if err != nil {
		return err
	}
	env := append(os.Environ(), extra...)
	if method.Type == "go" {
		binDir, err := BinDir()
		if err != nil {
			return err
		}
		if err := ensureBinDir(binDir); err != nil {
			return err
		}
		env = append(env, "GOBIN="+binDir, "GOTOOLCHAIN=local")
	}
	for _, kv := range extra {
		name, _, _ := strings.Cut(kv, "=")
		fmt.Printf("  env: %s=<redacted>\n", name)
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

func npmInstallation(ctx context.Context, entry *registry.PackageEntry, method *registry.InstallMethod, version, goos, goarch string) (string, string, error) {
	pkg, err := renderPackage(entry, method, version, goos, goarch)
	if err != nil {
		return "", "", err
	}
	if packageHasVersionSuffix(pkg) {
		pkg = pkg[:strings.LastIndex(pkg, "@")]
	}
	command := exec.CommandContext(ctx, "sh", "-c", "npm root -g")
	env, err := method.RenderEnv(entry.Name, version, goos, goarch)
	if err != nil {
		return "", "", err
	}
	command.Env = append(os.Environ(), env...)
	output, err := command.Output()
	if err != nil {
		return "", "", fmt.Errorf("query npm root: %w", err)
	}
	root := strings.TrimSpace(string(output))
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pkg), "package.json"))
	if err != nil {
		return "", "", err
	}
	var metadata struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", "", err
	}
	path := filepath.Join(filepath.Dir(filepath.Dir(root)), "bin", entry.GetBinaryName())
	if _, err := os.Stat(path); err != nil {
		path = ""
	}
	return metadata.Version, path, nil
}

func installedVersion(ctx context.Context, method registry.InstallMethod, path string) string {
	if method.Type == "go" {
		if info, err := buildinfo.ReadFile(path); err == nil {
			return info.Main.Version
		}
		return ""
	}
	var command *exec.Cmd
	switch method.Type {
	case "apt":
		command = exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Version}", method.Package)
	case "apk":
		command = exec.CommandContext(ctx, "apk", "info", "-v", "-e", method.Package)
	default:
		return ""
	}
	output, err := command.Output()
	if err != nil {
		return ""
	}
	if method.Type == "apk" {
		return strings.TrimPrefix(strings.TrimSpace(string(output)), method.Package+"-")
	}
	return strings.TrimSpace(string(output))
}

func renderPackage(entry *registry.PackageEntry, method *registry.InstallMethod, version, goos, goarch string) (string, error) {
	return renderTemplateIfNeeded(entry, method, method.Package, version, goos, goarch)
}

func versionedPackage(pkg, version string) string {
	if version == "" {
		return pkg
	}
	if packageHasVersionSuffix(pkg) {
		lastAt := strings.LastIndex(pkg, "@")
		return pkg[:lastAt+1] + version
	}
	return pkg + "@" + version
}

func packageHasVersionSuffix(pkg string) bool {
	lastAt := strings.LastIndex(pkg, "@")
	lastSlash := strings.LastIndex(pkg, "/")
	return lastAt > lastSlash
}

func commandUsesVersion(method *registry.InstallMethod) bool {
	if method.Type == "script" && usesVersionTemplate(method.URLTemplate) {
		return true
	}
	for _, arg := range method.ScriptArgs {
		if usesVersionTemplate(arg) {
			return true
		}
	}
	if usesVersionTemplate(method.Command) || usesVersionTemplate(method.Package) {
		return true
	}
	for _, command := range method.PreCommands {
		if usesVersionTemplate(command) {
			return true
		}
	}
	for _, command := range method.PostCommands {
		if usesVersionTemplate(command) {
			return true
		}
	}
	for _, value := range method.Env {
		if usesVersionTemplate(value) {
			return true
		}
	}
	return false
}

func usesVersionTemplate(value string) bool {
	return strings.Contains(value, ".Version")
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
