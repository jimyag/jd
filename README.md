# jd

A CLI tool that downloads and installs developer tools from GitHub Releases using a built-in registry.

## Installation

Install using curl:
```bash
curl -fsSL https://raw.githubusercontent.com/jimyag/jd/main/install.sh | sh

# Install jd, then install packages with it
curl -fsSL https://raw.githubusercontent.com/jimyag/jd/main/install.sh | sh -s -- gh kubectl

# Install jd, then install a specific package version
curl -fsSL https://raw.githubusercontent.com/jimyag/jd/main/install.sh | sh -s -- gh@2.80.0
```

Or using Go:
```bash
go install github.com/jimyag/jd@latest
```

## Usage

```bash
# Install the latest version of a tool
jd <package>

# Install a specific version
jd <package>@<version>

# Force a specific install method
jd <package> --method binary
jd <package> --method apt

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

You can also extend or override the built-in registry locally:
- `~/.config/jd/packages.yaml`
- `~/.config/jd/packages.d/*.yaml`

Load order is:
1. built-in registry
2. `~/.config/jd/packages.yaml`
3. `~/.config/jd/packages.d/*.yaml` in lexical filename order

If the same package name appears multiple times, the later definition replaces the earlier one.

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

Each package can declare one or more install methods in priority order. `jd` sorts methods by `priority` from high to low and stops at the first successful one.

Common method types:
- `binary` — download an archive or file and install the binary
- `command` — run an explicit shell command
- `brew`, `apt`, `dnf`, `yum`, `pacman` — package manager installs
- `go`, `npm` — language-specific installers

Package manager defaults:
- `apt`, `dnf`, `yum`, `pacman` use `sudo` by default
- `brew`, `go`, `npm`, `command`, `binary` do not use `sudo` by default
- `use_sudo: true|false` on a method overrides the default

Hooks:
- `pre_commands` run before the main method command
- `post_commands` run only after the main method command succeeds
- `doc_url` links to the upstream install documentation for that method

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

### Binary Method
```yaml
- name: mytool
  description: My tool description
  methods:
    - type: binary
      priority: 100
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
  methods:
    - type: command
      priority: 100
      command: "npm install -g some-package"
```

### NPM
```yaml
- name: codex
  methods:
    - type: npm
      priority: 100
      doc_url: "https://github.com/openai/codex/blob/main/README.md"
      package: "@openai/codex"
```

Template variables: `{{.Version}}`, `{{.VersionNoV}}` (without leading `v`), `{{.OS}}`, `{{.Arch}}`, `{{.Name}}`
