package coerce

import (
	"strconv"
	"strings"
)

// maxDepth bounds how deeply nested an object/array the tolerant parser will
// descend into. Adversarial or garbage input (e.g. a few million nested "[")
// would otherwise recurse one stack frame per level and overflow the goroutine
// stack — an unrecoverable fatal error, since stack overflow cannot be caught by
// recover(). Real model output is never this deep, so once the limit is reached
// the parser stops descending and unwinds with a partial value (truncated),
// matching how it already degrades on unterminated input. The cap also bounds
// the downstream node→value conversion, which walks the same tree.
const maxDepth = 1000

// parseTolerant extracts and parses the first JSON-ish value embedded in raw
// model output. It is deliberately lenient: it skips prose and Markdown code
// fences around the payload, and within the payload it tolerates trailing
// commas, // and /* */ comments, single-quoted and unquoted strings, unquoted
// keys, and truncated (unterminated) objects/arrays — recovering whatever
// parsed. The bool result reports whether the payload parsed to completion
// (false means it was truncated and the value is partial).
func parseTolerant(raw string) (node, bool) {
	p := &parser{s: stripFences(raw)}
	p.locateStart()
	n := p.parseValue(0)
	return n, !p.truncated
}

// stripFences returns the contents of the first Markdown fenced code block if
// one is present, otherwise the input unchanged. A language tag on the opening
// fence (```json) is dropped.
func stripFences(s string) string {
	i := strings.Index(s, "```")
	if i < 0 {
		return s
	}
	rest := s[i+3:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		first := strings.TrimSpace(rest[:nl])
		if !strings.ContainsAny(first, "{}[]\"") {
			rest = rest[nl+1:]
		}
	}
	if j := strings.Index(rest, "```"); j >= 0 {
		return rest[:j]
	}
	return rest
}

type parser struct {
	s         string
	i         int
	truncated bool
}

// locateStart advances to the first structural opener so leading prose like
// "Sure, here you go:" is skipped. If there is none, it positions at the first
// non-trivia byte so a bare scalar can still be read.
func (p *parser) locateStart() {
	for j := 0; j < len(p.s); j++ {
		if p.s[j] == '{' || p.s[j] == '[' {
			p.i = j
			return
		}
	}
	p.skipWS()
}

// skipWS consumes whitespace, comments, and stray commas (which separators are
// handled by treating them as trivia).
func (p *parser) skipWS() {
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			p.i++
		case c == '/' && p.i+1 < len(p.s) && p.s[p.i+1] == '/':
			for p.i < len(p.s) && p.s[p.i] != '\n' {
				p.i++
			}
		case c == '/' && p.i+1 < len(p.s) && p.s[p.i+1] == '*':
			p.i += 2
			for p.i+1 < len(p.s) && (p.s[p.i] != '*' || p.s[p.i+1] != '/') {
				p.i++
			}
			// Step past the closing "*/", but never past end-of-input — an
			// unterminated block comment must leave p.i at len, not beyond it.
			p.i += 2
			if p.i > len(p.s) {
				p.i = len(p.s)
			}
		default:
			return
		}
	}
}

func (p *parser) parseValue(depth int) node {
	p.skipWS()
	if p.i >= len(p.s) {
		p.truncated = true
		return node{kind: kNull}
	}
	if depth > maxDepth {
		// Nesting past the limit: refuse to descend further and unwind with a
		// partial value. This caps the recursion (and the downstream tree walk)
		// so adversarial input can never overflow the goroutine stack.
		p.truncated = true
		return node{kind: kNull, partial: true}
	}
	switch p.s[p.i] {
	case '{':
		return p.parseObject(depth)
	case '[':
		return p.parseArray(depth)
	case '"', '\'', '`':
		return p.parseString()
	default:
		start := p.i
		n := p.parseBareword()
		if p.i == start {
			// A stray structural byte appeared where a value was expected
			// (e.g. a dangling ':' or '}'). Skip it so the enclosing parse
			// loop always makes forward progress and can never spin.
			p.i++
		}
		return n
	}
}

func (p *parser) parseObject(depth int) node {
	p.i++ // consume '{'
	n := node{kind: kObj}
	for {
		p.skipWS()
		if p.i >= len(p.s) {
			n.partial = true
			p.truncated = true
			return n
		}
		if p.s[p.i] == '}' {
			p.i++
			return n
		}
		var key string
		if c := p.s[p.i]; c == '"' || c == '\'' || c == '`' {
			key = p.parseString().raw
		} else {
			key = p.scanBareword(true)
		}
		p.skipWS()
		if p.i < len(p.s) && p.s[p.i] == ':' {
			p.i++
		}
		val := p.parseValue(depth + 1)
		n.obj = append(n.obj, field{key: key, val: val})
		if p.truncated {
			n.partial = true
			return n
		}
	}
}

func (p *parser) parseArray(depth int) node {
	p.i++ // consume '['
	n := node{kind: kArr}
	for {
		p.skipWS()
		if p.i >= len(p.s) {
			n.partial = true
			p.truncated = true
			return n
		}
		if p.s[p.i] == ']' {
			p.i++
			return n
		}
		val := p.parseValue(depth + 1)
		n.arr = append(n.arr, val)
		if p.truncated {
			n.partial = true
			return n
		}
	}
}

func (p *parser) parseString() node {
	delim := p.s[p.i]
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '\\' && p.i+1 < len(p.s) {
			p.i++
			switch e := p.s[p.i]; e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'u':
				if r, ok := p.readUnicodeEscape(); ok {
					b.WriteRune(r)
				} else {
					b.WriteByte('u')
				}
			default:
				b.WriteByte(e)
			}
			p.i++
			continue
		}
		if c == delim {
			p.i++
			return node{kind: kStr, raw: b.String(), quoted: true}
		}
		b.WriteByte(c)
		p.i++
	}
	// EOF before the closing delimiter — tolerate and mark truncated.
	p.truncated = true
	return node{kind: kStr, raw: b.String(), quoted: true, partial: true}
}

// readUnicodeEscape reads the four hex digits of a \uXXXX escape. It is called
// with p.i positioned at the 'u'; on success it leaves p.i at the final hex
// digit (the caller's p.i++ then moves past it).
func (p *parser) readUnicodeEscape() (rune, bool) {
	if p.i+4 >= len(p.s) {
		return 0, false
	}
	v, err := strconv.ParseUint(p.s[p.i+1:p.i+5], 16, 32)
	if err != nil {
		return 0, false
	}
	p.i += 4
	return rune(v), true
}

func (p *parser) parseBareword() node {
	return classifyBareword(p.scanBareword(false))
}

// scanBareword reads an unquoted token up to the next structural byte. Keys
// additionally stop at whitespace (so `{foo bar: 1}` reads key "foo").
func (p *parser) scanBareword(isKey bool) string {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == ',' || c == '}' || c == ']' || c == ':' || c == '\n' || c == '\r' {
			break
		}
		if isKey && (c == ' ' || c == '\t') {
			break
		}
		p.i++
	}
	return strings.TrimSpace(p.s[start:p.i])
}

func classifyBareword(t string) node {
	switch strings.ToLower(t) {
	case "", "null", "nil", "none":
		return node{kind: kNull, raw: t}
	case "true":
		return node{kind: kBool, raw: "true"}
	case "false":
		return node{kind: kBool, raw: "false"}
	}
	if looksNumeric(t) {
		return node{kind: kNum, raw: t}
	}
	return node{kind: kStr, raw: t}
}
