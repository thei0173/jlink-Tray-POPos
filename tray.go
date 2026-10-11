package main

import (
	"fmt"
	"image/color"
	"sync"
	"time"

	"fyne.io/systray"
)

const (
	searchPollInterval = time.Second
	searchExtraPolls   = 3 // polls after the SDK scan returns, to catch late results
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
)

// uiState is the tray-only state: what the user is currently waiting on.
// Device state itself lives in jabraApi.go behind stateMu.
type uiState struct {
	mu           sync.Mutex
	busy         string // e.g. "Connecting to X…"; non-empty while an action is in flight
	searching    bool
	cancelSearch chan struct{}
	message      string // last error/notice, shown briefly
	messageAt    time.Time
}

func (u *uiState) snapshot() (busy string, searching bool, message string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	message = u.message
	if message != "" && time.Since(u.messageAt) > messageTTL {
		u.message = ""
		message = ""
	}
	return u.busy, u.searching, message
}

func (u *uiState) setMessage(m string) {
	u.mu.Lock()
	u.message = m
	u.messageAt = time.Now()
	u.mu.Unlock()
}

// begin marks the tray busy with msg. It fails if something else is already
// in flight, so overlapping clicks can't start conflicting SDK operations.
func (u *uiState) begin(msg string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.busy != "" || u.searching {
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

	// Rebuild on every state-change signal, coalescing bursts so a flurry of
	// updates (e.g. attach + first pairing-list poll) only triggers one
	// native menu rebuild instead of several.
	go func() {
		for range stateChangeCh {
			time.Sleep(300 * time.Millisecond)
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

// dongleSnapshot is a point-in-time, lock-free copy of the state needed to
// render the menu.
type dongleSnapshot struct {
	name   string
	paired []pairedDevice
}

func snapshotDongle() (dongleSnapshot, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()

	dongle, exists := deviceManager[selectedDongle]
	if !exists {
		return dongleSnapshot{}, false
	}

	snap := dongleSnapshot{name: dongle.deviceName}
	if dongle.pairingList != nil {
		snap.paired = append(snap.paired, dongle.pairingList.pairedDevices...)
	}
	return snap, true
}

func snapshotSearchResults() []pairedDevice {
	stateMu.Lock()
	defer stateMu.Unlock()
	return append([]pairedDevice(nil), searchDeviceList.pairedDevices...)
}

func clearSearchResults() {
	stateMu.Lock()
	searchDeviceList.count = 0
	searchDeviceList.pairedDevices = nil
	stateMu.Unlock()
}

func connectedDevice(paired []pairedDevice) (pairedDevice, bool) {
	for _, d := range paired {
		if d.isConnected {
			return d, true
		}
	}
	return pairedDevice{}, false
}

func isPaired(paired []pairedDevice, addr [6]byte) bool {
	for _, d := range paired {
		if d.deviceBTAddr == addr {
			return true
		}
	}
	return false
}

// statusLine is the one-line "what is happening right now" shown at the top
// of the menu and in the tooltip.
func statusLine(snap dongleSnapshot, busy string, searching bool) string {
	switch {
	case busy != "":
		return "⏳ " + busy
	case searching:
		return "🔍 Searching for new devices…"
	}
	if d, ok := connectedDevice(snap.paired); ok {
		return "● Connected to " + d.deviceName
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

	snap, ok := snapshotDongle()
	busy, searching, message := ui.snapshot()
	systray.ResetMenu()

	if !ok {
		setIcon(iconIdle)
		systray.SetTooltip("No Jabra dongle detected")
		item := systray.AddMenuItem("No Jabra dongle detected", "Plug in your Jabra dongle")
		item.Disable()
		systray.AddSeparator()
		addQuitItem()
		return
	}

	status := statusLine(snap, busy, searching)
	systray.SetTooltip(snap.name + " — " + status)
	switch {
	case busy != "" || searching:
		setIcon(iconBusy)
	default:
		if _, connected := connectedDevice(snap.paired); connected {
			setIcon(iconConnected)
		} else {
			setIcon(iconIdle)
		}
	}

	// --- Header: which dongle, and what it's doing right now ---
	header := systray.AddMenuItem(snap.name, "")
	header.Disable()
	statusItem := systray.AddMenuItem(status, "")
	statusItem.Disable()
	if message != "" {
		msg := systray.AddMenuItem(message, "")
		msg.Disable()
	}
	systray.AddSeparator()

	// --- Paired devices: click to connect / switch / disconnect ---
	locked := busy != "" || searching
	title := systray.AddMenuItem("Paired devices — click to connect", "")
	title.Disable()
	if len(snap.paired) == 0 {
		none := systray.AddMenuItem("   None yet — search below to pair one", "")
		none.Disable()
	}
	for i, dev := range snap.paired {
		addPairedDeviceItem(i, dev, locked)
	}
	systray.AddSeparator()

	// --- Pair a new device ---
	title = systray.AddMenuItem("Pair a new device", "")
	title.Disable()
	if searching {
		stop := systray.AddMenuItem("   Stop searching", "Leave pairing mode")
		go func() {
			for range stop.ClickedCh {
				stopSearch()
			}
		}()
	} else {
		label := "   Search for new devices (~20 s)"
		if len(snapshotSearchResults()) > 0 {
			label = "   Search again"
		}
		search := systray.AddMenuItem(label, "Put the dongle into pairing mode and scan")
		if busy != "" {
			search.Disable()
		}
		go func() {
			// for range (not a single receive): the item's ClickedCh is
			// closed, not sent-to, whenever this menu is torn down by the
			// next rebuild. Ranging only runs the body for a real click;
			// a close alone just ends the loop.
			for range search.ClickedCh {
				startSearch()
			}
		}()
	}

	found := snapshotSearchResults()
	shown := 0
	for i, dev := range found {
		if isPaired(snap.paired, dev.deviceBTAddr) {
			continue // already known; it's in the list above
		}
		if shown == 0 {
			hdr := systray.AddMenuItem("   Found nearby — click to pair:", "")
			hdr.Disable()
		}
		shown++
		addFoundDeviceItem(i, dev, busy != "")
	}
	if searching && shown == 0 {
		wait := systray.AddMenuItem("   Nothing found yet — put your device in pairing mode", "")
		wait.Disable()
	}
	if !searching && shown > 0 {
		clearItem := systray.AddMenuItem("   Clear results", "Dismiss the list and leave pairing mode")
		go func() {
			for range clearItem.ClickedCh {
				clearSearchResults()
				_ = setDongleInBTPairing(false)
				notifyStateChanged()
			}
		}()
	}

	systray.AddSeparator()
	addQuitItem()
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

func addPairedDeviceItem(index int, dev pairedDevice, locked bool) {
	label := "   " + dev.deviceName
	tooltip := "Connect to " + dev.deviceName
	if dev.isConnected {
		label += " — connected"
		tooltip = "Disconnect from " + dev.deviceName
	}
	item := systray.AddMenuItemCheckbox(label, tooltip, dev.isConnected)
	if locked {
		item.Disable()
	}
	go func() {
		for range item.ClickedCh {
			if dev.isConnected {
				disconnectFrom(dev, index)
			} else {
				switchTo(dev, index)
			}
		}
	}()
}

func addFoundDeviceItem(index int, dev pairedDevice, locked bool) {
	item := systray.AddMenuItem("   "+dev.deviceName, "Pair and connect to "+dev.deviceName)
	if locked {
		item.Disable()
	}
	go func() {
		for range item.ClickedCh {
			pairWith(dev, index)
		}
	}()
}

// waitForConnection polls the (1s-refreshed) pairing list until the device
// with the given address reports connected, or the timeout passes.
func waitForConnection(addr [6]byte, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if snap, ok := snapshotDongle(); ok {
			for _, d := range snap.paired {
				if d.deviceBTAddr == addr && d.isConnected {
					return true
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// switchTo makes dev the dongle's active connection. The SDK requires any
// currently connected device to be disconnected first, so do that.
func switchTo(dev pairedDevice, index int) {
	if !ui.begin("Connecting to " + dev.deviceName + "…") {
		return
	}
	notifyStateChanged()

	go func() {
		defer ui.end()

		if snap, ok := snapshotDongle(); ok {
			disconnected := false
			for i, d := range snap.paired {
				if i != index && d.isConnected {
					if err := disconnectDeviceFromPairedlist(uint16(i)); err != nil {
						fmt.Println("jlink-tray: disconnect failed:", err)
					} else {
						disconnected = true
					}
				}
			}
			if disconnected {
				time.Sleep(time.Second) // let the dongle settle
			}
		}

		if err := connectDeviceFromPairedlist(uint16(index)); err != nil {
			ui.setMessage(fmt.Sprintf("⚠ Couldn't connect to %s: %v", dev.deviceName, err))
			return
		}
		if !waitForConnection(dev.deviceBTAddr, connectTimeout) {
			ui.setMessage(fmt.Sprintf("⚠ %s didn't respond — is it switched on and in range?", dev.deviceName))
		}
	}()
}

func disconnectFrom(dev pairedDevice, index int) {
	if !ui.begin("Disconnecting from " + dev.deviceName + "…") {
		return
	}
	notifyStateChanged()

	go func() {
		defer ui.end()
		if err := disconnectDeviceFromPairedlist(uint16(index)); err != nil {
			ui.setMessage(fmt.Sprintf("⚠ Couldn't disconnect %s: %v", dev.deviceName, err))
			return
		}
		time.Sleep(time.Second)
	}()
}

// pairWith pairs and connects a device found by a search.
func pairWith(dev pairedDevice, index int) {
	if !ui.begin("Pairing with " + dev.deviceName + "…") {
		return
	}
	notifyStateChanged()

	go func() {
		defer ui.end()
		if err := connectNewDevice(uint16(index)); err != nil {
			ui.setMessage(fmt.Sprintf("⚠ Couldn't pair with %s: %v", dev.deviceName, err))
			return
		}
		clearSearchResults()
		if !waitForConnection(dev.deviceBTAddr, pairTimeout) {
			ui.setMessage(fmt.Sprintf("⚠ Paired with %s, but it hasn't connected yet", dev.deviceName))
		}
	}()
}

// stopSearch cancels a running search and takes the dongle out of pairing mode.
func stopSearch() {
	ui.mu.Lock()
	cancel := ui.cancelSearch
	ui.cancelSearch = nil
	ui.mu.Unlock()
	if cancel != nil {
		close(cancel)
	}
}

// startSearch puts the dongle into pairing mode and scans for nearby
// devices, publishing results to the menu as they appear.
func startSearch() {
	ui.mu.Lock()
	if ui.busy != "" || ui.searching {
		ui.mu.Unlock()
		return
	}
	ui.searching = true
	ui.message = ""
	cancel := make(chan struct{})
	ui.cancelSearch = cancel
	ui.mu.Unlock()

	clearSearchResults()
	notifyStateChanged()

	go func() {
		defer func() {
			ui.mu.Lock()
			ui.searching = false
			ui.cancelSearch = nil
			ui.mu.Unlock()
			notifyStateChanged()
		}()

		// The SDK scan blocks for ~20s, so run it aside and poll for
		// results meanwhile, letting devices show up as they're found.
		scanDone := make(chan error, 1)
		go func() { scanDone <- searchForNewDevices() }()

		deviceID, ok := dongleDeviceID()
		if !ok {
			return
		}

		finished := false
		extraPolls := searchExtraPolls
		for {
			if result := getSearchDeviceList(deviceID); result != nil {
				stateMu.Lock()
				searchDeviceList.count = result.count
				searchDeviceList.listType = result.listType
				searchDeviceList.pairedDevices = result.pairedDevices
				stateMu.Unlock()
				notifyStateChanged()
			}
			if finished {
				if extraPolls--; extraPolls < 0 {
					return
				}
			}

			select {
			case <-cancel:
				_ = setDongleInBTPairing(false)
				clearSearchResults()
				return
			case err := <-scanDone:
				scanDone = nil // a nil channel blocks forever: only handle this once
				finished = true
				if err != nil {
					ui.setMessage("⚠ Search failed: " + err.Error())
					return
				}
			case <-time.After(searchPollInterval):
			}
		}
	}()
}
