package http

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// goldenTool is one entry of the TS (FastMCP) tools/list export.
type goldenTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// TestToolsListMatchesTSFixture diffs the Go server's tools/list against the
// golden export taken from the TS bridge-core's MCP server
// (testdata/tools_list_ts.json). Comparison is semantic: the served tool
// order differs by design (go-sdk sorts by name, FastMCP kept registration
// order; the MCP spec treats the list as a set), and JSON object key order
// inside schemas is normalized by unmarshalling both sides.
func TestToolsListMatchesTSFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/tools_list_ts.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture []goldenTool
	if uerr := json.Unmarshal(data, &fixture); uerr != nil {
		t.Fatalf("parse fixture: %v", uerr)
	}
	if len(fixture) != 22 {
		t.Fatalf("fixture has %d tools, want 22", len(fixture))
	}

	srv := newTestServer(&fakeRouter{}, nil)
	session := newTestClient(t, srv)
	list, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}

	served := make(map[string]*mcp.Tool, len(list.Tools))
	for _, tool := range list.Tools {
		if _, dup := served[tool.Name]; dup {
			t.Fatalf("duplicate tool name %q", tool.Name)
		}
		served[tool.Name] = tool
	}
	if len(served) != len(fixture) {
		t.Errorf("served %d tools, fixture has %d", len(served), len(fixture))
	}

	for _, want := range fixture {
		tool, ok := served[want.Name]
		if !ok {
			t.Errorf("tool %q missing from Go tools/list", want.Name)
			continue
		}
		if tool.Description != want.Description {
			t.Errorf("tool %q description mismatch:\n got: %q\nwant: %q", want.Name, tool.Description, want.Description)
		}
		// The client unmarshals InputSchema as generic JSON; re-marshal and
		// compare semantically against the fixture.
		gotSchema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Errorf("tool %q: marshal served schema: %v", want.Name, err)
			continue
		}
		var gotAny, wantAny any
		if err := json.Unmarshal(gotSchema, &gotAny); err != nil {
			t.Errorf("tool %q: re-parse served schema: %v", want.Name, err)
			continue
		}
		if err := json.Unmarshal(want.InputSchema, &wantAny); err != nil {
			t.Errorf("tool %q: parse fixture schema: %v", want.Name, err)
			continue
		}
		if !reflect.DeepEqual(gotAny, wantAny) {
			gotPretty, _ := json.MarshalIndent(gotAny, "", "  ")
			wantPretty, _ := json.MarshalIndent(wantAny, "", "  ")
			t.Errorf("tool %q inputSchema mismatch:\n got: %s\nwant: %s", want.Name, gotPretty, wantPretty)
		}
	}

	for name := range served {
		found := false
		for _, want := range fixture {
			if want.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Go serves extra tool %q absent from the TS fixture", name)
		}
	}
}

// docs/mcp-setup.md carries a hand-maintained table of the same 22 tools the
// golden fixture pins, and it drifted: `snapshot` was missing entirely and
// `list_browsers` listed no parameters. The fixture is the contract, so the
// doc being behind it means the first thing a new user reads disagrees with
// the first thing a client fetches — and nothing caught it, because the doc is
// prose.
//
// This pins the tool *names* only. Parameter-level parity would mean parsing a
// markdown table into schema shapes, and a test that elaborate would be a
// worse thing to maintain than the drift it prevents. A missing tool is the
// failure worth catching automatically; a mistyped parameter column is not.
func TestDocsListEveryToolTheFixturePins(t *testing.T) {
	data, err := os.ReadFile("testdata/tools_list_ts.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture []goldenTool
	if uerr := json.Unmarshal(data, &fixture); uerr != nil {
		t.Fatalf("parse fixture: %v", uerr)
	}

	// From internal/http, the repo root is four levels up.
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "mcp-setup.md"))
	if err != nil {
		t.Fatalf("read docs/mcp-setup.md: %v", err)
	}
	for _, tool := range fixture {
		row := "| `" + tool.Name + "` |"
		if !strings.Contains(string(doc), row) {
			t.Errorf("docs/mcp-setup.md has no row for tool %q; the table has "+
				"drifted from the tools/list contract", tool.Name)
		}
	}
}
