package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// fakeEngine serves one canned reply per connection. reply receives the
// decoded request and returns the raw response line (without newline);
// an empty string means "never answer".
func fakeEngine(t *testing.T, reply func(req map[string]any) string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rpc.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var req map[string]any
				_ = json.Unmarshal(line, &req)
				out := reply(req)
				if out == "" {
					time.Sleep(2 * time.Second)
					return
				}
				conn.Write([]byte(out + "\n"))
			}(conn)
		}
	}()
	return path
}

func TestCallDecodesResult(t *testing.T) {
	requests := make(chan map[string]any, 1)
	path := fakeEngine(t, func(req map[string]any) string {
		requests <- req
		return `{"id":"x","ok":true,"result":{"status":"ok"}}`
	})
	c := &Client{SocketPath: path, Timeout: time.Second}
	var out struct{ Status string }
	if err := c.Call(context.Background(), "ping", nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "ok" {
		t.Fatalf("status = %q", out.Status)
	}
	seen := <-requests
	if seen["method"] != "ping" {
		t.Fatalf("method = %v", seen["method"])
	}
	if _, ok := seen["params"].(map[string]any); !ok {
		t.Fatalf("params should default to an object, got %#v", seen["params"])
	}
}

func TestCallReturnsEngineError(t *testing.T) {
	path := fakeEngine(t, func(map[string]any) string {
		return `{"id":"x","ok":false,"error":{"code":"conflict","message":"nope","data":{"actual_state":"DONE"}}}`
	})
	c := &Client{SocketPath: path, Timeout: time.Second}
	err := c.Call(context.Background(), "task.transition", map[string]any{}, nil)
	var rpcErr *Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != CodeConflict || rpcErr.Data["actual_state"] != "DONE" {
		t.Fatalf("err = %#v", err)
	}
}

func TestCallTimeoutIsUnavailable(t *testing.T) {
	path := fakeEngine(t, func(map[string]any) string { return "" })
	c := &Client{SocketPath: path, Timeout: 200 * time.Millisecond}
	start := time.Now()
	err := c.Call(context.Background(), "tasks.today", nil, nil)
	var rpcErr *Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != CodeUnavailable {
		t.Fatalf("err = %#v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestCallMissingSocketIsUnavailable(t *testing.T) {
	c := &Client{SocketPath: filepath.Join(t.TempDir(), "missing.sock"), Timeout: time.Second}
	err := c.Call(context.Background(), "ping", nil, nil)
	var rpcErr *Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != CodeUnavailable {
		t.Fatalf("err = %#v", err)
	}
}
