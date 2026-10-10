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
	var broken []string
	for _, h := range hosts {
		card, err := reader.ReadCard(h)
		if err != nil {
			// Collected, not skipped. Dropping it silently made an empty result
			// mean two things at once — nothing learned, and everything broken —
			// and an agent reading "No site cards yet" would drive a site from
			// scratch that the bridge has in fact been learning. memory_show
			// reports the same condition with the parse error; this reports it
			// as the count it is here.
			broken = append(broken, h)
			continue
		}
		if card == nil {
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
		if len(broken) > 0 {
			return toolText(fmt.Sprintf(
				"No readable site cards. %d card file(s) exist but will not parse: %s.\n"+
					"Ask a human about those, or run `bridge memory rm <host>` on them — this is not the same as having learned nothing.",
				len(broken), strings.Join(broken, ", "))), nil, nil
		}
		return toolText("No site cards yet. Drive a site and snapshot it — the learner builds the card from what actually happened."), nil, nil
	}
	out, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return toolError("memory_list", err.Error()), nil, nil
	}
	text := string(out)
	if len(broken) > 0 {
		// Below the table rather than above it: the readable rows are the answer,
		// and the broken ones are a caveat on them. Leading with the caveat
		// would bury a list the caller did ask for.
		text += fmt.Sprintf("\n\n(%d card file(s) omitted because they will not parse: %s — `bridge memory show <host>` reports the error in full.)",
			len(broken), strings.Join(broken, ", "))
	}
	return toolText(text), nil, nil
}

func (s *MCPServer) executeMemoryShow(_ context.Context, _ *mcp.CallToolRequest, args memoryShowArgs) (*mcp.CallToolResult, any, error) {
	reader, unavailable := s.memoryReader()
	if unavailable != nil {
		return unavailable, nil, nil
	}
	// Normalised the way `bridge memory show` normalises, and it has to be the
	// *same* way: this tool is documented as that command's MCP equivalent, and
	// a caller holding a URL from pageinfo would otherwise be told a host the
	// bridge has a card for has never been learned. An empty result here is
	// indistinguishable from a miss, which is the worst shape a wrong answer can
	// take (memory.Host: "one rule, one place").
	host := memory.Host(strings.TrimSpace(args.Host))
	if host == "" {
		return toolError("memory_show", fmt.Sprintf("%q is not a host name", args.Host)), nil, nil
	}

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
	//
	// A guide that will not read is a warning and not a failure, and the CLI
	// said so first: the card is what this tool exists for and it has already
	// been read, so throwing it away over a file the human wrote would make the
	// two documented-equivalent interfaces disagree about whether a learned host
	// still has a card.
	guide, guideErr := reader.ReadGuide(host)

	if args.Raw {
		guidePath := ""
		if guide != "" {
			guidePath = reader.GuidePath(host)
		}
		return toolText(rawCardJSON(host, card, guidePath)), nil, nil
	}

	var b strings.Builder
	if card == nil {
		fmt.Fprintf(&b, "Pulled from the store for %s — these notes were not checked against the page in your browser.\n", host)
		fmt.Fprintf(&b, "No card for %s — the bridge has not learned this host yet. Carry on without one;\n", host)
		fmt.Fprintf(&b, "driving the site and snapshotting it is what builds it.\n")
	} else {
		fmt.Fprint(&b, renderCardOffline(reader, host, card))
	}
	if guide != "" {
		fmt.Fprintf(&b, "\n[site guide] %s\n%s\n", reader.GuidePath(host), guide)
	}
	if guideErr != nil {
		fmt.Fprintf(&b, "\nWarning: the guide for %s exists but will not read: %v\n", host, guideErr)
	}
	return toolText(strings.TrimRight(b.String(), "\n")), nil, nil
}

// renderCardOffline renders a card with its site map resolved against the last
// page the control plane recorded, and says where that page came from.
//
// The rendering itself is memory.RenderResolvedOffline, shared with the CLI's
// `--resolve`, because ADR-0026's requirement — that the provenance be stated
// where the reader is already looking — is only maintainable if there is one
// copy of it to change.
func renderCardOffline(reader MemoryReader, host string, card *memory.SiteCard) string {
	var b strings.Builder
	b.WriteString(pulledFromStore(host))

	digest, atMs, err := reader.LastPageDigest(host)
	if err != nil {
		fmt.Fprintf(&b, "The trace could not be read (%v), so the site map is shown unresolved.\n\n%s\n",
			err, memory.RenderCard(card, memory.RenderOptions{MaxTokens: memory.DefaultInjectTokens}))
		return b.String()
	}
	if digest == nil {
		// Not a failure of the store: there is a card, it just has nothing to
		// resolve against, and RenderCard then drops the map section entirely
		// (ADR-0024's empty-section rule). Saying so is the difference between
		// "this site has no remembered landmarks" and "the map was withheld".
		fmt.Fprintf(&b, "No page has been recorded for %s yet, so the site map cannot be resolved —\n"+
			"what is below is the card's own claim, checked against nothing. Visit the site and\n"+
			"snapshot it, then pull again; the digest is kept in the trace.\n\n", host)
		return b.String() + cardBody(memory.RenderResolvedOffline(card, nil, 0).Text)
	}

	r := memory.RenderResolvedOffline(card, digest, atMs)
	fmt.Fprintf(&b, "Resolved offline against the last page the control plane recorded for %s.\n", host)
	fmt.Fprintf(&b, "  seen %s", formatMemoryMs(atMs))
	if r.PageURL != "" {
		fmt.Fprintf(&b, "  %s", r.PageURL)
	}
	if r.Total > 0 {
		// About the card, not about the lines below: RenderCard trims to
		// MaxTokens by shedding from the end with no marker, so a count
		// claiming "N of M" of what was *shown* would be wrong the moment the
		// cap bites. Naming the cap keeps the claim and the body in agreement.
		fmt.Fprintf(&b, "\n  %d of %d map entries matched that page; %d did not. The rendering is capped at ~%d tokens, so it may show fewer.",
			r.Matched, r.Total, r.Missing, memory.DefaultInjectTokens)
	}
	fmt.Fprintf(&b, "\n  These refs are not valid for whatever is in your browser now.\n\n")
	return b.String() + cardBody(r.Text)
}

// cardBody renders the card, or says plainly that there is nothing in it. A
// card with no entries is a real state — the learner can write one, and the
// file is hand-editable — and rendering it as a header followed by no content
// leaves the reader unable to tell it from a card whose every line was trimmed.
func cardBody(rendered string) string {
	if rendered == "" {
		return "This card has no entries — nothing was learned about this host that survived review.\n"
	}
	return rendered + "\n"
}

// pulledFromStore opens a pull's output.
//
// Deliberately *not* a bracketed label. memory.InjectionLabel is the marker the
// live injection uses, RenderCard emits it inside every rendering it produces,
// and a second bracketed label above it would put two of them in one payload —
// the reader would then have to tell "learned site patterns" (a claim about a
// site, resolved against whatever page the control plane last saw) from whatever
// this line was claiming, which is exactly the confusion ADR-0026 exists to
// prevent. So this is prose, saying the one thing that differs: where it came
// from, and that no page is in hand.
func pulledFromStore(host string) string {
	return fmt.Sprintf("Pulled from the store for %s — these notes were not checked against the page in your browser.\n", host)
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
