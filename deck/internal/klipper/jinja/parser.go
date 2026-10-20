package jinja

import (
	"strconv"
)

// The parser makes the tree of a template, with the grammar of Jinja2.

type node interface{}

// Statements

type nData struct{ text string }
type nOutput struct{ expr node }
type nIf struct {
	cond     node
	body     []node
	elseBody []node
}
type nFor struct {
	target   node
	iter     node
	filter   node
	body     []node
	elseBody []node
}
type nSet struct {
	target node
	value  node
	body   []node // a block set: {% set x %}...{% endset %}
}

// Expressions

type nConst struct{ v any }
type nName struct{ name string }
type nTuple struct{ items []node }
type nList struct{ items []node }
type nDict struct{ keys, values []node }
type nGetattr struct {
	obj  node
	attr string
}
type nGetitem struct {
	obj node
	key node
}
type nSlice struct{ start, stop, step node }
type nCall struct {
	fn     node
	args   []node
	kwargs []kwarg
}
type kwarg struct {
	name string
	v    node
}
type nFilter struct {
	expr   node
	name   string
	args   []node
	kwargs []kwarg
}
type nTest struct {
	expr   node
	name   string
	args   []node
	negate bool
}
type nUnary struct {
	op   string
	expr node
}
type nBinary struct {
	op          string
	left, right node
}
type nConcat struct{ items []node }
type nCompare struct {
	first node
	ops   []string
	rest  []node
}
type nCondExpr struct{ test, then, otherwise node }

type parser struct {
	toks   []token
	pos    int
	blocks []string // the tags of the open blocks, for the error of a missing end tag
}

func (p *parser) cur() token  { return p.toks[p.pos] }
func (p *parser) peek() token { return p.toks[min(p.pos+1, len(p.toks)-1)] }
func (p *parser) next() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) isOp(op string) bool  { t := p.cur(); return t.typ == tOp && t.val == op }
func (p *parser) isName(n string) bool { t := p.cur(); return t.typ == tName && t.val == n }
func (p *parser) skipOp(op string) bool {
	if p.isOp(op) {
		p.next()
		return true
	}
	return false
}
func (p *parser) skipName(n string) bool {
	if p.isName(n) {
		p.next()
		return true
	}
	return false
}

func (p *parser) expectOp(op string) {
	if !p.skipOp(op) {
		syntaxErr(p.cur().line, "expected token '%s', got %s", op, p.cur())
	}
}

func (p *parser) expectName() string {
	t := p.cur()
	if t.typ != tName {
		syntaxErr(t.line, "expected token 'name', got %s", t)
	}
	p.next()
	return t.val
}

func (p *parser) expectBlockEnd() {
	if p.cur().typ != tBlockEnd {
		syntaxErr(p.cur().line, "expected token 'end of statement block', got %s", p.cur())
	}
	p.next()
}

// parse makes the tree of a template.
func parse(src string) (body []node, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*SyntaxError); ok {
				err = e
				return
			}
			panic(r)
		}
	}()
	p := &parser{toks: lex(src)}
	body = p.subparse(nil)
	if p.cur().typ != tEOF {
		syntaxErr(p.cur().line, "unexpected %s", p.cur())
	}
	return body, nil
}

// subparse reads statements until a block tag with one of the end names. It leaves the parser
// at that name.
func (p *parser) subparse(endNames []string) []node {
	var body []node
	for {
		t := p.cur()
		switch t.typ {
		case tEOF:
			if endNames != nil {
				syntaxErr(t.line, "Unexpected end of template. Jinja was looking for the following tags: %s. "+
					"The innermost block that needs to be closed is '%s'.", quoteTags(endNames), p.blocks[len(p.blocks)-1])
			}
			return body
		case tData:
			p.next()
			body = append(body, &nData{t.val})
		case tVarBegin:
			p.next()
			e := p.parseTuple(false)
			if p.cur().typ != tVarEnd {
				syntaxErr(p.cur().line, "expected token 'end of print statement', got %s", p.cur())
			}
			p.next()
			body = append(body, &nOutput{e})
		case tBlockBegin:
			p.next()
			nt := p.cur()
			if nt.typ != tName {
				syntaxErr(nt.line, "tag name expected")
			}
			for _, en := range endNames {
				if nt.val == en {
					return body
				}
			}
			body = append(body, p.parseStatement())
		default:
			syntaxErr(t.line, "unexpected %s", t)
		}
	}
}

func quoteTags(names []string) string {
	s := ""
	for i, n := range names {
		if i > 0 {
			s += " or "
		}
		s += "'" + n + "'"
	}
	return s
}

func (p *parser) parseStatement() node {
	t := p.next()
	p.blocks = append(p.blocks, t.val)
	defer func() { p.blocks = p.blocks[:len(p.blocks)-1] }()
	switch t.val {
	case "if":
		return p.parseIf()
	case "for":
		return p.parseFor()
	case "set":
		return p.parseSet()
	}
	syntaxErr(t.line, "Encountered unknown tag '%s'.", t.val)
	return nil
}

func (p *parser) parseIf() node {
	n := &nIf{cond: p.parseTuple(false)}
	p.expectBlockEnd()
	n.body = p.subparse([]string{"elif", "else", "endif"})
	switch tag := p.next().val; tag {
	case "elif":
		n.elseBody = []node{p.parseIf()}
		return n
	case "else":
		p.expectBlockEnd()
		n.elseBody = p.subparse([]string{"endif"})
		p.next()
	}
	p.expectBlockEnd()
	return n
}

func (p *parser) parseFor() node {
	n := &nFor{target: p.parseAssignTarget()}
	if !p.skipName("in") {
		syntaxErr(p.cur().line, "expected token 'in', got %s", p.cur())
	}
	n.iter = p.parseTuple(true)
	if p.skipName("if") {
		n.filter = p.parseExpression(true)
	}
	p.skipName("recursive")
	p.expectBlockEnd()
	n.body = p.subparse([]string{"endfor", "else"})
	if p.next().val == "else" {
		p.expectBlockEnd()
		n.elseBody = p.subparse([]string{"endfor"})
		p.next()
	}
	p.expectBlockEnd()
	return n
}

func (p *parser) parseSet() node {
	target := p.parseAssignTarget()
	if p.skipOp("=") {
		v := p.parseTuple(false)
		p.expectBlockEnd()
		return &nSet{target: target, value: v}
	}
	p.expectBlockEnd()
	body := p.subparse([]string{"endset"})
	p.next()
	p.expectBlockEnd()
	return &nSet{target: target, body: body}
}

// parseAssignTarget reads a name, a tuple of names, or a namespace attribute (ns.x).
func (p *parser) parseAssignTarget() node {
	var items []node
	for {
		name := p.expectName()
		var target node = &nName{name}
		if p.skipOp(".") {
			target = &nGetattr{&nName{name}, p.expectName()}
		}
		items = append(items, target)
		if !p.skipOp(",") {
			break
		}
		if p.cur().typ != tName {
			break
		}
	}
	if len(items) == 1 {
		return items[0]
	}
	return &nTuple{items}
}

// parseTuple reads an expression, or a tuple of expressions with no parentheses.
func (p *parser) parseTuple(simplified bool) node {
	var items []node
	isTuple := false
	for {
		if len(items) > 0 {
			if !p.skipOp(",") {
				break
			}
			isTuple = true
		}
		if p.endOfTuple() {
			break
		}
		if simplified {
			items = append(items, p.parsePrimaryPostfix())
		} else {
			items = append(items, p.parseExpression(true))
		}
	}
	if !isTuple {
		if len(items) == 0 {
			syntaxErr(p.cur().line, "Expected an expression, got %s", p.cur())
		}
		return items[0]
	}
	return &nTuple{items}
}

func (p *parser) endOfTuple() bool {
	t := p.cur()
	switch t.typ {
	case tVarEnd, tBlockEnd, tEOF:
		return true
	case tName:
		return t.val == "in"
	case tOp:
		return t.val == ")" || t.val == "]" || t.val == "}" || t.val == "="
	}
	return false
}

func (p *parser) parsePrimaryPostfix() node {
	return p.parsePostfix(p.parsePrimary())
}

func (p *parser) parseExpression(withCondexpr bool) node {
	if withCondexpr {
		return p.parseCondexpr()
	}
	return p.parseOr()
}

func (p *parser) parseCondexpr() node {
	expr1 := p.parseOr()
	for p.skipName("if") {
		expr2 := p.parseOr()
		var expr3 node
		if p.skipName("else") {
			expr3 = p.parseCondexpr()
		}
		expr1 = &nCondExpr{expr2, expr1, expr3}
	}
	return expr1
}

func (p *parser) parseOr() node {
	left := p.parseAnd()
	for p.skipName("or") {
		left = &nBinary{"or", left, p.parseAnd()}
	}
	return left
}

func (p *parser) parseAnd() node {
	left := p.parseNot()
	for p.skipName("and") {
		left = &nBinary{"and", left, p.parseNot()}
	}
	return left
}

func (p *parser) parseNot() node {
	if p.skipName("not") {
		return &nUnary{"not", p.parseNot()}
	}
	return p.parseCompare()
}

func (p *parser) parseCompare() node {
	first := p.parseMath1()
	n := &nCompare{first: first}
	for {
		t := p.cur()
		switch {
		case t.typ == tOp && (t.val == "==" || t.val == "!=" || t.val == "<" || t.val == "<=" || t.val == ">" || t.val == ">="):
			p.next()
			n.ops = append(n.ops, t.val)
		case t.typ == tName && t.val == "in":
			p.next()
			n.ops = append(n.ops, "in")
		case t.typ == tName && t.val == "not" && p.peek().typ == tName && p.peek().val == "in":
			p.next()
			p.next()
			n.ops = append(n.ops, "notin")
		default:
			if len(n.ops) == 0 {
				return first
			}
			return n
		}
		n.rest = append(n.rest, p.parseMath1())
	}
}

func (p *parser) parseMath1() node {
	left := p.parseConcat()
	for p.isOp("+") || p.isOp("-") {
		op := p.next().val
		left = &nBinary{op, left, p.parseConcat()}
	}
	return left
}

func (p *parser) parseConcat() node {
	items := []node{p.parseMath2()}
	for p.skipOp("~") {
		items = append(items, p.parseMath2())
	}
	if len(items) == 1 {
		return items[0]
	}
	return &nConcat{items}
}

func (p *parser) parseMath2() node {
	left := p.parsePow()
	for p.isOp("*") || p.isOp("/") || p.isOp("//") || p.isOp("%") {
		op := p.next().val
		left = &nBinary{op, left, p.parsePow()}
	}
	return left
}

func (p *parser) parsePow() node {
	left := p.parseUnary(true)
	for p.skipOp("**") {
		left = &nBinary{"**", left, p.parseUnary(true)}
	}
	return left
}

func (p *parser) parseUnary(withFilter bool) node {
	var n node
	switch {
	case p.isOp("-"):
		p.next()
		n = &nUnary{"-", p.parseUnary(false)}
	case p.isOp("+"):
		p.next()
		n = &nUnary{"+", p.parseUnary(false)}
	default:
		n = p.parsePrimary()
	}
	n = p.parsePostfix(n)
	if withFilter {
		n = p.parseFilterExpr(n)
	}
	return n
}

func (p *parser) parsePrimary() node {
	t := p.cur()
	switch t.typ {
	case tName:
		p.next()
		switch t.val {
		case "true", "True":
			return &nConst{true}
		case "false", "False":
			return &nConst{false}
		case "none", "None":
			return &nConst{nil}
		}
		return &nName{t.val}
	case tString:
		s := ""
		for p.cur().typ == tString {
			s += p.next().val
		}
		return &nConst{s}
	case tInt:
		p.next()
		n, err := strconv.ParseInt(t.val, 10, 64)
		if err != nil {
			f, _ := strconv.ParseFloat(t.val, 64)
			return &nConst{f}
		}
		return &nConst{n}
	case tFloat:
		p.next()
		f, _ := strconv.ParseFloat(t.val, 64)
		return &nConst{f}
	case tOp:
		switch t.val {
		case "(":
			p.next()
			if p.skipOp(")") {
				return &nTuple{}
			}
			e := p.parseTuple(false)
			p.expectOp(")")
			return e
		case "[":
			p.next()
			var items []node
			for !p.isOp("]") {
				if len(items) > 0 {
					p.expectOp(",")
					if p.isOp("]") {
						break
					}
				}
				items = append(items, p.parseExpression(true))
			}
			p.expectOp("]")
			return &nList{items}
		case "{":
			p.next()
			d := &nDict{}
			for !p.isOp("}") {
				if len(d.keys) > 0 {
					p.expectOp(",")
					if p.isOp("}") {
						break
					}
				}
				d.keys = append(d.keys, p.parseExpression(true))
				p.expectOp(":")
				d.values = append(d.values, p.parseExpression(true))
			}
			p.expectOp("}")
			return d
		}
	}
	syntaxErr(t.line, "unexpected %s", t)
	return nil
}

func (p *parser) parsePostfix(n node) node {
	for {
		switch {
		case p.isOp("."):
			p.next()
			t := p.next()
			switch t.typ {
			case tName:
				n = &nGetattr{n, t.val}
			case tInt:
				v, _ := strconv.ParseInt(t.val, 10, 64)
				n = &nGetitem{n, &nConst{v}}
			default:
				syntaxErr(t.line, "expected name or number")
			}
		case p.isOp("["):
			p.next()
			var args []node
			for !p.isOp("]") {
				if len(args) > 0 {
					p.expectOp(",")
				}
				args = append(args, p.parseSubscribed())
			}
			p.expectOp("]")
			if len(args) == 1 {
				n = &nGetitem{n, args[0]}
			} else {
				n = &nGetitem{n, &nTuple{args}}
			}
		case p.isOp("("):
			n = p.parseCall(n)
		default:
			return n
		}
	}
}

func (p *parser) parseSubscribed() node {
	var start node
	if !p.isOp(":") {
		start = p.parseExpression(true)
		if !p.isOp(":") {
			return start
		}
	}
	p.next()
	s := &nSlice{start: start}
	if !p.isOp("]") && !p.isOp(",") && !p.isOp(":") {
		s.stop = p.parseExpression(true)
	}
	if p.skipOp(":") {
		if !p.isOp("]") && !p.isOp(",") {
			s.step = p.parseExpression(true)
		}
	}
	return s
}

func (p *parser) parseCallArgs() ([]node, []kwarg) {
	p.expectOp("(")
	var args []node
	var kwargs []kwarg
	first := true
	for !p.isOp(")") {
		if !first {
			p.expectOp(",")
			if p.isOp(")") {
				break
			}
		}
		first = false
		if p.cur().typ == tName && p.peek().typ == tOp && p.peek().val == "=" {
			name := p.next().val
			p.next()
			kwargs = append(kwargs, kwarg{name, p.parseExpression(true)})
		} else {
			if len(kwargs) > 0 {
				syntaxErr(p.cur().line, "Invalid argument syntax")
			}
			args = append(args, p.parseExpression(true))
		}
	}
	p.expectOp(")")
	return args, kwargs
}

func (p *parser) parseCall(fn node) node {
	args, kwargs := p.parseCallArgs()
	return &nCall{fn, args, kwargs}
}

func (p *parser) parseFilterExpr(n node) node {
	for {
		switch {
		case p.isOp("|"):
			p.next()
			name := p.expectName()
			for p.skipOp(".") {
				name += "." + p.expectName()
			}
			f := &nFilter{expr: n, name: name}
			if p.isOp("(") {
				f.args, f.kwargs = p.parseCallArgs()
			}
			if _, ok := filters[name]; !ok {
				syntaxErr(p.cur().line, "No filter named '%s'.", name)
			}
			n = f
		case p.isName("is"):
			p.next()
			negate := p.skipName("not")
			name := p.expectName()
			for p.skipOp(".") {
				name += "." + p.expectName()
			}
			t := &nTest{expr: n, name: name, negate: negate}
			if p.isOp("(") {
				t.args, _ = p.parseCallArgs()
			} else if c := p.cur(); c.typ == tString || c.typ == tInt || c.typ == tFloat ||
				(c.typ == tName && c.val != "else" && c.val != "or" && c.val != "and" && c.val != "if" &&
					c.val != "in" && c.val != "is" && c.val != "not") || (c.typ == tOp && (c.val == "[" || c.val == "{")) {
				t.args = []node{p.parsePrimaryPostfix()}
			}
			if _, ok := tests[name]; !ok {
				syntaxErr(p.cur().line, "No test named '%s'.", name)
			}
			n = t
		case p.isOp("("):
			n = p.parseCall(n)
		default:
			return n
		}
	}
}
