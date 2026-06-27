package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/zkrebbekx/promptr"
)

func TestComplete(t *testing.T) {
	Convey("Given a stub generateContent API and a configured client", t, func() {
		var gotReq reqBody
		var gotKeyHeader string
		var gotURL string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotKeyHeader = r.Header.Get("x-goog-api-key")
			gotURL = r.URL.String()
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotReq)
			w.Header().Set("content-type", "application/json")
			_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"hel"},{"text":"lo"}]}}]}`)
		}))
		defer srv.Close()

		c := New("k-test", "gemini-1.5-flash")
		c.BaseURL = srv.URL

		Convey("When Complete is called with system, user, assistant messages", func() {
			got, err := c.Complete(context.Background(), []promptr.Message{
				{Role: "system", Content: "be terse"},
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "yo"},
			})

			Convey("Then candidate parts are concatenated", func() {
				So(err, ShouldBeNil)
				So(got, ShouldEqual, "hello")
			})
			Convey("Then the API key rides in the header, never the URL", func() {
				So(gotKeyHeader, ShouldEqual, "k-test")
				So(strings.Contains(gotURL, "k-test"), ShouldBeFalse)
			})
			Convey("Then system is lifted to systemInstruction and assistant maps to model", func() {
				So(gotReq.SystemInstruction, ShouldNotBeNil)
				So(gotReq.SystemInstruction.Parts[0].Text, ShouldEqual, "be terse")
				So(gotReq.Contents, ShouldHaveLength, 2)
				So(gotReq.Contents[0].Role, ShouldEqual, "user")
				So(gotReq.Contents[1].Role, ShouldEqual, "model")
			})
		})
	})

	Convey("Given a server returning an API error", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"bad model"}}`)
		}))
		defer srv.Close()
		c := New("k", "m")
		c.BaseURL = srv.URL

		Convey("When Complete is called, the error surfaces status and message", func() {
			_, err := c.Complete(context.Background(), []promptr.Message{{Role: "user", Content: "hi"}})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "400")
			So(err.Error(), ShouldContainSubstring, "bad model")
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
