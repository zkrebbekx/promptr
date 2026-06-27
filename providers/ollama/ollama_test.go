package ollama

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
	Convey("Given a stub Ollama /api/chat and a configured client", t, func() {
		var gotReq reqBody
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotReq)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"local reply"}}`)
		}))
		defer srv.Close()

		c := New("llama3.2")
		c.BaseURL = srv.URL

		Convey("When Complete is called", func() {
			got, err := c.Complete(context.Background(), []promptr.Message{
				{Role: "user", Content: "hi"},
			})

			Convey("Then the assistant message content is returned", func() {
				So(err, ShouldBeNil)
				So(got, ShouldEqual, "local reply")
			})
			Convey("Then it posts to /api/chat with streaming disabled", func() {
				So(gotPath, ShouldEqual, "/api/chat")
				So(gotReq.Stream, ShouldBeFalse)
				So(gotReq.Model, ShouldEqual, "llama3.2")
			})
		})
	})

	Convey("Given a server returning an error payload", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"model not found"}`)
		}))
		defer srv.Close()
		c := New("nope")
		c.BaseURL = srv.URL

		Convey("When Complete is called, the error surfaces status and message", func() {
			_, err := c.Complete(context.Background(), []promptr.Message{{Role: "user", Content: "hi"}})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "404")
			So(err.Error(), ShouldContainSubstring, "model not found")
		})
	})
}

func TestCompleteTools(t *testing.T) {
	Convey("Given a stub /api/chat that returns a tool call", t, func() {
		var gotReq reqBody
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotReq)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"message":{"tool_calls":[{"function":{"name":"GetWeather","arguments":{"city":"Oslo"}}}]}}`)
		}))
		defer srv.Close()
		c := New("llama3.2")
		c.BaseURL = srv.URL

		Convey("When CompleteTools is called with a tool definition", func() {
			reply, err := c.CompleteTools(context.Background(),
				[]promptr.Message{{Role: "user", Content: "weather in Oslo?"}},
				[]promptr.ToolDef{{Name: "GetWeather", Description: "look it up", Params: "city: string"}},
			)
			So(err, ShouldBeNil)

			Convey("Then the request carried the OpenAI-style function tool", func() {
				So(gotReq.Tools, ShouldHaveLength, 1)
				So(gotReq.Tools[0].Type, ShouldEqual, "function")
				So(gotReq.Tools[0].Function.Name, ShouldEqual, "GetWeather")
				So(gotReq.Tools[0].Function.Parameters["type"], ShouldEqual, "object")
			})

			Convey("Then the reply parses the tool call (object args marshalled to JSON)", func() {
				So(reply.Text, ShouldEqual, "")
				So(reply.Calls, ShouldHaveLength, 1)
				So(reply.Calls[0].Name, ShouldEqual, "GetWeather")
				So(reply.Calls[0].Arguments, ShouldEqual, `{"city":"Oslo"}`)
				So(reply.Calls[0].ID, ShouldNotBeBlank)
			})
		})
	})

	Convey("Given a prior tool-call turn plus its result", t, func() {
		var raw map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &raw)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"message":{"content":"done"}}`)
		}))
		defer srv.Close()
		c := New("llama3.2")
		c.BaseURL = srv.URL

		Convey("When CompleteTools sends the assistant call and the tool result", func() {
			reply, err := c.CompleteTools(context.Background(), []promptr.Message{
				{Role: "user", Content: "go"},
				{Role: "assistant", ToolCalls: []promptr.ToolCall{{ID: "GetWeather-0", Name: "GetWeather", Arguments: `{"city":"Oslo"}`}}},
				{Role: "tool", ToolCallID: "GetWeather-0", Content: `{"high_c":3}`},
			}, nil)
			So(err, ShouldBeNil)
			So(reply.Text, ShouldEqual, "done")

			Convey("Then the wire carries tool_calls and a role:tool result named for its tool", func() {
				msgs := raw["messages"].([]any)
				So(msgs, ShouldHaveLength, 3)
				assistant := msgs[1].(map[string]any)
				So(assistant["tool_calls"], ShouldNotBeNil)
				toolMsg := msgs[2].(map[string]any)
				So(toolMsg["role"], ShouldEqual, "tool")
				So(toolMsg["tool_name"], ShouldEqual, "GetWeather") // recovered by ID->name
			})
		})
	})
}
