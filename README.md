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
jd list
```

## Examples

```bash
jd go                  # install latest Go toolchain
jd go@1.24.0           # install a specific Go version
jd kubectl --list      # show latest 10 kubectl versions
jd helm
jd gh
```

## Supported Packages

| Name | Description |
|------|-------------|
| age | Simple file encryption tool |
| chezmoi | Dotfiles manager |
| claude-code | Claude Code CLI by Anthropic |
| codex | OpenAI Codex CLI |
| cursor | Cursor AI code editor |
| dive | Docker image layer explorer |
| frpc | frp client — fast reverse proxy |
| frps | frp server — fast reverse proxy |
| gemini-cli | Google Gemini CLI |
| gh | GitHub CLI |
| go | Go programming language toolchain |
| gohttpserver | HTTP file server with web UI |
| golangci-lint | Go linters aggregator |
| helm | Kubernetes package manager |
| hugo | Static site generator (extended edition) |
| kubectl | Kubernetes CLI |
| mihomo | Rule-based proxy tunnel (Clash Meta) |
| nettrace | Linux network tracing tool (OpenCloudOS) |
| nexttrace | Visual traceroute tool |
| tailscale | VPN mesh network |
| task | Task runner / build tool (Taskfile) |
| virtctl | KubeVirt VM management CLI |

## Install Modes

- default — downloads a tar.gz/zip archive and extracts the binary to `~/.local/bin`
- `file` — downloads a single binary directly
- `command` — runs a shell command (e.g. curl pipe or npm install)

Special cases:
- `go` installs to `~/.go/<version>` and symlinks `~/.go/goroot`

## Environment Variables

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | GitHub personal access token to avoid rate limiting when resolving versions |

## Adding a Package

Edit `internal/registry/builtin/packages.yaml`. Each entry supports:

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

Template variables: `{{.Version}}`, `{{.VersionNoV}}` (without leading `v`), `{{.OS}}`, `{{.Arch}}`, `{{.Name}}`
