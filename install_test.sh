#!/bin/sh
set -eu

SCRIPT="$(cd "$(dirname "$0")" && pwd)/install.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
mkdir -p "$TMP_DIR/mock"
export CALLS_FILE="$TMP_DIR/calls" URLS_FILE="$TMP_DIR/urls"

cat > "$TMP_DIR/mock/uname" <<'EOF'
#!/bin/sh
case "$1" in -s) echo Linux ;; -m) echo x86_64 ;; esac
EOF
cat > "$TMP_DIR/mock/curl" <<'EOF'
#!/bin/sh
set -eu
url= output=
while [ "$#" -gt 0 ]; do
  case "$1" in
  -o) output="$2"; shift 2 ;;
  --connect-timeout | --max-time) shift 2 ;;
  https://*) url="$1"; shift ;;
  *) shift ;;
  esac
done
printf '%s\n' "$url" >> "$URLS_FILE"
case "$url" in
https://api.github.com/*)
  [ "$TEST_MODE" != github-api-failure ] || exit 22
  printf '{"tag_name": "v1.2.3"}\n'
  ;;
*/upstream-version.txt)
  [ "$TEST_MODE" != cnb-missing ] || exit 22
  printf 'v1.2.3\n'
  ;;
*)
  if [ "$TEST_MODE" = github-download-failure ]; then
    case "$url" in https://github.com/*) exit 22 ;; esac
  fi
  [ "$TEST_MODE" != cnb-binary-missing ] || exit 22
  cat > "$output" <<'BIN'
#!/bin/sh
printf '%s|%s\n' "${JD_DOWNLOAD_SOURCE:-github}" "$*" >> "$CALLS_FILE"
BIN
  ;;
esac
EOF
chmod +x "$TMP_DIR/mock/uname" "$TMP_DIR/mock/curl"
export PATH="$TMP_DIR/mock:$PATH"
export JD_DOWNLOAD_SOURCE=github

sh "$SCRIPT" --cnb --help | grep -F 'Use --cnb' >/dev/null
test ! -e "$URLS_FILE"

run_install() {
  TEST_MODE="$1"; export TEST_MODE
  shift
  : > "$CALLS_FILE"
  : > "$URLS_FILE"
  INSTALL_DIR="$TMP_DIR/bin" sh "$SCRIPT" "$@" > "$TMP_DIR/output" 2>&1
}

run_install success gh kubectl@1.32.0
test "$(cat "$CALLS_FILE")" = "$(printf 'github|--version\ngithub|gh kubectl@1.32.0')"
grep -F 'https://github.com/jimyag/jd/releases/download/v1.2.3/jd_1.2.3_linux_amd64' "$URLS_FILE" >/dev/null

for mode in success github-api-failure github-download-failure; do
  if [ "$mode" = success ]; then
    run_install "$mode" --cnb zellij
    if grep -F 'https://api.github.com/' "$URLS_FILE"; then exit 1; fi
  else
    run_install "$mode" zellij
  fi
  test "$(cat "$CALLS_FILE")" = "$(printf 'cnb|--version\ncnb|zellij')"
  grep -F 'github.com%2Fjimyag%2Fjd%2Flatest/jd_1.2.3_linux_amd64' "$URLS_FILE" >/dev/null
done

for mode in cnb-missing cnb-binary-missing; do
  if run_install "$mode" --cnb zellij; then
    echo "expected CNB failure: $mode" >&2
    exit 1
  fi
  test ! -s "$CALLS_FILE"
  if grep -F 'https://github.com/' "$URLS_FILE"; then exit 1; fi
done
echo 'installer tests passed'
