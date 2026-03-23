package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jimyag/jd/internal/installer"
	"github.com/jimyag/jd/internal/registry"
	"github.com/jimyag/jd/internal/versioner"
	"github.com/spf13/cobra"
)

var rootListVersions bool

var rootCmd = &cobra.Command{
	Use:   "jd",
	Short: "jimyag-download: install binaries from GitHub Releases",
	Long:  "jd installs and updates CLI tools from GitHub Releases using a built-in registry.",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		r, err := registry.LoadBuiltin()
		if err != nil {
			return err
		}
		name, version := parsePackageArg(args[0])
		entry, ok := r.Find(name)
		if !ok {
			return suggestNotFound(r, name)
		}
		if rootListVersions {
			return printVersions(entry)
		}
		return installer.Install(context.Background(), entry, version)
	},
}

func init() {
	rootCmd.Flags().BoolVar(&rootListVersions, "list", false, "List available versions")
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

func printVersions(entry *registry.PackageEntry) error {
	fmt.Printf("fetching versions for %s...\n", entry.Name)
	versions, err := versioner.ListVersions(entry.VersionFrom.Repo)
	if err != nil {
		return err
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
