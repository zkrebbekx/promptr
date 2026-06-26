// Package ticket is an end-to-end promptr example: a .promptr schema compiled
// to Go (ticket.promptr.go) and exercised against the fake provider in
// ticket_test.go. Regenerate with: go generate ./...
package ticket

//go:generate go run github.com/zkrebbekx/promptr/cmd/promptr generate .
