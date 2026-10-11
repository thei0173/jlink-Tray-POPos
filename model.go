package main

import (
	"sync"
	"time"
)

type pairedDevice struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Addr      string `json:"addr"`
	Connected bool   `json:"connected"`
}

type searchState struct {
	State   string         `json:"state"` // idle, starting, searching, complete, failed
	Count   int            `json:"count"`
	Session string         `json:"session"`
	Error   string         `json:"error"`
	Devices []pairedDevice `json:"devices"`
}

func (s searchState) active() bool { return s.State == "starting" || s.State == "searching" }

type serviceDevice struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	IsDongle bool   `json:"isDongle"`
}

// snapshot is a lock-free copy of everything the menu displays.
type snapshot struct {
	serviceUp bool
	hasDongle bool
	dongle    string
	paired    []pairedDevice
	search    searchState
}

var model struct {
	mu   sync.Mutex
	snap snapshot
}

func currentSnapshot() snapshot {
	model.mu.Lock()
	defer model.mu.Unlock()
	s := model.snap
	s.paired = append([]pairedDevice(nil), s.paired...)
	s.search.Devices = append([]pairedDevice(nil), s.search.Devices...)
	return s
}

func setServiceUp(up bool) {
	model.mu.Lock()
	changed := model.snap.serviceUp != up
	model.snap.serviceUp = up
	if !up {
		model.snap = snapshot{}
	}
	model.mu.Unlock()
	if changed {
		notifyStateChanged()
	}
}

// refresh reloads device state from the service and signals a menu update.
func refresh() {
	var devices []serviceDevice
	if err := call("devices.list", nil, &devices); err != nil {
		setServiceUp(false)
		return
	}
	next := snapshot{serviceUp: true}
	for _, d := range devices {
		if d.IsDongle {
			next.hasDongle, next.dongle = true, d.Name
			break
		}
	}
	if next.hasDongle {
		// These can fail if the dongle was just unplugged; show what we have.
		_ = call("bt.list", nil, &next.paired)
		_ = call("bt.search.status", nil, &next.search)
	}

	model.mu.Lock()
	model.snap = next
	model.mu.Unlock()
	notifyStateChanged()
}

// stateChangeCh receives a (non-blocking) notification whenever the model
// changes, so the tray can rebuild its menu.
var stateChangeCh = make(chan struct{}, 1)

func notifyStateChanged() {
	select {
	case stateChangeCh <- struct{}{}:
	default:
	}
}

// startBackground wires service events to model refreshes: on each event
// (debounced), plus a slow periodic refresh since events can be dropped.
func startBackground() {
	events := make(chan struct{}, 1)
	go watch(func(string) {
		select {
		case events <- struct{}{}:
		default:
		}
	}, setServiceUp)

	go func() {
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-events:
				time.Sleep(150 * time.Millisecond) // coalesce bursts
				drainPending(events)
			case <-tick.C:
			}
			refresh()
		}
	}()
}
