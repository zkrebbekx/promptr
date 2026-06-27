// Package stream is a promptr v0.4 example: a streaming function (-> stream T)
// that yields progressively-completed partial values, plus a multimodal
// function taking an image input. Compiled to Go (stream.promptr.go) and
// exercised against the fake provider in stream_test.go.
// Regenerate with: go generate ./...
package stream

//go:generate go run github.com/zkrebbekx/promptr/cmd/promptr generate .
