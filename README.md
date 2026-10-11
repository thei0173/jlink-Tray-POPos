# jlink-tray

A COSMIC (Pop!_OS) panel tray menu for switching between the Bluetooth devices
paired to a Jabra dongle -- a small, GUI front end for
[Jabridge](https://github.com/Watchdog0x/jabridge) (formerly jLink), the
community "Jabra Direct for Linux".

Jabridge's README invites exactly this: *"Build a GNOME, KDE, Hyprland or other
frontend using the IPC guide. No applet is bundled."* This is that applet. It
is a single static Go binary that talks to the Jabridge service over its local
JSON-RPC socket -- no Jabra SDK, no C compiler, no `sudo` for the tray itself.

## What you get

Click the tray icon. The top of the menu always says what's happening
(`● Connected to X`, `⏳ Connecting to X…`, `🔍 Searching…`), and the icon is
teal when connected, grey when not, and amber while busy.

- **Paired devices** -- click one to connect to it (any currently connected
  device is disconnected first). Click the connected one to disconnect.
- **Pair a new device** -- scans for ~20 seconds; nearby devices appear under
  "Found nearby" as they're discovered. Click one to pair and connect.
- Updates live from Jabridge's events, so connecting from your phone or the
  `jabridge` TUI is reflected immediately.

## Requirements

- A Jabra Bluetooth dongle (tested with the Jabra Link 380)
- Pop!_OS / COSMIC (or any desktop with a StatusNotifierItem tray host)
- Go 1.22+ to build (`setup.sh` offers to install it)
- [Jabridge](https://github.com/Watchdog0x/jabridge) -- `setup.sh` offers to
  download the latest release, verify its checksum, and run `jabridge setup`

## Install

```bash
git clone https://github.com/thei0173/jlink-Tray-POPos.git
cd jlink-Tray-POPos
./setup.sh
```

`setup.sh` runs as your normal user. The only privileged step is Jabridge's own
one-time `jabridge setup` (a normal administrator prompt that installs its
device-access rule). It then builds the tray, installs it to
`~/.local/bin/jlink-tray`, adds it to your app launcher as "jLink Tray", and
sets it to start at login. If the Jabridge service isn't running when the tray
starts, the tray starts it.

### Moving from the old SDK-based version?

Earlier versions linked Jabra's proprietary SDK. That's no longer needed. Both
can't drive the dongle at once, so stop the old one and optionally remove its
leftovers:

```bash
pkill -x jlink-tray
sudo rm -f /usr/local/bin/jlink-tray /usr/local/bin/jlink /etc/udev/rules.d/99-jabra.rules
sudo rm -rf /usr/lib/jabra /etc/ld.so.conf.d/jabra.conf && sudo ldconfig
```

## Credits and license

Built on [Jabridge](https://github.com/Watchdog0x/jabridge) by Watchdog0x
(this repository is a fork of it). Apache 2.0, same as upstream. See
[LICENSE](LICENSE). Not an official Jabra product.
