package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/devstack"
)

const mcpProtocol = "2025-06-18"

// Booted is a running composed app.
type Booted struct {
	BaseURL string
	// Stop terminates the app and the substrates its launcher provisioned, and waits
	// for them to be gone.
	Stop func() error
}

// Boot runs the app's own run contract, `go run ./cmd/devstack`, in dir and waits
// for it to print its ready line and for GET /readyz to answer 200 (devstack.Launch,
// the launcher the scenario host wraps). The launcher provisions its own substrates
// and removes the Postgres container with its data volume on SIGTERM.
func Boot(ctx context.Context, dir string) (Booted, error) {
	app, err := devstack.Launch(ctx, dir, nil, nil)
	if err != nil {
		return Booted{}, err
	}
	return Booted{BaseURL: app.BaseURL, Stop: app.Stop}, nil
}

// DockerInventory lists the containers and volumes docker holds now, as
// "container:<name>" and "volume:<name>".
func DockerInventory(ctx context.Context) ([]string, error) { return devstack.DockerInventory(ctx) }

// DockerLeftovers is the names in after that were not in before.
func DockerLeftovers(ctx context.Context, before, after []string) []string {
	return devstack.DockerLeftovers(ctx, before, after)
}

func get(ctx context.Context, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, body, err
}

// Probe holds the booted app to what a client sees: the SPA at / and a working
// MCP door at /mcp that lists at least one tool.
func Probe(ctx context.Context, b Booted) error {
	status, body, err := get(ctx, b.BaseURL+"/")
	if err != nil {
		return fmt.Errorf("GET /: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("GET / = %d, want 200", status)
	}
	if !strings.Contains(strings.ToLower(string(body)), "<html") {
		return fmt.Errorf("GET / body has no <html: %.200q", body)
	}
	return probeMCP(ctx, b.BaseURL+"/mcp")
}

// probeMCP drives streamable-HTTP MCP: initialize, the initialized notification,
// then tools/list.
func probeMCP(ctx context.Context, url string) error {
	init, session, err := rpc(ctx, url, "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": mcpProtocol, "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "conformance-probe", "version": "0"},
		},
	})
	if err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}
	if init.Error != nil {
		return fmt.Errorf("mcp initialize: %s", init.Error.Message)
	}
	if _, _, err := rpc(ctx, url, session, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return fmt.Errorf("mcp initialized notification: %w", err)
	}
	list, _, err := rpc(ctx, url, session, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	if err != nil {
		return fmt.Errorf("mcp tools/list: %w", err)
	}
	if list.Error != nil {
		return fmt.Errorf("mcp tools/list: %s", list.Error.Message)
	}
	var res struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(list.Result, &res); err != nil {
		return fmt.Errorf("mcp tools/list result: %w", err)
	}
	if len(res.Tools) == 0 {
		return errors.New("mcp tools/list returned no tools")
	}
	return nil
}

type rpcReply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// rpc posts one JSON-RPC message and returns the reply (zero for a notification) and
// the session id the server assigned.
func rpc(ctx context.Context, url, session string, msg map[string]any) (rpcReply, string, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return rpcReply{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return rpcReply{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
		req.Header.Set("MCP-Protocol-Version", mcpProtocol)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return rpcReply{}, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return rpcReply{}, "", err
	}
	if resp.StatusCode/100 != 2 {
		return rpcReply{}, "", fmt.Errorf("status %d: %.300q", resp.StatusCode, body)
	}
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		sid = session
	}
	if _, isCall := msg["id"]; !isCall {
		return rpcReply{}, sid, nil
	}
	reply, err := decodeReply(resp.Header.Get("Content-Type"), body)
	return reply, sid, err
}

// decodeReply reads a JSON body or the first data event of an event stream.
func decodeReply(contentType string, body []byte) (rpcReply, error) {
	var reply rpcReply
	if strings.HasPrefix(contentType, "text/event-stream") {
		for line := range strings.SplitSeq(string(body), "\n") {
			if data, ok := strings.CutPrefix(line, "data:"); ok {
				body = []byte(strings.TrimSpace(data))
				break
			}
		}
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return reply, fmt.Errorf("decode reply %.300q: %w", body, err)
	}
	return reply, nil
}
