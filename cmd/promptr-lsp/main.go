// Command promptr-lsp is a minimal Language Server for .promptr files. It speaks
// just enough of the protocol to publish diagnostics as you type: syntax errors
// from the error-tolerant parser plus the semantic checks in dsl.Validate
// (unresolved types/clients, bad unions, mismatched test blocks).
//
// Scope is deliberately small — diagnostics only. Hover, completion and
// go-to-definition are future work; the parser and validator already expose
// everything those would need.
//
// Wire it into an editor as the language server for the "promptr" language over
// stdio. Example (Neovim):
//
//	vim.lsp.start({ name = "promptr", cmd = { "promptr-lsp" } })
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/zkrebbekx/promptr/dsl"
)

func main() {
	srv := &server{out: bufio.NewWriter(os.Stdout)}
	if err := srv.run(os.Stdin); err != nil && err != io.EOF {
		fmt.Fprintln(os.Stderr, "promptr-lsp:", err)
		os.Exit(1)
	}
}

type server struct {
	out *bufio.Writer
}

// --- JSON-RPC 2.0 framing over stdio (LSP base protocol) ---

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
}

func (s *server) run(r io.Reader) error {
	br := bufio.NewReader(r)
	for {
		body, err := readMessage(br)
		if err != nil {
			return err
		}
		var msg rpcMessage
		if json.Unmarshal(body, &msg) != nil {
			continue
		}
		if s.handle(msg) {
			return nil // exit
		}
	}
}

// readMessage reads one Content-Length-framed JSON-RPC payload.
func readMessage(br *bufio.Reader) ([]byte, error) {
	length := 0
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	buf := make([]byte, length)
	_, err := io.ReadFull(br, buf)
	return buf, err
}

func (s *server) write(v any) {
	body, _ := json.Marshal(v)
	_, _ = fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n", len(body))
	_, _ = s.out.Write(body)
	_ = s.out.Flush()
}

func (s *server) reply(id json.RawMessage, result any) {
	s.write(rpcMessage{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *server) notify(method string, params any) {
	raw, _ := json.Marshal(params)
	s.write(rpcMessage{JSONRPC: "2.0", Method: method, Params: raw})
}

// handle dispatches one message; it returns true when the client asked to exit.
func (s *server) handle(msg rpcMessage) bool {
	switch msg.Method {
	case "initialize":
		s.reply(msg.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync": 1, // full document sync
			},
			"serverInfo": map[string]any{"name": "promptr-lsp"},
		})
	case "initialized":
		// no-op
	case "textDocument/didOpen":
		s.publishFromParams(msg.Params, "textDocument", "text")
	case "textDocument/didChange":
		s.publishChange(msg.Params)
	case "shutdown":
		s.reply(msg.ID, nil)
	case "exit":
		return true
	}
	return false
}

type didOpenParams struct {
	TextDocument struct {
		URI  string `json:"uri"`
		Text string `json:"text"`
	} `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

func (s *server) publishFromParams(raw json.RawMessage, _, _ string) {
	var p didOpenParams
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	s.notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         p.TextDocument.URI,
		"diagnostics": Diagnose(p.TextDocument.Text),
	})
}

func (s *server) publishChange(raw json.RawMessage) {
	var p didChangeParams
	if json.Unmarshal(raw, &p) != nil || len(p.ContentChanges) == 0 {
		return
	}
	// Full sync: the last change holds the whole document.
	text := p.ContentChanges[len(p.ContentChanges)-1].Text
	s.notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         p.TextDocument.URI,
		"diagnostics": Diagnose(text),
	})
}

// --- diagnostics computation (independently testable) ---

// lspDiagnostic is the subset of the LSP Diagnostic shape we emit.
type lspDiagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity"` // 1 = error
	Source   string   `json:"source"`
	Message  string   `json:"message"`
}

type lspRange struct {
	Start lspPos `json:"start"`
	End   lspPos `json:"end"`
}

type lspPos struct {
	Line      int `json:"line"`      // zero-based
	Character int `json:"character"` // zero-based
}

// Diagnose parses and validates .promptr source and returns LSP diagnostics:
// syntax errors from Parse plus semantic findings from dsl.Validate. It never
// panics on partial input — the parser is error-tolerant by design.
func Diagnose(text string) []lspDiagnostic {
	out := []lspDiagnostic{}
	f, perr := dsl.Parse(text)
	if perr != nil {
		for _, line := range strings.Split(perr.Error(), "; ") {
			ln, msg := splitLinePrefix(line)
			out = append(out, diag(ln, msg))
		}
	}
	for _, d := range dsl.Validate(f) {
		out = append(out, diag(d.Line, d.Msg))
	}
	return out
}

// splitLinePrefix parses a "line N: message" parse error into its 1-based line
// and bare message, falling back to line 1 when the prefix is absent.
func splitLinePrefix(s string) (int, string) {
	rest, ok := strings.CutPrefix(s, "line ")
	if !ok {
		return 1, s
	}
	i := strings.Index(rest, ":")
	if i < 0 {
		return 1, s
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest[:i]))
	if err != nil {
		return 1, s
	}
	return n, strings.TrimSpace(rest[i+1:])
}

// diag builds an error diagnostic spanning the whole 1-based source line.
func diag(line int, msg string) lspDiagnostic {
	row := line - 1
	if row < 0 {
		row = 0
	}
	return lspDiagnostic{
		Range:    lspRange{Start: lspPos{Line: row, Character: 0}, End: lspPos{Line: row, Character: 200}},
		Severity: 1,
		Source:   "promptr",
		Message:  msg,
	}
}
