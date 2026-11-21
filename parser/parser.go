package parser

import (
	"errors"
	"fmt"
	"slices"
)

var (
	ErrUnexpectedEOI   = errors.New("unexpected end of input")
	ErrNonCharInText   = errors.New("non-character found in text")
	ErrGrammarMismatch = errors.New("grammar mismatch")
	ErrEmptyText       = errors.New("empty text")
)

// VM operation kinds
const (
	opChar int32 = iota
	opRange
	opText
	opSeq
	opJmp
	opEof
)

// Node kinds
const (
	nkChar = "char"
	nkText = "text"
	nkSeq  = "seq"
)

type node struct {
	kind     string
	children []*node
	lexeme   string
}

type machine struct {
	pc    int32
	stack []*node
}

func (m *machine) push(s *node) {
	m.stack = append(m.stack, s)
}

func (m *machine) pop() (tos *node) {
	top := len(m.stack) - 1
	tos = m.stack[top]
	m.stack = m.stack[:top]
	return
}

func (m *machine) popn(n int32) (tos []*node) {
	top := len(m.stack) - int(n)
	// Copied because the caller pushes immediately, which would otherwise
	// overwrite the returned states through the shared backing array.
	tos = slices.Clone(m.stack[top:])
	m.stack = m.stack[:top]
	return
}

func op(p *program, ops ...int32) {
	*p = append(*p, ops...)
}

type program []int32
type Compiler func(p *program)

func Char(r rune) Compiler {
	return func(p *program) {
		op(p, opChar, r)
	}
}

func Range(lo, hi rune) Compiler {
	return func(p *program) {
		op(p, opRange, lo, hi)
	}
}

// Text panics on an empty string: it would emit no node, leaving opSeq and
// opEof to pop from a stack one short. Panicking here surfaces the grammar
// error at construction rather than at Run.
func Text(text string) Compiler {
	if text == "" {
		panic(ErrEmptyText)
	}
	return func(p *program) {
		i := 0
		for _, r := range text {
			op(p, opChar, r)
			i++
		}
		op(p, opText, int32(i))
	}
}

func Seq(first Compiler, rest ...Compiler) Compiler {
	return func(p *program) {
		first(p)
		for _, compiler := range rest {
			compiler(p)
		}
		op(p, opSeq, int32(len(rest)+1))
	}
}

func Run(c Compiler, runes []rune) (*node, error) {
	prog := make(program, 0, 64)
	c(&prog)         // compile state machine
	op(&prog, opEof) // add eof check
	start := &machine{
		pc:    0,
		stack: make([]*node, 0, 16),
	}
	running := make([]*machine, 0, 16)
	pending := make([]*machine, 0, 16)
	running = append(running, start)
	runes = append(runes, -1)
	for i, r := range runes {
		for _, m := range running {
		loop:
			op := prog[m.pc]
			switch op {
			case opChar:
				if r == prog[m.pc+1] {
					m.pc += 2
					m.push(&node{
						kind:   nkChar,
						lexeme: string(r),
					})
					pending = append(pending, m)
				}
			case opRange:
				if r >= prog[m.pc+1] && r <= prog[m.pc+2] {
					m.pc += 3
					m.push(&node{
						kind:   nkChar,
						lexeme: string(r),
					})
					pending = append(pending, m)
				}
			case opText:
				len := prog[m.pc+1]
				m.pc += 2
				children := m.popn(len)
				s := &node{
					kind:   nkText,
					lexeme: "",
				}
				for _, child := range children {
					if child.kind != nkChar && child.kind != nkText {
						return nil, fmt.Errorf("%w at offset %d", ErrNonCharInText, i)
					}
					s.lexeme = s.lexeme + child.lexeme
				}
				m.push(s)
				goto loop
			case opSeq:
				len := prog[m.pc+1]
				m.pc += 2
				m.push(&node{
					kind:     nkSeq,
					children: m.popn(len),
				})
				goto loop // didn't consume anything so try next operation
			case opEof:
				if r == -1 {
					return m.pop(), nil
				}
			}
		}
		if r == -1 {
			return nil, fmt.Errorf("%w at offset %d", ErrUnexpectedEOI, i)
		}
		// Fail at the first rune no machine accepts, rather than carrying on to
		// end of input, so the error reports where the input diverged.
		if len(pending) == 0 {
			return nil, fmt.Errorf("%w: unexpected %q at offset %d", ErrGrammarMismatch, r, i)
		}
		running, pending = pending, running
		pending = pending[:0]
	}
	return nil, nil // should never get here
}
