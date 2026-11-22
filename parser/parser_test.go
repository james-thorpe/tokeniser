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
		want  int32
	}{
		{"char", "a", Char('a'), NkChars},
		{"range", "a", Range('a', 'z'), NkChars},
		{"seq single", "a", Seq(Char('a')), NkSeq},
		{"seq", "ab", Seq(Char('a'), Char('b')), NkSeq},
		{"text single", "a", Text("a"), NkChars},
		{"text", "ab", Text("ab"), NkChars},
		{"optional present", "a", Optional('a'), NkChars},
		{"seq of skipped optional", "", Seq(Optional('a')), NkSeq},
		{"token", "ab", Token(nkWord, 'a', Seq('b')), nkWord},
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
			if n.Kind != tc.want {
				t.Errorf("kind = %d, want %d", n.Kind, tc.want)
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

// Multiple elements are emitted after a single fork, so the jump skips them
// all.
func Test_OptionalMultiCompile(t *testing.T) {
	compiles(t, Optional('a', "bc", Range('0', '9')),
		opFork, 14,
		opChar, 'a', 'a',
		opChar, 'b', 'b',
		opChar, 'c', 'c',
		opChar, '0', '9')
	compiles(t, Seq('x', Optional('a', Seq('b'))),
		opMark,
		opChar, 'x', 'x',
		opFork, 14,
		opChar, 'a', 'a',
		opMark,
		opChar, 'b', 'b',
		opSeq,
		opSeq)
}

// All elements match or none do; matched elements become children of the
// enclosing sequence rather than being grouped.
func Test_OptionalMulti(t *testing.T) {
	c := Seq('x', Optional('a', Seq('b')), 'y')
	match(t, "xaby", c, "(xa (b) y)")
	match(t, "xy", c, "(xy)")
	match(t, "ab", Optional('a', 'b'), "ab")
	match(t, "", Seq(Optional('a', 'b')), "()")

	noMatch(t, "xay", c, ErrGrammarMismatch)
	noMatch(t, "xby", c, ErrGrammarMismatch)
	noMatch(t, "a", Optional('a', 'b'), ErrUnexpectedEOI)
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
		name  string
		first any
		rest  []any
		want  error
	}{
		{"empty text", "", nil, ErrEmptyText},
		{"empty text rest", 'a', []any{""}, ErrEmptyText},
		{"unsupported", 1, nil, nil},
		{"unsupported rest", 'a', []any{1}, nil},
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
			Optional(tc.first, tc.rest...)
		})
	}
}

// The entry fork skips the loop; the backward fork after the body targets the
// start of the body.
func Test_ZeroOrMoreCompile(t *testing.T) {
	compiles(t, ZeroOrMore('a'),
		opFork, 7,
		opChar, 'a', 'a',
		opFork, 2)
	compiles(t, Seq('x', ZeroOrMore('a', 'b')),
		opMark,
		opChar, 'x', 'x',
		opFork, 14,
		opChar, 'a', 'a',
		opChar, 'b', 'b',
		opFork, 6,
		opSeq)
}

func Test_ZeroOrMore(t *testing.T) {
	match(t, "a", ZeroOrMore('a'), "a")
	match(t, "aaa", ZeroOrMore('a'), "aaa")
	match(t, "123", ZeroOrMore(Range('0', '9')), "123")
	match(t, "", Seq(ZeroOrMore('a')), "()")
	match(t, "aa", Seq(ZeroOrMore(Seq('a'))), "((a) (a))")

	noMatch(t, "b", ZeroOrMore('a'), ErrGrammarMismatch)
	noMatch(t, "aab", ZeroOrMore('a'), ErrGrammarMismatch)
}

// Zero iterations at top level leave the stack empty: a match with no tree.
func Test_ZeroOrMoreEmptyResult(t *testing.T) {
	n, err := run(t, "", ZeroOrMore('a'))
	if err != nil || n != nil {
		t.Errorf("got %v, %v; want nil tree and nil error", n, err)
	}
}

// Each iteration's nodes become children of the enclosing sequence.
func Test_ZeroOrMoreInSeq(t *testing.T) {
	c := Seq('x', ZeroOrMore(Seq('a')), 'y')
	match(t, "xy", c, "(xy)")
	match(t, "xay", c, "(x (a) y)")
	match(t, "xaay", c, "(x (a) (a) y)")
	match(t, "xaay", Seq('x', ZeroOrMore('a'), 'y'), "(xaay)")

	noMatch(t, "xaa", c, ErrUnexpectedEOI)
	noMatch(t, "xaby", c, ErrGrammarMismatch)
}

// Multiple elements repeat as a group.
func Test_ZeroOrMoreMulti(t *testing.T) {
	c := Seq(ZeroOrMore('a', Seq('b')))
	match(t, "", c, "()")
	match(t, "ab", c, "(a (b))")
	match(t, "abab", c, "(a (b) a (b))")

	noMatch(t, "aba", c, ErrUnexpectedEOI)
	noMatch(t, "abb", c, ErrGrammarMismatch)
}

// The back-edge prefers another iteration, so the loop takes every match it
// can before later elements are tried.
func Test_ZeroOrMoreGreedy(t *testing.T) {
	c := Seq(ZeroOrMore(Seq('a')), Optional('a'))
	match(t, "a", c, "((a))")
	match(t, "aa", c, "((a) (a))")
	match(t, "ab", Seq(ZeroOrMore(Seq('a')), Optional('a'), 'b'), "((a) b)")
}

// A body that can match empty input must not loop forever; the pc dedup stops
// the second empty iteration within a step.
func Test_ZeroOrMoreNullable(t *testing.T) {
	match(t, "", Seq(ZeroOrMore(Optional('a'))), "()")
	match(t, "aa", Seq(ZeroOrMore(Optional('a'))), "(aa)")
	match(t, "", Seq(ZeroOrMore(ZeroOrMore('a'))), "()")
	match(t, "aa", Seq(ZeroOrMore(ZeroOrMore(Seq('a')))), "((a) (a))")
}

// The dedup table is sized to the program and reused across every step.
func Test_ZeroOrMoreLong(t *testing.T) {
	s := strings.Repeat("ab", 500)
	match(t, s, ZeroOrMore("ab"), s)
	noMatch(t, s+"a", ZeroOrMore("ab"), ErrUnexpectedEOI)
}

func Test_ZeroOrMoreErrorOffset(t *testing.T) {
	_, err := run(t, "xaab", Seq('x', ZeroOrMore('a'), 'y'))
	want := `grammar mismatch: unexpected 'b' at offset 3`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func Test_ZeroOrMorePanics(t *testing.T) {
	cases := []struct {
		name  string
		first any
		rest  []any
		want  error
	}{
		{"empty text", "", nil, ErrEmptyText},
		{"empty text rest", 'a', []any{""}, ErrEmptyText},
		{"unsupported", 1, nil, nil},
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
			ZeroOrMore(tc.first, tc.rest...)
		})
	}
}

// The body comes first; the backward fork after it targets the body start.
func Test_OneOrMoreCompile(t *testing.T) {
	compiles(t, OneOrMore('a'),
		opChar, 'a', 'a',
		opFork, 0)
	compiles(t, Seq('x', OneOrMore('a', 'b')),
		opMark,
		opChar, 'x', 'x',
		opChar, 'a', 'a',
		opChar, 'b', 'b',
		opFork, 4,
		opSeq)
}

func Test_OneOrMore(t *testing.T) {
	match(t, "a", OneOrMore('a'), "a")
	match(t, "aaa", OneOrMore('a'), "aaa")
	match(t, "123", OneOrMore(Range('0', '9')), "123")
	match(t, "a", Seq(OneOrMore(Seq('a'))), "((a))")
	match(t, "aa", Seq(OneOrMore(Seq('a'))), "((a) (a))")

	noMatch(t, "", OneOrMore('a'), ErrUnexpectedEOI)
	noMatch(t, "b", OneOrMore('a'), ErrGrammarMismatch)
	noMatch(t, "aab", OneOrMore('a'), ErrGrammarMismatch)
}

func Test_OneOrMoreInSeq(t *testing.T) {
	c := Seq('x', OneOrMore(Seq('a')), 'y')
	match(t, "xay", c, "(x (a) y)")
	match(t, "xaay", c, "(x (a) (a) y)")

	noMatch(t, "xy", c, ErrGrammarMismatch)
	noMatch(t, "xaa", c, ErrUnexpectedEOI)
}

// Multiple elements repeat as a group.
func Test_OneOrMoreMulti(t *testing.T) {
	c := Seq(OneOrMore('a', Seq('b')))
	match(t, "ab", c, "(a (b))")
	match(t, "abab", c, "(a (b) a (b))")

	noMatch(t, "", c, ErrUnexpectedEOI)
	noMatch(t, "aba", c, ErrUnexpectedEOI)
}

func Test_OneOrMoreGreedy(t *testing.T) {
	c := Seq(OneOrMore(Seq('a')), Optional('a'))
	match(t, "a", c, "((a))")
	match(t, "aa", c, "((a) (a))")
}

// A body that can match empty input must not loop forever.
func Test_OneOrMoreNullable(t *testing.T) {
	match(t, "", Seq(OneOrMore(Optional('a'))), "()")
	match(t, "aa", Seq(OneOrMore(Optional('a'))), "(aa)")
	match(t, "aa", Seq(OneOrMore(ZeroOrMore(Seq('a')))), "((a) (a))")
}

// Several iterations at top level are wrapped by opEof; one is returned as is.
func Test_OneOrMoreTopLevel(t *testing.T) {
	match(t, "a", OneOrMore(Seq('a')), "(a)")
	match(t, "aa", OneOrMore(Seq('a')), "((a) (a))")
}

func Test_OneOrMorePanics(t *testing.T) {
	cases := []struct {
		name  string
		first any
		rest  []any
		want  error
	}{
		{"empty text", "", nil, ErrEmptyText},
		{"empty text rest", 'a', []any{""}, ErrEmptyText},
		{"unsupported", 1, nil, nil},
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
			OneOrMore(tc.first, tc.rest...)
		})
	}
}

// Each alternative but the last forks to the next and jumps to the end.
func Test_ChoiceCompile(t *testing.T) {
	compiles(t, Choice('a'), opChar, 'a', 'a')
	compiles(t, Choice('a', 'b'),
		opFork, 7,
		opChar, 'a', 'a',
		opJmp, 10,
		opChar, 'b', 'b')
	compiles(t, Choice('a', 'b', 'c'),
		opFork, 7,
		opChar, 'a', 'a',
		opJmp, 17,
		opFork, 14,
		opChar, 'b', 'b',
		opJmp, 17,
		opChar, 'c', 'c')
}

func Test_Choice(t *testing.T) {
	c := Choice('a', "bc", Range('0', '9'))
	match(t, "a", c, "a")
	match(t, "bc", c, "bc")
	match(t, "7", c, "7")

	noMatch(t, "", c, ErrUnexpectedEOI)
	noMatch(t, "x", c, ErrGrammarMismatch)
	noMatch(t, "b", c, ErrUnexpectedEOI)
	noMatch(t, "ab", c, ErrGrammarMismatch)
}

// Like Optional, a Choice adds no node; the chosen alternative's nodes join
// the enclosing sequence.
func Test_ChoiceInSeq(t *testing.T) {
	c := Seq('x', Choice('a', Seq('b')), 'y')
	match(t, "xay", c, "(xay)")
	match(t, "xby", c, "(x (b) y)")

	noMatch(t, "xy", c, ErrGrammarMismatch)
	noMatch(t, "xcy", c, ErrGrammarMismatch)
	noMatch(t, "xaby", c, ErrGrammarMismatch)
}

// When several alternatives match, the earliest wins.
func Test_ChoicePriority(t *testing.T) {
	match(t, "a", Choice(Seq('a'), 'a'), "(a)")
	match(t, "a", Choice('a', Seq('a')), "a")
	match(t, "a", Choice('b', Seq('a'), 'a'), "(a)")
}

// Alternatives that consume different lengths run in parallel; the one that
// fits the rest of the input succeeds.
func Test_ChoiceDifferentLengths(t *testing.T) {
	c := Seq(Choice(Seq("ab"), 'a'), 'b')
	match(t, "ab", c, "(ab)")
	match(t, "abb", c, "((ab) b)")

	c = Seq(Choice('a', Seq("ab")), 'c')
	match(t, "ac", c, "(ac)")
	match(t, "abc", c, "((ab) c)")
}

func Test_ChoiceNested(t *testing.T) {
	c := Choice(Choice('a', 'b'), Seq(Choice('c', 'd')))
	match(t, "a", c, "a")
	match(t, "b", c, "b")
	match(t, "d", c, "(d)")
	noMatch(t, "e", c, ErrGrammarMismatch)
}

// An alternative that can match empty input makes the Choice optional.
func Test_ChoiceNullable(t *testing.T) {
	c := Seq('x', Choice('a', Optional(Seq('b'))), 'y')
	match(t, "xy", c, "(xy)")
	match(t, "xay", c, "(xay)")
	match(t, "xby", c, "(x (b) y)")
}

func Test_ChoiceInRepetition(t *testing.T) {
	match(t, "aba", ZeroOrMore(Choice('a', Seq('b'))), "(a (b) a)")
	match(t, "abba", OneOrMore(Choice("ab", "ba")), "abba")
	noMatch(t, "abb", OneOrMore(Choice("ab", "ba")), ErrUnexpectedEOI)
}

func Test_ChoiceErrorOffset(t *testing.T) {
	_, err := run(t, "xcy", Seq('x', Choice('a', 'b'), 'y'))
	want := `grammar mismatch: unexpected 'c' at offset 1`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func Test_ChoicePanics(t *testing.T) {
	cases := []struct {
		name  string
		first any
		rest  []any
		want  error
	}{
		{"empty text", "", nil, ErrEmptyText},
		{"empty text rest", 'a', []any{""}, ErrEmptyText},
		{"unsupported", 1, nil, nil},
		{"unsupported rest", 'a', []any{1}, nil},
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
			Choice(tc.first, tc.rest...)
		})
	}
}

// User-defined kinds for Token tests.
const (
	nkWord int32 = iota
	nkInteger
	nkNumber
)

// Token brackets its elements like Seq but closes with opToken and its kind.
func Test_TokenCompile(t *testing.T) {
	compiles(t, Token(nkWord, Char('a')), opMark, opChar, 'a', 'a', opToken, nkWord)
	compiles(t, Token(nkNumber, Seq(Char('1')), Char('2')),
		opMark,
		opMark,
		opChar, '1', '1',
		opSeq,
		opChar, '2', '2',
		opToken, nkNumber)
}

// Nested sequences, however deep, collapse into one leaf node.
func Test_Token(t *testing.T) {
	match(t, "a", Token(nkWord, 'a'), "a")
	match(t, "abc", Token(nkWord, 'a', Seq('b'), 'c'), "abc")
	match(t, "abcd", Token(nkWord, Seq('a', Seq('b', Seq('c'))), 'd'), "abcd")
	match(t, "xaby", Seq('x', Token(nkWord, Seq('a'), Seq('b')), 'y'), "(x ab y)")
	match(t, "aaa", Token(nkWord, ZeroOrMore(Seq('a'))), "aaa")
	match(t, "b", Token(nkWord, Choice(Seq('a'), Seq('b'))), "b")
	match(t, "a1c", Token(nkWord, 'a', Token(nkNumber, '1'), 'c'), "a1c")

	noMatch(t, "", Token(nkWord, 'a'), ErrUnexpectedEOI)
	noMatch(t, "ax", Token(nkWord, 'a', Seq('b')), ErrGrammarMismatch)
}

// A token is sealed: neighbouring characters, and neighbouring tokens, stay
// separate nodes rather than merging into it, even when its kind is NkChars.
func Test_TokenBoundary(t *testing.T) {
	number := Token(nkNumber, OneOrMore(Range('0', '9')))
	match(t, "abc", Seq('a', Token(nkWord, 'b'), 'c'), "(a b c)")
	match(t, "abc", Seq('a', Token(NkChars, 'b'), 'c'), "(a b c)")
	match(t, "ab", Seq(Token(NkChars, 'a'), Token(NkChars, 'b')), "(a b)")
	match(t, "12+34", Seq(number, '+', number), "(12 + 34)")
}

func Test_TokenNodeShape(t *testing.T) {
	n, err := run(t, "abc", Token(nkWord, 'a', Seq('b'), 'c'))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Kind != nkWord || n.Lexeme != "abc" || n.Children != nil {
		t.Errorf("token = %d %q %v, want word \"abc\" with no children", n.Kind, n.Lexeme, n.Children)
	}
}

func Test_TokenKinds(t *testing.T) {
	n, err := run(t, "ab1", Seq(Token(nkWord, OneOrMore(Range('a', 'z'))), Token(nkNumber, Range('0', '9'))))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(n.Children) != 2 {
		t.Fatalf("children = %d, want 2", len(n.Children))
	}
	if w, d := n.Children[0], n.Children[1]; w.Kind != nkWord || d.Kind != nkNumber {
		t.Errorf("kinds = %d %d, want %d %d", w.Kind, d.Kind, nkWord, nkNumber)
	}
}

// A token that matches nothing still leaves a node, as an empty Seq does.
func Test_TokenEmpty(t *testing.T) {
	n, err := run(t, "", Token(nkWord, Optional('a')))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n == nil || n.Kind != nkWord || n.Lexeme != "" {
		t.Errorf("token = %v, want empty word node", n)
	}
	match(t, "xy", Seq('x', Token(nkWord, Optional('a')), 'y'), "(x  y)")
}

// The integer part is required, so ".5" is rejected; the mantissa requires
// digits after the point, so "1." is rejected.
func Test_FloatingPoint(t *testing.T) {
	digit := Range('0', '9')
	digits := OneOrMore(digit)
	sign := Choice('+', '-')
	mantissa := Seq('.', digits)
	exponent := Seq(Choice('e', 'E'), Optional(sign), digits)
	float := Token(nkNumber, Optional(sign), digits, Optional(mantissa), Optional(exponent))
	integer := Token(nkInteger, Optional(sign), digits)

	number := Choice(integer, float)

	match(t, "0", float, "0")
	match(t, "42", float, "42")
	match(t, "3.14", float, "3.14")
	match(t, "-3.14", float, "-3.14")
	match(t, "1e10", float, "1e10")
	match(t, "1.5e-3", float, "1.5e-3")
	match(t, "+6.02E+23", float, "+6.02E+23")

	noMatch(t, "", float, ErrUnexpectedEOI)
	noMatch(t, "-", float, ErrUnexpectedEOI)
	noMatch(t, "1.", float, ErrUnexpectedEOI)
	noMatch(t, "1e", float, ErrUnexpectedEOI)
	noMatch(t, "1e+", float, ErrUnexpectedEOI)
	noMatch(t, ".", float, ErrGrammarMismatch)
	noMatch(t, ".5", float, ErrGrammarMismatch)
	noMatch(t, "e5", float, ErrGrammarMismatch)
	noMatch(t, "--1", float, ErrGrammarMismatch)
	noMatch(t, "1.2.3", float, ErrGrammarMismatch)
	noMatch(t, "1x", float, ErrGrammarMismatch)

	// Choice prefers integer for whole numbers, signed or not; anything with a
	// fraction or exponent falls through to float.
	numbers := []struct {
		input string
		kind  int32
	}{
		{"0", nkInteger},
		{"42", nkInteger},
		{"-7", nkInteger},
		{"+7", nkInteger},
		{"3.14", nkNumber},
		{"1e10", nkNumber},
		{"+6.02E+23", nkNumber},
	}
	for _, tc := range numbers {
		n, err := run(t, tc.input, number)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.input, err)
			continue
		}
		if n.Kind != tc.kind || n.Lexeme != tc.input {
			t.Errorf("%q: number = %d %q, want %d %q", tc.input, n.Kind, n.Lexeme, tc.kind, tc.input)
		}
	}

	noMatch(t, "", number, ErrUnexpectedEOI)
	noMatch(t, "1.", number, ErrUnexpectedEOI)
	noMatch(t, ".5", number, ErrGrammarMismatch)
	noMatch(t, "1x", number, ErrGrammarMismatch)
}

// Top-level Optional and ZeroOrMore can leave several nodes at end of input;
// opEof wraps them in a seq node rather than returning only the last.
func Test_EofMultipleNodes(t *testing.T) {
	match(t, "aa", ZeroOrMore(Seq('a')), "((a) (a))")
	match(t, "a", ZeroOrMore(Seq('a')), "(a)")
	match(t, "ab", Optional('a', Seq('b')), "(a (b))")
	match(t, "abab", ZeroOrMore('a', Seq('b')), "(a (b) a (b))")
}

// A single node is returned as is, not wrapped.
func Test_EofSingleNode(t *testing.T) {
	match(t, "abc", Text("abc"), "abc")
	match(t, "aaa", ZeroOrMore('a'), "aaa")
	match(t, "a", Optional(Seq('a')), "(a)")
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
	if s.Kind != NkSeq || s.Lexeme != "" {
		t.Errorf("root = %d %q, want seq with empty lexeme", s.Kind, s.Lexeme)
	}
	wantKinds := []int32{NkChars, NkSeq, NkChars}
	if len(s.Children) != len(wantKinds) {
		t.Fatalf("children = %d, want %d", len(s.Children), len(wantKinds))
	}
	for i, c := range s.Children {
		if c.Kind != wantKinds[i] {
			t.Errorf("child %d kind = %d, want %d", i, c.Kind, wantKinds[i])
		}
		if c.Kind == NkChars && c.Children != nil {
			t.Errorf("chars child %d has Children: %v", i, c.Children)
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
	a, b, c := &Node{Lexeme: "a"}, &Node{Lexeme: "b"}, &Node{Lexeme: "c"}
	m := &machine{}
	m.push(a)
	m.push(b)
	m.push(c)

	for _, want := range []*Node{c, b, a} {
		if got := m.pop(); got != want {
			t.Fatalf("pop = %v, want %v", got, want)
		}
	}
	if m.tos != nil {
		t.Fatalf("stack = %v, want empty", m.tos)
	}
}

// Consecutive chars join into one node; a mark or other node starts a new one.
func Test_MachinePushChar(t *testing.T) {
	m := &machine{}
	m.pushChar('a')
	m.pushChar('😀')
	if m.tos.Kind != NkChars || m.tos.Lexeme != "a😀" {
		t.Fatalf("top = %d %q, want chars \"a😀\"", m.tos.Kind, m.tos.Lexeme)
	}
	if m.tos.nos != nil {
		t.Fatalf("joined chars left %v below", m.tos.nos)
	}

	m.push(&Node{Kind: nkMark})
	m.pushChar('b')
	if m.tos.Lexeme != "b" || m.tos.nos.Kind != nkMark {
		t.Fatalf("char after mark joined across it: top = %q", m.tos.Lexeme)
	}
}

// A fork shares the top node; extending it on one machine must not change
// what the other sees.
func Test_MachinePushCharShared(t *testing.T) {
	m1 := &machine{}
	m1.pushChar('a')
	m2 := &machine{tos: m1.tos}

	m1.pushChar('b')
	m2.pushChar('c')

	if m1.tos.Lexeme != "ab" {
		t.Errorf("m1 top = %q, want \"ab\"", m1.tos.Lexeme)
	}
	if m2.tos.Lexeme != "ac" {
		t.Errorf("m2 top = %q, want \"ac\"", m2.tos.Lexeme)
	}
}

func Test_MachineResult(t *testing.T) {
	a, b, c := &Node{Lexeme: "a"}, &Node{Lexeme: "b"}, &Node{Lexeme: "c"}

	if got := (&machine{}).result(); got != nil {
		t.Errorf("empty stack = %v, want nil", got)
	}

	m := &machine{}
	m.push(a)
	if got := m.result(); got != a {
		t.Errorf("single = %v, want a", got)
	}

	m = &machine{}
	for _, n := range []*Node{a, b, c} {
		m.push(n)
	}
	got := m.result()
	if got.Kind != NkSeq || !slices.Equal(got.Children, []*Node{a, b, c}) {
		t.Errorf("multiple = %d %v, want seq [a b c] in push order", got.Kind, got.Children)
	}
}

// popToMark stops at the nearest mark, discards it, and leaves anything below
// it, including outer marks, on the stack.
func Test_MachinePopToMark(t *testing.T) {
	outer, inner := &Node{Kind: nkMark}, &Node{Kind: nkMark}
	x, a, b := &Node{Lexeme: "x"}, &Node{Lexeme: "a"}, &Node{Lexeme: "b"}
	m := &machine{}
	for _, n := range []*Node{outer, x, inner, a, b} {
		m.push(n)
	}

	if got := m.popToMark(); !slices.Equal(got, []*Node{a, b}) {
		t.Fatalf("popToMark = %v, want [a b] in push order", got)
	}
	if m.tos != x {
		t.Fatalf("top = %v, want x", m.tos)
	}
	if got := m.popToMark(); !slices.Equal(got, []*Node{x}) {
		t.Fatalf("popToMark = %v, want [x]", got)
	}
	if m.tos != nil {
		t.Fatalf("stack = %v, want empty", m.tos)
	}
}

// A mark directly on top yields no children; nil is acceptable.
func Test_MachinePopToMarkEmpty(t *testing.T) {
	below := &Node{Lexeme: "below"}
	m := &machine{}
	m.push(below)
	m.push(&Node{Kind: nkMark})

	if got := m.popToMark(); len(got) != 0 {
		t.Fatalf("popToMark = %v, want empty", got)
	}
	if m.tos != below {
		t.Fatalf("top = %v, want below", m.tos)
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
	a, b, c := &Node{Lexeme: "a"}, &Node{Lexeme: "b"}, &Node{Lexeme: "c"}
	m1 := &machine{}
	m1.push(a)
	m1.push(b)
	m2 := &machine{tos: m1.tos}

	if got := m2.pop(); got != b {
		t.Fatalf("m2 pop = %v, want b", got)
	}
	m2.push(c)

	if got := []*Node{m1.pop(), m1.pop()}; !slices.Equal(got, []*Node{b, a}) {
		t.Errorf("m1 pops = %v, want [b a]", got)
	}
	if got := []*Node{m2.pop(), m2.pop()}; !slices.Equal(got, []*Node{c, a}) {
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
func run(t *testing.T, s string, c Compiler) (got *Node, err error) {
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
func show(s *Node) string {
	if s.Kind != NkSeq {
		return s.Lexeme
	}
	parts := make([]string, len(s.Children))
	for i, c := range s.Children {
		parts[i] = show(c)
	}
	return "(" + strings.Join(parts, " ") + ")"
}
