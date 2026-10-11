package main

import (
	"fmt"
	"image/color"
	"os/exec"
	"sync"
	"time"

	"fyne.io/systray"
)

const (
	searchPollInterval = time.Second
	connectTimeout     = 12 * time.Second
	pairTimeout        = 20 * time.Second
	messageTTL         = 15 * time.Second
)

var (
	// menuMu serializes rebuildMenu calls so ResetMenu/AddMenuItem from two
	// goroutines never interleave.
	menuMu sync.Mutex

	ui uiState

	// lastIcon avoids re-sending an identical icon on every menu rebuild.
	lastIcon color.RGBA
	iconSet  bool

	// lastMenuSig is a fingerprint of everything the menu displays. Rebuilding
	// an identical menu would visibly flicker while it's open, so skip those.
	lastMenuSig string
)

// uiState is the tray-only state: what the user is currently waiting on.
// Device state itself lives in the model, fed by the Jabridge service.
type uiState struct {
	mu        sync.Mutex
	busy      string // e.g. "Connecting to X…"; non-empty while an action is in flight
	message   string // last error/notice, shown briefly
	messageAt time.Time
	dismissed string // search session whose results the user has dismissed
}

func (u *uiState) snapshot() (busy, message, dismissed string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.message != "" && time.Since(u.messageAt) > messageTTL {
		u.message = ""
	}
	return u.busy, u.message, u.dismissed
}

func (u *uiState) setMessage(m string) {
	u.mu.Lock()
	u.message = m
	u.messageAt = time.Now()
	u.mu.Unlock()
}

func (u *uiState) dismiss(session string) {
	u.mu.Lock()
	u.dismissed = session
	u.mu.Unlock()
}

// begin marks the tray busy with msg. It fails if something else is already
// in flight, so overlapping clicks can't start conflicting operations.
func (u *uiState) begin(msg string) bool {
	searching := currentSnapshot().search.active()
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.busy != "" || searching {
		return false
	}
	u.busy = msg
	u.message = ""
	return true
}

func (u *uiState) end() {
	u.mu.Lock()
	u.busy = ""
	u.mu.Unlock()
	notifyStateChanged()
}

func onTrayReady() {
	systray.SetTooltip("jLink - Jabra Device Manager")
	rebuildMenu()
	startBackground()

	// If the Jabridge service isn't up yet (e.g. first login after install),
	// try to start it once rather than making the user do it.
	go func() {
		if call("service.ping", nil, nil) != nil {
			_ = exec.Command("jabridge", "service", "start").Run()
			refresh()
		}
	}()

	// Rebuild on every state-change signal, coalescing bursts.
	go func() {
		for range stateChangeCh {
			time.Sleep(100 * time.Millisecond)
			drainPending(stateChangeCh)
			rebuildMenu()
		}
	}()
}

func drainPending(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func onTrayExit() {}

func connectedDevice(paired []pairedDevice) (pairedDevice, bool) {
	for _, d := range paired {
		if d.Connected {
			return d, true
		}
	}
	return pairedDevice{}, false
}

func isPaired(paired []pairedDevice, name string) bool {
	for _, d := range paired {
		if d.Name == name {
			return true
		}
	}
	return false
}

// statusLine is the one-line "what is happening right now" shown at the top
// of the menu and in the tooltip.
func statusLine(snap snapshot, busy string) string {
	switch {
	case busy != "":
		return "⏳ " + busy
	case snap.search.active():
		return "🔍 Searching for new devices…"
	}
	if d, ok := connectedDevice(snap.paired); ok {
		return "● Connected to " + d.Name
	}
	return "○ Not connected"
}

func setIcon(c color.RGBA) {
	if iconSet && lastIcon == c {
		return
	}
	systray.SetIcon(generateIcon(c))
	lastIcon, iconSet = c, true
}

func rebuildMenu() {
	menuMu.Lock()
	defer menuMu.Unlock()

	snap := currentSnapshot()
	busy, message, dismissed := ui.snapshot()

	sig := fmt.Sprintf("%+v|%q|%q|%q", snap, busy, message, dismissed)
	if sig == lastMenuSig {
		return
	}
	lastMenuSig = sig

	systray.ResetMenu()

	if !snap.serviceUp || !snap.hasDongle {
		setIcon(iconIdle)
		if !snap.serviceUp {
			addServiceDownItems(message)
		} else {
			systray.SetTooltip("No Jabra dongle detected")
			systray.AddMenuItem("No Jabra dongle detected", "Plug in your Jabra dongle").Disable()
		}
		systray.AddSeparator()
		addQuitItem()
		return
	}

	status := statusLine(snap, busy)
	systray.SetTooltip(snap.dongle + " — " + status)
	_, connected := connectedDevice(snap.paired)
	switch {
	case busy != "" || snap.search.active():
		setIcon(iconBusy)
	case connected:
		setIcon(iconConnected)
	default:
		setIcon(iconIdle)
	}

	// --- Header: which dongle, and what it's doing right now ---
	systray.AddMenuItem(snap.dongle, "").Disable()
	systray.AddMenuItem(status, "").Disable()
	if message != "" {
		systray.AddMenuItem(message, "").Disable()
	}
	systray.AddSeparator()

	// --- Paired devices: click to connect / switch / disconnect ---
	locked := busy != "" || snap.search.active()
	systray.AddMenuItem("Paired devices — click to connect", "").Disable()
	if len(snap.paired) == 0 {
		systray.AddMenuItem("   None yet — search below to pair one", "").Disable()
	}
	for _, dev := range snap.paired {
		addPairedDeviceItem(dev, locked)
	}
	systray.AddSeparator()

	// --- Pair a new device ---
	systray.AddMenuItem("Pair a new device", "").Disable()
	showResults := snap.search.Session != dismissed
	if snap.search.active() {
		stop := systray.AddMenuItem("   Stop searching", "Leave pairing mode")
		go func() {
			for range stop.ClickedCh {
				stopSearch()
			}
		}()
	} else {
		label := "   Search for new devices (~20 s)"
		if showResults && len(snap.search.Devices) > 0 {
			label = "   Search again"
		}
		search := systray.AddMenuItem(label, "Put the dongle into pairing mode and scan")
		if busy != "" {
			search.Disable()
		}
		go func() {
			// for range (not a single receive): the item's ClickedCh is
			// closed, not sent-to, whenever this menu is torn down by the
			// next rebuild. Ranging only runs the body for a real click.
			for range search.ClickedCh {
				startSearch()
			}
		}()
	}

	shown := 0
	if showResults {
		for _, dev := range snap.search.Devices {
			if isPaired(snap.paired, dev.Name) {
				continue // already known; it's in the list above
			}
			if shown == 0 {
				systray.AddMenuItem("   Found nearby — click to pair:", "").Disable()
			}
			shown++
			addFoundDeviceItem(snap.search.Session, dev, busy != "")
		}
	}
	if snap.search.active() && shown == 0 {
		systray.AddMenuItem("   Nothing found yet — put your device in pairing mode", "").Disable()
	}
	if snap.search.State == "failed" && snap.search.Error != "" && showResults {
		systray.AddMenuItem("   ⚠ Search failed: "+snap.search.Error, "").Disable()
	}
	if !snap.search.active() && shown > 0 {
		clearItem := systray.AddMenuItem("   Clear results", "Dismiss the list")
		session := snap.search.Session
		go func() {
			for range clearItem.ClickedCh {
				ui.dismiss(session)
				notifyStateChanged()
			}
		}()
	}

	systray.AddSeparator()
	addQuitItem()
}

func addServiceDownItems(message string) {
	systray.SetTooltip("Jabridge service isn't running")
	systray.AddMenuItem("Jabridge service isn't running", "").Disable()
	if message != "" {
		systray.AddMenuItem(message, "").Disable()
	}
	start := systray.AddMenuItem("   Start Jabridge service", "Runs: jabridge service start")
	go func() {
		for range start.ClickedCh {
			if out, err := exec.Command("jabridge", "service", "start").CombinedOutput(); err != nil {
				ui.setMessage(fmt.Sprintf("⚠ Couldn't start Jabridge — is it installed? (%v) %s", err, out))
				notifyStateChanged()
			}
			refresh()
		}
	}()
}

func addQuitItem() {
	quit := systray.AddMenuItem("Quit", "Quit jLink Tray")
	go func() {
		// Must be `for range`, not a single receive, or a routine menu
		// rebuild would quit the app the moment this item's ClickedCh closes.
		for range quit.ClickedCh {
			systray.Quit()
		}
	}()
}

func addPairedDeviceItem(dev pairedDevice, locked bool) {
	label := "   " + dev.Name
	tooltip := "Connect to " + dev.Name
	if dev.Connected {
		label += " — connected"
		tooltip = "Disconnect from " + dev.Name
	}
	item := systray.AddMenuItemCheckbox(label, tooltip, dev.Connected)
	if locked {
		item.Disable()
	}
	go func() {
		for range item.ClickedCh {
			if dev.Connected {
				disconnectFrom(dev)
			} else {
				switchTo(dev)
			}
		}
	}()
}

func addFoundDeviceItem(session string, dev pairedDevice, locked bool) {
	item := systray.AddMenuItem("   "+dev.Name, "Pair and connect to "+dev.Name)
	if locked {
		item.Disable()
	}
	go func() {
		for range item.ClickedCh {
			pairWith(session, dev)
		}
	}()
}

// waitForConnection polls the service until the named device reports
// connected, or the timeout passes.
func waitForConnection(name string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		refresh()
		for _, d := range currentSnapshot().paired {
			if d.Name == name && d.Connected {
				return true
			}
		}
		time.Sleep(time.Second)
	}
	return false
}

// switchTo makes dev the dongle's active connection, disconnecting any
// currently connected device first.
func switchTo(dev pairedDevice) {
	if !ui.begin("Connecting to " + dev.Name + "…") {
		return
	}
	notifyStateChanged()

	go func() {
		defer ui.end()

		disconnected := false
		for _, d := range currentSnapshot().paired {
			if d.ID != dev.ID && d.Connected {
				if err := call("bt.disconnect", map[string]int{"index": d.ID}, nil); err != nil {
					fmt.Println("jlink-tray: disconnect failed:", err)
				} else {
					disconnected = true
				}
			}
		}
		if disconnected {
			time.Sleep(time.Second) // let the dongle settle
		}

		if err := call("bt.connect", map[string]int{"index": dev.ID}, nil); err != nil {
			ui.setMessage(fmt.Sprintf("⚠ Couldn't connect to %s: %v", dev.Name, err))
			return
		}
		if !waitForConnection(dev.Name, connectTimeout) {
			ui.setMessage(fmt.Sprintf("⚠ %s didn't respond — is it switched on and in range?", dev.Name))
		}
	}()
}

func disconnectFrom(dev pairedDevice) {
	if !ui.begin("Disconnecting from " + dev.Name + "…") {
		return
	}
	notifyStateChanged()

	go func() {
		defer ui.end()
		if err := call("bt.disconnect", map[string]int{"index": dev.ID}, nil); err != nil {
			ui.setMessage(fmt.Sprintf("⚠ Couldn't disconnect %s: %v", dev.Name, err))
			return
		}
		time.Sleep(time.Second)
		refresh()
	}()
}

// pairWith pairs and connects a device found by the current search.
func pairWith(session string, dev pairedDevice) {
	if !ui.begin("Pairing with " + dev.Name + "…") {
		return
	}
	notifyStateChanged()

	go func() {
		defer ui.end()
		params := map[string]any{"index": dev.ID, "session": session}
		if err := call("bt.search.connect", params, nil); err != nil {
			ui.setMessage(fmt.Sprintf("⚠ Couldn't pair with %s: %v", dev.Name, err))
			return
		}
		ui.dismiss(session)
		if !waitForConnection(dev.Name, pairTimeout) {
			ui.setMessage(fmt.Sprintf("⚠ Paired with %s, but it hasn't connected yet", dev.Name))
		}
	}()
}

func stopSearch() {
	if err := call("bt.search.stop", nil, nil); err != nil {
		ui.setMessage("⚠ Couldn't stop the search: " + err.Error())
	}
	refresh()
}

// startSearch asks the service to scan for nearby devices, then polls its
// status so results show up in the menu as they're found.
func startSearch() {
	if !ui.begin("Starting search…") {
		return
	}
	notifyStateChanged()
	err := call("bt.search", nil, nil)
	if err != nil {
		ui.setMessage("⚠ Couldn't start the search: " + err.Error())
	}
	ui.end()
	if err != nil {
		return
	}
	refresh()

	go func() {
		for i := 0; i < 60; i++ { // hard cap; a scan is ~20 s
			time.Sleep(searchPollInterval)
			refresh()
			if !currentSnapshot().search.active() {
				return
			}
		}
	}()
}
