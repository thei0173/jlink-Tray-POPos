#!/usr/bin/env bash
# One-command setup for jlink-tray. Run as your normal user (no sudo).
#
#   1. Makes sure Jabridge (the service that talks to your Jabra devices) is
#      installed. If not, it downloads the official release, verifies its
#      checksum, and runs `jabridge setup`, which shows the normal
#      administrator prompt for the one-time device-access step.
#   2. Builds jlink-tray (needs Go; no C compiler or Jabra SDK required).
#   3. Installs it to ~/.local/bin, adds it to your app launcher, and starts
#      it automatically at login.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

if [ "$EUID" -eq 0 ]; then
    echo "Run this as your normal user, not with sudo -- it asks for your" >&2
    echo "password itself for the one step that needs it." >&2
    exit 1
fi

ask() { # ask "question" -> true unless the answer starts with n/N
    local reply
    read -r -p "$1 [Y/n]: " reply
    [[ ! "$reply" =~ ^[Nn] ]]
}

export PATH="$HOME/.local/bin:$PATH"

echo "jlink-tray setup"
echo "================"

# --- 1. Jabridge ------------------------------------------------------------
if ! command -v jabridge &>/dev/null; then
    echo
    echo "jlink-tray needs Jabridge (https://github.com/Watchdog0x/jabridge),"
    echo "the community Linux app that talks to Jabra devices. It isn't installed."
    ask "Download the latest release and install it?" || { echo "Install Jabridge, then re-run ./setup.sh." >&2; exit 1; }

    [ "$(uname -m)" = "x86_64" ] || { echo "Only x86_64 release builds are supported by this script." >&2; exit 1; }
    for tool in curl tar sha256sum; do
        command -v "$tool" &>/dev/null || { echo "Missing required tool: $tool" >&2; exit 1; }
    done

    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    base="$(curl -fsSL https://api.github.com/repos/Watchdog0x/jabridge/releases/latest \
        | grep -o 'https://[^"]*_linux_amd64\.tar\.gz' | head -n1)"
    [ -n "$base" ] || { echo "Couldn't find the latest Jabridge release." >&2; exit 1; }
    echo "Downloading $base"
    (cd "$tmp" && curl -fsSLO "$base" && curl -fsSLO "$base.sha256" && sha256sum -c ./*.sha256 && tar xzf ./*.tar.gz)
    "$tmp"/jabridge_*/jabridge setup
    hash -r
fi

echo
echo "Jabridge: $(jabridge --version 2>&1 | head -n1)"

# --- 2. Build ------------------------------------------------------------------
if ! command -v go &>/dev/null; then
    echo
    echo "Go isn't installed, and it's needed to build jlink-tray."
    ask "Install it now with 'sudo apt install golang-go'?" || { echo "Install Go and re-run ./setup.sh." >&2; exit 1; }
    sudo apt update && sudo apt install -y golang-go
fi

echo
echo "Building jlink-tray..."
CGO_ENABLED=0 go build -trimpath -o jlink-tray .

# --- 3. Install for this user ---------------------------------------------------
bin="$HOME/.local/bin/jlink-tray"
install -Dm755 jlink-tray "$bin"

for dir in "$HOME/.config/autostart" "$HOME/.local/share/applications"; do
    mkdir -p "$dir"
    sed "s|@BIN@|$bin|" jlink-tray.desktop > "$dir/jlink-tray.desktop"
    chmod 644 "$dir/jlink-tray.desktop"
done
command -v update-desktop-database &>/dev/null && update-desktop-database "$HOME/.local/share/applications" 2>/dev/null || true

# --- 4. Start it now ---------------------------------------------------------------
pkill -x jlink-tray 2>/dev/null || true
sleep 1
nohup "$bin" >/dev/null 2>&1 &

echo
echo "Done! jlink-tray is running in your panel tray, will start automatically"
echo "at login, and is in your app launcher as \"jLink Tray\"."
