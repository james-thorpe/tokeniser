package parser

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func Test_CharCompile(t *testing.T) {
	compiles(t, Char('a'), opChar, 'a')
	compiles(t, Char('😀'), opChar, '😀')
}

func Test_RangeCompile(t *testing.T) {
	compiles(t, Range('a', 'z'), opRange, 'a', 'z')
}

func Test_SeqCompile(t *testing.T) {
	compiles(t, Seq(Char('a')), opChar, 'a', opSeq, 1)
	compiles(t, Seq(Char('a'), Range('0', '9'), Char('b')),
		opChar, 'a',
		opRange, '0', '9',
		opChar, 'b',
		opSeq, 3)
	compiles(t, Seq(Seq(Char('a'), Char('b')), Char('c')),
		opChar, 'a',
		opChar, 'b',
		opSeq, 2,
		opChar, 'c',
		opSeq, 2)
}

func Test_TextCompile(t *testing.T) {
	compiles(t, Text("a"), opChar, 'a', opText, 1)
	compiles(t, Text("abc"), opChar, 'a', opChar, 'b', opChar, 'c', opText, 3)
}

// The opText operand counts runes; a byte count would pop states belonging to
// earlier parts of the grammar.
func Test_TextCompileMultibyte(t *testing.T) {
	compiles(t, Text("é"), opChar, 'é', opText, 1)
	compiles(t, Text("a😀b"), opChar, 'a', opChar, '😀', opChar, 'b', opText, 3)
}

func Test_Text(t *testing.T) {
	match(t, "a", Text("a"), `"a"`)
	match(t, "abc", Text("abc"), `"abc"`)
	match(t, "a b", Text("a b"), `"a b"`)

	noMatch(t, "", Text("a"), ErrUnexpectedEOI)
	noMatch(t, "ab", Text("abc"), ErrUnexpectedEOI)
	noMatch(t, "abx", Text("abc"), ErrGrammarMismatch)
	noMatch(t, "xbc", Text("abc"), ErrGrammarMismatch)
	noMatch(t, "abcd", Text("abc"), ErrGrammarMismatch)
	noMatch(t, "ABC", Text("abc"), ErrGrammarMismatch)
}

func Test_TextMultibyte(t *testing.T) {
	match(t, "é", Text("é"), `"é"`)
	match(t, "a😀b", Text("a😀b"), `"a😀b"`)
	match(t, "é!", Seq(Text("é"), Char('!')), `("é" !)`)

	noMatch(t, "e", Text("é"), ErrGrammarMismatch)
}

// Text completes without consuming input, so execution must continue to the
// following operation within the same step.
func Test_TextInSeq(t *testing.T) {
	match(t, "abc", Seq(Text("ab"), Char('c')), `("ab" c)`)
	match(t, "abc", Seq(Char('a'), Text("bc")), `(a "bc")`)
	match(t, "abcd", Seq(Text("ab"), Text("cd")), `("ab" "cd")`)
	match(t, "a1", Seq(Text("a"), Range('0', '9')), `("a" 1)`)

	noMatch(t, "abx", Seq(Text("ab"), Char('c')), ErrGrammarMismatch)
	noMatch(t, "ab", Seq(Text("ab"), Char('c')), ErrUnexpectedEOI)
}

func Test_TextNested(t *testing.T) {
	match(t, "abc", Seq(Seq(Text("ab")), Text("c")), `(("ab") "c")`)
	match(t, "abcd", Seq(Char('a'), Seq(Text("bc"), Char('d'))), `(a ("bc" d))`)

	noMatch(t, "abx", Seq(Seq(Text("ab")), Text("c")), ErrGrammarMismatch)
}

// Exceeds the initial program (64) and stack (16) capacities in Run.
func Test_TextLong(t *testing.T) {
	s := strings.Repeat("abcdefghij", 10)
	match(t, s, Text(s), `"`+s+`"`)
	noMatch(t, s[:len(s)-1], Text(s), ErrUnexpectedEOI)
	noMatch(t, s[:50]+"x"+s[51:], Text(s), ErrGrammarMismatch)
}

// Invalid UTF-8 decodes to U+FFFD on both the grammar and input side, so the
// two still agree.
func Test_TextInvalidUTF8(t *testing.T) {
	compiles(t, Text("\xff"), opChar, '�', opText, 1)
	compiles(t, Text("a\xffb"), opChar, 'a', opChar, '�', opChar, 'b', opText, 3)
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

// Text only emits chars, so a non-char operand is reachable only through a
// hand-built program.
func Test_TextNonChar(t *testing.T) {
	seqInText := func(p *program) {
		op(p, opChar, 'a', opSeq, 1, opText, 1)
	}
	noMatch(t, "a", seqInText, ErrNonCharInText)
}

func Test_TextNodeShape(t *testing.T) {
	n, err := run(t, "ab", Text("ab"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.kind != nkText {
		t.Errorf("kind = %q, want %q", n.kind, nkText)
	}
	if n.lexeme != "ab" {
		t.Errorf("lexeme = %q, want %q", n.lexeme, "ab")
	}
	if n.children != nil {
		t.Errorf("children = %v, want nil", n.children)
	}
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
		{"seq", "a", Seq(Char('a')), nkSeq},
		{"text", "a", Text("a"), nkText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := run(t, tc.input, tc.c)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
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

func Test_Seq(t *testing.T) {
	match(t, "a", Seq(Char('a')), "(a)")
	match(t, "abc", Seq(Char('a'), Char('b'), Char('c')), "(a b c)")
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
	match(t, "a", Seq(Seq(Seq(Char('a')))), "(((a)))")

	noMatch(t, "ab", Seq(Seq(Char('a'), Char('b')), Char('c')), ErrUnexpectedEOI)
	noMatch(t, "abx", Seq(Char('a'), Seq(Char('b'), Char('c'))), ErrGrammarMismatch)
}

// Sequence nodes are interior only; leaves carry the lexeme.
func Test_SeqNodeShape(t *testing.T) {
	s, err := Run(Seq(Char('a'), Char('b')), []rune("ab"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.lexeme != "" {
		t.Errorf("seq lexeme = %q, want empty", s.lexeme)
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
	if len(m.stack) != 0 {
		t.Fatalf("stack len = %d, want 0", len(m.stack))
	}
}

// popn's result becomes a sequence node's children; a later push must not
// overwrite it.
func Test_MachinePopnAliasing(t *testing.T) {
	a, b, c := &node{lexeme: "a"}, &node{lexeme: "b"}, &node{lexeme: "c"}
	m := &machine{stack: make([]*node, 0, 4)}
	m.push(a)
	m.push(b)
	children := m.popn(2)
	m.push(c)

	if !slices.Equal(children, []*node{a, b}) {
		t.Errorf("popped states overwritten by push: %v", children)
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

// Renders chars as their lexeme, text as a quoted lexeme and interior nodes as
// a parenthesised list.
func show(s *node) string {
	if s.kind == nkText {
		return `"` + s.lexeme + `"`
	}
	if s.children == nil {
		return s.lexeme
	}
	parts := make([]string, len(s.children))
	for i, c := range s.children {
		parts[i] = show(c)
	}
	return "(" + strings.Join(parts, " ") + ")"
}
