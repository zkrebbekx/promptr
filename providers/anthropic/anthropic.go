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
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Stream    bool            `json:"stream,omitempty"`
	Tools     []anthropicTool `json:"tools,omitempty"`
	Messages  []apiMsg        `json:"messages"`
}

// anthropicTool is one entry in the request's `tools` array. input_schema is a
// JSON Schema object; we pass a permissive object schema carrying the
// human-readable parameter description, and let the coerce kernel parse whatever
// arguments come back.
type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

func toAnthropicTools(defs []promptr.ToolDef) []anthropicTool {
	out := make([]anthropicTool, len(defs))
	for i, d := range defs {
		out[i] = anthropicTool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: map[string]any{"type": "object", "description": d.Params},
		}
	}
	return out
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
		switch {
		case m.Role == "system":
			systems = append(systems, m.Content)
		case len(m.ToolCalls) > 0:
			// The assistant turn that requested tools must be echoed back as
			// tool_use content blocks.
			out = append(out, apiMsg{Role: "assistant", Content: toolUseBlocks(m.ToolCalls)})
		case m.ToolCallID != "":
			// A tool result is sent as a user turn carrying a tool_result block.
			out = append(out, apiMsg{Role: "user", Content: []map[string]any{{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     m.Content,
			}}})
		case len(m.Parts) == 0:
			out = append(out, apiMsg{Role: m.Role, Content: m.Content})
		default:
			out = append(out, apiMsg{Role: m.Role, Content: contentParts(m.Parts)})
		}
	}
	return strings.Join(systems, "\n\n"), out
}

// toolUseBlocks maps requested tool calls to Anthropic tool_use content blocks.
// The raw JSON arguments are embedded as the block's `input` object.
func toolUseBlocks(calls []promptr.ToolCall) []map[string]any {
	out := make([]map[string]any, len(calls))
	for i, c := range calls {
		var input any = map[string]any{}
		if strings.TrimSpace(c.Arguments) != "" {
			input = json.RawMessage(c.Arguments)
		}
		out[i] = map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": input}
	}
	return out
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
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`    // tool_use block id
		Name  string          `json:"name"`  // tool_use block name
		Input json.RawMessage `json:"input"` // tool_use block arguments
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

// CompleteTools sends the conversation plus the available tools and returns
// either the model's final text or the tool_use calls it requested. It
// implements promptr.ToolProvider; the runtime's agent loop drives dispatch.
func (c *Client) CompleteTools(ctx context.Context, messages []promptr.Message, tools []promptr.ToolDef) (promptr.Reply, error) {
	if c.APIKey == "" {
		return promptr.Reply{}, fmt.Errorf("anthropic: empty API key")
	}
	body := reqBody{Model: c.Model, MaxTokens: c.maxTokens(), Tools: toAnthropicTools(tools)}
	body.System, body.Messages = splitSystem(messages)

	buf, err := json.Marshal(body)
	if err != nil {
		return promptr.Reply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return promptr.Reply{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", c.version())

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return promptr.Reply{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return promptr.Reply{}, err
	}

	var out respBody
	if jerr := json.Unmarshal(raw, &out); jerr != nil {
		return promptr.Reply{}, fmt.Errorf("anthropic: decode response (%s): %w", resp.Status, jerr)
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error != nil {
			return promptr.Reply{}, fmt.Errorf("anthropic: %s: %s", resp.Status, out.Error.Message)
		}
		return promptr.Reply{}, fmt.Errorf("anthropic: %s", resp.Status)
	}

	var sb strings.Builder
	var calls []promptr.ToolCall
	for _, block := range out.Content {
		switch block.Type {
		case "text":
			sb.WriteString(block.Text)
		case "tool_use":
			calls = append(calls, promptr.ToolCall{ID: block.ID, Name: block.Name, Arguments: string(block.Input)})
		}
	}
	if len(calls) > 0 {
		return promptr.Reply{Calls: calls}, nil
	}
	return promptr.Reply{Text: sb.String()}, nil
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

// static assertions: Client satisfies all three runtime contracts.
var (
	_ promptr.Provider       = (*Client)(nil)
	_ promptr.StreamProvider = (*Client)(nil)
	_ promptr.ToolProvider   = (*Client)(nil)
)
