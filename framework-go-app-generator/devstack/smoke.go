package devstack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// MCPProtocol is the streamable-HTTP MCP protocol revision the smoke and the test clients speak.
const MCPProtocol = "2025-06-18"

// SmokeSpec is what a booted app must answer.
type SmokeSpec struct {
	// SPA: GET / must be 200 and contain "<html" (the model has a built browser surface whose web
	// project is seated: see SmokeSpecFor).
	SPA bool
	// Tools is every name tools/list must contain. Empty while the app mounts /mcp is an error:
	// the caller derives the list with ToolNames, so a tool table cannot go unchecked.
	Tools []string
}

// WebDir is the web project directory every browser surface shares, module-root-relative.
const WebDir = "webApp"

// WebSeated reports whether the module at moduleDir has seated its web project (webgen bootstrap
// writes webApp/package.json). A browser surface with no seated project is not yet built, not
// broken: the launcher serves the API and MCP only until the web client's integration seats it.
func WebSeated(moduleDir string) bool {
	info, err := os.Stat(filepath.Join(moduleDir, WebDir, "package.json"))
	return err == nil && info.Mode().IsRegular()
}

// SmokeSpecFor is what the app at moduleDir must answer, derived from m: the SPA iff m has a built
// browser surface and its web project is seated (a seated project that does not build is a smoke
// failure; an unseated one is not yet built), and every tool its MCP clients front (ToolNames,
// narrowed by exposed).
func SmokeSpecFor(m *projectmodel.Model, moduleDir string, exposed map[string][]string) SmokeSpec {
	return SmokeSpec{
		SPA:   m != nil && m.System != nil && m.System.HasBrowserSurface() && WebSeated(moduleDir),
		Tools: ToolNames(m, exposed),
	}
}

// ToolNames is the MCP tool names a composed server for m registers: for every built MCP machine
// client and every built Manager it fronts (a relationship from the client, or any Manager in
// exposed when exposed is non-nil), the Manager's operations narrowed by exposed (a nil entry is
// the whole interface), each named lowerFirst(<manager base>) + <operation> — the expression
// mcpgen registers them by. Sorted and without duplicates.
func ToolNames(m *projectmodel.Model, exposed map[string][]string) []string {
	if m == nil || m.System == nil {
		return nil
	}
	set := map[string]bool{}
	for _, c := range m.System.Components {
		if isMCPClient(c) {
			for _, name := range clientTools(m, c, exposed) {
				set[name] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// clientTools is the tool names of every built Manager the MCP client c fronts.
func clientTools(m *projectmodel.Model, c projectmodel.SystemComponent, exposed map[string][]string) []string {
	var out []string
	for key, contract := range m.Contracts {
		if isBuiltManager(contract) && fronts(m.System, c, key, exposed) {
			out = append(out, managerTools(contract.Doc, exposed[key])...)
		}
	}
	return out
}

// isMCPClient is a built machine client that speaks MCP: the ones clientgen writes tool tables
// for.
func isMCPClient(c projectmodel.SystemComponent) bool {
	return strings.EqualFold(c.Kind, "client") && c.IsBuilt() && !c.IsBrowserSurface() && c.IsMCPClient()
}

// isBuiltManager is a Manager contract the composition root constructs.
func isBuiltManager(c *projectmodel.Contract) bool {
	return strings.EqualFold(c.Layer, "manager") && c.Doc != nil && c.GoPackage != ""
}

// managerTools is the tool names of one Manager's operations, narrowed to served when non-nil.
func managerTools(doc *projectmodel.Doc, served []string) []string {
	prefix := projectmodel.LowerFirst(doc.ManagerBase())
	var out []string
	for _, op := range doc.Interface.Operations {
		if served == nil || slices.Contains(served, op.Name) {
			out = append(out, prefix+op.Name)
		}
	}
	return out
}

// fronts mirrors composegen and clientgen: with exposed set every exposed Manager is fronted by
// every MCP client, else the client's relationships decide.
func fronts(s *projectmodel.System, c projectmodel.SystemComponent, key string, exposed map[string][]string) bool {
	if exposed != nil {
		_, ok := exposed[key]
		return ok
	}
	comp, ok := s.ComponentByContractKey(key)
	if !ok {
		return false
	}
	return slices.ContainsFunc(s.Relationships, func(rel projectmodel.Relationship) bool {
		return rel.From == c.ID && rel.To == comp.ID
	})
}

// Smoke holds a booted app to what a client sees: GET /readyz answers 200, the SPA index is
// served at / when spec.SPA, and the MCP door at /mcp initializes and lists at least every tool in
// spec.Tools. The error names everything that is missing.
func Smoke(ctx context.Context, app *App, spec SmokeSpec) error {
	status, _, err := get(ctx, app.BaseURL+"/readyz")
	if err != nil {
		return fmt.Errorf("smoke: GET /readyz: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("smoke: GET /readyz = %d, want 200", status)
	}
	if spec.SPA {
		if err := checkSPA(ctx, app.BaseURL+"/"); err != nil {
			return fmt.Errorf("smoke: %w", err)
		}
	}
	mcpURL := app.MCPURL
	if mcpURL == "" {
		mcpURL = app.BaseURL + "/mcp"
	}
	if err := checkMCP(ctx, mcpURL, spec.Tools); err != nil {
		return fmt.Errorf("smoke: %w", err)
	}
	return nil
}

func checkSPA(ctx context.Context, url string) error {
	status, body, err := get(ctx, url)
	if err != nil {
		return fmt.Errorf("GET /: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("GET / = %d, want 200", status)
	}
	if !strings.Contains(strings.ToLower(string(body)), "<html") {
		return fmt.Errorf("GET / body has no <html: %.200q", body)
	}
	return nil
}

// checkMCP drives streamable-HTTP MCP: initialize, the initialized notification, then
// tools/list, which must contain every wanted tool. With none wanted the app must not mount /mcp.
func checkMCP(ctx context.Context, url string, want []string) error {
	session, err := initializeMCP(ctx, url)
	if len(want) == 0 {
		if err == nil {
			return errors.New("/mcp is mounted but no tool is expected; derive the list with ToolNames")
		}
		return nil
	}
	if err != nil {
		return err
	}
	have, err := listTools(ctx, url, session)
	if err != nil {
		return err
	}
	if len(have) == 0 {
		return errors.New("mcp tools/list returned no tools")
	}
	var missing []string
	for _, name := range want {
		if !slices.Contains(have, name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("mcp tools/list lacks %d of the %d tools the model generates: %s", len(missing), len(want), strings.Join(missing, ", "))
	}
	return nil
}

// initializeMCP opens a session: initialize, then the initialized notification.
func initializeMCP(ctx context.Context, url string) (string, error) {
	init, session, err := RPC(ctx, url, "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": MCPProtocol, "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "devstack-smoke", "version": "0"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("mcp initialize: %w", err)
	}
	if init.Error != nil {
		return "", fmt.Errorf("mcp initialize: %s", init.Error.Message)
	}
	if _, _, err := RPC(ctx, url, session, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return "", fmt.Errorf("mcp initialized notification: %w", err)
	}
	return session, nil
}

// listTools is the tool names tools/list returns in the session.
func listTools(ctx context.Context, url, session string) ([]string, error) {
	list, _, err := RPC(ctx, url, session, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	if err != nil {
		return nil, fmt.Errorf("mcp tools/list: %w", err)
	}
	if list.Error != nil {
		return nil, fmt.Errorf("mcp tools/list: %s", list.Error.Message)
	}
	var res struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(list.Result, &res); err != nil {
		return nil, fmt.Errorf("mcp tools/list result: %w", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names, nil
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

// Reply is one JSON-RPC reply: its result, or its error.
type Reply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// RPC posts one JSON-RPC message to a streamable-HTTP MCP endpoint and returns the reply (zero
// for a notification) and the session id the server assigned.
func RPC(ctx context.Context, url, session string, msg map[string]any) (Reply, string, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return Reply{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return Reply{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
		req.Header.Set("MCP-Protocol-Version", MCPProtocol)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Reply{}, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Reply{}, "", err
	}
	if resp.StatusCode/100 != 2 {
		return Reply{}, "", fmt.Errorf("status %d: %.300q", resp.StatusCode, body)
	}
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		sid = session
	}
	if _, isCall := msg["id"]; !isCall {
		return Reply{}, sid, nil
	}
	reply, err := decodeReply(resp.Header.Get("Content-Type"), body)
	return reply, sid, err
}

// decodeReply reads a JSON body or the first data event of an event stream.
func decodeReply(contentType string, body []byte) (Reply, error) {
	var reply Reply
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
