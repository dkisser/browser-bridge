package memory

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSafeURLDropsWhatTheLearnerCannotUse(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://news.example.com/", "https://news.example.com/"},
		{"https://news.example.com/a/b", "https://news.example.com/a/b"},
		// The part that can be a search term, a session token or a document id.
		{"https://news.example.com/s?q=my private search", "https://news.example.com/s"},
		{"https://news.example.com/s?token=abc123#frag", "https://news.example.com/s"},
		{"https://user:pass@news.example.com/", "https://news.example.com/"},
		// Internal pages keep enough to be recognised and teach nothing.
		{"about:blank", "about:blank"},
		{"chrome://extensions", "chrome://extensions"},
		// Unparseable is dropped rather than stored raw.
		{"://nope", ""},
		{"ht tp://x", ""},
	}
	for _, c := range cases {
		if got := safeURL(c.in); got != c.want {
			t.Errorf("safeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRedactArgsDropsTypedContentAndQueries(t *testing.T) {
	got := redactArgs("type", map[string]any{
		"selector": "@e3",
		"text":     "my password is hunter2",
		"submit":   true,
	})
	if v, ok := got["text_len"]; !ok || v != 22 {
		t.Errorf("text_len = %v (present=%v), want 22", v, ok)
	}
	if _, ok := got["text"]; ok {
		t.Error("the typed text survived redaction")
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("hunter2")) {
		t.Errorf("the typed text reached the recorded args: %s", blob)
	}

	nav := redactArgs("navigate", map[string]any{
		"url": "https://mail.example.com/u/0/#inbox/important",
	})
	if nav["url"] != "https://mail.example.com/u/0/" {
		t.Errorf("url = %v, want the query and fragment dropped", nav["url"])
	}
	// The host has to survive, because the host is the only thing the learner
	// keys off — a card per site is the whole unit of memory.
	redactedURL, isString := nav["url"].(string)
	if !isString {
		t.Fatalf("url is %T, want a string", nav["url"])
	}
	if got := HostFromURL(redactedURL); got != "mail.example.com" {
		t.Errorf("the host did not survive redaction: got %q from %v", got, redactedURL)
	}
}

func TestSafeSelectorReducesBareText(t *testing.T) {
	// A selector that is really prose must reduce to the fact that it was
	// text. The sentence is page content the moment querySelectorByText
	// matches it, and it reaches the card through two paths — the recorded
	// arg and the failure's Sel — so it cannot survive either.
	prose := []string{
		"Standup notes",
		"her lawyer private note",
		"some prose",
		"a sentence, with punctuation",
	}
	for _, in := range prose {
		got := safeSelector(in)
		if got != "…" {
			t.Errorf("safeSelector(%q) = %q, want … (the prose was kept)", in, got)
		}
		redacted := redactArgs("gettext", map[string]any{"selector": in})
		if got := redacted["selector"]; got != "…" {
			t.Errorf("recorded selector for %q = %v, want …", in, got)
		}
	}
}

func TestSafeSelectorKeepsStructure(t *testing.T) {
	// The other half: a real selector keeps the part that generalises, or the
	// reduction protects against nothing.
	cases := []struct{ in, want string }{
		{`[data-message-subject="Standup notes"]`, `[data-message-subject=…]`},
		{".entry-content", ".entry-content"},
		{"#main", "#main"},
		{"input[name]", "input[name]"},
		// A child combinator no longer keeps its shape. `Home > Inbox` is prose
		// an agent really does pass as a selector, and no flank test separates
		// it from `div > p` — so the ambiguity resolves toward the side that
		// cannot leak, exactly as it already does for a descendant selector.
		// The reduction still teaches the actionable thing: a bare selector did
		// not resolve here.
		{"div > p", "…"},
		// A ref is the one address form the feature is built on.
		{"@e14", "@e14"},
	}
	for _, c := range cases {
		if got := safeSelector(c.in); got != c.want {
			t.Errorf("safeSelector(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestErrCodeNeverStoresTheMessage(t *testing.T) {
	// Codes the control plane and the extension already mint pass through:
	// they are the generalising part, and flattening them would throw away the
	// distinction between "the browser was offline" and "no such element".
	for _, in := range []string{
		"selector_not_found",
		"browser_offline",
		"cannot_buffer",
		"sw_timeout",
		"forbidden_sender",
		"bb_sensitive_field_at_execution",
	} {
		if got := errCode(in); got != in {
			t.Errorf("errCode(%q) = %q, want the code unchanged", in, got)
		}
	}

	// Prose is classified on its fixed head, and the selector the message
	// quotes back is never part of the result.
	const sentence = `No element found for "her lawyer private note" — tried it as a CSS selector and as exact visible text.`
	if got := errCode(sentence); got != "no_element" {
		t.Errorf("errCode(not-found) = %q, want no_element", got)
	}
	if strings.Contains(errCode(sentence), "lawyer") {
		t.Errorf("the message survived into the code: %q", errCode(sentence))
	}

	others := map[string]string{
		`Element with text not found: her lawyer private note`: "no_element",
		"The element matched by \"...\" has no text content":   "empty_element",
		"Invalid ref selector: @e (expected @e<N>)":            "bad_ref",
		"Ref @e14 not found on page. Take a fresh snapshot":    "bad_ref",
		// wait:element times out with its own wording, and without this arm a
		// not-found lands in the same bucket as every unrecognised failure.
		"Element not found within 5000ms: her lawyer private note": "no_element",
		"Unknown DOM command: frobnicate":                          "unknown_command",
		"Missing required tabId":                                   "missing_tab",
		"Unable to capture screenshot: tab must be active":         "unknown",
		"": "",
	}
	for in, want := range others {
		if got := errCode(in); got != want {
			t.Errorf("errCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestErrCodeRefusesASentenceShapedLikeACode(t *testing.T) {
	// The passthrough is a shape test, so a message with no spaces in it must
	// still not be trusted. Anything carrying a capital or punctuation is prose
	// and is classified rather than stored.
	if got := errCode("Element not found:"); got != "no_element" {
		t.Errorf("errCode with a capital = %q, want it classified, not stored", got)
	}
}

// The prose the previous test did not cover: an ordinary sentence, which ends
// in a period and therefore contains a "selector character".
//
// The old test was `strings.ContainsAny(sel, ".#[]():>+~*=")`, so a period was
// enough to route prose down the structural path, where it is copied byte for
// byte into a card file that outlives the visit. `get_text "Contact support."`
// wrote the page's own words to $BB_HOME/data/cards/<host>.json permanently —
// the exact failure safeSelector's own doc says it exists to prevent, closed
// only for prose that happened to avoid a period.
func TestSafeSelectorReducesProseThatContainsPunctuation(t *testing.T) {
	prose := []string{
		"Contact support.",
		"Click here. Then wait.",
		"her lawyer's private note.",
		"See the attached note.",
		"v1.2 released",
		"issue #42",
		"Note: see below",
		"see [1]",
		"2 + 2",
		"~5 items left",
		"footnote*",
		"Home > Inbox",
	}
	for _, in := range prose {
		if got := safeSelector(in); got != "…" {
			t.Errorf("safeSelector(%q) = %q, want … (the prose was kept)", in, got)
		}
	}
}

// And the other direction: narrowing the marker test must not cost a real
// selector its shape, or the reduction protects against nothing.
func TestSafeSelectorStillKeepsASelectorsShape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a.b.c", "a.b.c"},
		{"#main", "#main"},
		{"li:nth-child(2)", "li:nth-child(2)"},
		// An attribute value is page-derived whether or not it was quoted, so
		// both forms reduce. This used to keep the value.
		{"[name=x]", "[name=…]"},
		// The value is the page's, quoted or not; the attribute name is the
		// structure worth keeping, and it survives.
		{"[data-order-id=ORD12345]", "[data-order-id=…]"},
		{`[class*="note"]`, "[class*=…]"},
	}
	for _, c := range cases {
		if got := safeSelector(c.in); got != c.want {
			t.Errorf("safeSelector(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
