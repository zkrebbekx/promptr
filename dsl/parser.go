package dsl

import (
	"fmt"
	"strings"
)

// Parse lexes and parses .promptr source into a File AST. Parse errors are
// collected and returned together; a partial AST is still returned so callers
// (an editor, the playground) can show what did parse.
func Parse(src string) (*File, error) {
	l := newLexer(src)
	var toks []token
	for {
		t := l.next()
		toks = append(toks, t)
		if t.kind == tEOF {
			break
		}
	}
	p := &parser{toks: toks}
	f := p.parseFile()
	if len(p.errs) > 0 {
		return f, fmt.Errorf("%s", strings.Join(p.errs, "; "))
	}
	return f, nil
}

type parser struct {
	toks []token
	i    int
	errs []string
}

func (p *parser) cur() token { return p.toks[p.i] }
func (p *parser) advance() token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

func (p *parser) errf(t token, format string, args ...any) {
	p.errs = append(p.errs, fmt.Sprintf("line %d: %s", t.line, fmt.Sprintf(format, args...)))
}

// accept consumes and returns true if the current token is of kind k.
func (p *parser) accept(k tokenKind) bool {
	if p.cur().kind == k {
		p.advance()
		return true
	}
	return false
}

// expect consumes a token of kind k, recording an error if it is missing.
func (p *parser) expect(k tokenKind, what string) token {
	t := p.cur()
	if t.kind != k {
		p.errf(t, "expected %s", what)
		return t
	}
	return p.advance()
}

func (p *parser) parseFile() *File {
	f := &File{}
	for p.cur().kind != tEOF {
		t := p.cur()
		if t.kind != tIdent {
			p.errf(t, "unexpected %q at top level", t.text)
			p.advance()
			continue
		}
		switch t.text {
		case "enum":
			f.Enums = append(f.Enums, p.parseEnum())
		case "class":
			f.Classes = append(f.Classes, p.parseClass())
		case "client":
			f.Clients = append(f.Clients, p.parseClient())
		case "function":
			f.Funcs = append(f.Funcs, p.parseFunc())
		case "test":
			f.Tests = append(f.Tests, p.parseTest())
		default:
			p.errf(t, "unknown declaration %q", t.text)
			p.advance()
		}
	}
	return f
}

func (p *parser) parseEnum() EnumDecl {
	kw := p.advance() // enum
	name := p.expect(tIdent, "enum name")
	d := EnumDecl{Name: name.text, Line: kw.line}
	p.expect(tLBrace, "'{'")
	for p.cur().kind != tRBrace && p.cur().kind != tEOF {
		m := p.cur()
		if m.kind != tIdent {
			p.errf(m, "expected enum member, got %q", m.text)
			p.advance()
			continue
		}
		d.Members = append(d.Members, m.text)
		p.advance()
		p.accept(tComma)
	}
	p.expect(tRBrace, "'}'")
	return d
}

func (p *parser) parseClass() ClassDecl {
	kw := p.advance() // class
	name := p.expect(tIdent, "class name")
	d := ClassDecl{Name: name.text, Line: kw.line}
	p.expect(tLBrace, "'{'")
	for p.cur().kind != tRBrace && p.cur().kind != tEOF {
		fn := p.cur()
		if fn.kind != tIdent {
			p.errf(fn, "expected field name, got %q", fn.text)
			p.advance()
			continue
		}
		p.advance()
		typ := p.parseTypeRef()
		d.Fields = append(d.Fields, FieldDecl{Name: fn.text, Type: typ})
		p.accept(tComma)
	}
	p.expect(tRBrace, "'}'")
	return d
}

// parseTypeRef reads `Base`, `Base[]`, `Base?`, or `Base[]?`.
func (p *parser) parseTypeRef() TypeRef {
	base := p.expect(tIdent, "type name")
	tr := TypeRef{Name: base.text}
	if p.cur().kind == tLBracket {
		p.advance()
		p.expect(tRBracket, "']'")
		tr.List = true
	}
	if p.cur().kind == tQuestion {
		p.advance()
		tr.Optional = true
	}
	return tr
}

func (p *parser) parseClient() ClientDecl {
	kw := p.advance() // client
	name := p.expect(tIdent, "client name")
	d := ClientDecl{Name: name.text, Line: kw.line, Extra: map[string]string{}}
	p.expect(tLBrace, "'{'")
	for p.cur().kind != tRBrace && p.cur().kind != tEOF {
		key := p.cur()
		if key.kind != tIdent {
			p.errf(key, "expected setting name, got %q", key.text)
			p.advance()
			continue
		}
		p.advance()
		switch key.text {
		case "retry":
			n := p.expect(tNumber, "retry count (number)")
			d.Policy.Retry = atoi(n.text)
		case "fallback":
			d.Policy.Fallback = p.parseIdentList()
		case "round_robin":
			d.Policy.RoundRobin = p.parseIdentList()
		case "provider":
			d.Provider = p.expect(tString, "provider value (string)").text
		case "model":
			d.Model = p.expect(tString, "model value (string)").text
		default:
			d.Extra[key.text] = p.expect(tString, "setting value (string)").text
		}
	}
	p.expect(tRBrace, "'}'")
	return d
}

// parseIdentList reads `[ A, B, C ]` — a comma-separated list of client names.
func (p *parser) parseIdentList() []string {
	p.expect(tLBracket, "'['")
	var out []string
	for p.cur().kind != tRBracket && p.cur().kind != tEOF {
		id := p.expect(tIdent, "client name")
		out = append(out, id.text)
		if !p.accept(tComma) {
			break
		}
	}
	p.expect(tRBracket, "']'")
	return out
}

// atoi parses a non-negative integer from already-validated digit text.
func atoi(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			break
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

func (p *parser) parseFunc() FuncDecl {
	kw := p.advance() // function
	name := p.expect(tIdent, "function name")
	d := FuncDecl{Name: name.text, Line: kw.line}
	p.expect(tLParen, "'('")
	for p.cur().kind != tRParen && p.cur().kind != tEOF {
		pn := p.expect(tIdent, "parameter name")
		p.expect(tColon, "':'")
		pt := p.parseTypeRef()
		d.Params = append(d.Params, Param{Name: pn.text, Type: pt})
		if !p.accept(tComma) {
			break
		}
	}
	p.expect(tRParen, "')'")
	p.expect(tArrow, "'->'")
	d.Ret = p.parseTypeRef()
	p.expect(tLBrace, "'{'")
	for p.cur().kind != tRBrace && p.cur().kind != tEOF {
		key := p.cur()
		if key.kind != tIdent {
			p.errf(key, "expected 'client' or 'prompt', got %q", key.text)
			p.advance()
			continue
		}
		p.advance()
		switch key.text {
		case "client":
			d.Client = p.expect(tIdent, "client name").text
		case "prompt":
			t := p.cur()
			if t.kind == tRawString || t.kind == tString {
				d.Prompt = t.text
				p.advance()
			} else {
				p.errf(t, "expected prompt template")
			}
		default:
			p.errf(key, "unknown function body key %q", key.text)
		}
	}
	p.expect(tRBrace, "'}'")
	return d
}

func (p *parser) parseTest() TestDecl {
	kw := p.advance() // test
	name := p.expect(tIdent, "test name")
	d := TestDecl{Name: name.text, Line: kw.line, Args: map[string]string{}}
	p.expect(tLBrace, "'{'")
	for p.cur().kind != tRBrace && p.cur().kind != tEOF {
		key := p.cur()
		if key.kind != tIdent {
			p.errf(key, "expected 'function' or 'args', got %q", key.text)
			p.advance()
			continue
		}
		p.advance()
		switch key.text {
		case "function":
			d.Func = p.expect(tIdent, "function name").text
		case "args":
			p.expect(tLBrace, "'{'")
			for p.cur().kind != tRBrace && p.cur().kind != tEOF {
				ak := p.expect(tIdent, "arg name")
				av := p.cur()
				switch av.kind {
				case tString, tRawString, tIdent:
					d.Args[ak.text] = av.text
					p.advance()
				default:
					p.errf(av, "expected arg value")
					p.advance()
				}
			}
			p.expect(tRBrace, "'}'")
		default:
			p.errf(key, "unknown test key %q", key.text)
		}
	}
	p.expect(tRBrace, "'}'")
	return d
}
