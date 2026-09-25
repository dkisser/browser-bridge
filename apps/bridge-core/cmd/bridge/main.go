// Command bridge is the Browser Bridge CLI — Go port of the deleted TS
// apps/cli (ADR-0012 phase 2b) plus the `bridge service` lifecycle tree
// from install/bridge.sh.tmpl (phase 3).
package main

import (
	"os"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/cli"
)

// version is the binary version reported by `bridge --version`. Overridable
// at link time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	os.Exit(cli.Execute(version))
}
