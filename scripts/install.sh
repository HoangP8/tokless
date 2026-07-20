#!/usr/bin/env bash
# tokless installer for macOS / Linux.
#   curl -fsSL https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.sh | bash

set -euo pipefail

OWNER="HoangP8"
REPO="tokless"
DEST="${HOME}/.local/bin"

ok()  { printf '\033[32m✔\033[0m %s\n' "$*"; }
err() { printf '\033[31m✖\033[0m %s\n' "$*" >&2; }

# OS + arch -> asset name.
case "$(uname -s)" in
  Linux*)  os="linux" ;;
  Darwin*) os="darwin" ;;
  *) err "Unsupported OS. Windows: use install.ps1 (irm … | iex)."; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  arch="x64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) err "Unsupported architecture: $(uname -m)."; exit 1 ;;
esac
asset="tokless-${os}-${arch}"
url="https://github.com/${OWNER}/${REPO}/releases/latest/download/${asset}"

# Download + install.
mkdir -p "$DEST"
tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
printf '\033[36m↓\033[0m Downloading %s…\n' "$asset"
if ! curl -fSL --progress-bar -o "$tmp" "$url" || [ ! -s "$tmp" ]; then
  err "Download failed ($asset). See https://github.com/${OWNER}/${REPO}/releases"
  exit 1
fi
chmod +x "$tmp"
install -m 0755 "$tmp" "${DEST}/tokless"
ok "installed tokless $("${DEST}/tokless" --version 2>/dev/null) → ${DEST}/tokless"

# Record the install channel so `tokless info` can report it exactly.
data_dir="${HOME}/.local/share/tokless"
mkdir -p "$data_dir"
printf '{"method":"install script","path":"%s","version":"%s","at":"%s"}\n' \
  "${DEST}/tokless" \
  "$("${DEST}/tokless" --version 2>/dev/null)" \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "${data_dir}/install.json"

# Ensure ~/.local/bin is on PATH for new shells.
case ":${PATH}:" in
  *":${DEST}:"*) : ;;
  *)
    case "$(basename "${SHELL:-bash}")" in
      zsh) rc="${ZDOTDIR:-$HOME}/.zshrc" ;;
      *)   rc="$HOME/.bashrc" ;;
    esac
    line="export PATH=\"${DEST}:\$PATH\""
    grep -qF "$DEST" "$rc" 2>/dev/null || printf '\n# tokless\n%s\n' "$line" >> "$rc"
    ok "Added ${DEST} to PATH in ${rc}."
    ;;
esac

# Run now, reconnecting the keyboard via /dev/tty so the picker works under a pipe.
if [ -r /dev/tty ]; then
  printf '\n'
  TOKLESS_INSTALLER_RUN=1 "${DEST}/tokless" </dev/tty || true
else
  ok "Run: tokless"
fi
