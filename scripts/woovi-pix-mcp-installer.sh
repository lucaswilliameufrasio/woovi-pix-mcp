#!/usr/bin/env sh
set -eu

REPO="lucaswilliameufrasio/woovi-pix-mcp"
TAG="latest"
BIN_DIR="${HOME}/.local/bin"
FORCE=""

usage() {
  cat <<USAGE
woovi-pix-mcp installer

Usage:
  woovi-pix-mcp-installer.sh [--tag <vX.Y.Z|latest>] [--bin-dir <path>] [--force]

Options:
  --tag       Release tag (default: latest)
  --bin-dir   Install directory (default: ~/.local/bin)
  --force     Replace an existing installation without prompting
  -h, --help  Show this help
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tag)
      [ "$#" -ge 2 ] || { echo "--tag requires a value" >&2; exit 1; }
      TAG="$2"
      shift 2
      ;;
    --bin-dir)
      [ "$#" -ge 2 ] || { echo "--bin-dir requires a value" >&2; exit 1; }
      BIN_DIR="$2"
      shift 2
      ;;
    --force)
      FORCE="yes"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { echo "tar is required" >&2; exit 1; }

OS="$(uname -s)"
ARCH="$(uname -m)"
case "$OS" in
  Linux) OS_NAME="linux" ;;
  Darwin) OS_NAME="darwin" ;;
  *)
    echo "Unsupported OS: $OS. Download a release artifact manually." >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH_NAME="amd64" ;;
  arm64|aarch64) ARCH_NAME="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

if [ "$TAG" = "latest" ]; then
  RELEASE_JSON="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest")"
  TAG="$(printf '%s' "$RELEASE_JSON" | tr -d '\n' | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  [ -n "$TAG" ] || { echo "Failed to resolve latest release tag for $REPO" >&2; exit 1; }
fi

printf '%s\n' "$TAG" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' || {
  echo "Invalid stable release tag: $TAG" >&2
  exit 1
}

VERSION="${TAG#v}"
ASSET="woovi-pix-mcp_${VERSION}_${OS_NAME}_${ARCH_NAME}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t woovi-pix-mcp-install)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT HUP INT TERM

ASSET_FILE="${TMP_DIR}/${ASSET}"
CHECKSUM_FILE="${TMP_DIR}/checksums.txt"
echo "Downloading ${BASE_URL}/${ASSET}"
curl -fsSL "${BASE_URL}/${ASSET}" -o "$ASSET_FILE"
curl -fsSL "${BASE_URL}/checksums.txt" -o "$CHECKSUM_FILE"

EXPECTED_HASH="$(awk -v asset="$ASSET" '$2 == asset || $2 == "*" asset { print $1 }' "$CHECKSUM_FILE")"
case "$EXPECTED_HASH" in
  *[!0123456789abcdefABCDEF]*|"") echo "Invalid or missing checksum for $ASSET" >&2; exit 1 ;;
esac
[ "${#EXPECTED_HASH}" -eq 64 ] || { echo "Invalid checksum for $ASSET" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL_HASH="$(sha256sum "$ASSET_FILE" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL_HASH="$(shasum -a 256 "$ASSET_FILE" | awk '{print $1}')"
else
  echo "sha256sum or shasum is required for checksum verification" >&2
  exit 1
fi
[ "$EXPECTED_HASH" = "$ACTUAL_HASH" ] || { echo "Checksum mismatch for $ASSET" >&2; exit 1; }
echo "Checksum verified"

tar -xf "$ASSET_FILE" -C "$TMP_DIR"
[ -f "${TMP_DIR}/woovi-pix-mcp" ] || { echo "Archive does not contain woovi-pix-mcp" >&2; exit 1; }
if [ -e "${BIN_DIR}/woovi-pix-mcp" ] && [ "$FORCE" != "yes" ]; then
  printf 'Replace %s? [y/N] ' "${BIN_DIR}/woovi-pix-mcp"
  if [ -r /dev/tty ]; then
    read -r answer </dev/tty || answer=n
  else
    answer=n
    echo "non-interactive, keeping existing installation" >&2
    echo "Use --force to replace it" >&2
  fi
  case "$answer" in [Yy]*) ;; *) echo "Installation cancelled"; exit 1 ;; esac
fi
mkdir -p "$BIN_DIR"
install -m 0755 "${TMP_DIR}/woovi-pix-mcp" "${BIN_DIR}/woovi-pix-mcp"

echo "Installed woovi-pix-mcp ${TAG} to ${BIN_DIR}/woovi-pix-mcp"
case ":${PATH}:" in
  *:"${BIN_DIR}":*) ;;
  *) echo "Add ${BIN_DIR} to PATH: export PATH=\"${BIN_DIR}:\$PATH\"" ;;
esac
