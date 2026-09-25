package service

import (
	_ "embed"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// launchAgentTmpl is the LaunchAgent template, formerly
// install/launchagent.plist.tmpl fetched/copied at install time. Embedding
// it removes the BB-E301 missing-template failure mode: the binary always
// carries the plist it supervises itself with.
//
//go:embed launchagent.plist.tmpl
var launchAgentTmpl string

// renderLaunchAgentPlist substitutes the placeholders the way the bash
// esc()/awk pipeline did, including the XML escaping of & < >.
func (e *Env) renderLaunchAgentPlist() string {
	repl := strings.NewReplacer(
		"{{BB_HOME}}", xmlEscape(e.BBHome),
		"{{WS_PORT}}", strconv.Itoa(e.WSPort),
		"{{WS_HOSTNAME}}", xmlEscape(e.WSHost),
		"{{LOCAL_PORT}}", strconv.Itoa(e.LocalPort),
		"{{LOCAL_HOSTNAME}}", xmlEscape(e.LocalHost),
		"{{MCP_PORT}}", strconv.Itoa(e.MCPPort),
		"{{MCP_HOSTNAME}}", xmlEscape(e.MCPHost),
	)
	return repl.Replace(launchAgentTmpl)
}

// xmlEscape is the bash esc(): only the three plist-significant characters.
func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// writeLaunchAgentPlists renders the plist to the staging dir and, when
// login auto-start is enabled, to ~/Library/LaunchAgents (write_launchagent_plists).
func (e *Env) writeLaunchAgentPlists() error {
	rendered := e.renderLaunchAgentPlist()
	if err := os.MkdirAll(filepath.Dir(e.StagingPlist()), 0o755); err != nil {
		return errf("BB-E301", "create staging dir: %v", err)
	}
	if err := os.WriteFile(e.StagingPlist(), []byte(rendered), 0o644); err != nil {
		return errf("BB-E301", "write %s: %v", e.StagingPlist(), err)
	}
	if e.Enabled() {
		if err := os.MkdirAll(filepath.Dir(e.LaunchAgentPlist()), 0o755); err != nil {
			return errf("BB-E301", "create LaunchAgents dir: %v", err)
		}
		if err := os.WriteFile(e.LaunchAgentPlist(), []byte(rendered), 0o644); err != nil {
			return errf("BB-E301", "write %s: %v", e.LaunchAgentPlist(), err)
		}
	}
	return nil
}
