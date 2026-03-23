package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	buildversion "github.com/jimmicro/version"
	"github.com/jimyag/jd/internal/installer"
	"github.com/jimyag/jd/internal/registry"
	"github.com/jimyag/jd/internal/versioner"
	"github.com/spf13/cobra"
)

var rootListVersions bool
var rootListAllVersions bool
var rootComplete string
var rootMethod string

var loadRegistry = registry.LoadBuiltin
var installPackage = installer.InstallWithOptions

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
		if rootComplete != "" {
			return runComplete(cmd, rootComplete)
		}

		r, err := loadRegistry()
		if err != nil {
			return err
		}

		if len(args) == 0 {
			if rootListVersions || rootListAllVersions {
				return printAllPackages(r)
			}
			return cmd.Help()
		}

		name, version := parsePackageArg(args[0])
		entry, ok := r.Find(name)
		if !ok {
			return suggestNotFound(r, name)
		}
		if rootListVersions || rootListAllVersions {
			return printVersions(entry, rootListAllVersions)
		}
		return installPackage(context.Background(), entry, version, installer.InstallOptions{
			Method: rootMethod,
		})
	},
}

func init() {
	rootCmd.Flags().BoolVar(&rootListVersions, "list", false, "List all packages or package versions")
	rootCmd.Flags().BoolVar(&rootListAllVersions, "list-all", false, "List all available versions for a package")
	rootCmd.Flags().StringVar(&rootComplete, "complete", "", "Generate shell completion script (bash, zsh, fish, powershell)")
	rootCmd.Flags().StringVar(&rootMethod, "method", "", "Force a specific install method (for example: binary, brew, apt)")
	rootCmd.SetVersionTemplate("{{.Version}}\n")
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
	return rootCmd.Execute()
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
