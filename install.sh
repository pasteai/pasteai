#!/usr/bin/env sh
# Install the pasteai binary from GitHub Releases.
# Usage: curl -sSL https://raw.githubusercontent.com/pasteai/pasteai/main/install.sh | sh
#
# Environment overrides (used in testing):
#   VERSION             — specific version to install, e.g. "0.0.1" (default: latest)
#   PASTEAI_BASE_URL    — override download base URL (default: GitHub releases)
#   PASTEAI_API_URL     — override GitHub API URL for version resolution
#   PASTEAI_INSTALL_DIR — install location (default: ~/.local/bin)
set -e

if [ -z "${HOME:-}" ]; then
  echo "error: HOME is not set" >&2
  exit 1
fi

REPO="pasteai/pasteai"
INSTALL_DIR="${PASTEAI_INSTALL_DIR:-$HOME/.local/bin}"
BASE_URL="${PASTEAI_BASE_URL:-https://github.com/${REPO}/releases/download}"
API_URL="${PASTEAI_API_URL:-https://api.github.com/repos/${REPO}/releases/latest}"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux|darwin) ;;
  *) echo "error: unsupported OS: $OS" >&2; exit 1 ;;
esac

# Detect arch
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "error: unsupported arch: $ARCH" >&2; exit 1 ;;
esac

# Resolve version
if [ -z "$VERSION" ]; then
  TAG=$(curl -sf "$API_URL" \
    | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  if [ -z "$TAG" ]; then
    echo "error: could not determine latest version from GitHub API" >&2
    exit 1
  fi
else
  TAG="v${VERSION#v}"
fi
VERSION="${TAG#v}"

URL="${BASE_URL}/${TAG}/pasteai_${VERSION}_${OS}_${ARCH}.tar.gz"

echo "Installing pasteai ${TAG} (${OS}/${ARCH}) to ${INSTALL_DIR}..."
mkdir -p "$INSTALL_DIR"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

curl -sSL --fail "$URL" | tar xz -C "$TMP" pasteai
mv "$TMP/pasteai" "$INSTALL_DIR/pasteai"
chmod +x "$INSTALL_DIR/pasteai"

echo "✓ pasteai ${TAG} installed to ${INSTALL_DIR}/pasteai"

# Add to shell profile if not already in PATH
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*)
    ;;
  *)
    SHELL_RC=""
    EXPORT_LINE=""
    case "${SHELL:-}" in
      */zsh)  SHELL_RC="$HOME/.zshrc"
              EXPORT_LINE="export PATH=\"\$PATH:${INSTALL_DIR}\"" ;;
      */bash) SHELL_RC="$HOME/.bashrc"
              EXPORT_LINE="export PATH=\"\$PATH:${INSTALL_DIR}\"" ;;
      */fish) SHELL_RC="$HOME/.config/fish/config.fish"
              if fish -c "fish_add_path --help" >/dev/null 2>&1; then
                EXPORT_LINE="fish_add_path ${INSTALL_DIR}"
              else
                EXPORT_LINE="set -gx PATH \$PATH ${INSTALL_DIR}"
              fi ;;
    esac

    if [ -n "$SHELL_RC" ]; then
      if ! grep -qF "$INSTALL_DIR" "$SHELL_RC" 2>/dev/null; then
        printf '\n# pasteai\n%s\n' "$EXPORT_LINE" >> "$SHELL_RC"
        echo "  Added ${INSTALL_DIR} to PATH in ${SHELL_RC}"
        echo "  Run: source ${SHELL_RC}"
      fi
    else
      echo "  Add to your shell profile manually:"
      echo "    export PATH=\"\$PATH:${INSTALL_DIR}\""
    fi
    ;;
esac

echo ""
"$INSTALL_DIR/pasteai" version

# Register MCP globally in ~/.claude.json via the installed binary.
# PASTEAI_MODE=embedded (default) | local | remote
# PASTEAI_URL required when PASTEAI_MODE=remote
# PASTEAI_API_KEY optional, included when mode=remote
_MODE="${PASTEAI_MODE:-embedded}"
_URL="${PASTEAI_URL:-}"
_KEY="${PASTEAI_API_KEY:-}"

if [ "$_MODE" = "remote" ] && [ -z "$_URL" ]; then
  echo "error: PASTEAI_URL is required when PASTEAI_MODE=remote" >&2
  exit 1
fi

# Build args with positional parameters — never eval, safe against special chars.
set -- "$INSTALL_DIR/pasteai" setup -mode "$_MODE"
[ -n "$_URL" ] && set -- "$@" -url "$_URL"
[ -n "$_KEY" ] && set -- "$@" -api-key "$_KEY"

if "$@"; then
  :
else
  echo "" >&2
  echo "Could not configure MCP automatically. Add to ~/.claude.json manually:" >&2
  printf '  {"mcpServers":{"pasteai":{"command":"%s","args":["mcp"]}}}\n' "$INSTALL_DIR/pasteai" >&2
  echo "Then run: pasteai doctor" >&2
fi
