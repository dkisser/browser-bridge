package http

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"browser-bridge/internal/memory"
)

// The memory tools are the pull half of ADR-0019's recall, on the MCP adapter
// (ADR-0039). They are read-only by construction: the store's write paths —
// guides and routines — are human-initiated and stay behind the CLI, because
// ADR-0028 argues that a model writing a file which then rides every later
// visit's context is an indirect prompt-injection persistence channel, and the
// review that argument asks for happens at the moment of writing.
//
// Two properties separate these from every other tool here. They do not go
// through dispatch, and that is not a shortcut: dispatch calls TakeSiteNote,
// which yields a card at most once per landing, so an agent that reached for
// memory_show before its first navigate would spend the landing's card and
// leave the browsing tools with nothing. And they do not touch the router, so
// they never reach the extension's policy gate (ADR-0006) — which is why they
// carry no tab_id and no timeout_ms.

// memoryListArgs — memory_list takes no input. The type exists so the handler
// has the same shape as every other one; the schema is the closed empty object.
type memoryListArgs struct{}

// memoryShowArgs mirrors `bridge memory show <host> --raw`: the rendered text is
// the default because that is what an agent reads, and raw is the machine card
// for the one caller that has to reason over the entries rather than read them.
type memoryShowArgs struct {
	Host string `json:"host"`
	Raw  bool   `json:"raw,omitempty"`
}

// memoryListRow is the same row `bridge memory list --json` emits, so an agent
// that learned one interface is not re-learning the shape on the other.
type memoryListRow struct {
	Host       string `json:"host"`
	Revision   int    `json:"revision"`
	UpdatedAt  string `json:"updatedAt"`
	Map        int    `json:"map"`
	Failures   int    `json:"failures"`
	Procedures int    `json:"procedures"`
}

func (s *MCPServer) executeMemoryList(_ context.Context, _ *mcp.CallToolRequest, _ memoryListArgs) (*mcp.CallToolResult, any, error) {
	reader, unavailable := s.memoryReader()
	if unavailable != nil {
		return unavailable, nil, nil
	}
	hosts := reader.ListHosts()
	rows := make([]memoryListRow, 0, len(hosts))
	for _, h := range hosts {
		card, err := reader.ReadCard(h)
		if err != nil || card == nil {
			// A card that will not parse is a fact about the store, not a row.
			// memory_show is where that is reported in full, with its path; here
			// it would only mean one fewer line in a list of hosts.
			continue
		}
		rows = append(rows, memoryListRow{
			Host:       card.Host,
			Revision:   card.Revision,
			UpdatedAt:  formatMemoryMs(card.UpdatedAtMs),
			Map:        len(card.Map),
			Failures:   len(card.Failures),
			Procedures: len(card.Procedures),
		})
	}
	if len(rows) == 0 {
		return toolText("No site cards yet. Drive a site and snapshot it — the learner builds the card from what actually happened."), nil, nil
	}
	out, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return toolError("memory_list", err.Error()), nil, nil
	}
	return toolText(string(out)), nil, nil
}

func (s *MCPServer) executeMemoryShow(_ context.Context, _ *mcp.CallToolRequest, args memoryShowArgs) (*mcp.CallToolResult, any, error) {
	reader, unavailable := s.memoryReader()
	if unavailable != nil {
		return unavailable, nil, nil
	}
	host := strings.TrimSpace(args.Host)

	card, err := reader.ReadCard(host)
	if err != nil {
		// Reported, not hidden: the field says which host, and the message
		// carries the reason, so an agent can ask a human rather than concluding
		// the card is empty.
		return toolError("memory_show", fmt.Sprintf("the card for %s exists but will not parse (%v)", host, err)), nil, nil
	}
	// A guide can legitimately arrive before any traffic has earned a card —
	// they are written at a human's request while cards are earned — so it is
	// read before the card is judged missing, and printed on its own when the
	// card is absent (ADR-0033).
	guide, gerr := reader.ReadGuide(host)
	if gerr != nil {
		return toolError("memory_show", fmt.Sprintf("the guide for %s exists but will not read: %v", host, gerr)), nil, nil
	}

	if args.Raw {
		guidePath := ""
		if guide != "" {
			guidePath = reader.GuidePath(host)
		}
		return toolText(rawCardJSON(host, card, guidePath)), nil, nil
	}

	var b strings.Builder
	if card == nil {
		fmt.Fprintf(&b, "%s\nNo card for %s — the bridge has not learned this host yet. Carry on without one;\n",
			memoryHeader(host), host)
		fmt.Fprintf(&b, "driving the site and snapshotting it is what builds it.\n")
	} else {
		fmt.Fprint(&b, renderCardOffline(reader, host, card))
	}
	if guide != "" {
		fmt.Fprintf(&b, "\n[site guide] %s\n%s\n", reader.GuidePath(host), guide)
	}
	return toolText(strings.TrimRight(b.String(), "\n")), nil, nil
}

// renderCardOffline renders a card with its site map resolved against the last
// page the control plane recorded, and says where that page came from.
//
// The provenance is stated in the header *and* carried into the map section's
// own title via PageNote, because that is what keeps this rendering honest:
// a card's refs only mean anything against the page they were resolved from,
// and the page in the browser now is not that page (ADR-0026). One statement
// is a footer nobody reads; the section title is the line a reader is already
// looking at when they decide whether to trust an @eN.
func renderCardOffline(reader MemoryReader, host string, card *memory.SiteCard) string {
	var b strings.Builder
	digest, atMs, err := reader.LastPageDigest(host)
	if err != nil {
		return fmt.Sprintf("%s\nThe trace could not be read (%v), so the site map is shown unresolved.\n\n%s\n",
			memoryHeader(host), err, memory.RenderCard(card, memory.RenderOptions{MaxTokens: memory.DefaultInjectTokens}))
	}
	if digest == nil {
		// Not a failure of the store: there is a card, it just has nothing to
		// resolve against, and RenderCard then drops the map section entirely
		// (ADR-0024's empty-section rule). Saying so is the difference between
		// "this site has no remembered landmarks" and "the map was withheld".
		return fmt.Sprintf("%s\nNo page has been recorded for %s yet, so the site map cannot be resolved —\n"+
			"what is below is the card's own claim, checked against nothing. Visit the site and\n"+
			"snapshot it, then pull again; the digest is kept in the trace.\n\n%s\n",
			memoryHeader(host), host,
			memory.RenderCard(card, memory.RenderOptions{MaxTokens: memory.DefaultInjectTokens}))
	}

	_, missing := memory.VerifyCard(card, digest, "")
	fmt.Fprintf(&b, "%s\nResolved offline against the last page the control plane recorded for %s.\n", memoryHeader(host), host)
	fmt.Fprintf(&b, "  seen %s", formatMemoryMs(atMs))
	if digest.URL != "" {
		fmt.Fprintf(&b, "  %s", digest.URL)
	}
	if len(card.Map) > 0 {
		fmt.Fprintf(&b, "\n  %d of %d map entries matched it; %d did not.", len(card.Map)-missing, len(card.Map), missing)
	}
	fmt.Fprintf(&b, "\n  These refs are not valid for whatever is in your browser now.\n\n")

	b.WriteString(memory.RenderCard(card, memory.RenderOptions{
		MaxTokens: memory.DefaultInjectTokens,
		Resolver:  func(p memory.Predicate) (string, bool) { return digest.Resolve(p) },
		// Deliberately never the empty default: that string means "the page in
		// hand", which is true of the injection and false of anything read out
		// of a file.
		PageNote: "the page seen " + formatMemoryMs(atMs),
	}))
	b.WriteString("\n")
	return b.String()
}

// rawCardJSON is the machine view, shaped like `bridge memory show <host>
// --json`'s cardJSON so the two agree field for field. The guide's *path*
// travels and its prose does not: raw exists to be piped, and a guide is
// unbounded human prose (MaxGuideBytes of it), which is the same reason it
// stays out of the landing injection (ADR-0033).
func rawCardJSON(host string, card *memory.SiteCard, guidePath string) string {
	type payload struct {
		Host      string           `json:"host"`
		Card      *memory.SiteCard `json:"card"`
		GuidePath string           `json:"guidePath,omitempty"`
	}
	p := payload{Host: host, Card: card, GuidePath: guidePath}
	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"host": %q, "error": %q}`, host, err.Error())
	}
	return string(out)
}

// memoryHeader opens every memory_show rendering, so the agent can tell a
// recalled claim from whatever the tool said. It is deliberately not
// memory.InjectionLabel: that label marks the push path, which is resolved
// against the page in hand, and reusing it here would make an offline pull
// indistinguishable from the live thing (ADR-0026).
func memoryHeader(host string) string {
	return fmt.Sprintf("[learned site memory for %s — pulled from the store, not from this page]", host)
}

// memoryReader returns the store or the tool error explaining its absence.
// Self-learning is optional (app.go comes up without it when the store will not
// open), and a tool that failed with "no such method" would be a worse answer
// than one that says the feature is off.
func (s *MCPServer) memoryReader() (MemoryReader, *mcp.CallToolResult) {
	if s.memories == nil {
		return nil, toolError("memory", "self-learning is disabled on this control plane — no site cards are being recorded or served")
	}
	return s.memories, nil
}

func formatMemoryMs(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04:05")
}
