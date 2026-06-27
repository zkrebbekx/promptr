// Package anthropic is a promptr.Provider backed by the Anthropic Messages API,
// built on net/http alone — no vendor SDK, in keeping with promptr's zero-SDK
// core. Import it only if you want this backend; the core never does.
//
//	c := anthropic.New(os.Getenv("ANTHROPIC_API_KEY"), "claude-opus-4-8")
//	ticket, err := ExtractTicket(ctx, c, "my server is down")
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zkrebbekx/promptr"
)

const (
	defaultBaseURL   = "https://api.anthropic.com"
	defaultVersion   = "2023-06-01"
	defaultMaxTokens = 1024
)

// Client implements promptr.Provider. The zero value is not usable; build one
// with New and adjust exported fields as needed before first use.
type Client struct {
	APIKey    string
	Model     string
	MaxTokens int
	BaseURL   string       // defaults to the public API
	Version   string       // anthropic-version header
	HTTP      *http.Client // defaults to a client with a 60s timeout
}

// New returns a Client with sensible defaults for the given key and model.
func New(apiKey, model string) *Client {
	return &Client{
		APIKey:    apiKey,
		Model:     model,
		MaxTokens: defaultMaxTokens,
		BaseURL:   defaultBaseURL,
		Version:   defaultVersion,
		HTTP:      &http.Client{Timeout: 60 * time.Second},
	}
}

type reqBody struct {
	Model     string   `json:"model"`
	MaxTokens int      `json:"max_tokens"`
	System    string   `json:"system,omitempty"`
	Stream    bool     `json:"stream,omitempty"`
	Messages  []apiMsg `json:"messages"`
}

// apiMsg.Content is `any`: a plain string for the text-only fast path, or an
// array of content blocks for multimodal turns.
type apiMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// splitSystem merges promptr "system" turns into the API's top-level system
// field and returns the remaining user/assistant turns as content blocks.
func splitSystem(messages []promptr.Message) (string, []apiMsg) {
	var systems []string
	out := make([]apiMsg, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			systems = append(systems, m.Content)
			continue
		}
		if len(m.Parts) == 0 {
			out = append(out, apiMsg{Role: m.Role, Content: m.Content})
			continue
		}
		out = append(out, apiMsg{Role: m.Role, Content: contentParts(m.Parts)})
	}
	return strings.Join(systems, "\n\n"), out
}

// contentParts maps promptr parts to Anthropic content blocks.
func contentParts(parts []promptr.Part) []map[string]any {
	out := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		switch p.Kind {
		case promptr.PartImage:
			out = append(out, map[string]any{"type": "image", "source": imageSource(p)})
		default:
			out = append(out, map[string]any{"type": "text", "text": p.Text})
		}
	}
	return out
}

// imageSource builds a base64 source for inline bytes or a url source.
func imageSource(p promptr.Part) map[string]any {
	if len(p.Data) > 0 {
		mime := p.MIME
		if mime == "" {
			mime = "image/png"
		}
		return map[string]any{
			"type":       "base64",
			"media_type": mime,
			"data":       base64.StdEncoding.EncodeToString(p.Data),
		}
	}
	return map[string]any{"type": "url", "url": p.URL}
}

type respBody struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends the conversation to the Messages API and returns the
// concatenated text of the reply. promptr's "system" messages are merged into
// the API's top-level system field; user/assistant turns are sent as-is.
func (c *Client) Complete(ctx context.Context, messages []promptr.Message) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("anthropic: empty API key")
	}
	body := reqBody{Model: c.Model, MaxTokens: c.maxTokens()}
	body.System, body.Messages = splitSystem(messages)

	buf, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", c.version())

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var out respBody
	if jerr := json.Unmarshal(raw, &out); jerr != nil {
		return "", fmt.Errorf("anthropic: decode response (%s): %w", resp.Status, jerr)
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error != nil {
			return "", fmt.Errorf("anthropic: %s: %s", resp.Status, out.Error.Message)
		}
		return "", fmt.Errorf("anthropic: %s", resp.Status)
	}

	var sb strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String(), nil
}

// streamEvent is one decoded SSE `data:` payload from the Messages stream. We
// only care about text deltas; other event types are ignored.
type streamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

// Stream sends the conversation with stream:true and yields each text delta as
// it arrives, parsing the server-sent events. It implements
// promptr.StreamProvider.
func (c *Client) Stream(ctx context.Context, messages []promptr.Message) (<-chan string, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("anthropic: empty API key")
	}
	body := reqBody{Model: c.Model, MaxTokens: c.maxTokens(), Stream: true}
	body.System, body.Messages = splitSystem(messages)
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", c.version())
	req.Header.Set("accept", "text/event-stream")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("anthropic: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	out := make(chan string)
	go func() {
		defer close(out)
		defer func() { _ = resp.Body.Close() }()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "data:")
			if !ok {
				continue
			}
			var ev streamEvent
			if json.Unmarshal([]byte(strings.TrimSpace(data)), &ev) != nil {
				continue
			}
			if ev.Type == "message_stop" {
				return
			}
			if ev.Delta.Text != "" {
				select {
				case out <- ev.Delta.Text:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (c *Client) maxTokens() int {
	if c.MaxTokens <= 0 {
		return defaultMaxTokens
	}
	return c.MaxTokens
}

func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *Client) version() string {
	if c.Version == "" {
		return defaultVersion
	}
	return c.Version
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP == nil {
		return http.DefaultClient
	}
	return c.HTTP
}

// static assertions: Client satisfies both runtime contracts.
var (
	_ promptr.Provider       = (*Client)(nil)
	_ promptr.StreamProvider = (*Client)(nil)
)
