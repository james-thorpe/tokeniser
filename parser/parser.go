package parser

import (
	"errors"
	"fmt"
	"slices"
)

var (
	ErrUnexpectedEOI   = errors.New("unexpected end of input")
	ErrGrammarMismatch = errors.New("grammar mismatch")
	ErrEmptyText       = errors.New("empty text")
)

// VM operation kinds
const (
	opChar int32 = iota
	opMark
	opSeq
	opFork
	opEof
)

// Node kinds
const (
	nkChars = "char"
	nkMark  = "mark"
	nkSeq   = "seq"
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
	parent *machine
	next   *machine
	pc     int32
	stack  *node
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

// pushChar extends a chars node on top of the stack, or pushes a new one. The
// top node is replaced rather than modified because forks share stack nodes.
func (m *machine) pushChar(r rune) {
	lexeme := string(r)
	if m.stack != nil && m.stack.kind == nkChars {
		lexeme = m.pop().lexeme + lexeme
	}
	m.push(&node{kind: nkChars, lexeme: lexeme})
}

// popToMark returns the nodes above the nearest mark in push order. The mark
// itself is popped and discarded. Nodes are collected top-down, so the slice
// is reversed before returning.
func (m *machine) popToMark() (tos []*node) {
	for s := m.pop(); s.kind != nkMark; s = m.pop() {
		tos = append(tos, s)
	}
	slices.Reverse(tos)
	return
}

// result returns the parse tree at end of input. Top-level Optional and
// ZeroOrMore can leave zero or several nodes: none yields nil, a single node
// is returned as is, and several are wrapped in a seq node in push order.
func (m *machine) result() *node {
	if m.stack == nil || m.stack.next == nil {
		return m.stack
	}
	var children []*node
	for m.stack != nil {
		children = append(children, m.pop())
	}
	slices.Reverse(children)
	return &node{kind: nkSeq, children: children}
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

// Text panics on an empty string: it would match nothing and contribute no
// node, which almost certainly indicates a grammar mistake. Panicking here
// surfaces it at construction rather than as a silently empty match at Run.
func Text(text string) Compiler {
	if text == "" {
		panic(ErrEmptyText)
	}
	return func(p *program) {
		runes := []rune(text)
		for _, r := range runes {
			Char(r)(p)
		}
	}
}

// Seq converts its elements eagerly so that unsupported types and empty text
// panic at construction, consistent with Text.
func Seq(first any, rest ...any) Compiler {
	convertedFirst := convertToCompiler(first)
	convertedRest := convertToCompilers(rest)
	return func(p *program) {
		op(p, opMark)
		convertedFirst(p)
		for _, compiler := range convertedRest {
			compiler(p)
		}
		op(p, opSeq)
	}
}

// Optional matches all of its elements or none of them. It adds no node of its
// own: matched elements contribute directly to the enclosing opSeq, and when
// skipped that sequence simply has fewer children.
func Optional(first any, rest ...any) Compiler {
	convertedFirst := convertToCompiler(first)
	convertedRest := convertToCompilers(rest)
	return func(p *program) {
		fork := len(*p)
		op(p, opFork, 0)
		emit(p, convertedFirst, convertedRest)
		// Target is only known once the elements have been emitted.
		(*p)[fork+1] = int32(len(*p))
	}
}

// ZeroOrMore matches its elements, as a group, any number of times. Like
// Optional it adds no node of its own; each iteration's nodes become children
// of the enclosing opSeq. The loop decision follows the body, so the body is
// emitted once and a backward opFork forms the loop. Matching is greedy.
func ZeroOrMore(first any, rest ...any) Compiler {
	convertedFirst := convertToCompiler(first)
	convertedRest := convertToCompilers(rest)
	return func(p *program) {
		exit := len(*p)
		op(p, opFork, 0)
		body := int32(len(*p))
		emit(p, convertedFirst, convertedRest)
		op(p, opFork, body)
		// Target is only known once the loop has been emitted.
		(*p)[exit+1] = int32(len(*p))
	}
}

// OneOrMore is ZeroOrMore without the entry fork, so the body must match at
// least once. Matching is greedy.
func OneOrMore(first any, rest ...any) Compiler {
	convertedFirst := convertToCompiler(first)
	convertedRest := convertToCompilers(rest)
	return func(p *program) {
		body := int32(len(*p))
		emit(p, convertedFirst, convertedRest)
		op(p, opFork, body)
	}
}

func emit(p *program, first Compiler, rest []Compiler) {
	first(p)
	for _, compiler := range rest {
		compiler(p)
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

func convertToCompilers(objs []any) []Compiler {
	convertedRest := make([]Compiler, len(objs))
	for i, item := range objs {
		convertedRest[i] = convertToCompiler(item)
	}
	return convertedRest
}

// appendPending adds m at the tail so machine order, and with it match
// priority, is the same on every step. Returns the updated head and tail.
func appendPending(head, tail, m *machine) (*machine, *machine) {
	m.next = nil
	if tail == nil {
		return m, m
	}
	tail.next = m
	return head, m
}

func Run(c Compiler, runes []rune) (*node, error) {
	prog := make(program, 0, 64)
	c(&prog)         // compile state machine
	op(&prog, opEof) // add eof check
	start := &machine{}
	running := start
	// Step (rune index + 1) at which each pc was last entered. A later arrival
	// at the same pc in the same step has the same future but lower priority,
	// so it is dropped. Without this, a loop whose body can match empty input
	// would fork forever.
	entered := make([]int, len(prog))
	var pending *machine = nil
	var pendingTail *machine = nil
	runes = append(runes, -1)
	for i, r := range runes {
		// Drained until empty because opFork queues forks that must run on the
		// same rune.
		for running != nil {
			m := running
			running = running.next
		loop:
			if entered[m.pc] == i+1 {
				continue
			}
			entered[m.pc] = i + 1
			op := prog[m.pc]
			switch op {
			case opChar:
				if r >= prog[m.pc+1] && r <= prog[m.pc+2] {
					m.pc += 3
					m.pushChar(r)
					pending, pendingTail = appendPending(pending, pendingTail, m)
				}
			case opMark:
				m.pc += 1
				m.push(&node{kind: nkMark})
				goto loop
			case opSeq:
				m.pc += 1
				m.push(&node{
					kind:     nkSeq,
					children: m.popToMark(),
				})
				goto loop
			case opFork:
				// m takes the preferred path; the fork takes the other and shares
				// the stack tail, which push never mutates. The fork is queued
				// directly after m so it runs on this rune ahead of lower-priority
				// machines. Falling through is preferred, except on a backward
				// fork (a loop back-edge), where staying in the loop is preferred
				// so repetition is greedy.
				target, next := prog[m.pc+1], m.pc+2
				if target < m.pc {
					target, next = next, target
				}
				running = &machine{pc: target, stack: m.stack, next: running}
				m.pc = next
				goto loop
			case opEof:
				if r == -1 {
					return m.result(), nil
				}
			}
		}
		if r == -1 {
			return nil, fmt.Errorf("%w at offset %d", ErrUnexpectedEOI, i)
		}
		// Fail at the first rune no machine accepts, rather than carrying on to
		// end of input, so the error reports where the input diverged.
		if pending == nil {
			return nil, fmt.Errorf("%w: unexpected %q at offset %d", ErrGrammarMismatch, r, i)
		}
		running, pending, pendingTail = pending, nil, nil
	}
	return nil, nil // should never get here
}
