#!/usr/bin/env bash
# One-command setup for jlink-tray.
#
# Run this as your normal user (no sudo): it builds the binary under your own
# account -- so it uses your Go module cache and never touches root's $HOME --
# then hands off to install.sh (which does need sudo) for the system-level
# parts: installing the Jabra SDK library, the udev rule, and the binary
# itself to /usr/local/bin. Finally it offers to set up autostart for you.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir"

if [ "$EUID" -eq 0 ]; then
    echo "Run this as your normal user, not with sudo -- it'll ask for your" >&2
    echo "password itself for the one step that actually needs it." >&2
    exit 1
fi

echo "jlink-tray setup"
echo "================"
echo

# --- 1. Make sure Go is available to build with -------------------------
if ! command -v go &>/dev/null; then
    echo "Go isn't installed, and it's needed to build jlink-tray."
    echo
    read -p "Install it now with 'sudo apt install golang-go'? [Y/n]: " go_response
    if [[ "$go_response" =~ ^[Nn]$ ]]; then
        echo "Okay -- install Go yourself and re-run ./setup.sh." >&2
        exit 1
    fi
    sudo apt update && sudo apt install -y golang-go build-essential libasound2-dev libcurl4-openssl-dev
fi

# --- 2. Build the binary as the current user ------------------------------
echo
echo "Building jlink-tray..."
go build -trimpath -o jlink-tray .
echo "Build complete."

# --- 3. Hand off to install.sh for the privileged parts -------------------
echo
echo "Now installing the Jabra SDK library, udev rule, and the binary."
echo "This step needs sudo."
echo
sudo ./install.sh

# --- 4. Offer to set up autostart (unprivileged, your own $HOME) ----------
echo
read -p "Start jlink-tray automatically when you log in? [Y/n]: " autostart_response
if [[ ! "$autostart_response" =~ ^[Nn]$ ]]; then
    ./setup-autostart.sh
fi

echo
echo "All done! If you were just added to the 'jlink' group for the first"
echo "time, log out and back in before starting jlink-tray."
echo "To start it right now: /usr/local/bin/jlink-tray &"
