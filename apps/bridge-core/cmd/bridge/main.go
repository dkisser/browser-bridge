// Command bridge is the Browser Bridge CLI — Go port of the deleted TS
// apps/cli (ADR-0012 phase 2b).
package main

import (
	"os"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
