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

type part struct {
	Text string `json:"text"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type reqBody struct {
	Contents          []content `json:"contents"`
	SystemInstruction *content  `json:"systemInstruction,omitempty"`
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
	var body reqBody
	var systems []string
	for _, m := range messages {
		switch m.Role {
		case "system":
			systems = append(systems, m.Content)
		case "assistant":
			body.Contents = append(body.Contents, content{Role: "model", Parts: []part{{Text: m.Content}}})
		default:
			body.Contents = append(body.Contents, content{Role: "user", Parts: []part{{Text: m.Content}}})
		}
	}
	if len(systems) > 0 {
		body.SystemInstruction = &content{Parts: []part{{Text: strings.Join(systems, "\n\n")}}}
	}

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
