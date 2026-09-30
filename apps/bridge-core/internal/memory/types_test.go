package memory

import (
	"bytes"
	"encoding/json"
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
