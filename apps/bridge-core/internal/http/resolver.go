package http

import (
	"fmt"
	"strings"

	"browser-bridge/internal/core"
)

// resolveBrowser is resolveBrowser in src/mcp/browser-resolver.ts: pick the
// browser a command should go to, or explain why none qualifies. explicit is
// the browserId pinned via set_browser ("" when unset).
//
// A browser in idle_wait is a legitimate target. That status is the router's
// "connected, socket currently between" state, and it is what the command
// buffer exists for: a command sent during the reconnect tolerance is held
// and delivered when the extension comes back. Resolving only to online
// browsers meant an agent's tool call failed for the whole 5s window with
// "No browser connected. Start the extension/local-proxy first." — advice to
// go start an extension that was running and merely reconnecting, and advice
// the CLI path did not give because it skips this resolver entirely.
func resolveBrowser(explicit string, browsers []core.BrowserConnection) (browserID, failureMessage string) {
	// targetable is online or reconnecting; offline means it is gone.
	targetable := func(s core.BrowserStatus) bool {
		return s == core.StatusOnline || s == core.StatusIdleWait
	}

	if explicit != "" {
		for _, b := range browsers {
			if b.BrowserID == explicit {
				if !targetable(b.Status) {
					return "", fmt.Sprintf("Browser \"%s\" is not online (status: %s).", explicit, b.Status)
				}
				return explicit, ""
			}
		}
		return "", fmt.Sprintf("Browser \"%s\" is not connected.", explicit)
	}

	// Prefer browsers that are live now; fall back to ones that are
	// reconnecting, so a command is still held for a browser that is about to
	// come back rather than refused outright.
	var online, reconnecting []core.BrowserConnection
	for _, b := range browsers {
		switch b.Status {
		case core.StatusOnline:
			online = append(online, b)
		case core.StatusIdleWait:
			reconnecting = append(reconnecting, b)
		}
	}
	candidates := online
	distinguishing := "No browser connected. Start the extension/local-proxy first."
	if len(candidates) == 0 && len(reconnecting) > 0 {
		candidates = reconnecting
		distinguishing = "No browser connected."
	}
	switch len(candidates) {
	case 0:
		return "", distinguishing
	case 1:
		return candidates[0].BrowserID, ""
	}
	return "", fmt.Sprintf("Multiple browsers are online. Call set_browser with one of:\n%s", formatBrowserList(candidates))
}

// formatBrowserList is formatBrowserList in browser-resolver.ts.
func formatBrowserList(browsers []core.BrowserConnection) string {
	lines := make([]string, len(browsers))
	for i, b := range browsers {
		lines[i] = fmt.Sprintf("- %s (%s)", b.BrowserID, b.Status)
	}
	return strings.Join(lines, "\n")
}

// resolveTargetBrowser is resolveTargetBrowser in src/mcp/browser-lookup.ts,
// with fetchBrowserList's WebSocket hop replaced by an in-process registry
// read (the same source the list_browsers event handler serves).
func (s *MCPServer) resolveTargetBrowser(sessionID string) (browserID, failureMessage string) {
	return resolveBrowser(s.sessions.get(sessionID), s.registry.ListBrowsers())
}
