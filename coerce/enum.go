package coerce

import (
	"reflect"
	"strings"
	"unicode"
)

// Enum is implemented by string-backed types that want fuzzy member matching
// without a hand-written encoding.TextUnmarshaler. The compiler emits this for
// every `enum` declaration, but any type can opt in:
//
//	type Severity string
//	func (Severity) CoerceMembers() []string { return []string{"LOW", "HIGH"} }
//
// Given model output of "high priority", coerce will set the value to "HIGH".
type Enum interface {
	CoerceMembers() []string
}

// membersOf returns the declared members if t (or *t) implements Enum.
func membersOf(t reflect.Type) ([]string, bool) {
	if t.Kind() == reflect.Interface {
		return nil, false
	}
	v := reflect.New(t).Elem()
	if e, ok := v.Interface().(Enum); ok {
		return e.CoerceMembers(), true
	}
	if e, ok := v.Addr().Interface().(Enum); ok {
		return e.CoerceMembers(), true
	}
	return nil, false
}

// fuzzyEnum maps a free-text value onto the closest declared member: first by
// normalised equality, then by either-direction substring containment.
func fuzzyEnum(s string, members []string) (string, bool) {
	ns := normKey(s)
	if ns == "" {
		return "", false
	}
	for _, m := range members {
		if normKey(m) == ns {
			return m, true
		}
	}
	for _, m := range members {
		nm := normKey(m)
		if nm != "" && (strings.Contains(ns, nm) || strings.Contains(nm, ns)) {
			return m, true
		}
	}
	return "", false
}

// normKey lowercases a key and drops separators so that "due_days",
// "DueDays", and "due-days" all compare equal.
func normKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '_' || r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
