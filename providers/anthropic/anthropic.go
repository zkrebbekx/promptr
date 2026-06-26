// Package anthropic is a promptr.Provider backed by the Anthropic Messages API,
// built on net/http alone — no vendor SDK, in keeping with promptr's zero-SDK
// core. Import it only if you want this backend; the core never does.
//
//	c := anthropic.New(os.Getenv("ANTHROPIC_API_KEY"), "claude-opus-4-8")
//	ticket, err := ExtractTicket(ctx, c, "my server is down")
package anthropic

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
	Messages  []apiMsg `json:"messages"`
}

type apiMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	var systems []string
	for _, m := range messages {
		if m.Role == "system" {
			systems = append(systems, m.Content)
			continue
		}
		body.Messages = append(body.Messages, apiMsg{Role: m.Role, Content: m.Content})
	}
	body.System = strings.Join(systems, "\n\n")

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

// static assertion: Client satisfies the runtime contract.
var _ promptr.Provider = (*Client)(nil)
