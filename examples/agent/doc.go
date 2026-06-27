// Package agent is a promptr v0.6 example: a tool-using function that hands the
// model two Go-backed tools and runs the model→tool→model agent loop, returning
// a typed Itinerary. Compiled to Go (weather.promptr.go) and exercised against
// the fake tool provider in agent_test.go.
// Regenerate with: go generate ./...
package agent

//go:generate go run github.com/zkrebbekx/promptr/cmd/promptr generate .
