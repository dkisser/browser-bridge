package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	return text, nil
}
