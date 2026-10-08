#!/bin/sh
set -e

main() {
  SOURCE="${JD_DOWNLOAD_SOURCE:-github}"
  while [ "$#" -gt 0 ]; do
    case "$1" in
    --cnb) SOURCE=cnb; shift ;;
    -h | --help | help) usage; return 0 ;;
    --) shift; break ;;
    *) break ;;
    esac
  done
  case "$SOURCE" in
  github | cnb) ;;
  *) echo "Unknown download source: $SOURCE" >&2; exit 1 ;;
  esac

  # Repository info
  REPO="${REPO:-jimyag/jd}"
  BINARY_NAME="${BINARY_NAME:-jd}"
  INSTALL_DIR="${INSTALL_DIR:-${HOME}/.local/bin}"

  # Detect OS
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "${OS}" in
  darwin) OS="darwin" ;;
  linux) OS="linux" ;;
  *)
    echo "Unsupported OS: ${OS}"
    exit 1
    ;;
  esac

  # Detect Architecture
  ARCH="$(uname -m)"
  case "${ARCH}" in
  x86_64) ARCH="amd64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported Architecture: ${ARCH}"
    exit 1
    ;;
  esac

  echo "Detected ${OS}-${ARCH}"

  CNB_TAG=$(printf 'github.com/%s/latest' "$REPO" | sed 's|/|%2F|g')
  CNB_BASE="https://cnb.cool/jimyag/release/-/releases/download/${CNB_TAG}"

  if [ "$SOURCE" = github ]; then
    echo "Fetching latest version for ${REPO}..."
    LATEST_TAG=$(curl -fsSL --connect-timeout 5 --max-time 15 "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$LATEST_TAG" ]; then
      echo "GitHub version lookup failed; trying CNB..." >&2
      SOURCE=cnb
    fi
  fi
  if [ "$SOURCE" = cnb ]; then
    fetch_cnb_version
  fi
  set_download_url

  # Create install directory if it doesn't exist
  mkdir -p "${INSTALL_DIR}"

  # Download to temporary file
  TMP_FILE=$(mktemp)
  trap 'rm -f "$TMP_FILE"' EXIT
  trap 'exit 1' HUP INT TERM
  echo "Downloading ${LATEST_TAG} from ${DOWNLOAD_URL}..."
  if ! curl -fsSL --connect-timeout 5 --max-time 120 "${DOWNLOAD_URL}" -o "${TMP_FILE}"; then
    if [ "$SOURCE" = cnb ]; then
      echo "CNB binary unavailable for ${REPO} (${OS}/${ARCH})." >&2
      exit 1
    fi
    echo "GitHub download failed; trying CNB..." >&2
    SOURCE=cnb
    fetch_cnb_version
    set_download_url
    curl -fsSL --connect-timeout 5 --max-time 120 "${DOWNLOAD_URL}" -o "${TMP_FILE}"
  fi

  # Install
  chmod +x "${TMP_FILE}"
  mv "${TMP_FILE}" "${INSTALL_DIR}/${BINARY_NAME}"

  if [ "$SOURCE" = cnb ]; then
    export JD_DOWNLOAD_SOURCE=cnb
  fi

  echo "Successfully installed ${BINARY_NAME} to ${INSTALL_DIR}/${BINARY_NAME}"

  # Check if INSTALL_DIR is in PATH
  case ":${PATH}:" in
  *:"${INSTALL_DIR}":*) ;;
  *)
    echo ""
    echo "Warning: ${INSTALL_DIR} is not in your PATH."
    echo "Please add it to your shell configuration (e.g., .bashrc or .zshrc):"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    ;;
  esac

  "${INSTALL_DIR}/${BINARY_NAME}" --version

  if [ "$#" -gt 0 ]; then
    echo "Installing packages with ${BINARY_NAME}: $*"
    "${INSTALL_DIR}/${BINARY_NAME}" "$@"
  fi
}

fetch_cnb_version() {
  LATEST_TAG=$(curl -fsSL --connect-timeout 5 --max-time 15 "${CNB_BASE}/upstream-version.txt") || {
    echo "CNB latest release unavailable for ${REPO}." >&2
    exit 1
  }
  case "$LATEST_TAG" in
  '' | *[!a-zA-Z0-9._+-]*) echo "Invalid CNB version: $LATEST_TAG" >&2; exit 1 ;;
  esac
}

set_download_url() {
  VERSION_NO_V="${LATEST_TAG#v}"
  ASSET="${BINARY_NAME}_${VERSION_NO_V}_${OS}_${ARCH}"
  if [ "$SOURCE" = cnb ]; then
    DOWNLOAD_URL="${CNB_BASE}/${ASSET}"
  else
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/${ASSET}"
  fi
}

usage() {
  cat <<'EOF'
jd installer

Usage:
  curl -fsSL https://jd.jimyag.com | sh
  curl -fsSL https://jd.jimyag.com | sh -s -- gh kubectl
  curl -fsSL https://jd.jimyag.com | sh -s -- gh@2.80.0
  curl -fsSL https://jd.jimyag.com | sh -s -- --help
  curl -fsSL https://jd.jimyag.com | sh -s -- --cnb zellij

Installs jd to ~/.local/bin, then passes any arguments to jd.
Set INSTALL_DIR to change the install location.
Use --cnb (before package arguments) to download jd and GitHub binaries only from CNB.
By default, failed GitHub requests are retried against CNB. CNB mirrors only latest releases.
EOF
}

main "$@"
