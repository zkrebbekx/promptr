package dsl

import "strings"

type tokenKind uint8

const (
	tEOF tokenKind = iota
	tIdent
	tNumber    // 123
	tString    // "double-quoted"
	tRawString // #" raw, multi-line template "#
	tLBrace
	tRBrace
	tLParen
	tRParen
	tLBracket
	tRBracket
	tColon
	tArrow    // ->
	tQuestion // ?
	tComma
	tLess    // <
	tGreater // >
	tPipe    // |
	tEquals  // =
	tAt      // @
)

type token struct {
	kind tokenKind
	text string
	pos  int
	line int
}

// lexer turns .promptr source into a token stream. It mirrors the structure of
// pgparse's lexer: a cursor over the source with small scan* helpers per token
// shape and a skipTrivia pass for whitespace and // comments.
type lexer struct {
	src  string
	pos  int
	line int
}

func newLexer(s string) *lexer { return &lexer{src: s, line: 1} }

func (l *lexer) next() token {
	l.skipTrivia()
	if l.pos >= len(l.src) {
		return token{kind: tEOF, pos: l.pos, line: l.line}
	}
	c := l.src[l.pos]
	start, line := l.pos, l.line
	switch {
	case c == '#' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '"':
		return l.scanRawString()
	case c == '"':
		return l.scanString()
	case isIdentStart(c):
		return l.scanIdent()
	case c >= '0' && c <= '9':
		return l.scanNumber()
	case c == '-' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '>':
		l.pos += 2
		return token{tArrow, "->", start, line}
	case c == '{':
		l.pos++
		return token{tLBrace, "{", start, line}
	case c == '}':
		l.pos++
		return token{tRBrace, "}", start, line}
	case c == '(':
		l.pos++
		return token{tLParen, "(", start, line}
	case c == ')':
		l.pos++
		return token{tRParen, ")", start, line}
	case c == '[':
		l.pos++
		return token{tLBracket, "[", start, line}
	case c == ']':
		l.pos++
		return token{tRBracket, "]", start, line}
	case c == ':':
		l.pos++
		return token{tColon, ":", start, line}
	case c == '?':
		l.pos++
		return token{tQuestion, "?", start, line}
	case c == ',':
		l.pos++
		return token{tComma, ",", start, line}
	case c == '<':
		l.pos++
		return token{tLess, "<", start, line}
	case c == '>':
		l.pos++
		return token{tGreater, ">", start, line}
	case c == '|':
		l.pos++
		return token{tPipe, "|", start, line}
	case c == '=':
		l.pos++
		return token{tEquals, "=", start, line}
	case c == '@':
		l.pos++
		return token{tAt, "@", start, line}
	}
	// Unknown byte — skip it and continue, so a stray character can't wedge
	// the lexer.
	l.pos++
	return l.next()
}

func (l *lexer) skipTrivia() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			l.pos++
		case c == '\n':
			l.line++
			l.pos++
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		default:
			return
		}
	}
}

func (l *lexer) scanIdent() token {
	start, line := l.pos, l.line
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.pos++
	}
	return token{tIdent, l.src[start:l.pos], start, line}
}

func (l *lexer) scanNumber() token {
	start, line := l.pos, l.line
	for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
		l.pos++
	}
	return token{tNumber, l.src[start:l.pos], start, line}
}

func (l *lexer) scanString() token {
	start, line := l.pos, l.line
	l.pos++ // opening "
	var b strings.Builder
	for l.pos < len(l.src) && l.src[l.pos] != '"' {
		c := l.src[l.pos]
		if c == '\\' && l.pos+1 < len(l.src) {
			l.pos++
			b.WriteByte(unescape(l.src[l.pos]))
		} else {
			if c == '\n' {
				l.line++
			}
			b.WriteByte(c)
		}
		l.pos++
	}
	l.pos++ // closing "
	return token{tString, b.String(), start, line}
}

// scanRawString reads a #" ... "# template literal verbatim (no escape
// processing), so prompt bodies can contain quotes, braces and newlines freely.
func (l *lexer) scanRawString() token {
	start, line := l.pos, l.line
	l.pos += 2 // #"
	cstart := l.pos
	for l.pos < len(l.src) {
		if l.src[l.pos] == '"' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '#' {
			break
		}
		if l.src[l.pos] == '\n' {
			l.line++
		}
		l.pos++
	}
	text := l.src[cstart:l.pos]
	l.pos += 2 // closing "#  (overshoots past EOF if unterminated)
	if l.pos > len(l.src) {
		l.pos = len(l.src)
	}
	return token{tRawString, text, start, line}
}

func unescape(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	default:
		return c
	}
}

func isIdentStart(c byte) bool {
	return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
