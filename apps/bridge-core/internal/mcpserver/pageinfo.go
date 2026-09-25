package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/protocol"
)

// tabIDGuidance is TAB_ID_GUIDANCE in src/mcp/tool-descriptions.ts.
const tabIDGuidance = "Requires a valid tab_id — call tab_list first to discover open tabs."

// pageinfoArgs mirrors PageinfoInputSchema in src/mcp/tools/pageinfo.ts.
type pageinfoArgs struct {
	TabID     int  `json:"tab_id"`
	TimeoutMS *int `json:"timeout_ms,omitempty"`
}

// pageinfoInputSchema matches the JSON schema xsschema derives from the zod
// PageinfoInputSchema (draft-07), verified against the TS build. The struct
// above carries the values; this schema is what tools/list serves and what
// the SDK validates against.
var pageinfoInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"tab_id":     {Type: "integer", Minimum: float64Ptr(0)},
		"timeout_ms": {Type: "integer", Minimum: float64Ptr(100), Maximum: float64Ptr(120000)},
	},
	Required:             []string{"tab_id"},
	AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	Schema:               "http://json-schema.org/draft-07/schema#",
	PropertyOrder:        []string{"tab_id", "timeout_ms"},
}

func float64Ptr(f float64) *float64 { return &f }

func (s *Server) registerTools(mcpServer *mcp.Server) {
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "pageinfo",
		Description: "Get title, URL, and active status of a specific tab. " + tabIDGuidance,
		InputSchema: pageinfoInputSchema,
	}, s.executePageinfo)
}

// executePageinfo is executePageinfo in src/mcp/tools/pageinfo.ts, with the
// WS-client sendCommand replaced by an in-process router dispatch.
func (s *Server) executePageinfo(ctx context.Context, _ *mcp.CallToolRequest, args pageinfoArgs) (*mcp.CallToolResult, any, error) {
	timeout := s.defaultTimeout
	if args.TimeoutMS != nil {
		timeout = time.Duration(*args.TimeoutMS) * time.Millisecond
	}

	browserID, resolveErr := s.resolveBrowser()
	if resolveErr != "" {
		return toolError("pageinfo", resolveErr), nil, nil
	}

	result, err := s.sendCommand(ctx, browserID, "pageinfo", args.TabID, timeout)
	if err != nil {
		return toolError("pageinfo", err.Error()), nil, nil
	}
	result = withRecoveryHint(result)
	if result.Status != "ok" {
		return toolError("pageinfo", commandErrorMessage(result, "pageinfo failed")), nil, nil
	}

	data := result.Data
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		data = json.RawMessage("{}")
	}
	// JSON.stringify(data, null, 2): re-indent the raw payload, preserving
	// key order and string bytes (json.Indent copies both verbatim).
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		return toolError("pageinfo", fmt.Sprintf("pageinfo failed: %v", err)), nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: out.String()}},
	}, nil, nil
}

// toolError maps a thrown Error to the wire shape FastMCP produces for
// non-UserError failures: text content prefixed with "Tool '<name>'
// execution failed: " and isError set.
func toolError(name, message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Tool '%s' execution failed: %s", name, message)}},
		IsError: true,
	}
}

// resolveBrowser is resolveTargetBrowser + resolveBrowser (implicit path) in
// src/mcp/browser-lookup.ts and browser-resolver.ts. The spike ships no
// set_browser tool, so there is no per-session explicit browserId.
func (s *Server) resolveBrowser() (browserID string, failureMessage string) {
	var online []protocol.BrowserConnection
	for _, b := range s.registry.ListBrowsers() {
		if b.Status == protocol.StatusOnline {
			online = append(online, b)
		}
	}
	switch len(online) {
	case 0:
		return "", "No browser connected. Start the extension/local-proxy first."
	case 1:
		return online[0].BrowserID, ""
	default:
		list := ""
		for i, b := range online {
			if i > 0 {
				list += "\n"
			}
			list += fmt.Sprintf("- %s (%s)", b.BrowserID, b.Status)
		}
		return "", fmt.Sprintf("Multiple browsers are online. Call set_browser with one of:\n%s", list)
	}
}

// channelSender is the protocol.TextSender half of the in-process
// sendCommand: it captures the single response envelope the router routes
// back for the command's id.
type channelSender struct {
	ch chan string
}

func (s channelSender) Send(text string) {
	// Exactly one response is routed per command id; the buffer absorbs it
	// even if the caller has already timed out.
	select {
	case s.ch <- text:
	default:
	}
}

// sendCommand is sendCommand in src/mcp/command-client.ts with the WS client
// hop replaced by an in-process router.HandleInboundCommand call. The router
// path preserves the browser_offline / buffer / sw_timeout behavior.
func (s *Server) sendCommand(ctx context.Context, browserID, command string, tabID int, timeout time.Duration) (protocol.ResponsePayload, error) {
	payload, err := json.Marshal(struct {
		Command string         `json:"command"`
		TabID   int            `json:"tabId"`
		Params  map[string]any `json:"params"`
	}{
		Command: command,
		TabID:   tabID,
		Params:  map[string]any{"tabId": tabID},
	})
	if err != nil {
		return protocol.ResponsePayload{}, fmt.Errorf("encode command payload: %w", err)
	}

	ch := make(chan string, 1)
	s.router.HandleInboundCommand(protocol.Envelope{
		ID:        protocol.NewID(),
		Type:      protocol.TypeCommand,
		BrowserID: browserID,
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}, channelSender{ch: ch})

	select {
	case text := <-ch:
		envelope, err := protocol.Decode(text)
		if err != nil {
			return protocol.ResponsePayload{}, fmt.Errorf("decode response envelope: %w", err)
		}
		if len(envelope.Payload) == 0 || bytes.Equal(envelope.Payload, []byte("null")) {
			// envelope.payload ?? { status: 'error', error: 'Empty response' }
			return protocol.ResponsePayload{Status: "error", Error: "Empty response"}, nil
		}
		var result protocol.ResponsePayload
		if err := json.Unmarshal(envelope.Payload, &result); err != nil {
			return protocol.ResponsePayload{}, fmt.Errorf("decode response payload: %w", err)
		}
		return result, nil
	case <-ctx.Done():
		return protocol.ResponsePayload{}, fmt.Errorf("command %s: %w", command, ctx.Err())
	case <-time.After(timeout):
		return protocol.ResponsePayload{}, fmt.Errorf("timeout: no response for command %s within %dms", command, timeout.Milliseconds())
	}
}
