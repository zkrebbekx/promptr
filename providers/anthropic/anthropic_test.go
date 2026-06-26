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
			io.WriteString(w, `{"content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}]}`)
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
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error":{"type":"rate_limit_error","message":"slow down"}}`)
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
