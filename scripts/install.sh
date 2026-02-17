#!/usr/bin/env bash
set -euo pipefail

APP_NAME="chatcode"
BIN_PATH="/usr/local/bin/chatcode"
REPO="OpenWiseLabs/chatcode"
VERSION="latest"
MODE="install"
YES=0
DRY_RUN=0
TMP_DIR=""

usage() {
  cat <<'USAGE'
Usage:
  install.sh [--version <vX.Y.Z|latest>] [--yes] [--dry-run]
  install.sh --upgrade [--version <vX.Y.Z|latest>] [--yes] [--dry-run]
  install.sh --uninstall [--yes] [--dry-run]
USAGE
}

log() {
  printf '[install] %s\n' "$*"
}

die() {
  printf '[install] error: %s\n' "$*" >&2
  exit 1
}

run_cmd() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '[dry-run] %s\n' "$*"
    return 0
  fi
  "$@"
}

require_root() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    return 0
  fi
  if [[ "$(id -u)" -ne 0 ]]; then
    die "please run as root"
  fi
}

require_tools() {
  local missing=()
  for t in curl tar shasum uname mktemp grep; do
    command -v "$t" >/dev/null 2>&1 || missing+=("$t")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    die "missing required tools: ${missing[*]}"
  fi
}

confirm_or_exit() {
  if [[ "$YES" -eq 1 ]]; then
    return 0
  fi
  local answer
  read -r -p "$1 [y/N]: " answer
  case "$answer" in
    y|Y|yes|YES) return 0 ;;
    *) die "aborted" ;;
  esac
}

detect_platform() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"

  case "$os" in
    Linux) GOOS="linux" ;;
    Darwin) GOOS="darwin" ;;
    *) die "unsupported OS: $os" ;;
  esac

  case "$arch" in
    x86_64|amd64) GOARCH="amd64" ;;
    arm64|aarch64) GOARCH="arm64" ;;
    *) die "unsupported arch: $arch" ;;
  esac
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --version)
        VERSION="${2:-}"
        shift 2
        ;;
      --upgrade)
        MODE="upgrade"
        shift
        ;;
      --uninstall)
        MODE="uninstall"
        shift
        ;;
      --yes)
        YES=1
        shift
        ;;
      --dry-run)
        DRY_RUN=1
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "unknown arg: $1"
        ;;
    esac
  done
}

download_release() {
  TMP_DIR="$(mktemp -d)"
  local api_base="https://api.github.com/repos/${REPO}/releases"
  local resolved

  if [[ "$VERSION" == "latest" ]]; then
    resolved="$(curl -fsSL "${api_base}/latest" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
  else
    resolved="$VERSION"
  fi

  [[ -n "$resolved" ]] || die "unable to resolve release version"

  RELEASE_VERSION="$resolved"
  RELEASE_TAR_NAME="chatcode_${RELEASE_VERSION}_${GOOS}_${GOARCH}.tar.gz"
  RELEASE_TAR_URL="https://github.com/${REPO}/releases/download/${RELEASE_VERSION}/${RELEASE_TAR_NAME}"
  RELEASE_SUMS_URL="https://github.com/${REPO}/releases/download/${RELEASE_VERSION}/checksums.txt"

  log "downloading ${RELEASE_TAR_NAME}"
  run_cmd curl -fL "$RELEASE_TAR_URL" -o "$TMP_DIR/$RELEASE_TAR_NAME"
  run_cmd curl -fL "$RELEASE_SUMS_URL" -o "$TMP_DIR/checksums.txt"

  if [[ "$DRY_RUN" -eq 0 ]]; then
    (cd "$TMP_DIR" && grep "  ${RELEASE_TAR_NAME}$" checksums.txt | shasum -a 256 -c -)
  else
    printf '[dry-run] verify checksum for %s\n' "$RELEASE_TAR_NAME"
  fi
}

install_binary() {
  local extract_dir="${TMP_DIR}/chatcode_${RELEASE_VERSION}_${GOOS}_${GOARCH}"
  run_cmd tar -xzf "${TMP_DIR}/${RELEASE_TAR_NAME}" -C "$TMP_DIR"

  if [[ "$DRY_RUN" -eq 0 ]]; then
    [[ -f "${extract_dir}/chatcode" ]] || die "binary missing in package"
    install -m 0755 "${extract_dir}/chatcode" "$BIN_PATH"
  else
    printf '[dry-run] install -m 0755 %s %s\n' "${extract_dir}/chatcode" "$BIN_PATH"
  fi

  log "installed ${APP_NAME} ${RELEASE_VERSION} to ${BIN_PATH}"
}

uninstall_binary() {
  run_cmd rm -f "$BIN_PATH"
  log "removed ${BIN_PATH}"
}

cleanup() {
  if [[ -n "$TMP_DIR" && -d "$TMP_DIR" ]]; then
    rm -rf "$TMP_DIR"
  fi
}

main() {
  parse_args "$@"
  trap cleanup EXIT

  case "$MODE" in
    install|upgrade)
      require_root
      require_tools
      detect_platform
      if [[ "$MODE" == "install" ]]; then
        confirm_or_exit "Install ${APP_NAME}?"
      else
        confirm_or_exit "Upgrade ${APP_NAME}?"
      fi
      download_release
      install_binary
      ;;
    uninstall)
      require_root
      confirm_or_exit "Uninstall ${APP_NAME}?"
      uninstall_binary
      ;;
    *)
      die "unknown mode: $MODE"
      ;;
  esac
}

main "$@"
