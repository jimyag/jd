# jd

A CLI tool that downloads and installs developer tools from GitHub Releases using a built-in registry.

## Installation

```bash
go install github.com/jimyag/jd@latest
```

## Usage

```bash
# Install the latest version of a tool
jd <package>

# Install a specific version
jd <package>@<version>

# List available versions (latest 10)
jd <package> --list

# List all available versions
jd <package> --list-all

# List all supported packages
jd --list

# Generate shell completion (bash, zsh, fish, powershell)
jd --complete zsh > ~/.zshrc # or as needed
```

## Examples

```bash
jd go                  # install latest Go toolchain
jd go@1.24.0           # install a specific Go version
jd --list              # list all supported packages
jd kubectl --list      # show latest 10 kubectl versions
jd helm
jd gh
```

## Supported Packages

The list of supported packages is maintained in the [internal/registry/builtin/packages.yaml](internal/registry/builtin/packages.yaml) file. 

You can also list all supported packages using the CLI:
```bash
jd --list
```

## Install Modes

- `default` — downloads a tar.gz/zip archive and extracts the binary to `~/.local/bin`
- `file` — downloads a single binary directly (useful for Go binaries distributed as single files)
- `command` — runs a shell command (e.g. `npm install` or custom curl script)

Special cases:
- `go` installs to `~/.go/<version>` and symlinks `~/.go/goroot`.

## Environment Variables

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | GitHub personal access token to avoid rate limiting when resolving versions |

## Uninstall

Since `jd` installs binaries directly to `~/.local/bin`, you can simply remove the binary file:
```bash
rm ~/.local/bin/<package>
```

## Adding a Package

Edit `internal/registry/builtin/packages.yaml`.

### Archive (Default)
```yaml
- name: mytool
  description: My tool description
  version_from:
    type: github          # github or godev
    repo: owner/repo
    tag_prefix: "v"
  url_template: "https://github.com/owner/repo/releases/download/{{.Version}}/mytool_{{.VersionNoV}}_{{.OS}}_{{.Arch}}.tar.gz"
  inner_path: "mytool"   # path to binary inside archive
  os_map:
    darwin: darwin
    linux: linux
  arch_map:
    amd64: amd64
    arm64: arm64
```

### Direct File
```yaml
- name: single-binary
  mode: file
  url_template: "https://github.com/owner/repo/releases/download/{{.Version}}/tool-{{.OS}}-{{.Arch}}"
```

### Command
```yaml
- name: npm-tool
  mode: command
  command: "npm install -g some-package"
```

Template variables: `{{.Version}}`, `{{.VersionNoV}}` (without leading `v`), `{{.OS}}`, `{{.Arch}}`, `{{.Name}}`
