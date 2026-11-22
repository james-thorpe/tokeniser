package parser

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func Test_CharCompile(t *testing.T) {
	compiles(t, Char('a'), opChar, 'a', 'a')
	compiles(t, Char('😀'), opChar, '😀', '😀')
}

func Test_RangeCompile(t *testing.T) {
	compiles(t, Range('a', 'z'), opChar, 'a', 'z')
}

// A sequence brackets its elements with opMark and opSeq.
func Test_SeqCompile(t *testing.T) {
	compiles(t, Seq(Char('a')), opMark, opChar, 'a', 'a', opSeq)
	compiles(t, Seq(Char('a'), Range('0', '9'), Char('b')),
		opMark,
		opChar, 'a', 'a',
		opChar, '0', '9',
		opChar, 'b', 'b',
		opSeq)
	compiles(t, Seq(Seq(Char('a'), Char('b')), Char('c')),
		opMark,
		opMark,
		opChar, 'a', 'a',
		opChar, 'b', 'b',
		opSeq,
		opChar, 'c', 'c',
		opSeq)
}

// Text emits only chars; they merge into a single node at run time.
func Test_TextCompile(t *testing.T) {
	compiles(t, Text("a"), opChar, 'a', 'a')
	compiles(t, Text("abc"),
		opChar, 'a', 'a',
		opChar, 'b', 'b',
		opChar, 'c', 'c')
}

// Text splits on runes; splitting on bytes would emit partial code points.
func Test_TextCompileMultibyte(t *testing.T) {
	compiles(t, Text("é"), opChar, 'é', 'é')
	compiles(t, Text("a😀b"),
		opChar, 'a', 'a',
		opChar, '😀', '😀',
		opChar, 'b', 'b')
}

// Invalid UTF-8 decodes to U+FFFD on both the grammar and input side, so the
// two still agree.
func Test_TextInvalidUTF8(t *testing.T) {
	compiles(t, Text("\xff"), opChar, '�', '�')
	compiles(t, Text("a\xffb"),
		opChar, 'a', 'a',
		opChar, '�', '�',
		opChar, 'b', 'b')
}

func Test_Text(t *testing.T) {
	match(t, "a", Text("a"), "a")
	match(t, "abc", Text("abc"), "abc")
	match(t, "a b", Text("a b"), "a b")

	noMatch(t, "", Text("a"), ErrUnexpectedEOI)
	noMatch(t, "ab", Text("abc"), ErrUnexpectedEOI)
	noMatch(t, "abx", Text("abc"), ErrGrammarMismatch)
	noMatch(t, "xbc", Text("abc"), ErrGrammarMismatch)
	noMatch(t, "abcd", Text("abc"), ErrGrammarMismatch)
	noMatch(t, "ABC", Text("abc"), ErrGrammarMismatch)
}

func Test_TextMultibyte(t *testing.T) {
	match(t, "é", Text("é"), "é")
	match(t, "a😀b", Text("a😀b"), "a😀b")
	match(t, "é!", Seq(Text("é"), Char('!')), "(é!)")

	noMatch(t, "e", Text("é"), ErrGrammarMismatch)
}

// opMark and opSeq consume no input, so execution must continue to the
// following operation within the same step.
func Test_TextInSeq(t *testing.T) {
	match(t, "abc", Seq(Text("ab"), Char('c')), "(abc)")
	match(t, "abc", Seq(Char('a'), Text("bc")), "(abc)")
	match(t, "abcd", Seq(Text("ab"), Text("cd")), "(abcd)")
	match(t, "a1", Seq(Text("a"), Range('0', '9')), "(a1)")

	noMatch(t, "abx", Seq(Text("ab"), Char('c')), ErrGrammarMismatch)
	noMatch(t, "ab", Seq(Text("ab"), Char('c')), ErrUnexpectedEOI)
}

func Test_TextNested(t *testing.T) {
	match(t, "abc", Seq(Seq(Text("ab")), Text("c")), "((ab) c)")
	match(t, "abcd", Seq(Char('a'), Seq(Text("bc"), Char('d'))), "(a (bcd))")

	noMatch(t, "abx", Seq(Seq(Text("ab")), Text("c")), ErrGrammarMismatch)
}

// Exceeds the initial program capacity (64) in Run.
func Test_TextLong(t *testing.T) {
	s := strings.Repeat("abcdefghij", 10)
	match(t, s, Text(s), s)
	noMatch(t, s[:len(s)-1], Text(s), ErrUnexpectedEOI)
	noMatch(t, s[:50]+"x"+s[51:], Text(s), ErrGrammarMismatch)
}

func Test_TextEmptyPanics(t *testing.T) {
	defer func() {
		r := recover()
		err, ok := r.(error)
		if !ok || !errors.Is(err, ErrEmptyText) {
			t.Errorf("recovered %v, want ErrEmptyText", r)
		}
	}()
	Text("")
}

// Adjacent chars merge into one node whatever produced them; only a nested
// sequence's mark separates them.
func Test_CharsMerge(t *testing.T) {
	match(t, "a5b", Seq('a', Range('0', '9'), 'b'), "(a5b)")
	match(t, "abc", Seq('a', Seq('b'), 'c'), "(a (b) c)")
	match(t, "abcde", Seq("ab", Optional("cd"), 'e'), "(abcde)")
	match(t, "abe", Seq("ab", Optional("cd"), 'e'), "(abe)")
}

func Test_NodeKinds(t *testing.T) {
	cases := []struct {
		name  string
		input string
		c     Compiler
		want  string
	}{
		{"char", "a", Char('a'), nkChars},
		{"range", "a", Range('a', 'z'), nkChars},
		{"seq single", "a", Seq(Char('a')), nkSeq},
		{"seq", "ab", Seq(Char('a'), Char('b')), nkSeq},
		{"text single", "a", Text("a"), nkChars},
		{"text", "ab", Text("ab"), nkChars},
		{"optional present", "a", Optional('a'), nkChars},
		{"seq of skipped optional", "", Seq(Optional('a')), nkSeq},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := run(t, tc.input, tc.c)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n == nil {
				t.Fatal("nil tree")
			}
			if n.kind != tc.want {
				t.Errorf("kind = %q, want %q", n.kind, tc.want)
			}
		})
	}
}

func Test_Char(t *testing.T) {
	match(t, "a", Char('a'), "a")
	match(t, "é", Char('é'), "é")
	match(t, "😀", Char('😀'), "😀")

	noMatch(t, "", Char('a'), ErrUnexpectedEOI)
	noMatch(t, "b", Char('a'), ErrGrammarMismatch)
	noMatch(t, "A", Char('a'), ErrGrammarMismatch)
	noMatch(t, "aa", Char('a'), ErrGrammarMismatch)
}

func Test_Range(t *testing.T) {
	match(t, "a", Range('a', 'z'), "a")
	match(t, "m", Range('a', 'z'), "m")
	match(t, "z", Range('a', 'z'), "z")
	match(t, "x", Range('x', 'x'), "x")

	noMatch(t, "", Range('a', 'z'), ErrUnexpectedEOI)
	noMatch(t, "`", Range('a', 'z'), ErrGrammarMismatch)
	noMatch(t, "{", Range('a', 'z'), ErrGrammarMismatch)
	noMatch(t, "A", Range('a', 'z'), ErrGrammarMismatch)
	noMatch(t, "ab", Range('a', 'z'), ErrGrammarMismatch)
	noMatch(t, "a", Range('z', 'a'), ErrGrammarMismatch)
}

// A single-element Seq still wraps its element in a sequence node.
func Test_Seq(t *testing.T) {
	match(t, "a", Seq(Char('a')), "(a)")
	match(t, "abc", Seq(Char('a'), Char('b'), Char('c')), "(abc)")
	match(t, "a5", Seq(Char('a'), Range('0', '9')), "(a5)")

	noMatch(t, "", Seq(Char('a')), ErrUnexpectedEOI)
	noMatch(t, "ab", Seq(Char('a'), Char('b'), Char('c')), ErrUnexpectedEOI)
	noMatch(t, "abd", Seq(Char('a'), Char('b'), Char('c')), ErrGrammarMismatch)
	noMatch(t, "xbc", Seq(Char('a'), Char('b'), Char('c')), ErrGrammarMismatch)
	noMatch(t, "abcd", Seq(Char('a'), Char('b'), Char('c')), ErrGrammarMismatch)
	noMatch(t, "ba", Seq(Char('a'), Char('b')), ErrGrammarMismatch)
}

func Test_SeqNested(t *testing.T) {
	match(t, "abc", Seq(Seq(Char('a'), Char('b')), Char('c')), "((ab) c)")
	match(t, "abc", Seq(Char('a'), Seq(Char('b'), Char('c'))), "(a (bc))")
	match(t, "abcd", Seq(Seq(Char('a'), Char('b')), Seq(Char('c'), Char('d'))), "((ab) (cd))")
	match(t, "a", Seq(Seq(Seq(Char('a')))), "(((a)))")

	noMatch(t, "ab", Seq(Seq(Char('a'), Char('b')), Char('c')), ErrUnexpectedEOI)
	noMatch(t, "abx", Seq(Char('a'), Seq(Char('b'), Char('c'))), ErrGrammarMismatch)
}

// Seq converts rune and string elements to Char and Text, so both forms
// compile identically.
func Test_SeqConvertCompile(t *testing.T) {
	compiles(t, Seq('a'), opMark, opChar, 'a', 'a', opSeq)
	compiles(t, Seq("ab"),
		opMark,
		opChar, 'a', 'a',
		opChar, 'b', 'b',
		opSeq)
	compiles(t, Seq('a', "bc", Range('0', '9')),
		opMark,
		opChar, 'a', 'a',
		opChar, 'b', 'b',
		opChar, 'c', 'c',
		opChar, '0', '9',
		opSeq)
}

func Test_SeqConvert(t *testing.T) {
	match(t, "a", Seq('a'), "(a)")
	match(t, "é", Seq('é'), "(é)")
	match(t, "ab", Seq("ab"), "(ab)")
	match(t, "abc", Seq('a', 'b', 'c'), "(abc)")
	match(t, "abc", Seq('a', "bc"), "(abc)")
	match(t, "abc", Seq("ab", 'c'), "(abc)")
	match(t, "ab1", Seq("ab", Range('0', '9')), "(ab1)")
	match(t, "abc", Seq('a', Seq('b', "c")), "(a (bc))")

	noMatch(t, "ax", Seq('a', 'b'), ErrGrammarMismatch)
	noMatch(t, "ab", Seq("ab", 'c'), ErrUnexpectedEOI)
}

func Test_SeqConvertEmptyTextPanics(t *testing.T) {
	defer func() {
		r := recover()
		err, ok := r.(error)
		if !ok || !errors.Is(err, ErrEmptyText) {
			t.Errorf("recovered %v, want ErrEmptyText", r)
		}
	}()
	Seq('a', "")
}

// Unsupported element types are coding errors, reported when Seq is called
// rather than when the program is compiled.
func Test_SeqConvertUnsupportedPanics(t *testing.T) {
	cases := []struct {
		name  string
		first any
		rest  []any
	}{
		{"int first", 1, nil},
		{"int rest", 'a', []any{1}},
		{"byte", byte('a'), nil},
		{"nil", 'a', []any{nil}},
		{"untyped func", func(p *program) {}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			Seq(tc.first, tc.rest...)
		})
	}
}

// The fork target is the first op after the optional element.
func Test_OptionalCompile(t *testing.T) {
	compiles(t, Optional('a'), opFork, 5, opChar, 'a', 'a')
	compiles(t, Seq('x', Optional('a')),
		opMark,
		opChar, 'x', 'x',
		opFork, 9,
		opChar, 'a', 'a',
		opSeq)
	compiles(t, Optional("ab"),
		opFork, 8,
		opChar, 'a', 'a',
		opChar, 'b', 'b')
	compiles(t, Optional(Optional('a')),
		opFork, 7,
		opFork, 7,
		opChar, 'a', 'a')
}

func Test_Optional(t *testing.T) {
	match(t, "a", Optional('a'), "a")
	match(t, "ab", Optional("ab"), "ab")
	match(t, "5", Optional(Range('0', '9')), "5")

	noMatch(t, "b", Optional('a'), ErrGrammarMismatch)
	noMatch(t, "aa", Optional('a'), ErrGrammarMismatch)
	noMatch(t, "a", Optional("ab"), ErrUnexpectedEOI)
}

// A skipped top-level Optional leaves the stack empty at opEof; Run reports a
// successful match with no tree rather than dereferencing an empty stack.
func Test_OptionalEmptyResult(t *testing.T) {
	cases := []struct {
		name string
		c    Compiler
	}{
		{"char", Optional('a')},
		{"text", Optional("ab")},
		{"nested", Optional(Optional('a'))},
		{"seq", Optional(Seq('a', Optional('b')))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := run(t, "", tc.c)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if n != nil {
				t.Errorf("tree = %s, want nil", show(n))
			}
		})
	}
}

// The skipped branch shares the stack with the matching branch, so both must
// still see the nodes pushed before the fork. A skipped element contributes no
// child.
func Test_OptionalInSeq(t *testing.T) {
	abc := Seq('a', Optional(Seq('b')), 'c')
	match(t, "abc", abc, "(a (b) c)")
	match(t, "ac", abc, "(ac)")
	match(t, "ab", Seq(Optional(Seq('a')), 'b'), "((a) b)")
	match(t, "b", Seq(Optional(Seq('a')), 'b'), "(b)")
	match(t, "a", Seq('a', Optional(Seq('b'))), "(a)")
	match(t, "ab", Seq('a', Optional(Seq('b'))), "(a (b))")
	match(t, "", Seq(Optional('a'), Optional('b')), "()")
	match(t, "b", Seq(Optional('a'), Optional('b')), "(b)")

	noMatch(t, "ab", abc, ErrUnexpectedEOI)
	noMatch(t, "abd", abc, ErrGrammarMismatch)
	noMatch(t, "abbc", abc, ErrGrammarMismatch)
}

// When both branches reach end of input, the branch that matched the earlier
// optional element wins. Machine order must therefore be preserved across
// steps. The optionals differ in shape so the winner is visible in the tree.
func Test_OptionalPriority(t *testing.T) {
	c := Seq(Optional(Seq('a')), Optional('a'))
	match(t, "a", c, "((a))")
	c = Seq(Optional(Seq('a')), Optional('a'), Optional(Seq('a')))
	match(t, "a", c, "((a))")
	match(t, "aa", c, "((a) a)")
}

// The pending tail must be reset each step; a stale tail points at a machine
// already moved to running, so the next step's matches would be linked onto
// the running list instead of pending.
func Test_PendingAcrossSteps(t *testing.T) {
	match(t, "ab", Text("ab"), "ab")
	match(t, "abcd", Seq('a', Optional('b'), Optional('c'), 'd'), "(abcd)")
	match(t, "ad", Seq('a', Optional('b'), Optional('c'), 'd'), "(ad)")
	noMatch(t, "ab", Text("abc"), ErrUnexpectedEOI)
}

// The fork must run on the same rune even when the forking machine is the
// last one queued.
func Test_OptionalForkLast(t *testing.T) {
	match(t, "b", Seq(Optional('a'), 'b'), "(b)")
	match(t, "xb", Seq('x', Optional('a'), 'b'), "(xb)")
}

// Both branches can succeed; each input length selects exactly one.
func Test_OptionalAmbiguous(t *testing.T) {
	c := Seq(Optional(Seq('a')), 'a')
	match(t, "a", c, "(a)")
	match(t, "aa", c, "((a) a)")
	noMatch(t, "aaa", c, ErrGrammarMismatch)
}

func Test_OptionalNested(t *testing.T) {
	match(t, "a", Optional(Optional('a')), "a")
	match(t, "ab", Optional(Seq('a', Optional('b'))), "(ab)")
	match(t, "a", Optional(Seq('a', Optional('b'))), "(a)")
	match(t, "abc", Optional(Seq('a', Optional(Seq('b')), Optional('c'))), "(a (b) c)")
	match(t, "ac", Optional(Seq('a', Optional(Seq('b')), Optional('c'))), "(ac)")
}

func Test_OptionalErrorOffset(t *testing.T) {
	_, err := run(t, "axc", Seq('a', Optional('b'), 'c'))
	want := `grammar mismatch: unexpected 'x' at offset 1`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func Test_OptionalPanics(t *testing.T) {
	cases := []struct {
		name string
		item any
		want error
	}{
		{"empty text", "", ErrEmptyText},
		{"unsupported", 1, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("no panic")
				}
				if tc.want == nil {
					return
				}
				if err, ok := r.(error); !ok || !errors.Is(err, tc.want) {
					t.Errorf("recovered %v, want %v", r, tc.want)
				}
			}()
			Optional(tc.item)
		})
	}
}

// Sequence nodes are interior only; leaves are chars nodes carrying the
// lexeme.
func Test_SeqNodeShape(t *testing.T) {
	s, err := run(t, "abc", Seq('a', Seq('b'), 'c'))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("nil tree")
	}
	if s.kind != nkSeq || s.lexeme != "" {
		t.Errorf("root = %q %q, want seq with empty lexeme", s.kind, s.lexeme)
	}
	wantKinds := []string{nkChars, nkSeq, nkChars}
	if len(s.children) != len(wantKinds) {
		t.Fatalf("children = %d, want %d", len(s.children), len(wantKinds))
	}
	for i, c := range s.children {
		if c.kind != wantKinds[i] {
			t.Errorf("child %d kind = %q, want %q", i, c.kind, wantKinds[i])
		}
		if c.kind == nkChars && c.children != nil {
			t.Errorf("chars child %d has children: %v", i, c.children)
		}
	}
}

// Offsets count runes, not bytes, and point at the first rejected position.
func Test_ErrorMessage(t *testing.T) {
	abc := Seq(Char('a'), Char('b'), Char('c'))
	cases := []struct {
		input string
		c     Compiler
		want  string
	}{
		{"", Char('a'), "unexpected end of input at offset 0"},
		{"ab", abc, "unexpected end of input at offset 2"},
		{"b", Char('a'), `grammar mismatch: unexpected 'b' at offset 0`},
		{"abd", abc, `grammar mismatch: unexpected 'd' at offset 2`},
		{"abcd", abc, `grammar mismatch: unexpected 'd' at offset 3`},
		{"éx", Seq(Char('é'), Char('y')), `grammar mismatch: unexpected 'x' at offset 1`},
	}
	for _, tc := range cases {
		_, err := run(t, tc.input, tc.c)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%q: error = %v, want %q", tc.input, err, tc.want)
		}
	}
}

func Test_RunReusable(t *testing.T) {
	c := Seq(Char('a'), Char('b'))
	match(t, "ab", c, "(ab)")
	noMatch(t, "ax", c, ErrGrammarMismatch)
	match(t, "ab", c, "(ab)")
}

func Test_MachineStack(t *testing.T) {
	a, b, c := &node{lexeme: "a"}, &node{lexeme: "b"}, &node{lexeme: "c"}
	m := &machine{}
	m.push(a)
	m.push(b)
	m.push(c)

	for _, want := range []*node{c, b, a} {
		if got := m.pop(); got != want {
			t.Fatalf("pop = %v, want %v", got, want)
		}
	}
	if m.stack != nil {
		t.Fatalf("stack = %v, want empty", m.stack)
	}
}

// Consecutive chars join into one node; a mark or other node starts a new one.
func Test_MachinePushChar(t *testing.T) {
	m := &machine{}
	m.pushChar('a')
	m.pushChar('😀')
	if m.stack.kind != nkChars || m.stack.lexeme != "a😀" {
		t.Fatalf("top = %q %q, want chars \"a😀\"", m.stack.kind, m.stack.lexeme)
	}
	if m.stack.next != nil {
		t.Fatalf("joined chars left %v below", m.stack.next)
	}

	m.push(&node{kind: nkMark})
	m.pushChar('b')
	if m.stack.lexeme != "b" || m.stack.next.kind != nkMark {
		t.Fatalf("char after mark joined across it: top = %q", m.stack.lexeme)
	}
}

// A fork shares the top node; extending it on one machine must not change
// what the other sees.
func Test_MachinePushCharShared(t *testing.T) {
	m1 := &machine{}
	m1.pushChar('a')
	m2 := &machine{stack: m1.stack}

	m1.pushChar('b')
	m2.pushChar('c')

	if m1.stack.lexeme != "ab" {
		t.Errorf("m1 top = %q, want \"ab\"", m1.stack.lexeme)
	}
	if m2.stack.lexeme != "ac" {
		t.Errorf("m2 top = %q, want \"ac\"", m2.stack.lexeme)
	}
}

// popToMark stops at the nearest mark, discards it, and leaves anything below
// it, including outer marks, on the stack.
func Test_MachinePopToMark(t *testing.T) {
	outer, inner := &node{kind: nkMark}, &node{kind: nkMark}
	x, a, b := &node{lexeme: "x"}, &node{lexeme: "a"}, &node{lexeme: "b"}
	m := &machine{}
	for _, n := range []*node{outer, x, inner, a, b} {
		m.push(n)
	}

	if got := m.popToMark(); !slices.Equal(got, []*node{a, b}) {
		t.Fatalf("popToMark = %v, want [a b] in push order", got)
	}
	if m.stack != x {
		t.Fatalf("top = %v, want x", m.stack)
	}
	if got := m.popToMark(); !slices.Equal(got, []*node{x}) {
		t.Fatalf("popToMark = %v, want [x]", got)
	}
	if m.stack != nil {
		t.Fatalf("stack = %v, want empty", m.stack)
	}
}

// A mark directly on top yields no children; nil is acceptable.
func Test_MachinePopToMarkEmpty(t *testing.T) {
	below := &node{lexeme: "below"}
	m := &machine{}
	m.push(below)
	m.push(&node{kind: nkMark})

	if got := m.popToMark(); len(got) != 0 {
		t.Fatalf("popToMark = %v, want empty", got)
	}
	if m.stack != below {
		t.Fatalf("top = %v, want below", m.stack)
	}
}

// appendPending keeps insertion order and clears a stale link left over from
// the running list.
func Test_AppendPending(t *testing.T) {
	a, b, c := &machine{pc: 1}, &machine{pc: 2}, &machine{pc: 3}
	a.next = c // stale link from the running list
	var head, tail *machine
	head, tail = appendPending(head, tail, a)
	head, tail = appendPending(head, tail, b)
	head, tail = appendPending(head, tail, c)

	var got []int32
	for m := head; m != nil; m = m.next {
		got = append(got, m.pc)
	}
	if !slices.Equal(got, []int32{1, 2, 3}) {
		t.Errorf("order = %v, want [1 2 3]", got)
	}
	if tail != c {
		t.Errorf("tail = %v, want c", tail)
	}
}

// Machines sharing a stack tail must not disturb each other: popping leaves
// the popped node's link intact.
func Test_MachineSharedTail(t *testing.T) {
	a, b, c := &node{lexeme: "a"}, &node{lexeme: "b"}, &node{lexeme: "c"}
	m1 := &machine{}
	m1.push(a)
	m1.push(b)
	m2 := &machine{stack: m1.stack}

	if got := m2.pop(); got != b {
		t.Fatalf("m2 pop = %v, want b", got)
	}
	m2.push(c)

	if got := []*node{m1.pop(), m1.pop()}; !slices.Equal(got, []*node{b, a}) {
		t.Errorf("m1 pops = %v, want [b a]", got)
	}
	if got := []*node{m2.pop(), m2.pop()}; !slices.Equal(got, []*node{c, a}) {
		t.Errorf("m2 pops = %v, want [c a]", got)
	}
}

func compiles(t *testing.T, c Compiler, want ...int32) {
	t.Helper()
	var p program
	c(&p)
	if !slices.Equal([]int32(p), want) {
		t.Errorf("program = %v, want %v", p, want)
	}
}

func match(t *testing.T, s string, c Compiler, want string) {
	t.Helper()
	got, err := run(t, s, c)
	if err != nil {
		t.Errorf("%q: unexpected error: %v", s, err)
		return
	}
	if got == nil {
		t.Errorf("%q: nil tree", s)
		return
	}
	if tree := show(got); tree != want {
		t.Errorf("%q: tree = %s, want %s", s, tree, want)
	}
}

func noMatch(t *testing.T, s string, c Compiler, want error) {
	t.Helper()
	got, err := run(t, s, c)
	if !errors.Is(err, want) {
		t.Errorf("%q: error = %v, want %v", s, err, want)
	}
	if got != nil {
		t.Errorf("%q: tree = %s, want nil", s, show(got))
	}
}

// Recovers so a VM panic fails only the offending case.
func run(t *testing.T, s string, c Compiler) (got *node, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%q: panic: %v", s, r)
		}
	}()
	return Run(c, []rune(s))
}

// Renders chars nodes as their lexeme and sequence nodes as a parenthesised
// list. Keyed on kind because an empty sequence has nil children.
func show(s *node) string {
	if s.kind != nkSeq {
		return s.lexeme
	}
	parts := make([]string, len(s.children))
	for i, c := range s.children {
		parts[i] = show(c)
	}
	return "(" + strings.Join(parts, " ") + ")"
}
