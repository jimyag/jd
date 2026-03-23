package cmd

import (
	"fmt"
	"sort"

	"github.com/jimyag/jd/internal/registry"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all supported packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := registry.LoadBuiltin()
		if err != nil {
			return err
		}

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
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
