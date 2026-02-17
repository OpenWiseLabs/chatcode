#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  package.sh --version <vX.Y.Z> --goos <os> --goarch <arch> --binary <path> [--out-dir <dir>] [--checksums-file <file>]
EOF
}

VERSION=""
GOOS=""
GOARCH=""
BINARY=""
OUT_DIR="dist"
CHECKSUMS_FILE="checksums.txt"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      VERSION="${2:-}"
      shift 2
      ;;
    --goos)
      GOOS="${2:-}"
      shift 2
      ;;
    --goarch)
      GOARCH="${2:-}"
      shift 2
      ;;
    --binary)
      BINARY="${2:-}"
      shift 2
      ;;
    --out-dir)
      OUT_DIR="${2:-}"
      shift 2
      ;;
    --checksums-file)
      CHECKSUMS_FILE="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown arg: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "${VERSION}" || -z "${GOOS}" || -z "${GOARCH}" || -z "${BINARY}" ]]; then
  echo "missing required args" >&2
  usage
  exit 1
fi

if [[ ! -f "${BINARY}" ]]; then
  echo "binary not found: ${BINARY}" >&2
  exit 1
fi

mkdir -p "${OUT_DIR}"

PKG_BASE="chatcode_${VERSION}_${GOOS}_${GOARCH}"
PKG_DIR="${OUT_DIR}/${PKG_BASE}"
PKG_TAR="${OUT_DIR}/${PKG_BASE}.tar.gz"

rm -rf "${PKG_DIR}" "${PKG_TAR}"
mkdir -p "${PKG_DIR}"

cp "${BINARY}" "${PKG_DIR}/chatcode"
chmod 0755 "${PKG_DIR}/chatcode"

copy_if_exists() {
  local src="$1"
  local dest="$2"
  if [[ -e "${src}" ]]; then
    mkdir -p "$(dirname "${dest}")"
    cp "${src}" "${dest}"
  fi
}

copy_if_exists "configs/config.example.yaml" "${PKG_DIR}/configs/config.example.yaml"
copy_if_exists "docs/INSTALL.md" "${PKG_DIR}/docs/INSTALL.md"
copy_if_exists "scripts/install.sh" "${PKG_DIR}/scripts/install.sh"
copy_if_exists "deploy/linux/chatcode.service" "${PKG_DIR}/deploy/linux/chatcode.service"
copy_if_exists "deploy/macos/com.chatcode.daemon.plist" "${PKG_DIR}/deploy/macos/com.chatcode.daemon.plist"

tar -C "${OUT_DIR}" -czf "${PKG_TAR}" "${PKG_BASE}"
rm -rf "${PKG_DIR}"

(
  cd "${OUT_DIR}"
  shasum -a 256 "$(basename "${PKG_TAR}")" >> "${CHECKSUMS_FILE}"
)

echo "packaged: ${PKG_TAR}"
