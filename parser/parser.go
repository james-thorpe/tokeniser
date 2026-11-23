package parser

import (
	"errors"
	"fmt"
	"slices"
	"strings"
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
	opToken
	opRule
	opFork
	opJmp
	opEof
)

// Node kinds. Negative values are reserved for the parser; zero and positive
// values are left for user-defined kinds.
const (
	NkChars int32 = -(iota + 1)
	nkMark
	NkSeq
)

type Node struct {
	Kind     int32
	Children []*Node
	Lexeme   string
	nos      *Node // next on stack
	sealed   bool  // set on Token and Rule nodes so later chars start a new node
}

// tos is the top of a linked list of nodes. Popping never clears next, so
// tos tails remain immutable and can be shared between machines.
type machine struct {
	parent *machine
	next   *machine
	pc     int32
	tos    *Node // top of stack
}

func (m *machine) push(s *Node) {
	s.nos = m.tos
	m.tos = s
}

func (m *machine) pop() (tos *Node) {
	tos = m.tos
	m.tos = tos.nos
	return
}

// pushChar extends a chars node on top of the tos, or pushes a new one. The
// top node is replaced rather than modified because forks share tos nodes.
func (m *machine) pushChar(r rune) {
	lexeme := string(r)
	if m.tos != nil && m.tos.Kind == NkChars && !m.tos.sealed {
		lexeme = m.pop().Lexeme + lexeme
	}
	m.push(&Node{Kind: NkChars, Lexeme: lexeme})
}

// popToMark returns the nodes above the nearest mark in push order. The mark
// itself is popped and discarded. Nodes are collected top-down, so the slice
// is reversed before returning.
func (m *machine) popToMark() (tos []*Node) {
	for s := m.pop(); s.Kind != nkMark; s = m.pop() {
		tos = append(tos, s)
	}
	slices.Reverse(tos)
	return
}

// result returns the parse tree at end of input. Top-level Optional and
// ZeroOrMore can leave zero or several nodes: none yields nil, a single node
// is returned as is, and several are wrapped in a seq node in push order.
func (m *machine) result() *Node {
	if m.tos == nil || m.tos.nos == nil {
		return m.tos
	}
	var children []*Node
	for m.tos != nil {
		children = append(children, m.pop())
	}
	slices.Reverse(children)
	return &Node{Kind: NkSeq, Children: children}
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

// Rule matches its elements like Seq and leaves one interior node of the given
// kind holding their nodes. The node is sealed so that, when kind is NkChars,
// characters matched after it start a new node instead of replacing it.
func Rule(kind int32, first any, rest ...any) Compiler {
	convertedFirst := convertToCompiler(first)
	convertedRest := convertToCompilers(rest)
	return func(p *program) {
		op(p, opMark)
		emit(p, convertedFirst, convertedRest)
		op(p, opRule, kind)
	}
}

// Token matches its elements like Seq but leaves a single leaf node of the
// given kind whose lexeme is every character matched beneath it, in order,
// however deeply nested. The node is sealed so that, even when kind is
// NkChars, characters matched after it start a new node instead of extending
// the token. A token that matches nothing yields a node with an empty lexeme.
func Token(kind int32, first any, rest ...any) Compiler {
	convertedFirst := convertToCompiler(first)
	convertedRest := convertToCompilers(rest)
	return func(p *program) {
		op(p, opMark)
		emit(p, convertedFirst, convertedRest)
		op(p, opToken, kind)
	}
}

func flatten(b *strings.Builder, nodes []*Node) {
	for _, n := range nodes {
		b.WriteString(n.Lexeme)
		flatten(b, n.Children)
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

// Choice matches exactly one of its alternatives, preferring earlier ones when
// several succeed. Each alternative is a single element; use Seq to group
// several. Like Optional it adds no node of its own. Each alternative but the
// last is preceded by a fork to the next and followed by a jump past the rest.
func Choice(first any, rest ...any) Compiler {
	alts := append([]Compiler{convertToCompiler(first)}, convertToCompilers(rest)...)
	return func(p *program) {
		last := len(alts) - 1
		jumps := make([]int, 0, last)
		for _, alt := range alts[:last] {
			fork := len(*p)
			op(p, opFork, 0)
			alt(p)
			jumps = append(jumps, len(*p))
			op(p, opJmp, 0)
			(*p)[fork+1] = int32(len(*p))
		}
		alts[last](p)
		// The end is only known once every alternative has been emitted.
		for _, jmp := range jumps {
			(*p)[jmp+1] = int32(len(*p))
		}
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

func Run(c Compiler, runes []rune) (*Node, error) {
	prog := make(program, 0, 64)
	c(&prog)         // compile state machine
	op(&prog, opEof) // add eof check
	start := &machine{}
	running := start
	// Step (rune index + 1) at which each pc was last entered. A later arrival
	// at the same pc in the same step has the same future but lower priority,
	// so it is dropped. Without this, a loop whose body can match empty input
	// would fork forever.
	entered := make([]int, len(prog)) // don't forget this is initialised to 0s
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
				m.push(&Node{Kind: nkMark})
				goto loop
			case opSeq:
				m.pc += 1
				m.push(&Node{
					Kind:     NkSeq,
					Children: m.popToMark(),
				})
				goto loop
			case opToken:
				kind := prog[m.pc+1]
				m.pc += 2
				var b strings.Builder
				flatten(&b, m.popToMark())
				m.push(&Node{Kind: kind, Lexeme: b.String(), sealed: true})
				goto loop
			case opRule:
				kind := prog[m.pc+1]
				m.pc += 2
				m.push(&Node{Kind: kind, Children: m.popToMark(), sealed: true})
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
				running = &machine{pc: target, tos: m.tos, next: running}
				m.pc = next
				goto loop
			case opJmp:
				m.pc = prog[m.pc+1]
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
