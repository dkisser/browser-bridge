package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ADR-0022, in full: the model call exists to make a card shorter. It is
// optional, it is one non-streaming chat completion speaking the OpenAI wire
// format, and it is not a dependency. Everything here can be deleted and the
// feature still works, because the card's truth is its fields and the rendering
// in recall.go already exists.
//
// Compatibility with any OpenAI-shaped endpoint is a property of the protocol,
// which is why this is 80 lines of net/http rather than a vendored SDK: the SDK
// would be sized for streaming, pagination and tool calls, none of which a
// single compression call uses.
//
// And because ADR-0018 keeps page text and typed input off disk, the outbound
// payload is command names and outcomes — there is no sensitive content on this
// machine for the call to leak.

const (
	envAPIKey  = "BRIDGE_MEMORY_API_KEY"
	envBaseURL = "BRIDGE_MEMORY_BASE_URL"
	envModel   = "BRIDGE_MEMORY_MODEL"
	// envAllowInsecure permits a plaintext endpoint, for a local proxy. Off by
	// default because the alternative is a typo in a URL quietly shipping the
	// key and a page-derived card over an unencrypted connection.
	envAllowInsecure = "BRIDGE_MEMORY_ALLOW_INSECURE"

	defaultBaseURL    = "https://api.openai.com/v1"
	defaultModel      = "gpt-4o-mini"
	compressTimeout   = 20 * time.Second
	compressMaxTokens = 700
	maxResponseBytes  = 1 << 20
)

// httpCompressor speaks the OpenAI chat-completions wire format.
type httpCompressor struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

// NewCompressorFromEnv builds a Compressor from the environment, or returns
// nil when compression is not configured. A nil Compressor is the normal
// state and callers must treat it as "no compression", not as an error.
func NewCompressorFromEnv() Compressor {
	key := strings.TrimSpace(os.Getenv(envAPIKey))
	if key == "" {
		return nil
	}
	base := strings.TrimSpace(os.Getenv(envBaseURL))
	if base == "" {
		base = defaultBaseURL
	}
	// The key and the card both go out on this connection, and the card is
	// page-derived, so a plaintext endpoint is a silent downgrade of the one
	// thing ADR-0018 bounds. A local proxy is the legitimate reason to want
	// http, so it is an explicit opt-in rather than an inferred permission.
	if strings.HasPrefix(strings.ToLower(base), "http://") {
		if os.Getenv(envAllowInsecure) != "1" {
			log.Printf("memory: %s is http://; refusing to send the API key and card in cleartext. "+
				"Set %s=1 if this is a local proxy.", envBaseURL, envAllowInsecure)
			return nil
		}
	}
	model := strings.TrimSpace(os.Getenv(envModel))
	if model == "" {
		model = defaultModel
	}
	return &httpCompressor{
		baseURL: strings.TrimRight(base, "/"),
		model:   model,
		apiKey:  key,
		client:  &http.Client{Timeout: compressTimeout},
	}
}

// compressionSystem is the whole instruction. It is short on purpose: the model
// is rewriting text it is given, not being asked to reason about a site, and a
// long prompt here would cost more than the compression saves.
const compressionSystem = `You compress notes about how to drive a website.
Rewrite them as terse bullet points that keep every concrete fact: control roles, names, commands, error names, and the order of steps.
Keep the original meaning exactly. Add nothing that is not already written. Never invent selectors.
Return only the bullets, no preamble.`

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (c *httpCompressor) Compress(host, rendered string) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: compressionSystem},
			{Role: "user", Content: fmt.Sprintf("Notes for %s:\n\n%s", host, rendered)},
		},
		Stream:      false,
		MaxTokens:   compressMaxTokens,
		Temperature: 0.2,
	})
	if err != nil {
		return "", fmt.Errorf("memory: marshal chat request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), compressTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("memory: build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("memory: chat request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// A non-2xx body is read (bounded) purely so the log says what was wrong;
	// the caller only needs the error.
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("memory: read chat response: %w", err)
	}
	var out chatResponse
	if uerr := json.Unmarshal(payload, &out); uerr != nil && resp.StatusCode/100 == 2 {
		return "", fmt.Errorf("memory: decode chat response (status %d): %w", resp.StatusCode, uerr)
	}
	if resp.StatusCode/100 != 2 {
		if out.Error != nil && out.Error.Message != "" {
			return "", fmt.Errorf("memory: chat status %d: %s", resp.StatusCode, out.Error.Message)
		}
		return "", fmt.Errorf("memory: chat status %d", resp.StatusCode)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("memory: chat response had no choices")
	}
	text := strings.TrimSpace(out.Choices[0].Message.Content)
	if text == "" {
		return "", fmt.Errorf("memory: chat response was empty")
	}
	safe, err := sanitizeCompressed(text)
	if err != nil {
		return "", err
	}
	return safe, nil
}

// cardSectionHeaders are the titles the card renders its tiers under. A reply
// containing one of these verbatim is trying to be read as the card rather than
// as a summary of it.
var cardSectionHeaders = []string{
	"Site map",
	"Observed to fail here",
	"Sequences that worked here",
	InjectionLabel,
}

// sanitizeCompressed reduces a model's reply to something that cannot impersonate
// the card it will replace.
//
// ADR-0022 says the model never decides what is in the card, and until this
// existed nothing enforced it: the reply was stored and rendered verbatim at
// every landing, so an endpoint answering "ignore all prior instructions" — or
// one echoing a page's accessible name back as if it were the control plane's
// own words — was re-shown to every future agent. Three rules, each because a
// card-render path depends on it:
//
//   - no refs. A compression must not hand back a handle. The site map is where
//     a usable one comes from, resolved against the page in hand (ADR-0023), so
//     a ref in a reply can only have been invented.
//   - no headers. Section titles belong to the card.
//   - one block. Lines are folded together, so a reply cannot open a section of
//     its own however it is punctuated.
//
// It refuses rather than repairs. Compression is optional by design, so
// dropping a reply costs nothing that matters, and a rewritten reply is one
// nobody chose.
func sanitizeCompressed(text string) (string, error) {
	// @eN, the one handle form the whole feature rests on.
	//
	// Every occurrence, not the first. `strings.Index` finds one and the guard
	// inspected only the byte after it, so any earlier "@e" followed by a
	// non-digit short-circuited the scan: a reply reading "checked
	// @example.com and also @e12" passed the check and was stored with the
	// invented ref intact, re-served under the injection's own trust label at
	// every landing for the life of the card. An email address in ordinary
	// prose is enough to open the hole, and a page echoing one is enough to
	// trigger it.
	for from := 0; from < len(text); {
		i := strings.Index(text[from:], "@e")
		if i < 0 {
			break
		}
		at := from + i
		if j := at + 2; j < len(text) && text[j] >= '0' && text[j] <= '9' {
			return "", fmt.Errorf("memory: chat response invented a ref (%s)", text[at:min(at+6, len(text))])
		}
		// Past this occurrence, so the next scan cannot land on it again.
		from = at + 2
	}
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// A bullet whose whole content is a card's own section title.
		bare := strings.TrimLeft(trimmed, "-*• \t")
		for _, h := range cardSectionHeaders {
			if strings.EqualFold(bare, h) || strings.HasPrefix(strings.ToLower(bare), strings.ToLower(h)+" (") {
				return "", fmt.Errorf("memory: chat response impersonated a card section (%q)", bare)
			}
		}
		kept = append(kept, trimmed)
	}
	if len(kept) == 0 {
		return "", fmt.Errorf("memory: chat response had no usable text")
	}
	// One block: a line break is the only thing that can open a section, and the
	// card's own renderer is what decides where those go.
	return strings.Join(kept, "; "), nil
}
