package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"

	buildversion "github.com/jimmicro/version"
	"github.com/jimyag/jd/internal/installer"
	"github.com/jimyag/jd/internal/registry"
	"github.com/jimyag/jd/internal/state"
	"github.com/jimyag/jd/internal/versioner"
	"github.com/spf13/cobra"
)

var (
	rootListVersions    bool
	rootListAllVersions bool
	rootComplete        string
	rootMethod          string
	rootRefreshRegistry bool
	rootCNB             bool
	rootUpgrade         bool
	rootInstalled       bool
)

var (
	loadRegistry   = registry.Load
	installPackage = installer.InstallTracked
)

const defaultVersionLimit = 10

var rootCmd = &cobra.Command{
	Use:   "jd",
	Short: "jimyag-download: install binaries from GitHub Releases",
	Long:  "jd installs and updates CLI tools from GitHub Releases using a built-in registry.",
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
	Version: buildversion.Version(),
	Args:    cobra.ArbitraryArgs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		lock, err := state.Lock()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		defer func() { _ = lock.Close() }()
		r, err := loadRegistry()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		pkgs := r.List()
		var names []string
		for _, p := range pkgs {
			if strings.HasPrefix(p.Name, toComplete) {
				names = append(names, p.Name)
			}
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	},
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if rootInstalled {
			if len(args) > 0 || rootUpgrade {
				return fmt.Errorf("--installed cannot be combined with packages or --upgrade")
			}
			return printInstalled()
		}
		if len(args) == 0 && !rootUpgrade && !rootListVersions && !rootListAllVersions && !rootRefreshRegistry && rootComplete == "" {
			return cmd.Help()
		}
		lock, err := state.Lock()
		if err != nil {
			return err
		}
		defer func() {
			if err := lock.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "release jd lock:", err)
			}
		}()
		if rootCNB {
			previous, present := os.LookupEnv("JD_DOWNLOAD_SOURCE")
			if err := os.Setenv("JD_DOWNLOAD_SOURCE", "cnb"); err != nil {
				return err
			}
			defer func() {
				if present {
					_ = os.Setenv("JD_DOWNLOAD_SOURCE", previous)
				} else {
					_ = os.Unsetenv("JD_DOWNLOAD_SOURCE")
				}
			}()
		}
		if rootComplete != "" {
			return runComplete(cmd, rootComplete)
		}

		var r *registry.Registry
		if rootRefreshRegistry {
			r, err = registry.LoadWithOptions(registry.LoadOptions{RefreshRemote: true})
		} else {
			r, err = loadRegistry()
		}
		if err != nil {
			return err
		}
		if rootUpgrade {
			return upgradePackages(cmd.Context(), r, args)
		}

		if len(args) == 0 {
			if rootListVersions || rootListAllVersions {
				return printAllPackages(r)
			}
			return cmd.Help()
		}

		name, _ := parsePackageArg(args[0])
		entry, ok := r.Find(name)
		if !ok {
			return suggestNotFound(r, name)
		}
		if rootListVersions || rootListAllVersions {
			return printVersions(entry, rootListAllVersions)
		}
		installed, err := state.Load()
		if err != nil {
			return err
		}
		for _, arg := range args {
			name, version := parsePackageArg(arg)
			entry, ok := r.Find(name)
			if !ok {
				return suggestNotFound(r, name)
			}
			if err := installAndSave(cmd.Context(), entry, version, installer.InstallOptions{Method: rootMethod}, installed); err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	rootCmd.Flags().BoolVar(&rootUpgrade, "upgrade", false, "Upgrade recorded tools; upgrading Go also updates and checks installed Go-sensitive tools")
	rootCmd.Flags().BoolVar(&rootInstalled, "installed", false, "List tools successfully installed by jd")
	rootCmd.Flags().BoolVar(&rootCNB, "cnb", false, "Use only the latest binaries mirrored on CNB")
	rootCmd.Flags().BoolVar(&rootListVersions, "list", false, "List all packages or package versions")
	rootCmd.Flags().BoolVar(&rootListAllVersions, "list-all", false, "List all available versions for a package")
	rootCmd.Flags().StringVar(&rootComplete, "complete", "", "Generate shell completion script (bash, zsh, fish, powershell)")
	rootCmd.Flags().StringVar(&rootMethod, "method", "", "Force a specific install method (for example: binary, brew, apt)")
	rootCmd.Flags().BoolVar(&rootRefreshRegistry, "refresh-registry", false, "Refresh the remote package registry cache")
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}

func printInstalled() error {
	installed, err := state.Load()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(installed))
	for name := range installed {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		fmt.Println("No tools recorded by jd.")
		return nil
	}
	fmt.Printf("%-20s %-18s %-10s %-18s %s\n", "NAME", "VERSION", "METHOD", "PIN", "PATH")
	for _, name := range names {
		i := installed[name]
		fmt.Printf("%-20s %-18s %-10s %-18s %s\n", name, i.Version, i.Method, i.RequestedVersion, i.Path)
	}
	return nil
}

func installAndSave(ctx context.Context, entry *registry.PackageEntry, version string, opts installer.InstallOptions, installed map[string]state.Installation) error {
	if version == "latest" {
		version = ""
	}
	if previous, ok := installed[entry.Name]; ok {
		opts.Current = new(previous)
		if opts.Method == "" {
			opts.Method = previous.Method
		}
	}
	result, err := installPackage(ctx, entry, version, opts)
	if err != nil {
		return fmt.Errorf("%s: %w", entry.Name, err)
	}
	installed[entry.Name] = result
	if err := state.Save(installed); err != nil {
		return fmt.Errorf("%s installed but recording failed: %w", entry.Name, err)
	}
	return nil
}

func upgradePackages(ctx context.Context, r *registry.Registry, args []string) error {
	if rootListVersions || rootListAllVersions {
		return fmt.Errorf("--upgrade cannot be combined with --list or --list-all")
	}
	installed, err := state.Load()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		for name := range installed {
			args = append(args, name)
		}
	}
	if len(args) == 0 {
		fmt.Println("No tools recorded by jd; install tools with jd first.")
		return nil
	}
	if rootMethod != "" {
		return fmt.Errorf("--upgrade preserves recorded methods; use a normal installation to change --method")
	}
	requests := make(map[string]string)
	for _, arg := range args {
		name, version := parsePackageArg(arg)
		if _, ok := installed[name]; !ok {
			return fmt.Errorf("%s is not recorded by jd; install it with jd first", name)
		}
		requests[name] = version
	}
	_, upgradingGo := requests["go"]
	if upgradingGo {
		for _, name := range goSensitiveTools {
			if _, ok := installed[name]; ok {
				if _, exists := requests[name]; !exists {
					requests[name] = ""
				}
			}
		}
	}
	names := make([]string, 0, len(requests))
	for name := range requests {
		if name != "go" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if upgradingGo {
		names = append([]string{"go"}, names...)
	}
	var failures []error
	goFailed := false
	for _, name := range names {
		previous := installed[name]
		if previous.RequestedVersion != "" && requests[name] == "" {
			fmt.Printf("  skipping %s: pinned to %s\n", name, previous.RequestedVersion)
			continue
		}
		if goFailed && isGoSensitive(name) {
			failures = append(failures, fmt.Errorf("%s skipped because Go upgrade failed", name))
			continue
		}
		entry, ok := r.Find(name)
		if !ok {
			failures = append(failures, suggestNotFound(r, name))
			continue
		}
		err := installAndSave(ctx, entry, requests[name], installer.InstallOptions{Method: previous.Method, Upgrade: true}, installed)
		if err != nil {
			failures = append(failures, err)
			fmt.Fprintln(os.Stderr, err)
			if name == "go" {
				goFailed = true
			}
		} else if name == "go" {
			oldRoot, hadRoot := os.LookupEnv("GOROOT")
			oldPath := os.Getenv("PATH")
			defer func() {
				if hadRoot {
					_ = os.Setenv("GOROOT", oldRoot)
				} else {
					_ = os.Unsetenv("GOROOT")
				}
				_ = os.Setenv("PATH", oldPath)
			}()
			root := installed["go"].Path
			if err := os.Setenv("GOROOT", root); err != nil {
				return err
			}
			if err := os.Setenv("PATH", root+"/bin"+string(os.PathListSeparator)+oldPath); err != nil {
				return err
			}
		}
	}
	if upgradingGo && !goFailed {
		if err := verifyGoTools(ctx, installed); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func runComplete(cmd *cobra.Command, shell string) error {
	var err error
	switch shell {
	case "bash":
		err = cmd.Root().GenBashCompletion(os.Stdout)
	case "zsh":
		err = cmd.Root().GenZshCompletion(os.Stdout)
	case "fish":
		err = cmd.Root().GenFishCompletion(os.Stdout, true)
	case "powershell":
		err = cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
	default:
		return fmt.Errorf("unsupported shell: %q (supported: bash, zsh, fish, powershell)", shell)
	}
	return err
}

func printAllPackages(r *registry.Registry) error {
	pkgs := r.List()
	sort.Slice(pkgs, func(i, j int) bool {
		return pkgs[i].Name < pkgs[j].Name
	})

	fmt.Printf("%-20s  %s\n", "NAME", "DESCRIPTION")
	fmt.Printf("%-20s  %s\n", "----", "-----------")
	for _, p := range pkgs {
		fmt.Printf("%-20s  %s\n", p.Name, p.Description)
	}
	return nil
}

// Execute runs the root command.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if os.Getenv("GITHUB_TOKEN") == "" {
		token := os.Getenv("GH_TOKEN")
		if token == "" {
			if output, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com").Output(); err == nil {
				token = strings.TrimSpace(string(output))
			}
		}
		if token != "" {
			if err := os.Setenv("GITHUB_TOKEN", token); err != nil {
				return err
			}
		}
	}
	return rootCmd.ExecuteContext(ctx)
}

func parsePackageArg(arg string) (name, version string) {
	parts := strings.SplitN(arg, "@", 2)
	name = parts[0]
	if len(parts) == 2 {
		version = parts[1]
	}
	return
}

func printVersions(entry *registry.PackageEntry, all bool) error {
	versionSource, ok := entry.VersionSourceForMethod(rootMethod)
	if !ok {
		mode := entry.Mode
		if rootMethod != "" {
			mode = rootMethod
		}
		if mode == "" && len(entry.Methods) > 0 {
			mode = entry.Methods[0].Type
		}
		fmt.Printf("package %s does not support version listing (installed via %s)\n", entry.Name, mode)
		return nil
	}
	fmt.Printf("fetching versions for %s...\n", entry.Name)
	versions, err := versioner.List(versionSource)
	if err != nil {
		return err
	}
	if !all && len(versions) > defaultVersionLimit {
		versions = versions[:defaultVersionLimit]
	}
	for i, v := range versions {
		if i == 0 {
			fmt.Printf("  %s  (latest)\n", v)
		} else {
			fmt.Printf("  %s\n", v)
		}
	}
	return nil
}

func suggestNotFound(r *registry.Registry, name string) error {
	pkgs := r.List()
	var suggestions []string
	nameLower := strings.ToLower(name)
	for _, p := range pkgs {
		if strings.Contains(strings.ToLower(p.Name), nameLower) ||
			strings.Contains(nameLower, strings.ToLower(p.Name)) {
			suggestions = append(suggestions, p.Name)
		}
	}
	sort.Strings(suggestions)
	msg := fmt.Sprintf("package %q not found", name)
	if len(suggestions) > 0 {
		msg += fmt.Sprintf("\n  did you mean: %s", strings.Join(suggestions, ", "))
	}
	msg += "\n  run `jd list` to see all available packages"
	return fmt.Errorf("%s", msg)
}
