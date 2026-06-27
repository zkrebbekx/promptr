// Package validate is an end-to-end promptr example for field validation:
// @assert rules drive a repair re-ask on violation, @check rules are surfaced
// softly via OnCheck. The .promptr schema is compiled to Go (account.promptr.go)
// and exercised against the fake provider in account_test.go. Regenerate with:
// go generate ./...
package validate

//go:generate go run github.com/zkrebbekx/promptr/cmd/promptr generate .
