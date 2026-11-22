package parser

import (
	"errors"
	"fmt"
)

var (
	ErrUnexpectedEOI   = errors.New("unexpected end of input")
	ErrGrammarMismatch = errors.New("grammar mismatch")
	ErrEmptyText       = errors.New("empty text")
)

// VM operation kinds
const (
	opChar int32 = iota
	opCat
	opEof
)

// Node kinds
const (
	nkChar = "char"
	nkSeq  = "seq"
)

type node struct {
	kind     string
	children []*node
	lexeme   string
	next     *node // stack link
}

// stack is the top of a linked list of nodes. Popping never clears next, so
// stack tails remain immutable and can be shared between machines.
type machine struct {
	pc    int32
	stack *node
}

func (m *machine) push(s *node) {
	s.next = m.stack
	m.stack = s
}

func (m *machine) pop() (tos *node) {
	tos = m.stack
	m.stack = tos.next
	return
}

// popn returns nodes in push order, so the list is filled from the end.
func (m *machine) popn(n int32) (tos []*node) {
	tos = make([]*node, n)
	for i := n - 1; i >= 0; i-- {
		tos[i] = m.pop()
	}
	return
}

func op(p *program, ops ...int32) {
	*p = append(*p, ops...)
}

type program []int32
type Compiler func(p *program)

func Char(r rune) Compiler {
	return Range(r, r)
}

func Range(lo, hi rune) Compiler {
	return func(p *program) {
		op(p, opChar, lo, hi)
	}
}

// Text panics on an empty string: it would emit no node, leaving opCat and
// opEof to pop from a stack one short. Panicking here surfaces the grammar
// error at construction rather than at Run.
func Text(text string) Compiler {
	if text == "" {
		panic(ErrEmptyText)
	}
	return func(p *program) {
		runes := []rune(text)
		Char(runes[0])(p)
		for _, r := range runes[1:] {
			Char(r)(p)
			op(p, opCat)
		}
	}
}

// Seq converts its elements eagerly so that unsupported types and empty text
// panic at construction, consistent with Text.
func Seq(first any, rest ...any) Compiler {
	cFirst := convertToCompiler(first)
	cRest := make([]Compiler, len(rest))
	for i, obj := range rest {
		cRest[i] = convertToCompiler(obj)
	}
	return func(p *program) {
		cFirst(p)
		for _, c := range cRest {
			c(p)
			op(p, opCat)
		}
	}
}

func convertToCompiler(obj any) Compiler {
	switch v := obj.(type) {
	case Compiler:
		return v
	case string:
		return Text(v)
	case rune:
		return Char(v)
	default:
		panic("unable to convert to compiler") // coding error not runtime error
	}
}

func Run(c Compiler, runes []rune) (*node, error) {
	prog := make(program, 0, 64)
	c(&prog)         // compile state machine
	op(&prog, opEof) // add eof check
	start := &machine{}
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
				if r >= prog[m.pc+1] && r <= prog[m.pc+2] {
					m.pc += 3
					m.push(&node{
						kind:   nkChar,
						lexeme: string(r),
					})
					pending = append(pending, m)
				}
			case opCat:
				m.pc += 1
				right := m.pop()
				left := m.pop()
				m.push(&node{
					kind:     nkSeq,
					children: []*node{left, right},
				})
				goto loop
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
