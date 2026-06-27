// Package ollama is a promptr.Provider for a local Ollama server, built on
// net/http alone — no vendor SDK. It targets the /api/chat endpoint with
// streaming disabled, so a whole reply comes back in one response.
//
//	c := ollama.New("llama3.2") // talks to http://localhost:11434 by default
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zkrebbekx/promptr"
)

const defaultBaseURL = "http://localhost:11434"

// Client implements promptr.Provider against an Ollama server.
type Client struct {
	Model   string
	BaseURL string       // defaults to http://localhost:11434
	HTTP    *http.Client // defaults to a client with a 120s timeout (local models can be slow)
}

// New returns a Client for the given local model, using the default server URL.
func New(model string) *Client {
	return &Client{
		Model:   model,
		BaseURL: defaultBaseURL,
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

// apiToolCall is Ollama's OpenAI-compatible tool-call shape; Arguments is a JSON
// object (not a string), kept raw so it round-trips through promptr unchanged.
type apiToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	} `json:"function"`
}

type apiMsg struct {
	Role      string        `json:"role"`
	Content   string        `json:"content,omitempty"`
	ToolCalls []apiToolCall `json:"tool_calls,omitempty"`
	ToolName  string        `json:"tool_name,omitempty"` // names the tool a role:"tool" result answers
}

// apiTool mirrors Ollama's OpenAI-compatible tools[] entry. The parameter schema
// is a permissive object carrying our description; coerce parses the arguments.
type apiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Parameters  map[string]any `json:"parameters,omitempty"`
	} `json:"function"`
}

type reqBody struct {
	Model    string    `json:"model"`
	Messages []apiMsg  `json:"messages"`
	Stream   bool      `json:"stream"`
	Tools    []apiTool `json:"tools,omitempty"`
}

type respBody struct {
	Message struct {
		Content   string        `json:"content"`
		ToolCalls []apiToolCall `json:"tool_calls"`
	} `json:"message"`
	Error string `json:"error"`
}

// Complete sends the conversation to /api/chat (stream=false) and returns the
// assistant message content. Ollama accepts system/user/assistant roles as-is.
func (c *Client) Complete(ctx context.Context, messages []promptr.Message) (string, error) {
	body := reqBody{Model: c.Model, Messages: buildMessages(messages)}

	buf, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}

	var out respBody
	if jerr := json.Unmarshal(raw, &out); jerr != nil {
		return "", fmt.Errorf("ollama: decode response (%s): %w", resp.Status, jerr)
	}
	if resp.StatusCode != http.StatusOK || out.Error != "" {
		if out.Error != "" {
			return "", fmt.Errorf("ollama: %s: %s", resp.Status, out.Error)
		}
		return "", fmt.Errorf("ollama: %s", resp.Status)
	}
	return out.Message.Content, nil
}

// buildMessages maps promptr messages to Ollama's chat messages. System/user/
// assistant text map across as-is; an assistant tool-call turn becomes tool_calls
// (arguments embedded as a raw JSON object), and a role:"tool" result carries the
// answering tool's name (recovered from the matching call) so Ollama can pair it.
func buildMessages(messages []promptr.Message) []apiMsg {
	idToName := toolNameByID(messages)
	out := make([]apiMsg, 0, len(messages))
	for _, m := range messages {
		switch {
		case len(m.ToolCalls) > 0:
			am := apiMsg{Role: m.Role, Content: m.Content}
			am.ToolCalls = make([]apiToolCall, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				am.ToolCalls[i].Function.Name = tc.Name
				if tc.Arguments != "" {
					am.ToolCalls[i].Function.Arguments = json.RawMessage(tc.Arguments)
				}
			}
			out = append(out, am)
		case m.ToolCallID != "":
			out = append(out, apiMsg{Role: "tool", Content: m.Content, ToolName: idToName[m.ToolCallID]})
		default:
			out = append(out, apiMsg{Role: m.Role, Content: m.Content})
		}
	}
	return out
}

// toolNameByID maps each tool-call ID in the conversation to its function name,
// so a later role:"tool" result can name the tool it answers.
func toolNameByID(messages []promptr.Message) map[string]string {
	m := map[string]string{}
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			m[tc.ID] = tc.Name
		}
	}
	return m
}

// CompleteTools sends the conversation plus the available tools to /api/chat and
// returns either the model's final text or the tool calls it wants run. It
// implements promptr.ToolProvider; the runtime's agent loop drives the dispatch.
func (c *Client) CompleteTools(ctx context.Context, messages []promptr.Message, tools []promptr.ToolDef) (promptr.Reply, error) {
	body := reqBody{Model: c.Model, Messages: buildMessages(messages)}
	for _, d := range tools {
		var t apiTool
		t.Type = "function"
		t.Function.Name = d.Name
		t.Function.Description = d.Description
		t.Function.Parameters = map[string]any{"type": "object", "description": d.Params}
		body.Tools = append(body.Tools, t)
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return promptr.Reply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return promptr.Reply{}, err
	}
	req.Header.Set("content-type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return promptr.Reply{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return promptr.Reply{}, err
	}

	var out respBody
	if jerr := json.Unmarshal(raw, &out); jerr != nil {
		return promptr.Reply{}, fmt.Errorf("ollama: decode response (%s): %w", resp.Status, jerr)
	}
	if resp.StatusCode != http.StatusOK || out.Error != "" {
		if out.Error != "" {
			return promptr.Reply{}, fmt.Errorf("ollama: %s: %s", resp.Status, out.Error)
		}
		return promptr.Reply{}, fmt.Errorf("ollama: %s", resp.Status)
	}

	if len(out.Message.ToolCalls) > 0 {
		calls := make([]promptr.ToolCall, len(out.Message.ToolCalls))
		for i, tc := range out.Message.ToolCalls {
			calls[i] = promptr.ToolCall{
				ID:        fmt.Sprintf("%s-%d", tc.Function.Name, i),
				Name:      tc.Function.Name,
				Arguments: string(tc.Function.Arguments),
			}
		}
		return promptr.Reply{Calls: calls}, nil
	}
	return promptr.Reply{Text: out.Message.Content}, nil
}

func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP == nil {
		return http.DefaultClient
	}
	return c.HTTP
}

// static assertions: Client satisfies the runtime contracts.
var (
	_ promptr.Provider     = (*Client)(nil)
	_ promptr.ToolProvider = (*Client)(nil)
)
