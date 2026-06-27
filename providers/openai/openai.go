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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	Model    string   `json:"model"`
	Messages []apiMsg `json:"messages"`
}

type apiMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type respBody struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
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
	body := reqBody{Model: c.Model}
	for _, m := range messages {
		body.Messages = append(body.Messages, apiMsg{Role: m.Role, Content: m.Content})
	}

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

// static assertion: Client satisfies the runtime contract.
var _ promptr.Provider = (*Client)(nil)
