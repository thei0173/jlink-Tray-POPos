#!/usr/bin/env bash
# Installs jlink-tray's desktop entry in two places: ~/.config/autostart (so
# it launches automatically at login) and ~/.local/share/applications (so it
# shows up as a launchable app in the COSMIC app grid/launcher). Run as your
# normal user (no sudo) after install.sh has installed /usr/local/bin/jlink-tray
# and set up the udev rule.
set -euo pipefail

if [ "$EUID" -eq 0 ]; then
    echo "Run this as your normal user, not with sudo/root." >&2
    exit 1
fi

if [ ! -x /usr/local/bin/jlink-tray ]; then
    echo "Warning: /usr/local/bin/jlink-tray not found yet. Run install.sh first." >&2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

autostart_dir="$HOME/.config/autostart"
mkdir -p "$autostart_dir"
install -m 0644 "$script_dir/jlink-tray.desktop" "$autostart_dir/jlink-tray.desktop"

apps_dir="$HOME/.local/share/applications"
mkdir -p "$apps_dir"
install -m 0644 "$script_dir/jlink-tray.desktop" "$apps_dir/jlink-tray.desktop"

if command -v update-desktop-database &>/dev/null; then
    update-desktop-database "$apps_dir" 2>/dev/null || true
fi

echo "Installed:"
echo "  $autostart_dir/jlink-tray.desktop  (launches automatically at login)"
echo "  $apps_dir/jlink-tray.desktop  (shows up in the app launcher/grid)"
echo
echo "Autostart only takes effect on your *next* login/reboot -- it won't"
echo "launch retroactively in a session that's already running."
echo "To start it right now instead: /usr/local/bin/jlink-tray &"
