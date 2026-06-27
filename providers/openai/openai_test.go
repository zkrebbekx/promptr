package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/zkrebbekx/promptr"
)

func TestComplete(t *testing.T) {
	Convey("Given a stub Chat Completions API and a configured client", t, func() {
		var gotReq reqBody
		var gotAuth string
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("authorization")
			gotPath = r.URL.Path
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotReq)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hi there"}}]}`)
		}))
		defer srv.Close()

		c := New("sk-test", "gpt-4o")
		c.BaseURL = srv.URL

		Convey("When Complete is called with system and user messages", func() {
			got, err := c.Complete(context.Background(), []promptr.Message{
				{Role: "system", Content: "be terse"},
				{Role: "user", Content: "hi"},
			})

			Convey("Then the first choice's content is returned", func() {
				So(err, ShouldBeNil)
				So(got, ShouldEqual, "hi there")
			})
			Convey("Then it posts to the chat/completions path with a Bearer token", func() {
				So(gotPath, ShouldEqual, "/v1/chat/completions")
				So(gotAuth, ShouldEqual, "Bearer sk-test")
			})
			Convey("Then all roles pass through unchanged", func() {
				So(gotReq.Model, ShouldEqual, "gpt-4o")
				So(gotReq.Messages, ShouldHaveLength, 2)
				So(gotReq.Messages[0].Role, ShouldEqual, "system")
				So(gotReq.Messages[1].Role, ShouldEqual, "user")
			})
		})
	})

	Convey("Given a server returning an API error", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"bad key"}}`)
		}))
		defer srv.Close()
		c := New("sk-test", "m")
		c.BaseURL = srv.URL

		Convey("When Complete is called, the error surfaces status and message", func() {
			_, err := c.Complete(context.Background(), []promptr.Message{{Role: "user", Content: "hi"}})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "401")
			So(err.Error(), ShouldContainSubstring, "bad key")
		})
	})

	Convey("Given a client with no API key", t, func() {
		c := New("", "m")
		Convey("When Complete is called, it fails fast without a request", func() {
			_, err := c.Complete(context.Background(), nil)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "empty API key")
		})
	})
}

func TestBuildMessagesMultimodal(t *testing.T) {
	Convey("Given a stub API capturing the raw request body", t, func() {
		var raw map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &raw)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
		}))
		defer srv.Close()
		c := New("sk-test", "gpt-4o")
		c.BaseURL = srv.URL

		Convey("When a user message carries an inline image part", func() {
			_, err := c.Complete(context.Background(), []promptr.Message{{
				Role:  "user",
				Parts: []promptr.Part{promptr.TextPart("what is this?"), promptr.ImagePart("image/png", []byte{1, 2, 3})},
			}})
			So(err, ShouldBeNil)

			Convey("Then the content is an array with a text and an image_url part", func() {
				msgs := raw["messages"].([]any)
				content := msgs[0].(map[string]any)["content"].([]any)
				So(content, ShouldHaveLength, 2)
				So(content[0].(map[string]any)["type"], ShouldEqual, "text")
				img := content[1].(map[string]any)
				So(img["type"], ShouldEqual, "image_url")
				url := img["image_url"].(map[string]any)["url"].(string)
				So(url, ShouldStartWith, "data:image/png;base64,")
			})
		})
	})
}

func TestCompleteTools(t *testing.T) {
	Convey("Given a stub API that returns a tool call", t, func() {
		var gotReq reqBody
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotReq)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"GetWeather","arguments":"{\"city\":\"Oslo\"}"}}]}}]}`)
		}))
		defer srv.Close()
		c := New("sk-test", "gpt-4o")
		c.BaseURL = srv.URL

		Convey("When CompleteTools is called with a tool definition", func() {
			reply, err := c.CompleteTools(context.Background(),
				[]promptr.Message{{Role: "user", Content: "weather in Oslo?"}},
				[]promptr.ToolDef{{Name: "GetWeather", Description: "look it up", Params: "city: string"}},
			)
			So(err, ShouldBeNil)

			Convey("Then the request carried the function tool definition", func() {
				So(gotReq.Tools, ShouldHaveLength, 1)
				So(gotReq.Tools[0].Type, ShouldEqual, "function")
				So(gotReq.Tools[0].Function.Name, ShouldEqual, "GetWeather")
				So(gotReq.Tools[0].Function.Parameters["type"], ShouldEqual, "object")
			})

			Convey("Then the reply parses the requested tool call", func() {
				So(reply.Text, ShouldEqual, "")
				So(reply.Calls, ShouldHaveLength, 1)
				So(reply.Calls[0].ID, ShouldEqual, "call_1")
				So(reply.Calls[0].Name, ShouldEqual, "GetWeather")
				So(reply.Calls[0].Arguments, ShouldEqual, `{"city":"Oslo"}`)
			})
		})
	})

	Convey("Given a stub API and a prior tool-call turn plus result", t, func() {
		var raw map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &raw)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"done"}}]}`)
		}))
		defer srv.Close()
		c := New("sk-test", "gpt-4o")
		c.BaseURL = srv.URL

		Convey("When CompleteTools sends the assistant call and the tool result", func() {
			reply, err := c.CompleteTools(context.Background(), []promptr.Message{
				{Role: "user", Content: "go"},
				{Role: "assistant", ToolCalls: []promptr.ToolCall{{ID: "call_1", Name: "GetWeather", Arguments: `{"city":"Oslo"}`}}},
				{Role: "tool", ToolCallID: "call_1", Content: `{"high_c":3}`},
			}, nil)
			So(err, ShouldBeNil)
			So(reply.Text, ShouldEqual, "done")

			Convey("Then the wire messages carry tool_calls and tool_call_id", func() {
				msgs := raw["messages"].([]any)
				So(msgs, ShouldHaveLength, 3)
				assistant := msgs[1].(map[string]any)
				So(assistant["tool_calls"], ShouldNotBeNil)
				toolMsg := msgs[2].(map[string]any)
				So(toolMsg["tool_call_id"], ShouldEqual, "call_1")
				So(toolMsg["role"], ShouldEqual, "tool")
			})
		})
	})
}

func TestStream(t *testing.T) {
	Convey("Given a server emitting SSE deltas", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("content-type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			for _, tok := range []string{"Hel", "lo", " world"} {
				_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"`+tok+`"}}]}`+"\n\n")
				if fl != nil {
					fl.Flush()
				}
			}
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		}))
		defer srv.Close()
		c := New("sk-test", "gpt-4o")
		c.BaseURL = srv.URL

		Convey("When Stream is consumed, the deltas arrive in order and the channel closes", func() {
			ch, err := c.Stream(context.Background(), []promptr.Message{{Role: "user", Content: "hi"}})
			So(err, ShouldBeNil)
			var got string
			for tok := range ch {
				got += tok
			}
			So(got, ShouldEqual, "Hello world")
		})
	})
}

func TestStreamToolsText(t *testing.T) {
	Convey("Given a streaming server that emits only text deltas", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("content-type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			for _, tok := range []string{`{"to`, `tal":`, `5}`} {
				enc, _ := json.Marshal(tok)
				_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":`+string(enc)+`}}]}`+"\n\n")
				if fl != nil {
					fl.Flush()
				}
			}
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		}))
		defer srv.Close()
		c := New("sk-test", "gpt-4o")
		c.BaseURL = srv.URL

		Convey("When StreamTools is drained then Reply called", func() {
			ts, err := c.StreamTools(context.Background(), []promptr.Message{{Role: "user", Content: "hi"}}, nil)
			So(err, ShouldBeNil)
			var got string
			for tok := range ts.Deltas {
				got += tok
			}
			reply, rerr := ts.Reply()
			So(rerr, ShouldBeNil)

			Convey("Then deltas reassemble and Reply carries the full text, no calls", func() {
				So(got, ShouldEqual, `{"total":5}`)
				So(reply.Calls, ShouldBeEmpty)
				So(reply.Text, ShouldEqual, `{"total":5}`)
			})
		})
	})
}

func TestStreamToolsCalls(t *testing.T) {
	Convey("Given a streaming server that emits tool_call deltas across fragments", t, func() {
		var gotStream, gotTools bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotStream, _ = body["stream"].(bool)
			_, gotTools = body["tools"]
			w.Header().Set("content-type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			frames := []string{
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"add","arguments":"{\"a\":"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"2,\"b\":3}"}}]}}]}`,
			}
			for _, f := range frames {
				_, _ = io.WriteString(w, "data: "+f+"\n\n")
				if fl != nil {
					fl.Flush()
				}
			}
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		}))
		defer srv.Close()
		c := New("sk-test", "gpt-4o")
		c.BaseURL = srv.URL

		Convey("When StreamTools is consumed", func() {
			ts, err := c.StreamTools(context.Background(), []promptr.Message{{Role: "user", Content: "add"}}, []promptr.ToolDef{{Name: "add", Params: "a,b"}})
			So(err, ShouldBeNil)
			for tok := range ts.Deltas {
				_ = tok
			}
			reply, rerr := ts.Reply()
			So(rerr, ShouldBeNil)

			Convey("Then the request was a streamed tool call and the fragments reassembled", func() {
				So(gotStream, ShouldBeTrue)
				So(gotTools, ShouldBeTrue)
				So(reply.Calls, ShouldHaveLength, 1)
				So(reply.Calls[0].ID, ShouldEqual, "call_a")
				So(reply.Calls[0].Name, ShouldEqual, "add")
				So(reply.Calls[0].Arguments, ShouldEqual, `{"a":2,"b":3}`)
			})
		})
	})
}
