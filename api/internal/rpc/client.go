// Package rpc is the client side of the engine socket protocol
// (docs/architecture.md §6.1): one newline-terminated JSON request per
// connection, one newline-terminated JSON response back.
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"
)

// Error codes sent by the engine. Unavailable is also used by this client
// when the engine cannot be reached or does not answer in time.
const (
	CodeInvalid     = "invalid"
	CodeNotFound    = "not_found"
	CodeConflict    = "conflict"
	CodeUnavailable = "unavailable"
	// The note index is being rebuilt; node methods can be retried later.
	CodeIndexRebuilding = "index_rebuilding"
	CodePromptBlocked   = "prompt_blocked"
	CodeInternal        = "internal"
)

// Error is an error response from the engine (or an unreachable engine).
type Error struct {
	Code    string
	Message string
	Data    map[string]any
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Client calls methods on the engine socket.
type Client struct {
	SocketPath string
	Timeout    time.Duration // per call; the whole round trip must finish in time

	seq atomic.Uint64
}

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type response struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	} `json:"error"`
}

// Call invokes method with params (any JSON-encodable value; nil means {})
// and decodes the result into out (if non-nil). Engine errors are returned
// as *Error; transport failures and timeouts as *Error with CodeUnavailable.
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	if params == nil {
		params = struct{}{}
	}
	line, err := json.Marshal(request{
		ID:     "api-" + strconv.FormatUint(c.seq.Add(1), 10),
		Method: method,
		Params: params,
	})
	if err != nil {
		return fmt.Errorf("rpc: encode %s: %w", method, err)
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		return unavailable("engine socket not reachable: " + err.Error())
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if _, err := conn.Write(append(line, '\n')); err != nil {
		return unavailable("writing request: " + err.Error())
	}
	raw, err := bufio.NewReaderSize(conn, 64*1024).ReadBytes('\n')
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return unavailable(fmt.Sprintf("engine did not answer %s within %s", method, timeout))
		}
		return unavailable("reading response: " + err.Error())
	}

	var resp response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return &Error{Code: CodeInternal, Message: "malformed engine response: " + err.Error()}
	}
	if !resp.OK {
		if resp.Error == nil {
			return &Error{Code: CodeInternal, Message: "engine returned an error without details"}
		}
		return &Error{Code: resp.Error.Code, Message: resp.Error.Message, Data: resp.Error.Data}
	}
	if out != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return &Error{Code: CodeInternal, Message: fmt.Sprintf("unexpected %s result: %v", method, err)}
		}
	}
	return nil
}

func unavailable(msg string) error { return &Error{Code: CodeUnavailable, Message: msg} }
