// Package openai is a promptr.Provider for any OpenAI Chat Completions-compatible
// API, built on net/http alone — no vendor SDK. Because the endpoint shape is a
// de-facto standard, one adapter covers OpenAI, Azure OpenAI, Groq, Together,
// OpenRouter, and most local servers (llama.cpp, vLLM, LM Studio): point BaseURL
// at the server and go.
//
//	c := openai.New(os.Getenv("OPENAI_API_KEY"), "gpt-4o")
//	c.BaseURL = "https://api.groq.com/openai" // or any compatible host
package openai

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
	defaultBaseURL = "https://api.openai.com"
)

// Client implements promptr.Provider against a Chat Completions endpoint. Build
// one with New; adjust exported fields before first use.
type Client struct {
	APIKey  string
	Model   string
	BaseURL string       // defaults to the public OpenAI API
	HTTP    *http.Client // defaults to a client with a 60s timeout
}

// New returns a Client with sensible defaults for the given key and model.
func New(apiKey, model string) *Client {
	return &Client{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: defaultBaseURL,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

type reqBody struct {
	Model    string    `json:"model"`
	Messages []apiMsg  `json:"messages"`
	Stream   bool      `json:"stream,omitempty"`
	Tools    []apiTool `json:"tools,omitempty"`
}

// apiMsg.Content is `any` because the Chat Completions API accepts either a
// plain string (text-only) or an array of typed content parts (multimodal). For
// tool turns it also carries tool_calls (assistant) or tool_call_id (tool role).
type apiMsg struct {
	Role       string        `json:"role"`
	Content    any           `json:"content"`
	ToolCalls  []apiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

// apiTool is one entry in the request's `tools` array (a callable function).
type apiTool struct {
	Type     string          `json:"type"` // always "function"
	Function apiToolFunction `json:"function"`
}

type apiToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

// apiToolCall is a function call the model requested (response) or that we echo
// back (request) on the assistant turn.
type apiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// buildMessages maps promptr messages to the API shape: a content array for
// multimodal Parts, tool_calls for an assistant tool-call turn, or tool_call_id
// for a tool result; otherwise a plain string content.
func buildMessages(messages []promptr.Message) []apiMsg {
	out := make([]apiMsg, 0, len(messages))
	for _, m := range messages {
		switch {
		case len(m.ToolCalls) > 0:
			out = append(out, apiMsg{Role: m.Role, ToolCalls: toAPIToolCalls(m.ToolCalls)})
		case m.ToolCallID != "":
			out = append(out, apiMsg{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID})
		case len(m.Parts) == 0:
			out = append(out, apiMsg{Role: m.Role, Content: m.Content})
		default:
			out = append(out, apiMsg{Role: m.Role, Content: contentParts(m.Parts)})
		}
	}
	return out
}

// toAPITools maps promptr tool definitions to the request's tools array. The
// parameter schema is passed as a permissive object schema carrying our
// human-readable description; the tolerant coerce kernel parses whatever
// arguments the model returns.
func toAPITools(defs []promptr.ToolDef) []apiTool {
	out := make([]apiTool, len(defs))
	for i, d := range defs {
		fn := apiToolFunction{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  map[string]any{"type": "object", "description": d.Params},
		}
		out[i] = apiTool{Type: "function", Function: fn}
	}
	return out
}

func toAPIToolCalls(calls []promptr.ToolCall) []apiToolCall {
	out := make([]apiToolCall, len(calls))
	for i, c := range calls {
		tc := apiToolCall{ID: c.ID, Type: "function"}
		tc.Function.Name = c.Name
		tc.Function.Arguments = c.Arguments
		out[i] = tc
	}
	return out
}

func contentParts(parts []promptr.Part) []map[string]any {
	out := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		switch p.Kind {
		case promptr.PartImage:
			out = append(out, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": imageURL(p)},
			})
		default: // text (and any kind this endpoint can't carry) → text
			out = append(out, map[string]any{"type": "text", "text": p.Text})
		}
	}
	return out
}

// imageURL returns a data: URL for inline image bytes, or the Part's URL.
func imageURL(p promptr.Part) string {
	if len(p.Data) > 0 {
		mime := p.MIME
		if mime == "" {
			mime = "image/png"
		}
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(p.Data)
	}
	return p.URL
}

type respBody struct {
	Choices []struct {
		Message struct {
			Content   string        `json:"content"`
			ToolCalls []apiToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends the conversation to /v1/chat/completions and returns the text
// of the first choice. OpenAI takes system/user/assistant roles directly, so
// promptr's messages map across unchanged.
func (c *Client) Complete(ctx context.Context, messages []promptr.Message) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("openai: empty API key")
	}
	body := reqBody{Model: c.Model, Messages: buildMessages(messages)}

	buf, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+c.APIKey)

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
		return "", fmt.Errorf("openai: decode response (%s): %w", resp.Status, jerr)
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error != nil {
			return "", fmt.Errorf("openai: %s: %s", resp.Status, out.Error.Message)
		}
		return "", fmt.Errorf("openai: %s", resp.Status)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai: response had no choices")
	}
	return out.Choices[0].Message.Content, nil
}

// CompleteTools sends the conversation plus the available tools and returns
// either the model's final text or the tool calls it wants run. It implements
// promptr.ToolProvider; the agent loop in the runtime drives the dispatch.
func (c *Client) CompleteTools(ctx context.Context, messages []promptr.Message, tools []promptr.ToolDef) (promptr.Reply, error) {
	if c.APIKey == "" {
		return promptr.Reply{}, fmt.Errorf("openai: empty API key")
	}
	body := reqBody{Model: c.Model, Messages: buildMessages(messages), Tools: toAPITools(tools)}
	buf, err := json.Marshal(body)
	if err != nil {
		return promptr.Reply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return promptr.Reply{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+c.APIKey)

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
		return promptr.Reply{}, fmt.Errorf("openai: decode response (%s): %w", resp.Status, jerr)
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error != nil {
			return promptr.Reply{}, fmt.Errorf("openai: %s: %s", resp.Status, out.Error.Message)
		}
		return promptr.Reply{}, fmt.Errorf("openai: %s", resp.Status)
	}
	if len(out.Choices) == 0 {
		return promptr.Reply{}, fmt.Errorf("openai: response had no choices")
	}

	msg := out.Choices[0].Message
	if len(msg.ToolCalls) > 0 {
		calls := make([]promptr.ToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			calls[i] = promptr.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments}
		}
		return promptr.Reply{Calls: calls}, nil
	}
	return promptr.Reply{Text: msg.Content}, nil
}

// streamChunk is one server-sent delta in a streaming completion.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// Stream sends the conversation with stream:true and yields each token delta as
// it arrives, parsing the server-sent `data:` events. It implements
// promptr.StreamProvider. The channel closes when the model emits [DONE] or the
// response ends.
func (c *Client) Stream(ctx context.Context, messages []promptr.Message) (<-chan string, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("openai: empty API key")
	}
	body := reqBody{Model: c.Model, Messages: buildMessages(messages), Stream: true}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+c.APIKey)
	req.Header.Set("accept", "text/event-stream")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("openai: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	out := make(chan string)
	go func() {
		defer close(out)
		defer func() { _ = resp.Body.Close() }()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			data, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
			data = strings.TrimSpace(data)
			if data == "[DONE]" {
				return
			}
			var ch streamChunk
			if json.Unmarshal([]byte(data), &ch) != nil || len(ch.Choices) == 0 {
				continue
			}
			if delta := ch.Choices[0].Delta.Content; delta != "" {
				select {
				case out <- delta:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return defaultBaseURL
	}
	return trimSlash(c.BaseURL)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP == nil {
		return http.DefaultClient
	}
	return c.HTTP
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// static assertions: Client satisfies all three runtime contracts.
var (
	_ promptr.Provider       = (*Client)(nil)
	_ promptr.StreamProvider = (*Client)(nil)
	_ promptr.ToolProvider   = (*Client)(nil)
)
