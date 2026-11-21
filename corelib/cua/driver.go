// Package cua is the MaClaw bridge to the Cua Driver (trycua/cua):
// the desktop-automation daemon that serves window, input, accessibility
// and verification operations over MCP.
//
// # Topology
//
// A host process runs `cua-driver serve --socket <path>` (on Windows a
// named pipe, on Linux a unix socket). The daemon creates one pipe
// instance per connecting client, so every MaClaw process opens its own
// connection. MaClaw talks to the daemon over JSON-RPC 2.0:
//
//   - tools/list            -> tool surface of this driver build
//   - tools/call {name}     -> one driver tool (health_report, click, ...)
//
// Tool names are bare; a name the daemon does not know is retried once
// with its "computer." prefix. The daemon answers unknown tool names with
// a JSON-RPC error, and unknown method names with an out-of-envelope
// {"ok":false,"error":"..."} object.
//
// # No silent behavior
//
// A missing or unreachable socket returns ErrNoDriver; hosts keep their
// legacy execution paths as the fallback (see docs/cua-driver-computer-use.md).
package cua

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

// EnvSocket is the environment variable a host uses to hand MaClaw the
// driver's socket. Values: \\.\pipe\name, unix:///path, host:port.
const EnvSocket = "MACLAW_CUA_SOCKET"

// ErrNoDriver means no reachable driver socket is known to this process.
var ErrNoDriver = errors.New("cua: no driver socket (set MACLAW_CUA_SOCKET)")

// Driver is one live MCP connection to the driver's daemon.
type Driver struct {
	addr string
	rt   *runtime
}

// Open connects to the driver at addr (socket syntax per EnvSocket) and
// verifies it answers a tools/list.
func Open(ctx context.Context, addr string) (*Driver, error) {
	rt, err := newRuntime(addr)
	if err != nil {
		return nil, err
	}
	d := &Driver{addr: addr, rt: rt}
	probeCtx, cancel := context.WithDeadline(ctx, time.Now().Add(30*time.Second))
	defer cancel()
	if err := d.rt.listTools(probeCtx); err != nil {
		rt.close()
		return nil, err
	}
	return d, nil
}

// OpenDefault opens the socket named by the MACLAW_CUA_SOCKET environment
// variable, returning ErrNoDriver when it is unset.
func OpenDefault(ctx context.Context) (*Driver, error) {
	addr := strings.TrimSpace(os.Getenv(EnvSocket))
	if addr == "" {
		return nil, ErrNoDriver
	}
	return Open(ctx, addr)
}

// Health asks the driver for its end-to-end diagnostics. The result carries
// overall:"ok"|"fail" plus per-check entries.
func (d *Driver) Health(ctx context.Context) (map[string]any, error) {
	return d.Call(ctx, "health_report", map[string]any{})
}

// Call runs one driver tool by its bare name (click, verify_state, ...) and
// returns its structured result, or its text answer parsed as JSON. A tool
// with no structure and no JSON text returns {"text": <answer>}.
func (d *Driver) Call(ctx context.Context, tool string, args map[string]any) (map[string]any, error) {
	structured, text, err := d.CallDetail(ctx, tool, args)
	if err != nil {
		return nil, err
	}
	if len(structured) > 0 {
		return structured, nil
	}
	if strings.TrimSpace(text) == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if json.Unmarshal([]byte(text), &out) == nil {
		return out, nil
	}
	return map[string]any{"text": text}, nil
}

// Close releases the connection. Subsequent calls fail with ErrNoDriver.
func (d *Driver) Close() {
	d.rt.close()
}

// Addr reports the dial target, for diagnostics.
func (d *Driver) Addr() string { return d.addr }
