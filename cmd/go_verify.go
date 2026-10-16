package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jimyag/jd/internal/state"
)

var goSensitiveTools = []string{"golangci-lint", "staticcheck", "gopls", "dlv", "goimports", "gofumpt"}

func isGoSensitive(name string) bool { return slices.Contains(goSensitiveTools, name) }

func verifyGoTools(ctx context.Context, installed map[string]state.Installation) error {
	goInstall, ok := installed["go"]
	if !ok {
		return fmt.Errorf("go is not recorded by jd")
	}
	goBin := filepath.Join(goInstall.Path, "bin", "go")
	dir, err := os.MkdirTemp("", "jd-go-check-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"jd Go verification\")\n}\n"), 0o600); err != nil {
		return err
	}
	env := append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOROOT="+goInstall.Path, "PATH="+filepath.Dir(goBin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	run := func(path, input string, args ...string) (string, error) {
		checkCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		command := exec.CommandContext(checkCtx, path, args...)
		command.Dir, command.Env, command.Stdin = dir, env, strings.NewReader(input)
		output, err := command.CombinedOutput()
		if err != nil {
			return string(output), fmt.Errorf("%s %s: %w\n%s", path, strings.Join(args, " "), err, output)
		}
		return string(output), nil
	}
	output, err := run(goBin, "", "version")
	if err != nil {
		return err
	}
	fmt.Print(output)
	if _, err := run(goBin, "", "mod", "init", "example.com/jd-go-check"); err != nil {
		return err
	}
	if _, err := run(goBin, "", "build", "-gcflags=all=-N -l", "-o", "fixture", "."); err != nil {
		return err
	}
	if output, err := run(filepath.Join(dir, "fixture"), ""); err != nil || strings.TrimSpace(output) != "jd Go verification" {
		return fmt.Errorf("go fixture execution failed: %q: %v", output, err)
	}
	var failures []error
	for _, name := range goSensitiveTools {
		i, ok := installed[name]
		if !ok {
			continue
		}
		actual, err := exec.LookPath(name)
		expected, expectedErr := filepath.EvalSymlinks(i.Path)
		resolved, resolveErr := filepath.EvalSymlinks(actual)
		if err != nil || expectedErr != nil || resolveErr != nil || resolved != expected {
			failures = append(failures, fmt.Errorf("%s resolves to %q, expected jd installation %q; remove the shadowing installation or fix PATH", name, actual, i.Path))
			continue
		}
		var args []string
		input := ""
		switch name {
		case "golangci-lint":
			args = []string{"run", "./..."}
		case "staticcheck":
			args = []string{"./..."}
		case "gopls":
			args = []string{"check", "main.go"}
		case "goimports", "gofumpt":
			args = []string{"-l", "main.go"}
		case "dlv":
			args, input = []string{"exec", "--allow-non-terminal-interactive", "./fixture"}, "continue\nquit\n"
		}
		output, err := run(i.Path, input, args...)
		if err == nil && (name == "goimports" || name == "gofumpt") && strings.TrimSpace(output) != "" {
			err = fmt.Errorf("unexpected formatting changes: %s", output)
		}
		if err == nil && name == "dlv" && !strings.Contains(output, "jd Go verification") {
			err = fmt.Errorf("debugger did not execute fixture: %s", output)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("verify %s: %w", name, err))
			continue
		}
		fmt.Printf("  verified %s using Go %s\n", name, goInstall.Version)
	}
	return errors.Join(failures...)
}
