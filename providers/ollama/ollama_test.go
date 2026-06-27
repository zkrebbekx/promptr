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
