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

// Each element after the first is followed by an opCat joining it to
// everything before, so a sequence of n elements emits n-1 cats.
func Test_SeqCompile(t *testing.T) {
	compiles(t, Seq(Char('a')), opChar, 'a', 'a')
	compiles(t, Seq(Char('a'), Range('0', '9'), Char('b')),
		opChar, 'a', 'a',
		opChar, '0', '9',
		opCat,
		opChar, 'b', 'b',
		opCat)
	compiles(t, Seq(Seq(Char('a'), Char('b')), Char('c')),
		opChar, 'a', 'a',
		opChar, 'b', 'b',
		opCat,
		opChar, 'c', 'c',
		opCat)
}

func Test_TextCompile(t *testing.T) {
	compiles(t, Text("a"), opChar, 'a', 'a')
	compiles(t, Text("abc"),
		opChar, 'a', 'a',
		opChar, 'b', 'b',
		opCat,
		opChar, 'c', 'c',
		opCat)
}

// Text splits on runes; splitting on bytes would emit partial code points.
func Test_TextCompileMultibyte(t *testing.T) {
	compiles(t, Text("é"), opChar, 'é', 'é')
	compiles(t, Text("a😀b"),
		opChar, 'a', 'a',
		opChar, '😀', '😀',
		opCat,
		opChar, 'b', 'b',
		opCat)
}

// Invalid UTF-8 decodes to U+FFFD on both the grammar and input side, so the
// two still agree.
func Test_TextInvalidUTF8(t *testing.T) {
	compiles(t, Text("\xff"), opChar, '�', '�')
	compiles(t, Text("a\xffb"),
		opChar, 'a', 'a',
		opChar, '�', '�',
		opCat,
		opChar, 'b', 'b',
		opCat)
}

func Test_Text(t *testing.T) {
	match(t, "a", Text("a"), "a")
	match(t, "abc", Text("abc"), "((a b) c)")
	match(t, "a-b", Text("a-b"), "((a -) b)")

	noMatch(t, "", Text("a"), ErrUnexpectedEOI)
	noMatch(t, "ab", Text("abc"), ErrUnexpectedEOI)
	noMatch(t, "abx", Text("abc"), ErrGrammarMismatch)
	noMatch(t, "xbc", Text("abc"), ErrGrammarMismatch)
	noMatch(t, "abcd", Text("abc"), ErrGrammarMismatch)
	noMatch(t, "ABC", Text("abc"), ErrGrammarMismatch)
}

func Test_TextMultibyte(t *testing.T) {
	match(t, "é", Text("é"), "é")
	match(t, "a😀b", Text("a😀b"), "((a 😀) b)")
	match(t, "é!", Seq(Text("é"), Char('!')), "(é !)")

	noMatch(t, "e", Text("é"), ErrGrammarMismatch)
}

// opCat consumes no input, so execution must continue to the following
// operation within the same step.
func Test_TextInSeq(t *testing.T) {
	match(t, "abc", Seq(Text("ab"), Char('c')), "((a b) c)")
	match(t, "abc", Seq(Char('a'), Text("bc")), "(a (b c))")
	match(t, "abcd", Seq(Text("ab"), Text("cd")), "((a b) (c d))")
	match(t, "a1", Seq(Text("a"), Range('0', '9')), "(a 1)")

	noMatch(t, "abx", Seq(Text("ab"), Char('c')), ErrGrammarMismatch)
	noMatch(t, "ab", Seq(Text("ab"), Char('c')), ErrUnexpectedEOI)
}

func Test_TextNested(t *testing.T) {
	match(t, "abc", Seq(Seq(Text("ab")), Text("c")), "((a b) c)")
	match(t, "abcd", Seq(Char('a'), Seq(Text("bc"), Char('d'))), "(a ((b c) d))")

	noMatch(t, "abx", Seq(Seq(Text("ab")), Text("c")), ErrGrammarMismatch)
}

// Exceeds the initial program capacity (64) in Run.
func Test_TextLong(t *testing.T) {
	s := strings.Repeat("abcdefghij", 10)
	want := s[:1]
	for _, r := range s[1:] {
		want = "(" + want + " " + string(r) + ")"
	}
	match(t, s, Text(s), want)
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

func Test_NodeKinds(t *testing.T) {
	cases := []struct {
		name  string
		input string
		c     Compiler
		want  string
	}{
		{"char", "a", Char('a'), nkChar},
		{"range", "a", Range('a', 'z'), nkChar},
		{"seq single", "a", Seq(Char('a')), nkChar},
		{"seq", "ab", Seq(Char('a'), Char('b')), nkSeq},
		{"text single", "a", Text("a"), nkChar},
		{"text", "ab", Text("ab"), nkSeq},
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

// A single-element Seq emits no opCat, so it is transparent in the tree.
func Test_Seq(t *testing.T) {
	match(t, "a", Seq(Char('a')), "a")
	match(t, "abc", Seq(Char('a'), Char('b'), Char('c')), "((a b) c)")
	match(t, "a5", Seq(Char('a'), Range('0', '9')), "(a 5)")

	noMatch(t, "", Seq(Char('a')), ErrUnexpectedEOI)
	noMatch(t, "ab", Seq(Char('a'), Char('b'), Char('c')), ErrUnexpectedEOI)
	noMatch(t, "abd", Seq(Char('a'), Char('b'), Char('c')), ErrGrammarMismatch)
	noMatch(t, "xbc", Seq(Char('a'), Char('b'), Char('c')), ErrGrammarMismatch)
	noMatch(t, "abcd", Seq(Char('a'), Char('b'), Char('c')), ErrGrammarMismatch)
	noMatch(t, "ba", Seq(Char('a'), Char('b')), ErrGrammarMismatch)
}

func Test_SeqNested(t *testing.T) {
	match(t, "abc", Seq(Seq(Char('a'), Char('b')), Char('c')), "((a b) c)")
	match(t, "abc", Seq(Char('a'), Seq(Char('b'), Char('c'))), "(a (b c))")
	match(t, "abcd", Seq(Seq(Char('a'), Char('b')), Seq(Char('c'), Char('d'))), "((a b) (c d))")
	match(t, "a", Seq(Seq(Seq(Char('a')))), "a")

	noMatch(t, "ab", Seq(Seq(Char('a'), Char('b')), Char('c')), ErrUnexpectedEOI)
	noMatch(t, "abx", Seq(Char('a'), Seq(Char('b'), Char('c'))), ErrGrammarMismatch)
}

// Seq converts rune and string elements to Char and Text, so both forms
// compile identically.
func Test_SeqConvertCompile(t *testing.T) {
	compiles(t, Seq('a'), opChar, 'a', 'a')
	compiles(t, Seq("ab"),
		opChar, 'a', 'a',
		opChar, 'b', 'b',
		opCat)
	compiles(t, Seq('a', "bc", Range('0', '9')),
		opChar, 'a', 'a',
		opChar, 'b', 'b',
		opChar, 'c', 'c',
		opCat,
		opCat,
		opChar, '0', '9',
		opCat)
}

func Test_SeqConvert(t *testing.T) {
	match(t, "a", Seq('a'), "a")
	match(t, "é", Seq('é'), "é")
	match(t, "ab", Seq("ab"), "(a b)")
	match(t, "abc", Seq('a', 'b', 'c'), "((a b) c)")
	match(t, "abc", Seq('a', "bc"), "(a (b c))")
	match(t, "abc", Seq("ab", 'c'), "((a b) c)")
	match(t, "ab1", Seq("ab", Range('0', '9')), "((a b) 1)")
	match(t, "abc", Seq('a', Seq('b', "c")), "(a (b c))")

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

// Sequence nodes are binary and interior only; leaves carry the lexeme.
func Test_SeqNodeShape(t *testing.T) {
	s, err := run(t, "ab", Seq(Char('a'), Char('b')))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("nil tree")
	}
	if s.lexeme != "" {
		t.Errorf("seq lexeme = %q, want empty", s.lexeme)
	}
	if len(s.children) != 2 {
		t.Fatalf("seq children = %d, want 2", len(s.children))
	}
	for i, c := range s.children {
		if c.children != nil {
			t.Errorf("leaf %d has children: %v", i, c.children)
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
	match(t, "ab", c, "(a b)")
	noMatch(t, "ax", c, ErrGrammarMismatch)
	match(t, "ab", c, "(a b)")
}

func Test_MachineStack(t *testing.T) {
	a, b, c := &node{lexeme: "a"}, &node{lexeme: "b"}, &node{lexeme: "c"}
	m := &machine{}
	m.push(a)
	m.push(b)
	m.push(c)

	if got := m.pop(); got != c {
		t.Fatalf("pop = %v, want c", got)
	}
	got := m.popn(2)
	if !slices.Equal(got, []*node{a, b}) {
		t.Fatalf("popn(2) = %v, want [a b] in push order", got)
	}
	if m.stack != nil {
		t.Fatalf("stack = %v, want empty", m.stack)
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

	if got := m1.popn(2); !slices.Equal(got, []*node{a, b}) {
		t.Errorf("m1 popn(2) = %v, want [a b]", got)
	}
	if got := m2.popn(2); !slices.Equal(got, []*node{a, c}) {
		t.Errorf("m2 popn(2) = %v, want [a c]", got)
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

// Renders leaves as their lexeme and sequence nodes as a parenthesised list.
func show(s *node) string {
	if s.children == nil {
		return s.lexeme
	}
	parts := make([]string, len(s.children))
	for i, c := range s.children {
		parts[i] = show(c)
	}
	return "(" + strings.Join(parts, " ") + ")"
}
