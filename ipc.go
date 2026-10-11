package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Minimal JSON-RPC 2.0 client for the Jabridge service socket.
// See https://github.com/Watchdog0x/jabridge (docs/IPC.md).

const (
	callTimeout   = 30 * time.Second // pairing/connecting can take a while
	keepalive     = 15 * time.Second
	reconnectWait = 2 * time.Second
)

var rpcID atomic.Int64

func socketPath() (string, error) {
	if p := os.Getenv("JABRIDGE_SOCKET"); p != "" {
		return p, nil
	}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return "", errors.New("XDG_RUNTIME_DIR is not set; run from your desktop session")
	}
	return filepath.Join(runtime, "jabridge.sock"), nil
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcMessage struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func dial() (net.Conn, error) {
	path, err := socketPath()
	if err != nil {
		return nil, err
	}
	return net.DialTimeout("unix", path, 3*time.Second)
}

func send(conn net.Conn, method string, params any) (int64, error) {
	id := rpcID.Add(1)
	b, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return 0, err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.Write(append(b, '\n'))
	return id, err
}

// call makes one request on its own short-lived connection and decodes the
// result into out (which may be nil).
func call(method string, params any, out any) error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	id, err := send(conn, method, params)
	if err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(callTimeout))
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
		var msg rpcMessage
		if json.Unmarshal(line, &msg) != nil || msg.ID == nil || *msg.ID != id {
			continue // a notification or someone else's reply
		}
		if msg.Error != nil {
			return errors.New(msg.Error.Message)
		}
		if out != nil && len(msg.Result) > 0 {
			return json.Unmarshal(msg.Result, out)
		}
		return nil
	}
}

// watch keeps a subscription open and calls onEvent for every notification
// (and once after every (re)connect, so callers reload their state). It
// reconnects forever; connected reports whether the service is reachable.
func watch(onEvent func(method string), connected func(bool)) {
	for {
		watchOnce(onEvent, connected)
		connected(false)
		time.Sleep(reconnectWait)
	}
}

func watchOnce(onEvent func(string), connected func(bool)) {
	conn, err := dial()
	if err != nil {
		return
	}
	defer conn.Close()

	subID, err := send(conn, "subscribe", nil)
	if err != nil {
		return
	}

	lines := make(chan []byte)
	done := make(chan struct{})
	quit := make(chan struct{})
	defer close(quit) // releases the reader if it's blocked sending
	go func() {
		defer close(done)
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			select {
			case lines <- line:
			case <-quit:
				return
			}
		}
	}()

	ping := time.NewTicker(keepalive)
	defer ping.Stop()
	for {
		select {
		case line := <-lines:
			var msg rpcMessage
			if json.Unmarshal(line, &msg) != nil {
				continue
			}
			switch {
			case msg.ID != nil && *msg.ID == subID:
				if msg.Error != nil {
					return
				}
				connected(true)
				onEvent("subscribed")
			case msg.ID == nil && msg.Method != "":
				onEvent(msg.Method)
			}
		case <-ping.C:
			if _, err := send(conn, "service.ping", nil); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
