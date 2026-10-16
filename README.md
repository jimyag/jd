# jd

A CLI tool that installs developer tools from a built-in registry.

[![Check](https://github.com/jimyag/jd/actions/workflows/check.yaml/badge.svg)](https://github.com/jimyag/jd/actions/workflows/check.yaml)
[![Release](https://github.com/jimyag/jd/actions/workflows/release.yml/badge.svg)](https://github.com/jimyag/jd/actions/workflows/release.yml)

`jd` can install release binaries, run package managers such as Homebrew and APT, and use language installers such as Go and npm.

## Installation

Install using curl:
```bash
curl -fsSL https://jd.jimyag.com | sh

# Install jd, then install packages with it
curl -fsSL https://jd.jimyag.com | sh -s -- gh kubectl

# Install jd, then install a specific package version
curl -fsSL https://jd.jimyag.com | sh -s -- gh@2.80.0

# Install jd and a GitHub release binary using only the CNB mirror
curl -fsSL https://jd.jimyag.com | sh -s -- --cnb zellij

# Show installer help without installing anything
curl -fsSL https://jd.jimyag.com | sh -s -- --help
```

Or using Go:
```bash
go install github.com/jimyag/jd@latest
```

## Usage

```bash
# Install the latest version of a tool
jd <package>

# Install a specific version when the selected method supports versions
jd <package>@<version>

# Use only the latest GitHub release binary mirrored on CNB
jd --cnb zellij

# Force a specific install method
jd <package> --method binary
jd <package> --method apt

# List available versions (latest 10)
jd <package> --list

# List all available versions
jd <package> --list-all

# List all supported packages
jd --list

# Force refresh the remote registry cache
jd --refresh-registry --list

# Show successful installations and their recorded sources
jd --installed

# Upgrade all recorded tools, preserving exact version pins and install methods
jd --upgrade
jd --upgrade golangci-lint

# Upgrade Go, then installed Go-sensitive tools and run real compatibility checks
jd --upgrade go

# Generate shell completion (bash, zsh, fish, powershell)
jd --complete zsh > ~/.zshrc # or as needed
```

## Examples

```bash
jd go                  # install the latest Go toolchain
jd go@1.24.0           # install a specific Go version
jd --list              # list all supported packages
jd kubectl --list      # show latest 10 kubectl versions
jd helm
jd gh
jd jq --method brew    # force Homebrew
jd jq --method apt     # force APT
```

## Supported Packages

The list of supported packages is maintained in the [internal/registry/builtin/packages.yaml](internal/registry/builtin/packages.yaml) file. 

You can also extend or override the built-in registry locally:
- `~/.config/jd/packages.yaml`
- `~/.config/jd/packages.d/*.yaml`

Load order is:
1. built-in registry
2. remote registry from `JD_REGISTRY_URL` or the default `main` branch registry
3. `~/.config/jd/packages.yaml`
4. `~/.config/jd/packages.d/*.yaml` in lexical filename order

If the same package name appears in different registry files, the later file replaces the earlier one. A single YAML file cannot define the same package name twice.

The remote registry is cached for 30 minutes in the system cache directory. If the remote registry is unavailable, `jd` falls back to the bundled registry and still applies local overrides. Use `--refresh-registry` or `JD_REFRESH_REMOTE_REGISTRY=1` to force a remote refresh. Set `JD_DISABLE_REMOTE_REGISTRY=1` to use only the bundled and local registries.

By default, jd tries CNB when GitHub registry, latest-version, or binary download
requests fail. A binary download retry requires CNB to mirror the same version.
The installer script also retries CNB when downloading jd itself fails. Installer
requests allow 30 seconds to connect and retry failures twice. Version lookups
allow 60 seconds per attempt, and binary downloads allow 10 minutes per attempt.

Use `--cnb` or `JD_DOWNLOAD_SOURCE=cnb` to require CNB for the registry and GitHub
release binaries. CNB keeps only the latest release per upstream repository.
Missing releases, assets, older pinned versions, and packages without a mirrored
GitHub binary return an error; this mode does not fall back to GitHub or package
managers. `install.sh --cnb` applies this mode to the packages it installs; use
`jd --cnb` or the environment variable for subsequent invocations.

Example local registry:
```yaml
packages:
  - name: kubectl
    description: local kubectl override
    doc_url: "https://kubernetes.io/docs/tasks/tools/"
    mode: file
    version_from:
      type: github
      repo: kubernetes/kubernetes
      tag_prefix: "v"
    url_template: "https://dl.k8s.io/release/{{.Version}}/bin/{{.OS}}/{{.Arch}}/kubectl"

  - name: my-tool
    description: custom local package
    doc_url: "https://example.com/my-tool"
    mode: command
    command: "echo install my-tool"
```

You can also list all supported packages using the CLI:
```bash
jd --list
```

## Install Methods

Each package can declare one or more install methods.

Official binary methods are preferred, followed by official `script` methods and available system package managers,
then language installers and custom commands. Numeric priorities order methods within
each group. Selection filters the OS and architecture and checks package-manager
availability. Once selected, a failed method returns an error; download failures do
not silently switch the installation to another package manager. `--method` explicitly
selects a source.

Successful CLI installations are recorded in `${XDG_STATE_HOME:-~/.local/state}/jd/installed.json`.
`jd --upgrade` only manages those records. Explicit versions are pinned; `@latest`
remains eligible for updates. Reinstalls and upgrades preserve the recorded source;
use `--method` on a normal installation to change it. An explicit version changes its pin.
Package-manager versions follow their distribution repositories; explicit version pins
are currently rejected for these methods. Command methods may not report a version;
upgrading reruns their installer. Go methods install into `~/.local/bin` using `GOBIN`
and the active SDK (`GOTOOLCHAIN=local`), without downloading another Go toolchain.
Npm methods use `~/.local` as their prefix so switching Node versions does not create
another global installation. A method can explicitly override `NPM_CONFIG_PREFIX` for
a legacy runtime. Npm executables use the active Node runtime.

For GitHub version lookups, jd uses `GITHUB_TOKEN`, then `GH_TOKEN`, then an existing
GitHub CLI login when available. Credentials are not written to installation records.

One non-blocking OS file lock covers a whole CLI operation, including registry writes,
installation and state updates. Another writer exits with the holder's PID. Batch
upgrades run sequentially and report all independent failures with a nonzero exit.
`--installed` reads the atomically replaced records without taking the writer lock.

When upgrading Go, jd also updates already recorded `golangci-lint`, `staticcheck`,
`gopls`, `dlv`, `goimports` and `gofumpt`. Uninstalled tools are not added and pinned
tools remain pinned. A temporary module is compiled and executed, analyzed, formatted,
and debugged with the installed tools. Shadowing PATH entries and unsupported Go
versions fail verification. Previous versioned Go directories are retained; a failure
does not claim that the whole upgrade was rolled back.

Common method types:
- `binary` — download an archive or file and install the binary
- `script` — download an upstream installer completely, then run it with Bash (or `command: sh`); `script_args` supplies arguments and `version_command` reports its installed version
- `command` — run an explicit shell command
- `brew`, `apt`, `apk`, `dnf`, `yum`, `pacman` — package manager installs
- `go`, `npm` — language-specific installers

Package manager defaults:
- `apt`, `apk`, `dnf`, `yum`, `pacman` use `sudo` by default
- `brew`, `go`, `npm`, `script`, `command`, `binary` do not use `sudo` by default
- `use_sudo: true|false` on a method overrides the default

Hooks:
- `pre_commands` run before the main method command
- `post_commands` run only after the main method command succeeds
- `doc_url` links to the upstream install documentation for that method

Package-level fields such as `doc_url`, `version_from`, `url_template`, `os_map`, `arch_map`, and `supported_platforms` are inherited by methods unless the method overrides them.

Version pins:
- Binary methods use `version_from`, `url_template`, and `version_prefix`.
- Go and npm methods replace an existing package suffix such as `@latest` when you run `jd <package>@<version>`.
- Command methods can use `{{.Version}}` in `command`, hooks, package names, or env values.
- Package manager methods such as `brew`, `apt`, and `apk` reject version pins. Scripts reject pins unless their URL, arguments, command, or environment uses `{{.Version}}`.

## Environment Variables

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | GitHub personal access token to avoid rate limiting when resolving versions |
| `JD_DOWNLOAD_SOURCE` | Set to `cnb` to require the latest GitHub binaries mirrored on CNB. |
| `JD_REGISTRY_URL` | Remote registry YAML URL. Defaults to the `main` branch built-in registry file. |
| `JD_REFRESH_REMOTE_REGISTRY` | Set to `1` to skip the 30-minute remote registry cache. |
| `JD_DISABLE_REMOTE_REGISTRY` | Set to `1` to disable remote registry loading. |
| `JD_REGISTRY_STRICT` | Set to `1` to fail when the remote registry cannot be loaded. |

## Uninstall

Since `jd` installs binaries directly to `~/.local/bin`, you can simply remove the binary file:
```bash
rm ~/.local/bin/<package>
```

## Adding a Package

Edit `internal/registry/builtin/packages.yaml`.

### Binary Method
```yaml
- name: mytool
  description: My tool description
  doc_url: "https://example.com/mytool"
  version_from:
    type: github          # github or godev
    repo: owner/repo
    tag_prefix: "v"
  url_template: "https://github.com/owner/repo/releases/download/{{.Version}}/mytool_{{.VersionNoV}}_{{.OS}}_{{.Arch}}.tar.gz"
  os_map:
    darwin: darwin
    linux: linux
  arch_map:
    amd64: amd64
    arm64: arm64
  methods:
    - type: binary
      priority: 100
      inner_path: "mytool"   # path to binary inside archive
```

For a direct single-file download, set `mode: file`. If the downloaded file is gzip-compressed, `jd` detects and decompresses it before installing.

### Package Manager Fallback
```yaml
- name: gh
  description: GitHub CLI
  methods:
    - type: binary
      priority: 100
      doc_url: "https://github.com/cli/cli/releases/latest"
      version_from:
        type: github
        repo: cli/cli
        tag_prefix: "v"
      url_template: "https://github.com/cli/cli/releases/download/{{.Version}}/gh_{{.VersionNoV}}_{{.OS}}_{{.Arch}}{{if eq .OS \"macOS\"}}.zip{{else}}.tar.gz{{end}}"
      inner_path: "gh_{{.VersionNoV}}_{{.OS}}_{{.Arch}}/bin/gh"

    - type: apt
      priority: 60
      doc_url: "https://github.com/cli/cli/blob/trunk/docs/install_linux.md"
      package: gh
      supported_platforms: ["linux"]
      pre_commands:
        - "(type -p wget >/dev/null || (sudo apt update && sudo apt install wget -y))"
        - "sudo mkdir -p -m 755 /etc/apt/keyrings"
        - "out=$(mktemp) && wget -nv -O$out https://cli.github.com/packages/githubcli-archive-keyring.gpg && cat $out | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg >/dev/null"
        - "sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg"
        - "sudo mkdir -p -m 755 /etc/apt/sources.list.d"
        - "echo \"deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main\" | sudo tee /etc/apt/sources.list.d/github-cli.list >/dev/null"
        - "sudo apt update"
```

### Command
```yaml
- name: npm-tool
  description: Example command installer
  doc_url: "https://example.com/npm-tool"
  methods:
    - type: command
      priority: 100
      command: "npm install -g some-package"
```

### NPM
```yaml
- name: codex
  description: OpenAI Codex CLI
  doc_url: "https://github.com/openai/codex"
  methods:
    - type: npm
      priority: 100
      package: "@openai/codex@latest"
```

Template variables: `{{.Version}}`, `{{.VersionNoV}}` (without leading `v`), `{{.OS}}`, `{{.Arch}}`, `{{.Name}}`

## Development

Run the Go tests locally:

```bash
go test ./...
```

CI also uses golangci-lint to check newly introduced findings.

## Release

Pushing a `v*` tag builds and publishes binaries with GoReleaser. The release notes list commits since the previous tag.
