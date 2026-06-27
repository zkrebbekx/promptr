package anthropic

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
	Convey("Given a stub Messages API and a configured client", t, func() {
		var gotReq reqBody
		var gotHeaders http.Header
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHeaders = r.Header
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotReq)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}]}`)
		}))
		defer srv.Close()

		c := New("sk-test", "claude-opus-4-8")
		c.BaseURL = srv.URL

		Convey("When Complete is called with system and user messages", func() {
			got, err := c.Complete(context.Background(), []promptr.Message{
				{Role: "system", Content: "be terse"},
				{Role: "user", Content: "hi"},
			})

			Convey("Then text blocks are concatenated into the reply", func() {
				So(err, ShouldBeNil)
				So(got, ShouldEqual, "hello world")
			})

			Convey("Then auth and version headers are set", func() {
				So(gotHeaders.Get("x-api-key"), ShouldEqual, "sk-test")
				So(gotHeaders.Get("anthropic-version"), ShouldEqual, defaultVersion)
			})

			Convey("Then system is lifted out and only user/assistant remain in messages", func() {
				So(gotReq.System, ShouldEqual, "be terse")
				So(gotReq.Messages, ShouldHaveLength, 1)
				So(gotReq.Messages[0].Role, ShouldEqual, "user")
				So(gotReq.Model, ShouldEqual, "claude-opus-4-8")
				So(gotReq.MaxTokens, ShouldEqual, defaultMaxTokens)
			})
		})
	})

	Convey("Given a server that returns an API error", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"type":"rate_limit_error","message":"slow down"}}`)
		}))
		defer srv.Close()
		c := New("sk-test", "m")
		c.BaseURL = srv.URL

		Convey("When Complete is called", func() {
			_, err := c.Complete(context.Background(), []promptr.Message{{Role: "user", Content: "hi"}})

			Convey("Then the error surfaces the status and API message", func() {
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, "429")
				So(err.Error(), ShouldContainSubstring, "slow down")
			})
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

func TestStream(t *testing.T) {
	Convey("Given a Messages API emitting SSE text deltas", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("content-type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			for _, tok := range []string{"Hel", "lo"} {
				_, _ = io.WriteString(w, `event: content_block_delta`+"\n"+
					`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"`+tok+`"}}`+"\n\n")
				if fl != nil {
					fl.Flush()
				}
			}
			_, _ = io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
		}))
		defer srv.Close()
		c := New("k", "claude-opus-4-8")
		c.BaseURL = srv.URL

		Convey("When Stream is consumed, deltas arrive in order then the channel closes", func() {
			ch, err := c.Stream(context.Background(), []promptr.Message{{Role: "user", Content: "hi"}})
			So(err, ShouldBeNil)
			var got string
			for tok := range ch {
				got += tok
			}
			So(got, ShouldEqual, "Hello")
		})
	})
}

func TestCompleteTools(t *testing.T) {
	Convey("Given a stub Messages API returning a tool_use block", t, func() {
		var gotReq reqBody
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotReq)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"content":[{"type":"tool_use","id":"tu_1","name":"GetWeather","input":{"city":"Oslo"}}]}`)
		}))
		defer srv.Close()
		c := New("sk-test", "claude-opus-4-8")
		c.BaseURL = srv.URL

		Convey("When CompleteTools is called with a tool definition", func() {
			reply, err := c.CompleteTools(context.Background(),
				[]promptr.Message{{Role: "user", Content: "weather?"}},
				[]promptr.ToolDef{{Name: "GetWeather", Description: "look it up", Params: "city: string"}},
			)
			So(err, ShouldBeNil)

			Convey("Then the request carried the tool with an object input_schema", func() {
				So(gotReq.Tools, ShouldHaveLength, 1)
				So(gotReq.Tools[0].Name, ShouldEqual, "GetWeather")
				So(gotReq.Tools[0].InputSchema["type"], ShouldEqual, "object")
			})

			Convey("Then the tool_use block becomes a ToolCall with JSON arguments", func() {
				So(reply.Calls, ShouldHaveLength, 1)
				So(reply.Calls[0].ID, ShouldEqual, "tu_1")
				So(reply.Calls[0].Name, ShouldEqual, "GetWeather")
				So(reply.Calls[0].Arguments, ShouldContainSubstring, `"city":"Oslo"`)
			})
		})
	})

	Convey("Given a stub capturing a tool-call turn and its result", t, func() {
		var raw map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &raw)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"done"}]}`)
		}))
		defer srv.Close()
		c := New("sk-test", "claude-opus-4-8")
		c.BaseURL = srv.URL

		Convey("When CompleteTools sends the assistant tool_use and the tool_result", func() {
			reply, err := c.CompleteTools(context.Background(), []promptr.Message{
				{Role: "user", Content: "go"},
				{Role: "assistant", ToolCalls: []promptr.ToolCall{{ID: "tu_1", Name: "GetWeather", Arguments: `{"city":"Oslo"}`}}},
				{Role: "tool", ToolCallID: "tu_1", Content: `{"high_c":3}`},
			}, nil)
			So(err, ShouldBeNil)
			So(reply.Text, ShouldEqual, "done")

			Convey("Then the assistant turn carries a tool_use block and the user turn a tool_result", func() {
				msgs := raw["messages"].([]any)
				So(msgs, ShouldHaveLength, 3)
				assistant := msgs[1].(map[string]any)
				So(assistant["role"], ShouldEqual, "assistant")
				ablock := assistant["content"].([]any)[0].(map[string]any)
				So(ablock["type"], ShouldEqual, "tool_use")
				So(ablock["id"], ShouldEqual, "tu_1")
				result := msgs[2].(map[string]any)
				So(result["role"], ShouldEqual, "user")
				rblock := result["content"].([]any)[0].(map[string]any)
				So(rblock["type"], ShouldEqual, "tool_result")
				So(rblock["tool_use_id"], ShouldEqual, "tu_1")
			})
		})
	})
}

func TestMultimodalContent(t *testing.T) {
	Convey("Given a stub capturing the raw request body", t, func() {
		var raw map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &raw)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
		}))
		defer srv.Close()
		c := New("k", "claude-opus-4-8")
		c.BaseURL = srv.URL

		Convey("When a user message carries an inline image part", func() {
			_, err := c.Complete(context.Background(), []promptr.Message{{
				Role:  "user",
				Parts: []promptr.Part{promptr.TextPart("caption this"), promptr.ImagePart("image/jpeg", []byte{9, 9})},
			}})
			So(err, ShouldBeNil)

			Convey("Then the content is an array with a base64 image block", func() {
				msgs := raw["messages"].([]any)
				content := msgs[0].(map[string]any)["content"].([]any)
				So(content, ShouldHaveLength, 2)
				img := content[1].(map[string]any)
				So(img["type"], ShouldEqual, "image")
				src := img["source"].(map[string]any)
				So(src["type"], ShouldEqual, "base64")
				So(src["media_type"], ShouldEqual, "image/jpeg")
			})
		})
	})
}

func TestStreamToolsText(t *testing.T) {
	Convey("Given a Messages stream emitting only text_delta events", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("content-type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			emit := func(s string) {
				_, _ = io.WriteString(w, "data: "+s+"\n\n")
				if fl != nil {
					fl.Flush()
				}
			}
			emit(`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`)
			for _, tok := range []string{`{"to`, `tal":`, `5}`} {
				enc, _ := json.Marshal(tok)
				emit(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":` + string(enc) + `}}`)
			}
			emit(`{"type":"message_stop"}`)
		}))
		defer srv.Close()
		c := New("sk-test", "claude-opus-4-8")
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

			Convey("Then deltas reassemble and Reply carries the text, no calls", func() {
				So(got, ShouldEqual, `{"total":5}`)
				So(reply.Calls, ShouldBeEmpty)
				So(reply.Text, ShouldEqual, `{"total":5}`)
			})
		})
	})
}

func TestStreamToolsCalls(t *testing.T) {
	Convey("Given a Messages stream emitting a tool_use block across input_json_delta fragments", t, func() {
		var gotStream, gotTools bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotStream, _ = body["stream"].(bool)
			_, gotTools = body["tools"]
			w.Header().Set("content-type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			emit := func(s string) {
				_, _ = io.WriteString(w, "data: "+s+"\n\n")
				if fl != nil {
					fl.Flush()
				}
			}
			emit(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_a","name":"add"}}`)
			emit(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"a\":"}}`)
			emit(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"2,\"b\":3}"}}`)
			emit(`{"type":"content_block_stop","index":0}`)
			emit(`{"type":"message_stop"}`)
		}))
		defer srv.Close()
		c := New("sk-test", "claude-opus-4-8")
		c.BaseURL = srv.URL

		Convey("When StreamTools is consumed", func() {
			ts, err := c.StreamTools(context.Background(), []promptr.Message{{Role: "user", Content: "add"}}, []promptr.ToolDef{{Name: "add", Params: "a,b"}})
			So(err, ShouldBeNil)
			for tok := range ts.Deltas {
				_ = tok
			}
			reply, rerr := ts.Reply()
			So(rerr, ShouldBeNil)

			Convey("Then the request streamed tools and the tool_use input reassembled", func() {
				So(gotStream, ShouldBeTrue)
				So(gotTools, ShouldBeTrue)
				So(reply.Calls, ShouldHaveLength, 1)
				So(reply.Calls[0].ID, ShouldEqual, "toolu_a")
				So(reply.Calls[0].Name, ShouldEqual, "add")
				So(reply.Calls[0].Arguments, ShouldEqual, `{"a":2,"b":3}`)
			})
		})
	})
}
