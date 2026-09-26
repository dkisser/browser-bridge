package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxReadResultChars is MAX_READ_RESULT_CHARS in packages/shared/src/constants.ts.
const maxReadResultChars = 100_000

// selectorMatchedButEmptyMessage is selectorMatchedButEmptyMessage in
// packages/shared/src/messages.ts.
func selectorMatchedButEmptyMessage(selector string) string {
	return fmt.Sprintf("The element matched by \"%s\" has no text content — it is "+
		"empty, hidden, or not yet rendered. If you expected list or feed "+
		"items here, the page may virtualize its list; a full-page snapshot "+
		"shows what is actually in the DOM right now.", selector)
}

// defaultMaxCharsForFilter is defaultMaxCharsForFilter in
// packages/shared/src/snapshot.ts (8000 interactive, 3000 full).
func defaultMaxCharsForFilter(filter string) int {
	if filter == "full" {
		return 3000
	}
	return 8000
}

// utf16Length counts UTF-16 code units, matching the JavaScript
// string.length the TS read guards compare against MAX_READ_RESULT_CHARS.
func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// jsTrimSpace mirrors String.prototype.trim (WhiteSpace ∪ LineTerminator):
// like strings.TrimSpace plus U+FEFF, minus U+0085.
func jsTrimSpace(s string) string {
	return strings.Trim(s, "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff")
}

// indentJSON renders data like JSON.stringify(data, null, 2): two-space
// indent, key order and string bytes preserved (json.Indent copies both
// verbatim). A missing or null value becomes emptyDefault, matching
// `result.data ?? <default>` in the TS tools.
func indentJSON(data json.RawMessage, emptyDefault string) (string, error) {
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		data = json.RawMessage(emptyDefault)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		return "", err
	}
	return out.String(), nil
}

// executePageinfo is executePageinfo in src/mcp/tools/pageinfo.ts.
func (s *MCPServer) executePageinfo(ctx context.Context, req *mcp.CallToolRequest, args tabIDTimeoutArgs) (*mcp.CallToolResult, any, error) {
	result, fail := s.dispatch(ctx, req, "pageinfo", "pageinfo",
		map[string]any{"tabId": args.TabID}, args.TimeoutMS, "pageinfo failed")
	if fail != nil {
		return fail, nil, nil
	}
	text, err := indentJSON(result.Data, "{}")
	if err != nil {
		return toolError("pageinfo", fmt.Sprintf("pageinfo failed: %v", err)), nil, nil
	}
	return toolText(text), nil, nil
}

// executeTabList is executeTabList in src/mcp/tools/tab-list.ts.
func (s *MCPServer) executeTabList(ctx context.Context, req *mcp.CallToolRequest, args timeoutOnlyArgs) (*mcp.CallToolResult, any, error) {
	result, fail := s.dispatch(ctx, req, "tab_list", "tab:list",
		map[string]any{}, args.TimeoutMS, "tab:list failed")
	if fail != nil {
		return fail, nil, nil
	}
	text, err := indentJSON(result.Data, "[]")
	if err != nil {
		return toolError("tab_list", fmt.Sprintf("tab:list failed: %v", err)), nil, nil
	}
	return toolText(text), nil, nil
}

// executeGettext is executeGettext in src/mcp/tools/get-text.ts.
func (s *MCPServer) executeGettext(ctx context.Context, req *mcp.CallToolRequest, args selectorTabArgs) (*mcp.CallToolResult, any, error) {
	result, fail := s.dispatch(ctx, req, "get_text", "gettext",
		map[string]any{"selector": args.Selector, "tabId": args.TabID}, args.TimeoutMS, "gettext failed")
	if fail != nil {
		return fail, nil, nil
	}
	var data struct {
		Text string `json:"text"`
	}
	// `data.text ?? ''`: a missing field reads as empty.
	_ = json.Unmarshal(result.Data, &data)
	if jsTrimSpace(data.Text) == "" {
		return toolText(selectorMatchedButEmptyMessage(args.Selector)), nil, nil
	}
	if n := utf16Length(data.Text); n > maxReadResultChars {
		return toolError("get_text", fmt.Sprintf("get_text matched %d chars — almost certainly "+
			"whole-page text (nav, ads, sidebars included), not the "+
			"content you want. Take a snapshot to find the content "+
			"container, then narrow with a selector.", n)), nil, nil
	}
	return toolText(data.Text), nil, nil
}

// executeGethtml is executeGethtml in src/mcp/tools/get-html.ts. Where the TS
// crashes on a missing data field, a missing html reads as "" here.
func (s *MCPServer) executeGethtml(ctx context.Context, req *mcp.CallToolRequest, args selectorTabArgs) (*mcp.CallToolResult, any, error) {
	result, fail := s.dispatch(ctx, req, "get_html", "gethtml",
		map[string]any{"selector": args.Selector, "tabId": args.TabID}, args.TimeoutMS, "gethtml failed")
	if fail != nil {
		return fail, nil, nil
	}
	var data struct {
		HTML string `json:"html"`
	}
	_ = json.Unmarshal(result.Data, &data)
	if n := utf16Length(data.HTML); n > maxReadResultChars {
		return toolError("get_html", fmt.Sprintf("get_html matched %d chars — almost certainly "+
			"whole-page chrome (nav, ads, sidebars), not the content you "+
			"want. Take a snapshot to find the content container, then "+
			"narrow with a selector.", n)), nil, nil
	}
	return toolText(data.HTML), nil, nil
}

type snapshotArgs struct {
	Selector  *string `json:"selector,omitempty"`
	Filter    *string `json:"filter,omitempty"`
	MaxChars  *int    `json:"max_chars,omitempty"`
	TabID     int     `json:"tab_id"`
	TimeoutMS *int    `json:"timeout_ms,omitempty"`
}

// executeSnapshot is executeSnapshot in src/mcp/tools/snapshot.ts.
func (s *MCPServer) executeSnapshot(ctx context.Context, req *mcp.CallToolRequest, args snapshotArgs) (*mcp.CallToolResult, any, error) {
	filter := "interactive"
	if args.Filter != nil {
		filter = *args.Filter
	}
	maxChars := defaultMaxCharsForFilter(filter)
	if args.MaxChars != nil {
		maxChars = *args.MaxChars
	}
	params := map[string]any{
		"filter":    filter,
		"max_chars": maxChars,
		"tabId":     args.TabID,
	}
	if args.Selector != nil {
		params["selector"] = *args.Selector
	}
	result, fail := s.dispatch(ctx, req, "snapshot", "snapshot", params, args.TimeoutMS, "snapshot failed")
	if fail != nil {
		return fail, nil, nil
	}
	var data struct {
		Snapshot     string `json:"snapshot"`
		Truncated    bool   `json:"truncated"`
		NodesTotal   int    `json:"nodes_total"`
		NodesEmitted int    `json:"nodes_emitted"`
		Tier         int    `json:"tier"`
	}
	_ = json.Unmarshal(result.Data, &data)
	truncated := ""
	if data.Truncated {
		truncated = " | truncated"
	}
	stats := fmt.Sprintf("[%d/%d nodes | tier=%d%s]", data.NodesEmitted, data.NodesTotal, data.Tier, truncated)
	return toolText(data.Snapshot + "\n\n" + stats), nil, nil
}

type screenshotArgs struct {
	FullPage  *bool `json:"fullPage,omitempty"`
	TabID     int   `json:"tab_id"`
	TimeoutMS *int  `json:"timeout_ms,omitempty"`
}

// dataURLPrefix captures the MIME subtype from the data URL head in
// executeScreenshot (src/mcp/tools/screenshot.ts). The TS regex strips the
// prefix; we capture group 1 to set ImageContent.MIMEType correctly — the
// underlying bytes can be JPEG / WebP / PNG and reporting "image/png" for
// any of them breaks downstream clients that validate the MIME or save the
// bytes under the declared extension.
var dataURLPrefix = regexp.MustCompile(`^data:image/([a-zA-Z]+);base64,`)

// defaultScreenshotMIMEType is the fallback when the extension returns a
// data URL with an unrecognised subtype. PNG is the most common format the
// extension captures; if the head is missing entirely the bytes are
// rejected upstream with "Screenshot failed: browser returned no image
// data".
const defaultScreenshotMIMEType = "image/png"

// executeScreenshot is executeScreenshot in src/mcp/tools/screenshot.ts,
// returning MCP image content. The TS passes the base64 payload through
// verbatim; the go-sdk's ImageContent carries decoded bytes, so the payload
// is decoded and re-encoded (canonically) on the way out.
func (s *MCPServer) executeScreenshot(ctx context.Context, req *mcp.CallToolRequest, args screenshotArgs) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"tabId": args.TabID}
	if args.FullPage != nil {
		params["fullPage"] = *args.FullPage
	}
	result, fail := s.dispatch(ctx, req, "screenshot", "screenshot", params, args.TimeoutMS, "Screenshot failed")
	if fail != nil {
		return fail, nil, nil
	}
	var data struct {
		DataURL string `json:"dataUrl"`
	}
	_ = json.Unmarshal(result.Data, &data)
	match := dataURLPrefix.FindStringSubmatch(data.DataURL)
	if match == nil {
		return toolError("screenshot", "Screenshot failed: browser returned no image data"), nil, nil
	}
	mimeType := "image/" + strings.ToLower(match[1])
	if mimeType == "" || mimeType == "image/" {
		mimeType = defaultScreenshotMIMEType
	}
	encoded := strings.TrimPrefix(data.DataURL, match[0])
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return toolError("screenshot", fmt.Sprintf("Screenshot failed: invalid base64 image data: %v", err)), nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.ImageContent{Data: raw, MIMEType: mimeType}},
	}, nil, nil
}
