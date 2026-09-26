// Command bridge is the Browser Bridge binary — Go port of the deleted TS
// apps/cli (ADR-0012 phase 2b), the `bridge service` lifecycle tree from
// install/bridge.sh.tmpl (phase 3), and, via the hidden `serve` subcommand,
// the control plane formerly shipped as the standalone bridge-core binary
// (ADR-0013).
package main

import (
	"os"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/cli"
)

// version is reported by `bridge --version` and by the MCP server when the
// binary runs `bridge serve`. Overridable at link time:
// -ldflags "-X main.version=v1.2.3". The default matches package.json (the
// standalone bridge-core binary hardcoded it).
var version = "0.3.2"

func main() {
	os.Exit(cli.Execute(version))
}
