package azure

import (
	"strconv"
	"strings"
)

// Lector de expresiones de plantilla ("[format('rg-{0}', variables('baseName'))]"). vex no las evalúa: solo las
// descompone para poder decir, sin desplegar, si dos expresiones nombran lo mismo y de dónde sale un valor. Lo que
// no entiende se queda como texto opaco, y lo opaco nunca se da por bueno.

type nodeKind int

const (
	nodeLiteral  nodeKind = iota // 'texto'
	nodeNumber                   // 3
	nodeCall                     // nombre(args…)
	nodeProperty                 // objetivo.nombre
	nodeIndex                    // objetivo[índice]
	nodeRaw                      // algo que no se pudo leer
)

const maxExpressionBytes = 8 << 10

type exprNode struct {
	kind nodeKind
	// text es el literal, el número, el nombre de la función (en minúsculas), el de la propiedad o el texto crudo.
	text string
	// args son los argumentos de una llamada; en una propiedad o un índice, args[0] es el objetivo.
	args []*exprNode
}

// parseExpression lee el valor de una propiedad de la plantilla. Un texto que no es una expresión es un literal.
func parseExpression(value string) *exprNode {
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return &exprNode{kind: nodeLiteral, text: value}
	}
	if strings.HasPrefix(value, "[[") {
		return &exprNode{kind: nodeLiteral, text: value[1:]}
	}
	if len(value) > maxExpressionBytes {
		return &exprNode{kind: nodeRaw, text: value}
	}
	p := &exprParser{input: value[1 : len(value)-1]}
	node := p.expression(0)
	p.spaces()
	if node == nil || p.failed || p.pos != len(p.input) {
		return &exprNode{kind: nodeRaw, text: value}
	}
	return node
}

type exprParser struct {
	input  string
	pos    int
	failed bool
}

func (p *exprParser) spaces() {
	for p.pos < len(p.input) && strings.ContainsRune(" \t\r\n", rune(p.input[p.pos])) {
		p.pos++
	}
}

func (p *exprParser) accept(char byte) bool {
	p.spaces()
	if p.pos < len(p.input) && p.input[p.pos] == char {
		p.pos++
		return true
	}
	return false
}

func (p *exprParser) fail() *exprNode {
	p.failed = true
	return nil
}

func (p *exprParser) expression(depth int) *exprNode {
	if depth > maxJSONDepth {
		return p.fail()
	}
	node := p.primary(depth)
	for node != nil && !p.failed {
		switch {
		case p.accept('.'):
			name := p.identifier()
			if name == "" {
				return p.fail()
			}
			node = &exprNode{kind: nodeProperty, text: name, args: []*exprNode{node}}
		case p.accept('['):
			index := p.expression(depth + 1)
			if index == nil || !p.accept(']') {
				return p.fail()
			}
			node = &exprNode{kind: nodeIndex, args: []*exprNode{node, index}}
		default:
			return node
		}
	}
	return node
}

func (p *exprParser) primary(depth int) *exprNode {
	p.spaces()
	if p.pos >= len(p.input) {
		return p.fail()
	}
	switch char := p.input[p.pos]; {
	case char == '\'':
		return p.literal()
	case char >= '0' && char <= '9' || char == '-':
		start := p.pos
		p.pos++
		for p.pos < len(p.input) && (p.input[p.pos] >= '0' && p.input[p.pos] <= '9' || p.input[p.pos] == '.') {
			p.pos++
		}
		return &exprNode{kind: nodeNumber, text: p.input[start:p.pos]}
	}
	name := p.identifier()
	if name == "" || !p.accept('(') {
		return p.fail()
	}
	call := &exprNode{kind: nodeCall, text: strings.ToLower(name)}
	if p.accept(')') {
		return call
	}
	for {
		arg := p.expression(depth + 1)
		if arg == nil {
			return p.fail()
		}
		call.args = append(call.args, arg)
		if p.accept(')') {
			return call
		}
		if !p.accept(',') {
			return p.fail()
		}
	}
}

// identifier lee un nombre; el de una función propia lleva puntos (espacio.función).
func (p *exprParser) identifier() string {
	p.spaces()
	start := p.pos
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$') {
			break
		}
		p.pos++
	}
	return p.input[start:p.pos]
}

// literal lee 'texto'; una comilla dentro se escribe doble.
func (p *exprParser) literal() *exprNode {
	var text strings.Builder
	for p.pos++; p.pos < len(p.input); p.pos++ {
		if p.input[p.pos] != '\'' {
			text.WriteByte(p.input[p.pos])
			continue
		}
		if p.pos+1 < len(p.input) && p.input[p.pos+1] == '\'' {
			text.WriteByte('\'')
			p.pos++
			continue
		}
		p.pos++
		return &exprNode{kind: nodeLiteral, text: text.String()}
	}
	return p.fail()
}

// isCall dice si el nodo es una llamada a name.
func (n *exprNode) isCall(name string) bool { return n != nil && n.kind == nodeCall && n.text == name }

// literalArg devuelve el argumento i de una llamada si es un literal.
func (n *exprNode) literalArg(i int) (string, bool) {
	if n == nil || i >= len(n.args) || n.args[i].kind != nodeLiteral {
		return "", false
	}
	return n.args[i].text, true
}

// path deshace una cadena de propiedades (a.b.c): devuelve el objetivo y los nombres, en orden.
func (n *exprNode) path() (*exprNode, []string) {
	var names []string
	for n != nil && n.kind == nodeProperty {
		names = append([]string{n.text}, names...)
		n = n.args[0]
	}
	return n, names
}

// placeholders sustituye los {n} sencillos de un patrón de format() por fill(n). Si el patrón tiene algo más
// (formatos, llaves escapadas, un hueco sin argumento) devuelve false.
func placeholders(pattern string, count int, fill func(int) string) (string, bool) {
	var out strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '}':
			return "", false
		case '{':
			end := strings.IndexByte(pattern[i:], '}')
			if end < 2 {
				return "", false
			}
			index, err := strconv.Atoi(pattern[i+1 : i+end])
			if err != nil || index < 0 || index >= count {
				return "", false
			}
			out.WriteString(fill(index))
			i += end
		default:
			out.WriteByte(pattern[i])
		}
	}
	return out.String(), true
}
