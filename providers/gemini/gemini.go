// Package gemini is a promptr.Provider for Google's Generative Language API
// (Gemini), built on net/http alone — no vendor SDK. The API key is sent in the
// x-goog-api-key header (never in the URL), so it stays out of logs and history.
//
//	c := gemini.New(os.Getenv("GEMINI_API_KEY"), "gemini-1.5-flash")
package gemini

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

const defaultBaseURL = "https://generativelanguage.googleapis.com"

// Client implements promptr.Provider against Gemini's generateContent endpoint.
type Client struct {
	APIKey  string
	Model   string
	BaseURL string       // defaults to the public API
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

type functionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type functionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

// fnDecl/tool mirror Gemini's tools[].functionDeclarations[] shape. The parameter
// schema is a permissive object carrying our human-readable description; the
// tolerant coerce kernel parses whatever arguments the model returns.
type fnDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type tool struct {
	FunctionDeclarations []fnDecl `json:"functionDeclarations"`
}

type reqBody struct {
	Contents          []content `json:"contents"`
	SystemInstruction *content  `json:"systemInstruction,omitempty"`
	Tools             []tool    `json:"tools,omitempty"`
}

type respBody struct {
	Candidates []struct {
		Content content `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete maps promptr messages to Gemini's content list. Gemini uses the
// roles "user" and "model" (assistant→model) and carries system text in a
// separate systemInstruction field, so promptr's system messages are merged
// there.
func (c *Client) Complete(ctx context.Context, messages []promptr.Message) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("gemini: empty API key")
	}
	body := buildBody(messages)

	buf, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", c.baseURL(), c.Model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-goog-api-key", c.APIKey)

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
		return "", fmt.Errorf("gemini: decode response (%s): %w", resp.Status, jerr)
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error != nil {
			return "", fmt.Errorf("gemini: %s: %s", resp.Status, out.Error.Message)
		}
		return "", fmt.Errorf("gemini: %s", resp.Status)
	}
	if len(out.Candidates) == 0 {
		return "", fmt.Errorf("gemini: response had no candidates")
	}
	var sb strings.Builder
	for _, p := range out.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}
	return sb.String(), nil
}

// buildBody maps promptr messages to a Gemini request. Gemini uses the roles
// "user" and "model" (assistant→model) and carries system text in a separate
// systemInstruction field. Tool turns map to functionCall parts (assistant) and
// functionResponse parts (tool results); Gemini correlates a response to its call
// by tool name, so the name is recovered from the matching assistant tool-call
// turn already present in the conversation.
func buildBody(messages []promptr.Message) reqBody {
	idToName := toolNameByID(messages)
	var body reqBody
	var systems []string
	for _, m := range messages {
		switch {
		case m.Role == "system":
			systems = append(systems, m.Content)
		case len(m.ToolCalls) > 0:
			parts := make([]part, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				parts[i] = part{FunctionCall: &functionCall{Name: tc.Name, Args: toArgsObj(tc.Arguments)}}
			}
			body.Contents = append(body.Contents, content{Role: "model", Parts: parts})
		case m.ToolCallID != "":
			body.Contents = append(body.Contents, content{Role: "user", Parts: []part{{
				FunctionResponse: &functionResponse{Name: idToName[m.ToolCallID], Response: toResponseObj(m.Content)},
			}}})
		case m.Role == "assistant":
			body.Contents = append(body.Contents, content{Role: "model", Parts: []part{{Text: m.Content}}})
		default:
			body.Contents = append(body.Contents, content{Role: "user", Parts: []part{{Text: m.Content}}})
		}
	}
	if len(systems) > 0 {
		body.SystemInstruction = &content{Parts: []part{{Text: strings.Join(systems, "\n\n")}}}
	}
	return body
}

// toolNameByID maps each tool-call ID seen in the conversation to its function
// name, so a later functionResponse (which Gemini keys by name) can be filled in.
func toolNameByID(messages []promptr.Message) map[string]string {
	m := map[string]string{}
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			m[tc.ID] = tc.Name
		}
	}
	return m
}

// toArgsObj parses a tool-call argument JSON string into an object, or nil.
func toArgsObj(s string) map[string]any {
	if s == "" {
		return nil
	}
	var obj map[string]any
	_ = json.Unmarshal([]byte(s), &obj)
	return obj
}

// toResponseObj shapes a tool result (any JSON) into the object Gemini's
// functionResponse.response field requires, wrapping non-objects under "result".
func toResponseObj(s string) map[string]any {
	var obj map[string]any
	if json.Unmarshal([]byte(s), &obj) == nil {
		return obj
	}
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return map[string]any{"result": v}
	}
	return map[string]any{"result": s}
}

// CompleteTools sends the conversation plus the available tools and returns
// either the model's final text or the tool calls it wants run. It implements
// promptr.ToolProvider; the agent loop in the runtime drives the dispatch.
func (c *Client) CompleteTools(ctx context.Context, messages []promptr.Message, tools []promptr.ToolDef) (promptr.Reply, error) {
	if c.APIKey == "" {
		return promptr.Reply{}, fmt.Errorf("gemini: empty API key")
	}
	body := buildBody(messages)
	if len(tools) > 0 {
		decls := make([]fnDecl, len(tools))
		for i, d := range tools {
			decls[i] = fnDecl{Name: d.Name, Description: d.Description, Parameters: map[string]any{"type": "object", "description": d.Params}}
		}
		body.Tools = []tool{{FunctionDeclarations: decls}}
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return promptr.Reply{}, err
	}
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", c.baseURL(), c.Model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return promptr.Reply{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-goog-api-key", c.APIKey)

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
		return promptr.Reply{}, fmt.Errorf("gemini: decode response (%s): %w", resp.Status, jerr)
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error != nil {
			return promptr.Reply{}, fmt.Errorf("gemini: %s: %s", resp.Status, out.Error.Message)
		}
		return promptr.Reply{}, fmt.Errorf("gemini: %s", resp.Status)
	}
	if len(out.Candidates) == 0 {
		return promptr.Reply{}, fmt.Errorf("gemini: response had no candidates")
	}

	var calls []promptr.ToolCall
	var text strings.Builder
	for i, p := range out.Candidates[0].Content.Parts {
		switch {
		case p.FunctionCall != nil:
			args, _ := json.Marshal(p.FunctionCall.Args)
			calls = append(calls, promptr.ToolCall{
				ID:        fmt.Sprintf("%s-%d", p.FunctionCall.Name, i),
				Name:      p.FunctionCall.Name,
				Arguments: string(args),
			})
		default:
			text.WriteString(p.Text)
		}
	}
	if len(calls) > 0 {
		return promptr.Reply{Calls: calls}, nil
	}
	return promptr.Reply{Text: text.String()}, nil
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
