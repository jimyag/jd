#!/bin/sh
set -e

# Repository info
REPO="${REPO:-jimyag/jd}"
BINARY_NAME="${BINARY_NAME:-jd}"
INSTALL_DIR="${INSTALL_DIR:-${HOME}/.local/bin}"

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${OS}" in
  darwin) OS="darwin" ;;
  linux) OS="linux" ;;
  *) echo "Unsupported OS: ${OS}"; exit 1 ;;
esac

# Detect Architecture
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported Architecture: ${ARCH}"; exit 1 ;;
esac

echo "Detected ${OS}-${ARCH}"

# Get latest version from GitHub API
echo "Fetching latest version for ${REPO}..."
LATEST_TAG=$(curl -s https://api.github.com/repos/${REPO}/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "${LATEST_TAG}" ]; then
  echo "Failed to fetch latest version. Please check your internet connection or GitHub API limits."
  exit 1
fi

# Remove 'v' prefix for the filename part
VERSION_NO_V=$(echo "${LATEST_TAG}" | sed 's/^v//')

# Construct download URL
# Example: https://github.com/jimyag/jd/releases/download/v0.0.1-alpha/jd_0.0.1-alpha_darwin_arm64
DOWNLOAD_URL="https://github.com/jimyag/jd/releases/download/${LATEST_TAG}/${BINARY_NAME}_${VERSION_NO_V}_${OS}_${ARCH}"

echo "Downloading ${LATEST_TAG} from ${DOWNLOAD_URL}..."

# Create install directory if it doesn't exist
mkdir -p "${INSTALL_DIR}"

# Download to temporary file
TMP_FILE=$(mktemp)
curl -fsSL "${DOWNLOAD_URL}" -o "${TMP_FILE}"

# Install
mv "${TMP_FILE}" "${INSTALL_DIR}/${BINARY_NAME}"
chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

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
