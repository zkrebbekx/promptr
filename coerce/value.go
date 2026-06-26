package coerce

import (
	"strconv"
	"strings"
)

// kind enumerates the shapes a tolerantly-parsed value can take.
type kind uint8

const (
	kNull kind = iota
	kBool
	kNum
	kStr
	kArr
	kObj
)

// node is the intermediate representation produced by the tolerant parser,
// before it is coerced into a caller-supplied Go type. Scalars keep their raw
// lexeme so coercion can decide how to interpret them: the string "5" can
// become an int, the number 1 can become a bool, and "$1,200" can become a
// float. quoted records whether a scalar came from a quoted string literal.
type node struct {
	kind    kind
	raw     string // scalar text (decoded for strings; literal for bool/num/null)
	quoted  bool   // scalar originated from a quoted string literal
	arr     []node
	obj     []field
	partial bool // container was truncated before its closing bracket
}

type field struct {
	key string
	val node
}

// asString renders the node as a string, re-serialising containers compactly.
func (n node) asString() string {
	switch n.kind {
	case kNull:
		return ""
	case kStr, kNum, kBool:
		return n.raw
	case kArr, kObj:
		return n.toJSON()
	}
	return n.raw
}

// asBool interprets the node as a boolean, accepting the many truthy spellings
// models use.
func (n node) asBool() bool {
	switch n.kind {
	case kBool:
		return n.raw == "true"
	case kNum:
		return parseNum(n.raw) != 0
	case kStr:
		switch strings.ToLower(strings.TrimSpace(n.raw)) {
		case "true", "yes", "y", "1", "on", "t":
			return true
		}
		return false
	}
	return false
}

// asNumber interprets the node as a float, stripping currency, percent, and
// thousands separators that commonly leak into model output.
func (n node) asNumber() float64 {
	switch n.kind {
	case kNum, kStr:
		return parseNum(n.raw)
	case kBool:
		if n.raw == "true" {
			return 1
		}
	}
	return 0
}

// asAny produces a generic Go value (map[string]any / []any / string / float64
// / bool / nil), used when coercing into an empty interface.
func (n node) asAny() any {
	switch n.kind {
	case kNull:
		return nil
	case kBool:
		return n.raw == "true"
	case kNum:
		return parseNum(n.raw)
	case kStr:
		return n.raw
	case kArr:
		out := make([]any, len(n.arr))
		for i, e := range n.arr {
			out[i] = e.asAny()
		}
		return out
	case kObj:
		m := make(map[string]any, len(n.obj))
		for _, f := range n.obj {
			m[f.key] = f.val.asAny()
		}
		return m
	}
	return nil
}

// toJSON re-serialises a node as compact JSON (best effort; used only when a
// container needs to be coerced into a string target).
func (n node) toJSON() string {
	var b strings.Builder
	n.writeJSON(&b)
	return b.String()
}

func (n node) writeJSON(b *strings.Builder) {
	switch n.kind {
	case kNull:
		b.WriteString("null")
	case kBool:
		b.WriteString(n.raw)
	case kNum:
		b.WriteString(strconv.FormatFloat(parseNum(n.raw), 'g', -1, 64))
	case kStr:
		b.WriteString(strconv.Quote(n.raw))
	case kArr:
		b.WriteByte('[')
		for i, e := range n.arr {
			if i > 0 {
				b.WriteByte(',')
			}
			e.writeJSON(b)
		}
		b.WriteByte(']')
	case kObj:
		b.WriteByte('{')
		for i, f := range n.obj {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Quote(f.key))
			b.WriteByte(':')
			f.val.writeJSON(b)
		}
		b.WriteByte('}')
	}
}

// parseNum extracts a float from a possibly-decorated numeric string such as
// "$1,200", "42%", or " 3.14 ". If the whole string is not numeric it falls
// back to salvaging the first numeric run embedded in prose (so a model that
// answers "The count is 42." still yields 42 for an int target).
func parseNum(s string) float64 {
	clean := strings.TrimSpace(s)
	clean = strings.TrimPrefix(clean, "$")
	clean = strings.TrimSuffix(clean, "%")
	clean = strings.ReplaceAll(clean, ",", "")
	if f, err := strconv.ParseFloat(clean, 64); err == nil {
		return f
	}
	if f, ok := firstNumber(s); ok {
		return f
	}
	return 0
}

// firstNumber scans for the first numeric run in a string and parses it.
func firstNumber(s string) (float64, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		isDigit := c >= '0' && c <= '9'
		negDigit := c == '-' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9'
		if !isDigit && !negDigit {
			continue
		}
		j := i
		if s[j] == '-' {
			j++
		}
		for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.' || s[j] == ',') {
			j++
		}
		tok := strings.TrimRight(strings.ReplaceAll(s[i:j], ",", ""), ".")
		if f, err := strconv.ParseFloat(tok, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// looksNumeric reports whether a bareword token should be classified as a
// number rather than an unquoted string.
func looksNumeric(t string) bool {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "$")
	t = strings.TrimSuffix(t, "%")
	t = strings.ReplaceAll(t, ",", "")
	if t == "" {
		return false
	}
	_, err := strconv.ParseFloat(t, 64)
	return err == nil
}
