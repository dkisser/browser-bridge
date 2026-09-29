package http

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"browser-bridge/internal/core"
)

// Description fragments from src/mcp/tool-descriptions.ts.
const (
	tabIDGuidance    = "Requires a valid tab_id — call tab_list first to discover open tabs."
	selectorGuidance = `The selector must exist on the page — if you have not seen the DOM yet, ` +
		`call snapshot first and use a rendered selector or its @eN ref. A ` +
		`semantic guess like "article" only matches real <article> tags.`
)

// toolText is the FastMCP shape for a tool returning a plain string:
// one text content part, no error flag.
func toolText(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
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

// sessionID mirrors the `sessionId ?? 'anonymous'` fallback in every TS
// tool registration.
func sessionID(req *mcp.CallToolRequest) string {
	if session := req.GetSession(); session != nil {
		if id := session.ID(); id != "" {
			return id
		}
	}
	return "anonymous"
}

// commandTimeout applies `args.timeout_ms ?? session.defaultTimeoutMs` from
// the TS tools (the session default is server-wide here).
func (s *MCPServer) commandTimeout(timeoutMS *int) time.Duration {
	if timeoutMS != nil {
		return time.Duration(*timeoutMS) * time.Millisecond
	}
	return s.defaultTimeout
}

// dispatch is the shared head of every command tool: resolve the target
// browser (honoring set_browser), send the command, apply recovery hints,
// and map failures to the FastMCP tool-error shape. On success it returns
// the response payload and a nil result.
func (s *MCPServer) dispatch(ctx context.Context, req *mcp.CallToolRequest, toolName string, spec commandSpec, timeoutMS *int, errFallback string) (core.ResponsePayload, *mcp.CallToolResult) {
	browserID, failure := s.resolveTargetBrowser(sessionID(req))
	if failure != "" {
		return core.ResponsePayload{}, toolError(toolName, failure)
	}
	result, err := s.sendCommand(ctx, browserID, spec, transportTimeout(s, spec, timeoutMS))
	if err != nil {
		return core.ResponsePayload{}, toolError(toolName, err.Error())
	}
	result = withRecoveryHint(result)
	if result.Status != "ok" {
		return core.ResponsePayload{}, toolError(toolName, commandErrorMessage(result, errFallback))
	}
	return result, nil
}

// runMessageTool is the shared shape of tools whose success output is
// `result.message ?? fallback`.
func (s *MCPServer) runMessageTool(ctx context.Context, req *mcp.CallToolRequest, toolName string, spec commandSpec, timeoutMS *int, errFallback, okFallback string) (*mcp.CallToolResult, any, error) {
	result, fail := s.dispatch(ctx, req, toolName, spec, timeoutMS, errFallback)
	if fail != nil {
		return fail, nil, nil
	}
	if result.Message != "" {
		return toolText(result.Message), nil, nil
	}
	return toolText(okFallback), nil, nil
}

// channelSender is the core.TextSender half of the in-process
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

// registerTools wires all tools in the same order as src/mcp/server.ts.
// (The go-sdk serves tools/list sorted by name; FastMCP served it in
// registration order. The MCP spec treats the list as a set.)
func (s *MCPServer) registerTools(mcpServer *mcp.Server) {
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "list_browsers",
		Description: "List all browsers connected to Browser Bridge. Call this first before any other tool; use set_browser when multiple browsers are online.",
		InputSchema: toolInputSchemas["list_browsers"],
	}, s.executeListBrowsers)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "set_browser",
		Description: "Explicitly choose which connected browser to control for this MCP session. Requires a browserId obtained from list_browsers.",
		InputSchema: toolInputSchemas["set_browser"],
	}, s.executeSetBrowser)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "navigate",
		Description: "Navigate a specific tab of the selected browser to a URL. " + tabIDGuidance,
		InputSchema: toolInputSchemas["navigate"],
	}, s.executeNavigate)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "go_back",
		Description: "Go back one page in the browser history. " + tabIDGuidance,
		InputSchema: toolInputSchemas["go_back"],
	}, s.executeGoBack)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "go_forward",
		Description: "Go forward one page in the browser history. " + tabIDGuidance,
		InputSchema: toolInputSchemas["go_forward"],
	}, s.executeGoForward)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "refresh",
		Description: "Refresh the current page. " + tabIDGuidance,
		InputSchema: toolInputSchemas["refresh"],
	}, s.executeRefresh)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "tab_list",
		Description: "List all tabs in the selected browser. Each entry's inAgentGroup field marks membership in the 'browser-bridge' tab group.",
		InputSchema: toolInputSchemas["tab_list"],
	}, s.executeTabList)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "tab_new",
		Description: "Open a new tab in the selected browser. Defaults to opening in the background (active=false). New tabs are automatically placed in the 'browser-bridge' tab group.",
		InputSchema: toolInputSchemas["tab_new"],
	}, s.executeTabNew)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "tab_close",
		Description: "Close a tab in the selected browser by tab ID. " + tabIDGuidance,
		InputSchema: toolInputSchemas["tab_close"],
	}, s.executeTabClose)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "tab_switch",
		Description: "Switch to a tab by its ID in the selected browser. " + tabIDGuidance,
		InputSchema: toolInputSchemas["tab_switch"],
	}, s.executeTabSwitch)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "click",
		Description: "Click an element in the selected browser by CSS selector. " + selectorGuidance + " " + tabIDGuidance,
		InputSchema: toolInputSchemas["click"],
	}, s.executeClick)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "type",
		Description: "Type text into an input element in the selected browser. " + selectorGuidance + " " + tabIDGuidance,
		InputSchema: toolInputSchemas["type"],
	}, s.executeType)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "select",
		Description: "Select an option from a dropdown in the selected browser by CSS selector. " + selectorGuidance + " " + tabIDGuidance,
		InputSchema: toolInputSchemas["select"],
	}, s.executeSelect)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "scroll",
		Description: "Scroll the page or an element in the selected browser. " + tabIDGuidance,
		InputSchema: toolInputSchemas["scroll"],
	}, s.executeScroll)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "hover",
		Description: "Hover over an element in the selected browser by CSS selector. " + selectorGuidance + " " + tabIDGuidance,
		InputSchema: toolInputSchemas["hover"],
	}, s.executeHover)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "get_text",
		Description: "Get the text content of an element by CSS selector. " + selectorGuidance + " " + tabIDGuidance,
		InputSchema: toolInputSchemas["get_text"],
	}, s.executeGettext)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "get_html",
		Description: "Get the raw innerHTML of an element by CSS selector. Escape hatch for untouched markup — prefer the snapshot tool for reading and understanding page content. " + selectorGuidance + " " + tabIDGuidance,
		InputSchema: toolInputSchemas["get_html"],
	}, s.executeGethtml)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "snapshot",
		Description: "Take a compact, budgeted snapshot of the page: interactive " +
			"elements (links, buttons, text boxes, checkboxes, combos) and " +
			"headings, with stable @eN refs you can pass to other tools as " +
			"selectors (e.g. \"@e12\"). Use this to see what you can ACT ON. " +
			"Default filter is \"interactive\"; pass filter=\"full\" for the " +
			"complete pseudo-tree including text runs, images and structural " +
			"containers. To READ page content (article body, email subjects, " +
			"feed items), use get_text instead — it returns plain text and is " +
			"the right tool for reading. When output is truncated, text runs " +
			"may be replaced by placeholders (full filter) or dropped " +
			"(interactive filter); narrow with selector or raise max_chars. " +
			tabIDGuidance,
		InputSchema: toolInputSchemas["snapshot"],
	}, s.executeSnapshot)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "screenshot",
		Description: "Take a screenshot of the selected browser. For getting html or text content, use the get_html or get_text tools first. " + tabIDGuidance,
		InputSchema: toolInputSchemas["screenshot"],
	}, s.executeScreenshot)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "pageinfo",
		Description: "Get title, URL, and active status of a specific tab. " + tabIDGuidance,
		InputSchema: toolInputSchemas["pageinfo"],
	}, s.executePageinfo)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "wait_element",
		Description: "Wait for an element to appear in the selected browser by CSS selector. " + selectorGuidance + " " + tabIDGuidance,
		InputSchema: toolInputSchemas["wait_element"],
	}, s.executeWaitElement)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "wait_navigation",
		Description: "Wait for navigation to complete in the selected browser. " + tabIDGuidance,
		InputSchema: toolInputSchemas["wait_navigation"],
	}, s.executeWaitNavigation)
}

// --- Argument shapes (json tags match the zod property names) ---

type timeoutOnlyArgs struct {
	TimeoutMS *int `json:"timeout_ms,omitempty"`
}

type setBrowserArgs struct {
	BrowserID string `json:"browserId"`
}

type tabIDTimeoutArgs struct {
	TabID     int  `json:"tab_id"`
	TimeoutMS *int `json:"timeout_ms,omitempty"`
}

type navigateArgs struct {
	URL       string `json:"url"`
	TabID     int    `json:"tab_id"`
	TimeoutMS *int   `json:"timeout_ms,omitempty"`
}

type tabNewArgs struct {
	URL       *string `json:"url,omitempty"`
	Active    *bool   `json:"active,omitempty"`
	AutoClose *bool   `json:"auto_close,omitempty"`
	TimeoutMS *int    `json:"timeout_ms,omitempty"`
}

type selectorTabArgs struct {
	Selector  string `json:"selector"`
	TabID     int    `json:"tab_id"`
	TimeoutMS *int   `json:"timeout_ms,omitempty"`
}

type typeArgs struct {
	Selector  string `json:"selector"`
	Text      string `json:"text"`
	Submit    *bool  `json:"submit,omitempty"`
	TabID     int    `json:"tab_id"`
	TimeoutMS *int   `json:"timeout_ms,omitempty"`
}

type selectArgs struct {
	Selector  string `json:"selector"`
	Value     string `json:"value"`
	TabID     int    `json:"tab_id"`
	TimeoutMS *int   `json:"timeout_ms,omitempty"`
}

type scrollArgs struct {
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Selector  string `json:"selector"`
	TabID     int    `json:"tab_id"`
	TimeoutMS *int   `json:"timeout_ms,omitempty"`
}

// --- Session tools ---

// executeListBrowsers is executeListBrowsers in src/mcp/tools/list-browsers.ts;
// fetchBrowserList's WS round-trip becomes an in-process registry read.
func (s *MCPServer) executeListBrowsers(_ context.Context, _ *mcp.CallToolRequest, _ timeoutOnlyArgs) (*mcp.CallToolResult, any, error) {
	browsers := s.registry.ListBrowsers()
	if len(browsers) == 0 {
		return toolText("No browsers connected."), nil, nil
	}
	return toolText(formatBrowserList(browsers)), nil, nil
}

// executeSetBrowser is executeSetBrowser in src/mcp/tools/set-browser.ts.
// Note the TS original does not validate that the browser exists; the error
// surfaces at the next command through resolveBrowser.
func (s *MCPServer) executeSetBrowser(_ context.Context, req *mcp.CallToolRequest, args setBrowserArgs) (*mcp.CallToolResult, any, error) {
	s.sessions.set(sessionID(req), args.BrowserID)
	return toolText(fmt.Sprintf("Browser set to \"%s\" for this session.", args.BrowserID)), nil, nil
}

// --- Navigation ---

func (s *MCPServer) executeNavigate(ctx context.Context, req *mcp.CallToolRequest, args navigateArgs) (*mcp.CallToolResult, any, error) {
	// The extension's own wait for the page to reach 'complete' gets the
	// caller's budget, and the transport gets the slack on top — the same
	// arrangement as the wait commands, so a stalled navigation reports
	// "Navigation timeout" from the extension rather than a bare control-plane
	// timeout on a command that is still running.
	budget := s.commandTimeout(args.TimeoutMS)
	return s.runMessageTool(ctx, req, "navigate", commandSpec{
		name:   "navigate",
		tabID:  args.TabID,
		params: navParams{URL: args.URL, TabID: args.TabID, Timeout: int(budget.Milliseconds())},
		// waitBudget makes transportTimeout use budget+waitSlack, and
		// signals that params carries an in-page budget.
		waitBudget: budget,
	}, args.TimeoutMS, "Navigation failed",
		fmt.Sprintf("Navigated to %s in tab %d", args.URL, args.TabID))
}

func (s *MCPServer) executeGoBack(ctx context.Context, req *mcp.CallToolRequest, args tabIDTimeoutArgs) (*mcp.CallToolResult, any, error) {
	return s.runMessageTool(ctx, req, "go_back", commandSpec{
		name:   "goBack",
		tabID:  args.TabID,
		params: tabParams{TabID: args.TabID},
	}, args.TimeoutMS, "Go back failed", "Went back")
}

func (s *MCPServer) executeGoForward(ctx context.Context, req *mcp.CallToolRequest, args tabIDTimeoutArgs) (*mcp.CallToolResult, any, error) {
	return s.runMessageTool(ctx, req, "go_forward", commandSpec{
		name:   "goForward",
		tabID:  args.TabID,
		params: tabParams{TabID: args.TabID},
	}, args.TimeoutMS, "Go forward failed", "Went forward")
}

func (s *MCPServer) executeRefresh(ctx context.Context, req *mcp.CallToolRequest, args tabIDTimeoutArgs) (*mcp.CallToolResult, any, error) {
	return s.runMessageTool(ctx, req, "refresh", commandSpec{
		name:   "refresh",
		tabID:  args.TabID,
		params: tabParams{TabID: args.TabID},
	}, args.TimeoutMS, "Refresh failed", "Refreshed")
}

// --- Tabs ---

func (s *MCPServer) executeTabNew(ctx context.Context, req *mcp.CallToolRequest, args tabNewArgs) (*mcp.CallToolResult, any, error) {
	// Pointers + omitempty drop absent optionals from the wire, matching the
	// TS tools' `undefined` handling.
	return s.runMessageTool(ctx, req, "tab_new", commandSpec{
		name: "tab:new",
		params: tabNewParams{
			URL:       args.URL,
			Active:    args.Active,
			AutoClose: args.AutoClose,
		},
	}, args.TimeoutMS, "tab:new failed", "New tab opened")
}

func (s *MCPServer) executeTabClose(ctx context.Context, req *mcp.CallToolRequest, args tabIDTimeoutArgs) (*mcp.CallToolResult, any, error) {
	return s.runMessageTool(ctx, req, "tab_close", commandSpec{
		name:   "tab:close",
		tabID:  args.TabID,
		params: tabParams{TabID: args.TabID},
	}, args.TimeoutMS, "tab:close failed", fmt.Sprintf("Tab %d closed", args.TabID))
}

func (s *MCPServer) executeTabSwitch(ctx context.Context, req *mcp.CallToolRequest, args tabIDTimeoutArgs) (*mcp.CallToolResult, any, error) {
	return s.runMessageTool(ctx, req, "tab_switch", commandSpec{
		name:   "tab:switch",
		tabID:  args.TabID,
		params: tabParams{TabID: args.TabID},
	}, args.TimeoutMS, "tab:switch failed", fmt.Sprintf("Switched to tab %d", args.TabID))
}

// --- Interaction ---

func (s *MCPServer) executeClick(ctx context.Context, req *mcp.CallToolRequest, args selectorTabArgs) (*mcp.CallToolResult, any, error) {
	return s.runMessageTool(ctx, req, "click", commandSpec{
		name:   "click",
		tabID:  args.TabID,
		params: selectorTabParams{Selector: args.Selector, TabID: args.TabID},
	}, args.TimeoutMS, "Click failed", fmt.Sprintf("Clicked %s", args.Selector))
}

func (s *MCPServer) executeType(ctx context.Context, req *mcp.CallToolRequest, args typeArgs) (*mcp.CallToolResult, any, error) {
	return s.runMessageTool(ctx, req, "type", commandSpec{
		name:  "type",
		tabID: args.TabID,
		params: typeParams{
			Selector: args.Selector,
			Text:     args.Text,
			Submit:   args.Submit,
			TabID:    args.TabID,
		},
	}, args.TimeoutMS, "Type failed", fmt.Sprintf("Typed into %s", args.Selector))
}

func (s *MCPServer) executeSelect(ctx context.Context, req *mcp.CallToolRequest, args selectArgs) (*mcp.CallToolResult, any, error) {
	return s.runMessageTool(ctx, req, "select", commandSpec{
		name:  "select",
		tabID: args.TabID,
		params: selectParams{
			Selector: args.Selector,
			Value:    args.Value,
			TabID:    args.TabID,
		},
	}, args.TimeoutMS, "Select failed", fmt.Sprintf("Selected %s in %s", args.Value, args.Selector))
}

func (s *MCPServer) executeScroll(ctx context.Context, req *mcp.CallToolRequest, args scrollArgs) (*mcp.CallToolResult, any, error) {
	return s.runMessageTool(ctx, req, "scroll", commandSpec{
		name:  "scroll",
		tabID: args.TabID,
		params: scrollParams{
			Selector: args.Selector,
			X:        args.X,
			Y:        args.Y,
			TabID:    args.TabID,
		},
	}, args.TimeoutMS, "Scroll failed", fmt.Sprintf("Scrolled to (%d, %d)", args.X, args.Y))
}

func (s *MCPServer) executeHover(ctx context.Context, req *mcp.CallToolRequest, args selectorTabArgs) (*mcp.CallToolResult, any, error) {
	return s.runMessageTool(ctx, req, "hover", commandSpec{
		name:   "hover",
		tabID:  args.TabID,
		params: selectorTabParams{Selector: args.Selector, TabID: args.TabID},
	}, args.TimeoutMS, "Hover failed", fmt.Sprintf("Hovered %s", args.Selector))
}

// --- Waits ---

// transportTimeout is how long the control plane waits for a command's
// response. Normally that is the caller's timeout_ms; for the wait commands
// it is the in-page budget plus waitSlack, so the content script's rejection
// reaches the agent before the control plane gives up on it.
func transportTimeout(s *MCPServer, spec commandSpec, timeoutMS *int) time.Duration {
	if spec.waitBudget > 0 {
		return spec.waitBudget + core.InPageTimeoutSlack
	}
	return s.commandTimeout(timeoutMS)
}

func (s *MCPServer) executeWaitElement(ctx context.Context, req *mcp.CallToolRequest, args selectorTabArgs) (*mcp.CallToolResult, any, error) {
	budget := s.commandTimeout(args.TimeoutMS)
	return s.runMessageTool(ctx, req, "wait_element", commandSpec{
		name:  "wait:element",
		tabID: args.TabID,
		params: waitParams{
			Selector: &args.Selector,
			Timeout:  int(budget.Milliseconds()),
			TabID:    args.TabID,
		},
		waitBudget: budget,
	}, args.TimeoutMS, "Wait element failed", fmt.Sprintf("Element %s found", args.Selector))
}

func (s *MCPServer) executeWaitNavigation(ctx context.Context, req *mcp.CallToolRequest, args tabIDTimeoutArgs) (*mcp.CallToolResult, any, error) {
	budget := s.commandTimeout(args.TimeoutMS)
	return s.runMessageTool(ctx, req, "wait_navigation", commandSpec{
		name:       "wait:navigation",
		tabID:      args.TabID,
		params:     waitParams{Timeout: int(budget.Milliseconds()), TabID: args.TabID},
		waitBudget: budget,
	}, args.TimeoutMS, "Wait navigation failed", "Navigation complete")
}
