#!/bin/sh
set -eu

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

INSTALL_DIR="${TMP_DIR}/bin"
CALLS_FILE="${TMP_DIR}/calls.txt"

cat > "${INSTALL_DIR}.jd" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "__CALLS_FILE__"
EOF

sed -i.bak "s|__CALLS_FILE__|${CALLS_FILE}|g" "${INSTALL_DIR}.jd"
rm -f "${INSTALL_DIR}.jd.bak"
chmod +x "${INSTALL_DIR}.jd"

INSTALL_DIR="${INSTALL_DIR}" BINARY_NAME="jd" sh -c '
  if [ "$#" -gt 0 ]; then
    "${INSTALL_DIR}.${BINARY_NAME}" "$@"
  fi
' -- gh kubectl@1.32.0

EXPECTED='gh kubectl@1.32.0'
ACTUAL="$(cat "${CALLS_FILE}")"
if [ "${ACTUAL}" != "${EXPECTED}" ]; then
  echo "expected: ${EXPECTED}"
  echo "actual: ${ACTUAL}"
  exit 1
fi
