package http

import (
	"fmt"
	"strings"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/core"
)

// resolveBrowser is resolveBrowser in src/mcp/browser-resolver.ts: pick the
// browser a command should go to, or explain why none qualifies. explicit is
// the browserId pinned via set_browser ("" when unset).
func resolveBrowser(explicit string, browsers []core.BrowserConnection) (browserID, failureMessage string) {
	if explicit != "" {
		for _, b := range browsers {
			if b.BrowserID == explicit {
				if b.Status != core.StatusOnline {
					return "", fmt.Sprintf("Browser \"%s\" is not online (status: %s).", explicit, b.Status)
				}
				return explicit, ""
			}
		}
		return "", fmt.Sprintf("Browser \"%s\" is not connected.", explicit)
	}

	var online []core.BrowserConnection
	for _, b := range browsers {
		if b.Status == core.StatusOnline {
			online = append(online, b)
		}
	}
	switch len(online) {
	case 0:
		return "", "No browser connected. Start the extension/local-proxy first."
	case 1:
		return online[0].BrowserID, ""
	}
	return "", fmt.Sprintf("Multiple browsers are online. Call set_browser with one of:\n%s", formatBrowserList(online))
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
