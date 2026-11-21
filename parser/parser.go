package parser

import (
	"errors"
	"fmt"
	"slices"
)

var (
	ErrUnexpectedEOI        = errors.New("unexpected end of input")
	ErrNonCharInText        = errors.New("non-character found in text")
	ErrGrammarMismatch      = errors.New("grammar mismatch")
	ErrEmptyText            = errors.New("empty text")
	ErrEmptyName            = errors.New("empty rule name")
	ErrDuplicateRule        = errors.New("duplicate rule")
	ErrUnresolvedLazy       = errors.New("lazy variable is nil")
	ErrRecursionWithoutRule = errors.New("recursion without a rule")
	ErrLeftRecursion        = errors.New("left recursion")
)

// VM operation kinds
const (
	opChar int32 = iota
	opRange
	opText
	opSeq
	opRule
	opCall
	opRet
	opSplit
	opJmp
	opMark
	opOptional
	opMany
	opEof
)

// Operation widths, including operands, for walking the bytecode.
var widths = [...]int32{
	opChar:     2,
	opRange:    3,
	opText:     2,
	opSeq:      2,
	opRule:     3,
	opCall:     2,
	opRet:      1,
	opSplit:    3,
	opJmp:      2,
	opMark:     1,
	opOptional: 1,
	opMany:     1,
	opEof:      1,
}

// Values of Node.Kind.
const (
	KindChar     = "char"     // a rune matched by Char or Range
	KindText     = "text"     // a string matched by Text
	KindSeq      = "seq"      // the parts matched by Seq
	KindRule     = "rule"     // the parts matched by a Rule
	KindOptional = "optional" // Optional: zero or one child
	KindMany     = "many"     // Many or OneOrMore: one child per repetition
	// Internal: bounds the variable number of nodes opOptional and opMany
	// collect. Never appears in a returned tree.
	nkMark = "mark"
)

// Node is a node of the parse tree returned by Run. Choice and Lazy add
// no node of their own; the node is that of the alternative or rule matched.
type Node struct {
	Kind     string  // one of the Kind constants
	Name     string  // rule name; set only for KindRule
	Children []*Node // set only for KindSeq, KindRule, KindOptional and KindMany
	Lexeme   string  // matched text; set only for KindChar and KindText
}

// Immutable so machines forked from a common ancestor can share their return
// history rather than copy it. Interned per run, so equal stacks are the same
// pointer.
type frame struct {
	pc   int32
	next *frame
}

type machine struct {
	pc     int32
	frames *frame
	stack  []*Node
}

// The node stack is copied because each thread appends to its own; frames are
// immutable and shared.
func (m *machine) fork(pc int32) *machine {
	return &machine{
		pc:     pc,
		frames: m.frames,
		stack:  slices.Clone(m.stack),
	}
}

func (m *machine) push(s *Node) {
	m.stack = append(m.stack, s)
}

func (m *machine) pop() (tos *Node) {
	top := len(m.stack) - 1
	tos = m.stack[top]
	m.stack = m.stack[:top]
	return
}

// Pops every node above the nearest mark, and the mark itself. Nested Optional
// and Many each push their own mark, so the nearest one is always the right
// one.
func (m *machine) popMark() []*Node {
	i := len(m.stack) - 1
	for m.stack[i].Kind != nkMark {
		i--
	}
	tos := slices.Clone(m.stack[i+1:])
	m.stack = m.stack[:i]
	return tos
}

func (m *machine) popn(n int32) (tos []*Node) {
	top := len(m.stack) - int(n)
	// Copied because the caller pushes immediately, which would otherwise
	// overwrite the returned states through the shared backing array.
	tos = slices.Clone(m.stack[top:])
	m.stack = m.stack[:top]
	return
}

func op(p *program, ops ...int32) {
	p.code = append(p.code, ops...)
}

// Identity of a rule is the value Rule returns, so using that value in several
// places shares one body.
type ruleDef struct {
	name  string
	parts []Compiler
}

// A rule body is compiled once, out of line, and reached through opCall, so a
// rule can refer to itself without unbounded inline expansion.
type rule struct {
	def   *ruleDef
	entry int32 // set once the body is compiled
}

// Rules live outside the bytecode so every operation stays a fixed width of
// int32 operands; opCall and opRule refer to a rule by index.
type program struct {
	code   []int32
	rules  []rule
	active map[*lazy]bool // Lazy compilers currently expanding
	err    error
}

// Returns the rule index for d, adding it on first use. Names label tree
// nodes, so two distinct rules sharing one would be indistinguishable in the
// tree.
func (p *program) define(d *ruleDef) int32 {
	for i, r := range p.rules {
		if r.def == d {
			return int32(i)
		}
		if r.def.name == d.name {
			p.fail(fmt.Errorf("%w: %q", ErrDuplicateRule, d.name))
		}
	}
	p.rules = append(p.rules, rule{def: d})
	return int32(len(p.rules) - 1)
}

// Compilers cannot return errors, so the first is recorded and reported once
// compilation finishes.
func (p *program) fail(err error) {
	if p.err == nil {
		p.err = err
	}
}

type Compiler func(p *program)

// Char matches a single rune, yielding a char node.
//
// Compiles to:
//
//	opChar r
func Char(r rune) Compiler {
	return func(p *program) {
		op(p, opChar, r)
	}
}

// Range matches a single rune in lo..hi inclusive, yielding a char node.
//
// Compiles to:
//
//	opRange lo hi
func Range(lo, hi rune) Compiler {
	return func(p *program) {
		op(p, opRange, lo, hi)
	}
}

// Text matches a literal string, yielding one text node. The opText operand
// counts runes, not bytes:
//
//	opChar r1
//	...
//	opChar rN
//	opText N
//
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

// Seq matches each part in order, yielding a seq node with one child per part.
//
// Compiles to:
//
//	<first>
//	...
//	<last>
//	opSeq N
func Seq(first Compiler, rest ...Compiler) Compiler {
	return func(p *program) {
		first(p)
		for _, compiler := range rest {
			compiler(p)
		}
		op(p, opSeq, int32(len(rest)+1))
	}
}

// Choice matches any one of its alternatives and yields that alternative's
// node. Where several match, the leftmost wins.
//
// Compiles to a chain of splits; each alternative but the last jumps to the
// end, whose address is patched in once known:
//
//	    opSplit L1 L2
//	L1: <first>
//	    opJmp END
//	L2: opSplit L3 L4
//	L3: <second>
//	    opJmp END
//	L4: <last>
//	END:
func Choice(first Compiler, rest ...Compiler) Compiler {
	alts := append([]Compiler{first}, rest...)
	return func(p *program) {
		jumps := make([]int, 0, len(alts)-1)
		for _, alt := range alts[:len(alts)-1] {
			split := len(p.code)
			op(p, opSplit, int32(split+3), 0)
			alt(p)
			op(p, opJmp, 0)
			jumps = append(jumps, len(p.code)-1)
			p.code[split+2] = int32(len(p.code))
		}
		alts[len(alts)-1](p)
		for _, at := range jumps {
			p.code[at] = int32(len(p.code))
		}
	}
}

// Optional matches p or nothing, preferring p. Yields an optional node with
// one child, or none.
//
// The child count is not known at compile time, so a mark bounds the nodes
// that opOptional collects:
//
//	    opMark
//	    opSplit L1 END
//	L1: <p>
//	END: opOptional
func Optional(p Compiler) Compiler {
	return func(prog *program) {
		op(prog, opMark)
		split := len(prog.code)
		op(prog, opSplit, int32(split+3), 0)
		p(prog)
		prog.code[split+2] = int32(len(prog.code))
		op(prog, opOptional)
	}
}

// Many matches p zero or more times, greedily. Yields a many node with one
// child per repetition.
//
// A mark bounds the nodes that opMany collects; the loop's split prefers
// another repetition over leaving:
//
//	    opMark
//	L1: opSplit L2 END
//	L2: <p>
//	    opJmp L1
//	END: opMany
//
// If p can match nothing, the loop could repeat without consuming; the VM
// drops a thread that revisits a split within one step, ending such loops.
func Many(p Compiler) Compiler {
	return func(prog *program) {
		op(prog, opMark)
		loop := len(prog.code)
		op(prog, opSplit, int32(loop+3), 0)
		p(prog)
		op(prog, opJmp, int32(loop))
		prog.code[loop+2] = int32(len(prog.code))
		op(prog, opMany)
	}
}

// OneOrMore matches p one or more times, greedily. Yields a many node with one
// child per repetition.
//
// The body runs once before the loop's split, which prefers jumping back for
// another repetition over leaving:
//
//	    opMark
//	L1: <p>
//	    opSplit L1 END
//	END: opMany
//
// As with Many, a body that can match nothing is ended by the VM dropping a
// thread that revisits the split within one step.
func OneOrMore(p Compiler) Compiler {
	return func(prog *program) {
		op(prog, opMark)
		loop := len(prog.code)
		p(prog)
		op(prog, opSplit, int32(loop), int32(len(prog.code)+3))
		op(prog, opMany)
	}
}

// Rule is a named sequence, yielding a rule node with one child per part.
// Assign the result to a variable and pass it into later rules; every use of
// that value calls one shared body, compiled once after the main program's
// opEof, however many times or however recursively it is used. A rule that
// refers to itself, or to a rule assigned later, does so through Lazy.
//
// Each use compiles to:
//
//	opCall ID
//
// and the body, once, to:
//
//	ID: <first>
//	    ...
//	    <last>
//	    opRule N ID
//	    opRet
func Rule(name string, first Compiler, rest ...Compiler) Compiler {
	if name == "" {
		panic(ErrEmptyName)
	}
	d := &ruleDef{name: name, parts: append([]Compiler{first}, rest...)}
	return func(p *program) {
		op(p, opCall, p.define(d))
	}
}

type lazy struct {
	c *Compiler
}

// Lazy reads the variable c points to at compile time rather than now, so a
// rule can refer to itself, or to a rule assigned later:
//
//	var p Compiler
//	p = Rule("p", Char('('), Optional(Lazy(&p)), Char(')'))
//
// Recursion must pass through a Rule: a rule's use emits only a call, so the
// expansion stops there. A Lazy that reaches itself without one would expand
// forever, and instead fails compilation with ErrRecursionWithoutRule. A
// variable still nil at compile time fails with ErrUnresolvedLazy; a nil
// pointer panics with ErrUnresolvedLazy immediately.
//
// Emits nothing of its own; compiles to whatever *c holds:
//
//	<*c>
func Lazy(c *Compiler) Compiler {
	if c == nil {
		panic(ErrUnresolvedLazy)
	}
	l := &lazy{c: c}
	return func(p *program) {
		if p.active[l] {
			p.fail(ErrRecursionWithoutRule)
			return
		}
		target := *l.c
		if target == nil {
			p.fail(ErrUnresolvedLazy)
			return
		}
		if p.active == nil {
			p.active = make(map[*lazy]bool)
		}
		p.active[l] = true
		target(p)
		delete(p.active, l)
	}
}

func compile(c Compiler) (*program, error) {
	p := &program{code: make([]int32, 0, 64)}
	c(p)         // compile state machine
	op(p, opEof) // add eof check
	p.compileBodies()
	if p.err != nil {
		return nil, p.err
	}
	if err := p.checkLeftRecursion(); err != nil {
		return nil, err
	}
	return p, nil
}

// Indexed rather than ranged: compiling a body can add rules, which later
// iterations then compile.
func (p *program) compileBodies() {
	for i := 0; i < len(p.rules); i++ {
		d := p.rules[i].def
		p.rules[i].entry = int32(len(p.code))
		for _, part := range d.parts {
			part(p)
		}
		op(p, opRule, int32(len(d.parts)), int32(i), opRet)
	}
}

// A rule is left recursive when it can reach a call to itself without
// consuming input, directly or through other rules. Calls to nullable rules
// are stepped over, since execution continues past them without consuming.
func (p *program) checkLeftRecursion() error {
	nullable := p.nullableRules()
	state := make([]visit, len(p.rules))
	for i := range p.rules {
		if id, ok := p.leftCycle(int32(i), state, nullable); ok {
			return fmt.Errorf("%w: %q", ErrLeftRecursion, p.rules[id].def.name)
		}
	}
	return nil
}

// Rules that can complete without consuming input. Iterated to a fixpoint
// because a rule's nullability depends on the rules it calls.
func (p *program) nullableRules() []bool {
	nullable := make([]bool, len(p.rules))
	for changed := true; changed; {
		changed = false
		for i, r := range p.rules {
			if nullable[i] {
				continue
			}
			if _, ret := p.leading(r.entry, nullable); ret {
				nullable[i] = true
				changed = true
			}
		}
	}
	return nullable
}

type visit int8

const (
	unvisited visit = iota
	visiting
	visited
)

// Depth-first search over leading calls; reaching a rule still being visited
// closes a cycle.
func (p *program) leftCycle(id int32, state []visit, nullable []bool) (int32, bool) {
	switch state[id] {
	case visiting:
		return id, true
	case visited:
		return 0, false
	}
	state[id] = visiting
	calls, _ := p.leading(p.rules[id].entry, nullable)
	for _, callee := range calls {
		if c, ok := p.leftCycle(callee, state, nullable); ok {
			return c, true
		}
	}
	state[id] = visited
	return 0, false
}

// Walks every path from pc that consumes no input, returning the rules called
// along them and whether the end of the rule body is reachable. Visited pcs
// are tracked because Many emits a backward jump.
func (p *program) leading(pc int32, nullable []bool) (calls []int32, ret bool) {
	seen := make(map[int32]bool)
	work := []int32{pc}
	for len(work) > 0 {
		pc := work[len(work)-1]
		work = work[:len(work)-1]
		if seen[pc] {
			continue
		}
		seen[pc] = true
		switch c := p.code[pc]; c {
		case opChar, opRange, opEof:
		case opRet:
			ret = true
		case opCall:
			id := p.code[pc+1]
			calls = append(calls, id)
			if nullable[id] {
				work = append(work, pc+2)
			}
		case opSplit:
			work = append(work, p.code[pc+1], p.code[pc+2])
		case opJmp:
			work = append(work, p.code[pc+1])
		default:
			work = append(work, pc+widths[c])
		}
	}
	return calls, ret
}

// A thread's future depends only on its pc and return stack; its node stack
// affects the tree it builds, not whether it matches.
type threadKey struct {
	pc     int32
	frames *frame
}

type vm struct {
	prog    *program
	threads []*machine             // forks awaiting execution within the current step
	pending []*machine             // threads that consumed the current rune
	seen    map[threadKey]struct{} // states claimed by consuming this step
	visited map[threadKey]struct{} // splits reached without consuming this step
	frames  map[frame]*frame
	peak    int // most threads pending after any step; instrumentation only
}

func newVM(prog *program) *vm {
	return &vm{
		prog:    prog,
		threads: make([]*machine, 0, 16),
		pending: make([]*machine, 0, 16),
		seen:    make(map[threadKey]struct{}),
		visited: make(map[threadKey]struct{}),
		frames:  make(map[frame]*frame),
	}
}

func Run(c Compiler, runes []rune) (*Node, error) {
	prog, err := compile(c)
	if err != nil {
		return nil, err
	}
	return newVM(prog).run(runes)
}

func (v *vm) run(runes []rune) (*Node, error) {
	running := make([]*machine, 0, 16)
	running = append(running, &machine{stack: make([]*Node, 0, 16)})
	runes = append(runes, -1)
	for i, r := range runes {
		clear(v.seen)
		clear(v.visited)
		for _, m := range running {
			if n, err := v.step(m, r, i); n != nil || err != nil {
				return n, err
			}
		}
		if r == -1 {
			return nil, fmt.Errorf("%w at offset %d", ErrUnexpectedEOI, i)
		}
		// Fail at the first rune no machine accepts, rather than carrying on to
		// end of input, so the error reports where the input diverged.
		if len(v.pending) == 0 {
			return nil, fmt.Errorf("%w: unexpected %q at offset %d", ErrGrammarMismatch, r, i)
		}
		v.peak = max(v.peak, len(v.pending))
		running, v.pending = v.pending, running[:0]
	}
	return nil, nil // should never get here
}

// Threads are claimed in priority order, so when two reach the same state the
// one kept is the leftmost alternative. Without this, ambiguous grammars
// double the thread count at every ambiguity.
func (v *vm) consume(t *machine, width int32, r rune) {
	t.pc += width
	key := threadKey{pc: t.pc, frames: t.frames}
	if _, ok := v.seen[key]; ok {
		return
	}
	v.seen[key] = struct{}{}
	t.push(&Node{Kind: KindChar, Lexeme: string(r)})
	v.pending = append(v.pending, t)
}

// Kept separate from seen: a consumed thread at pc X will execute X next step,
// while a thread visiting X now is at a different input position.
//
// Every bytecode cycle passes through Many's split, so dropping a thread that
// revisits a split ends loops whose body matched nothing. It also merges
// threads converging on a split, keeping the higher-priority one.
func (v *vm) visit(t *machine) bool {
	key := threadKey{pc: t.pc, frames: t.frames}
	if _, ok := v.visited[key]; ok {
		return false
	}
	v.visited[key] = struct{}{}
	return true
}

// Interning makes identity equality of return stacks imply structural
// equality, so threadKey can compare frames by pointer.
func (v *vm) frame(pc int32, next *frame) *frame {
	key := frame{pc: pc, next: next}
	if f, ok := v.frames[key]; ok {
		return f
	}
	f := &key
	v.frames[key] = f
	return f
}

// Runs m and every thread forked from it against r. Forks are taken LIFO so a
// thread's own alternatives all run before alternatives forked earlier,
// keeping pending, and therefore acceptance, in leftmost-alternative order.
func (v *vm) step(m *machine, r rune, i int) (*Node, error) {
	v.threads = append(v.threads[:0], m)
	for len(v.threads) > 0 {
		t := v.threads[len(v.threads)-1]
		v.threads = v.threads[:len(v.threads)-1]
		if n, err := v.exec(t, r, i); n != nil || err != nil {
			return n, err
		}
	}
	return nil, nil
}

// Runs t until it consumes r, dies, or accepts at end of input.
func (v *vm) exec(t *machine, r rune, i int) (*Node, error) {
	code := v.prog.code
	for {
		switch code[t.pc] {
		case opChar:
			if r == code[t.pc+1] {
				v.consume(t, 2, r)
			}
			return nil, nil
		case opRange:
			if r >= code[t.pc+1] && r <= code[t.pc+2] {
				v.consume(t, 3, r)
			}
			return nil, nil
		case opText:
			s, err := text(t.popn(code[t.pc+1]), i)
			if err != nil {
				return nil, err
			}
			t.pc += 2
			t.push(s)
		case opSeq:
			n := code[t.pc+1]
			t.pc += 2
			t.push(&Node{Kind: KindSeq, Children: t.popn(n)})
		case opRule:
			n, id := code[t.pc+1], code[t.pc+2]
			t.pc += 3
			t.push(&Node{Kind: KindRule, Name: v.prog.rules[id].def.name, Children: t.popn(n)})
		case opCall:
			t.frames = v.frame(t.pc+2, t.frames)
			t.pc = v.prog.rules[code[t.pc+1]].entry
		case opRet:
			t.pc = t.frames.pc
			t.frames = t.frames.next
		case opSplit:
			if !v.visit(t) {
				return nil, nil
			}
			v.threads = append(v.threads, t.fork(code[t.pc+2]))
			t.pc = code[t.pc+1]
		case opJmp:
			t.pc = code[t.pc+1]
		case opMark:
			t.pc++
			t.push(&Node{Kind: nkMark})
		case opOptional:
			t.pc++
			t.push(&Node{Kind: KindOptional, Children: t.popMark()})
		case opMany:
			t.pc++
			t.push(&Node{Kind: KindMany, Children: t.popMark()})
		case opEof:
			if r != -1 {
				return nil, nil
			}
			return t.pop(), nil
		default:
			panic(fmt.Sprintf("parser: unknown op %d at pc %d", code[t.pc], t.pc))
		}
	}
}

func text(children []*Node, i int) (*Node, error) {
	s := &Node{Kind: KindText}
	for _, child := range children {
		if child.Kind != KindChar && child.Kind != KindText {
			return nil, fmt.Errorf("%w at offset %d", ErrNonCharInText, i)
		}
		s.Lexeme += child.Lexeme
	}
	return s, nil
}
