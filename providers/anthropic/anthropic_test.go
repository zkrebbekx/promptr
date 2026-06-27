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
