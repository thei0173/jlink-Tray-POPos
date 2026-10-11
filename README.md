# jlink-tray

A COSMIC (Pop!_OS) panel tray GUI for switching between Bluetooth devices
connected to a Jabra dongle, built as a fork of
[Watchdog0x/jLink](https://github.com/Watchdog0x/jLink).

Upstream jLink is a terminal (keyboard-navigated) tool for managing Jabra
headsets/dongles on Linux -- the closest thing to "Jabra Direct" available on
Linux. This fork keeps all of its device-management code (built on Jabra's
native SDK) but replaces the terminal UI with a system tray menu, so you can
switch your active Bluetooth device with a couple of clicks from the COSMIC
panel instead of running a CLI.

## What's different from upstream

- **Tray menu instead of a TUI.** Uses [fyne.io/systray](https://github.com/fyne-io/systray)
  (StatusNotifierItem), the same tray protocol COSMIC's built-in indicators
  use, so no extra panel applet installation is needed.
- **Switch-device actually works.** Upstream's `connectDeviceFromPairedlist`,
  `disconnectDeviceFromPairedlist`, `removeDeviceFromPairedlist`, and
  `connectNewDevice` always acted on the *first* device in the list
  regardless of which one was selected, and passed the wrong ID to the SDK
  call. This is why "switch device" was never wired up in the original TUI.
  Fixed here.
- **Thread-safe.** A tray app has several goroutines clicking into the SDK
  concurrently (menu clicks, the pairing-list poller, device attach/remove
  callbacks). Added a mutex around the shared device state, which the
  original single-threaded TUI didn't need.
- **Fixed the udev rule.** The installer created a `jlink` group but the
  actual udev rule granted access to a hardcoded `users` group instead, so
  the group it created was never used. Now the rule references `jlink`, and
  the invoking user is added to it automatically.

## Requirements

- A Jabra Bluetooth dongle (tested with the Jabra Link 380)
- Pop!_OS / COSMIC desktop (or any Linux desktop with a StatusNotifierItem
  tray host)
- Go 1.22+ and a C toolchain (for cgo) -- `setup.sh` will offer to install
  these for you

## Install

```bash
git clone <this-repo-url>
cd jlink-tray
./setup.sh
```

`setup.sh` builds the binary as your normal user, then runs `install.sh`
(which needs `sudo`) to install the Jabra SDK library, set up a udev rule so
the tray app never needs root at runtime, and install the binary to
`/usr/local/bin/jlink-tray`. It'll then offer to set up autostart so it
launches automatically on login.

If you were just added to the `jlink` group for the first time, log out and
back in (or run `newgrp jlink`) before starting the tray app.

## Usage

Click the tray icon. The top of the menu always shows what's happening
(`● Connected to X`, `⏳ Connecting to X…`, `🔍 Searching…`), and the icon is
teal when connected, grey when not, and amber while busy.

- **Paired devices** -- click one to connect to it (any currently connected
  device is disconnected first). Click the connected one to disconnect.
- **Pair a new device** -- puts the dongle into pairing mode for ~20 seconds;
  nearby devices appear under "Found nearby" as they're discovered. Click one
  to pair and connect.

## License

Apache 2.0, same as upstream. See [LICENSE](LICENSE).
