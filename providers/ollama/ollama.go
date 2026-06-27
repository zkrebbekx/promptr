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

type apiMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type reqBody struct {
	Model    string   `json:"model"`
	Messages []apiMsg `json:"messages"`
	Stream   bool     `json:"stream"`
}

type respBody struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error"`
}

// Complete sends the conversation to /api/chat (stream=false) and returns the
// assistant message content. Ollama accepts system/user/assistant roles as-is.
func (c *Client) Complete(ctx context.Context, messages []promptr.Message) (string, error) {
	body := reqBody{Model: c.Model}
	for _, m := range messages {
		body.Messages = append(body.Messages, apiMsg{Role: m.Role, Content: m.Content})
	}

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

// static assertion: Client satisfies the runtime contract.
var _ promptr.Provider = (*Client)(nil)
