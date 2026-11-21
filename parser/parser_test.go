package parser

import (
	"errors"
	"fmt"
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
	if n.Kind != KindText {
		t.Errorf("kind = %q, want %q", n.Kind, KindText)
	}
	if n.Lexeme != "ab" {
		t.Errorf("lexeme = %q, want %q", n.Lexeme, "ab")
	}
	if n.Children != nil {
		t.Errorf("children = %v, want nil", n.Children)
	}
}

func Test_NodeKinds(t *testing.T) {
	cases := []struct {
		name  string
		input string
		c     Compiler
		want  string
	}{
		{"char", "a", Char('a'), KindChar},
		{"range", "a", Range('a', 'z'), KindChar},
		{"seq", "a", Seq(Char('a')), KindSeq},
		{"text", "a", Text("a"), KindText},
		{"rule", "a", Rule("x", Char('a')), KindRule},
		{"choice", "a", Choice(Char('a'), Char('b')), KindChar},
		{"optional", "a", Optional(Char('a')), KindOptional},
		{"optional empty", "", Optional(Char('a')), KindOptional},
		{"many", "aa", Many(Char('a')), KindMany},
		{"many empty", "", Many(Char('a')), KindMany},
		{"one or more", "a", OneOrMore(Char('a')), KindMany},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := run(t, tc.input, tc.c)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n.Kind != tc.want {
				t.Errorf("kind = %q, want %q", n.Kind, tc.want)
			}
		})
	}
}

// Bodies are compiled once, after the main program's opEof; each use is a
// call by rule index.
func Test_RuleCompile(t *testing.T) {
	links(t, Rule("x", Char('a')),
		opCall, 0, opEof,
		opChar, 'a', opRule, 1, 0, opRet)
	links(t, Rule("x", Char('a'), Text("bc")),
		opCall, 0, opEof,
		opChar, 'a',
		opChar, 'b',
		opChar, 'c',
		opText, 2,
		opRule, 2, 0, opRet)
}

func Test_RuleCompileReused(t *testing.T) {
	ab := Rule("ab", Text("ab"))
	links(t, Seq(ab, ab),
		opCall, 0, opCall, 0, opSeq, 2, opEof,
		opChar, 'a', opChar, 'b', opText, 2, opRule, 1, 0, opRet)
}

func Test_RuleCompileNested(t *testing.T) {
	p := links(t, Rule("x", Rule("y", Char('a')), Char('b')),
		opCall, 0, opEof,
		opCall, 1, opChar, 'b', opRule, 2, 0, opRet,
		opChar, 'a', opRule, 1, 1, opRet)
	if p == nil {
		return
	}
	entries := []int32{p.rules[0].entry, p.rules[1].entry}
	if !slices.Equal(entries, []int32{3, 11}) {
		t.Errorf("entries = %v, want [3 11]", entries)
	}
}

// A self-reference becomes a call back to the rule's own entry rather than an
// inline expansion, so compilation terminates.
func Test_RuleCompileRecursive(t *testing.T) {
	var p Compiler
	p = Rule("p", Char('('), Lazy(&p), Char(')'))
	links(t, p,
		opCall, 0, opEof,
		opChar, '(', opCall, 0, opChar, ')', opRule, 3, 0, opRet)
}

func Test_Rule(t *testing.T) {
	match(t, "a", Rule("x", Char('a')), "x(a)")
	match(t, "abc", Rule("x", Char('a'), Char('b'), Char('c')), "x(a b c)")
	match(t, "a5", Rule("x", Char('a'), Range('0', '9')), "x(a 5)")
	match(t, "abc", Rule("x", Text("ab"), Char('c')), `x("ab" c)`)

	noMatch(t, "", Rule("x", Char('a')), ErrUnexpectedEOI)
	noMatch(t, "ab", Rule("x", Char('a'), Char('b'), Char('c')), ErrUnexpectedEOI)
	noMatch(t, "abd", Rule("x", Char('a'), Char('b'), Char('c')), ErrGrammarMismatch)
	noMatch(t, "abcd", Rule("x", Char('a'), Char('b'), Char('c')), ErrGrammarMismatch)
}

// Calls and returns consume no input, so execution must continue to the
// following operation within the same step.
func Test_RuleNested(t *testing.T) {
	match(t, "abc", Seq(Rule("x", Char('a'), Char('b')), Char('c')), "(x(a b) c)")
	match(t, "abc", Rule("x", Char('a'), Seq(Char('b'), Char('c'))), "x(a (b c))")
	match(t, "abc", Rule("x", Rule("y", Char('a'), Char('b')), Char('c')), "x(y(a b) c)")
	match(t, "abc", Rule("x", Char('a'), Rule("y", Char('b')), Char('c')), "x(a y(b) c)")

	noMatch(t, "abx", Seq(Rule("x", Char('a'), Char('b')), Char('c')), ErrGrammarMismatch)
}

func Test_RuleReused(t *testing.T) {
	ab := Rule("ab", Text("ab"))
	match(t, "abab", Seq(ab, ab), `(ab("ab") ab("ab"))`)
	match(t, "abcab", Seq(ab, Char('c'), ab), `(ab("ab") c ab("ab"))`)

	noMatch(t, "abax", Seq(ab, ab), ErrGrammarMismatch)
}

// Each return must resume after its own call site; a long chain of nested
// rules unwinds entirely within the final step.
func Test_RuleDeepNesting(t *testing.T) {
	c, want := Char('a'), "a"
	for i := range 30 {
		name := fmt.Sprintf("r%d", i)
		c = Rule(name, c)
		want = name + "(" + want + ")"
	}
	match(t, "a", c, want)
	match(t, "ab", Seq(c, Char('b')), "("+want+" b)")
}

// A rule can be used before the variable holding it is assigned.
func Test_RuleAssignedLater(t *testing.T) {
	var x Compiler
	c := Seq(Lazy(&x), Char('b'), Lazy(&x))
	x = Rule("x", Char('a'))
	match(t, "aba", c, "(x(a) b x(a))")
	noMatch(t, "abb", c, ErrGrammarMismatch)
}

// A rule first reached inside another rule's body is added to the table while
// bodies are being compiled, and compiled in turn.
func Test_RuleFirstUsedInBody(t *testing.T) {
	y := Rule("y", Char('a'))
	x := Rule("x", y)
	match(t, "aa", Seq(x, y), "(x(y(a)) y(a))")
}

// With no alternative to end the recursion these can never complete; they
// confirm recursion executes and fails at the right place rather than hanging.
func Test_RuleRecursive(t *testing.T) {
	var parens Compiler
	parens = Rule("p", Char('('), Lazy(&parens), Char(')'))
	noMatch(t, "", parens, ErrUnexpectedEOI)
	noMatch(t, "((((", parens, ErrUnexpectedEOI)
	noMatch(t, "(()", parens, ErrGrammarMismatch)
	noMatch(t, "(x", parens, ErrGrammarMismatch)

	var as Compiler
	as = Rule("as", Char('a'), Lazy(&as))
	noMatch(t, strings.Repeat("a", 100), as, ErrUnexpectedEOI)
	noMatch(t, "aab", as, ErrGrammarMismatch)
}

func Test_RuleMutuallyRecursive(t *testing.T) {
	var xy Compiler
	xy = Rule("a", Char('x'), Rule("b", Char('y'), Lazy(&xy)))
	noMatch(t, "xyxy", xy, ErrUnexpectedEOI)
	noMatch(t, "xyy", xy, ErrGrammarMismatch)
}

func Test_RuleRecursiveErrorOffset(t *testing.T) {
	var p Compiler
	p = Rule("p", Char('('), Lazy(&p), Char(')'))
	_, err := run(t, "((()", p)
	want := `grammar mismatch: unexpected ')' at offset 3`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

// Lazy defers reading the Go variable until compile time, by which point Rule
// has returned and the variable is assigned.
func Test_Lazy(t *testing.T) {
	var parens Compiler
	parens = Rule("p", Char('('), Lazy(&parens), Char(')'))
	links(t, parens,
		opCall, 0, opEof,
		opChar, '(', opCall, 0, opChar, ')', opRule, 3, 0, opRet)
	noMatch(t, "(()", parens, ErrGrammarMismatch)
}

func Test_LazyNonRecursive(t *testing.T) {
	var b Compiler
	c := Seq(Char('a'), Lazy(&b))
	b = Char('b')
	match(t, "ab", c, "(a b)")
}

// A variable still nil at compile time cannot be expanded.
func Test_LazyUnresolved(t *testing.T) {
	var x Compiler
	noMatch(t, "a", Seq(Char('a'), Lazy(&x)), ErrUnresolvedLazy)
	noMatch(t, "a", Rule("r", Char('a'), Lazy(&x)), ErrUnresolvedLazy)
	noMatch(t, "a", Lazy(&x), ErrUnresolvedLazy)
}

func Test_LazyNilPointerPanics(t *testing.T) {
	defer func() {
		r := recover()
		err, ok := r.(error)
		if !ok || !errors.Is(err, ErrUnresolvedLazy) {
			t.Errorf("recovered %v, want ErrUnresolvedLazy", r)
		}
	}()
	Lazy(nil)
}

// Without a Rule to stop at, a self-reference would expand inline forever.
func Test_LazyRecursionWithoutRule(t *testing.T) {
	var s Compiler
	s = Seq(Char('a'), Optional(Lazy(&s)))
	noMatch(t, "a", s, ErrRecursionWithoutRule)

	var c Compiler
	c = Choice(Char('a'), Lazy(&c))
	noMatch(t, "a", c, ErrRecursionWithoutRule)

	var r Compiler
	r = Rule("r", Seq(Char('a'), Optional(Lazy(&s))))
	noMatch(t, "a", r, ErrRecursionWithoutRule)
}

// A Rule anywhere in the cycle stops expansion, even if the Lazy resolves to
// a compiler that only reaches the Rule further in.
func Test_LazyRecursionThroughRule(t *testing.T) {
	var r Compiler
	inner := Seq(Char('a'), Optional(Lazy(&r)))
	r = Rule("r", inner)
	match(t, "aa", r, "r((a ?(r((a ?())))))")
}

// Only re-entry into the same Lazy is an error; using it side by side is not.
func Test_LazyReused(t *testing.T) {
	var x Compiler
	l := Lazy(&x)
	x = Char('a')
	match(t, "aa", Seq(l, l), "(a a)")
	match(t, "aa", Seq(l, Optional(l)), "(a ?(a))")
}

// A grammar compiles afresh on every Run, so expansion state must not leak
// between runs.
func Test_LazyRunTwice(t *testing.T) {
	var p Compiler
	p = Rule("p", Char('['), Optional(Lazy(&p)), Char(']'))
	match(t, "[[]]", p, "p([ ?(p([ ?() ])) ])")
	match(t, "[]", p, "p([ ?() ])")
}

// Rule identity is the Rule value, not its name, so two definitions sharing a
// name conflict even if their bodies are equivalent.
func Test_RuleDuplicate(t *testing.T) {
	noMatch(t, "ab", Seq(Rule("x", Char('a')), Rule("x", Char('b'))), ErrDuplicateRule)
	noMatch(t, "aa", Seq(Rule("x", Char('a')), Rule("x", Char('a'))), ErrDuplicateRule)
	noMatch(t, "ab", Rule("x", Char('a'), Rule("x", Char('b'))), ErrDuplicateRule)
}

func Test_RuleLeftRecursion(t *testing.T) {
	var x Compiler
	x = Rule("x", Lazy(&x), Char('a'))
	noMatch(t, "a", x, ErrLeftRecursion)

	var xs Compiler
	xs = Rule("x", Seq(Lazy(&xs), Char('a')))
	noMatch(t, "a", xs, ErrLeftRecursion)

	var a Compiler
	a = Rule("a", Rule("b", Lazy(&a), Char('y')), Char('x'))
	noMatch(t, "a", a, ErrLeftRecursion)

	var only Compiler
	only = Rule("x", Lazy(&only))
	noMatch(t, "a", Seq(Char('a'), only), ErrLeftRecursion)

	_, err := run(t, "a", x)
	if want := `left recursion: "x"`; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

// Calling a non-recursive rule before reading a rune is not left recursion.
func Test_RuleLeadingCallAllowed(t *testing.T) {
	match(t, "ab", Rule("x", Rule("y", Char('a')), Char('b')), "x(y(a) b)")
	match(t, "ab", Rule("x", Rule("y", Rule("z", Char('a'))), Char('b')), "x(y(z(a)) b)")
}

func Test_RuleNodeShape(t *testing.T) {
	n, err := run(t, "ab", Rule("pair", Char('a'), Char('b')))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Kind != KindRule {
		t.Errorf("kind = %q, want %q", n.Kind, KindRule)
	}
	if n.Name != "pair" {
		t.Errorf("name = %q, want %q", n.Name, "pair")
	}
	if n.Lexeme != "" {
		t.Errorf("lexeme = %q, want empty", n.Lexeme)
	}
	if len(n.Children) != 2 {
		t.Fatalf("children = %d, want 2", len(n.Children))
	}
}

// Only Rule nodes carry a name.
func Test_SeqUnnamed(t *testing.T) {
	n, err := run(t, "a", Seq(Char('a')))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Name != "" {
		t.Errorf("seq name = %q, want empty", n.Name)
	}
}

func Test_EmptyNamePanics(t *testing.T) {
	cases := map[string]func(){
		"rule": func() { Rule("", Char('a')) },
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				r := recover()
				err, ok := r.(error)
				if !ok || !errors.Is(err, ErrEmptyName) {
					t.Errorf("recovered %v, want ErrEmptyName", r)
				}
			}()
			f()
		})
	}
}

func Test_RuleInTextRejected(t *testing.T) {
	ruleInText := func(p *program) {
		Rule("x", Char('a'))(p)
		op(p, opText, 1)
	}
	noMatch(t, "a", ruleInText, ErrNonCharInText)
}

func Test_ChoiceCompile(t *testing.T) {
	compiles(t, Choice(Char('a'), Char('b')),
		opSplit, 3, 7,
		opChar, 'a',
		opJmp, 9,
		opChar, 'b')
	compiles(t, Choice(Char('a'), Char('b'), Char('c')),
		opSplit, 3, 7,
		opChar, 'a',
		opJmp, 16,
		opSplit, 10, 14,
		opChar, 'b',
		opJmp, 16,
		opChar, 'c')
	compiles(t, Choice(Char('a')), opChar, 'a')
}

// Addresses are absolute, so an Choice that does not start at 0 must offset its
// targets by its own position.
func Test_ChoiceCompileOffset(t *testing.T) {
	compiles(t, Seq(Char('x'), Choice(Char('a'), Char('b'))),
		opChar, 'x',
		opSplit, 5, 9,
		opChar, 'a',
		opJmp, 11,
		opChar, 'b',
		opSeq, 2)
}

func Test_Choice(t *testing.T) {
	ab := Choice(Char('a'), Char('b'))
	match(t, "a", ab, "a")
	match(t, "b", ab, "b")
	match(t, "c", Choice(Char('a'), Char('b'), Char('c')), "c")
	match(t, "a", Choice(Char('a')), "a")
	match(t, "q", Choice(Range('a', 'z'), Range('0', '9')), "q")
	match(t, "7", Choice(Range('a', 'z'), Range('0', '9')), "7")

	noMatch(t, "", ab, ErrUnexpectedEOI)
	noMatch(t, "c", ab, ErrGrammarMismatch)
	noMatch(t, "ab", ab, ErrGrammarMismatch)
}

// Alternatives of different lengths run in parallel; each survives only as
// long as the input agrees with it.
func Test_ChoiceDifferentLengths(t *testing.T) {
	c := Choice(Text("ab"), Text("abc"), Text("abcd"))
	match(t, "ab", c, `"ab"`)
	match(t, "abc", c, `"abc"`)
	match(t, "abcd", c, `"abcd"`)

	noMatch(t, "a", c, ErrUnexpectedEOI)
	noMatch(t, "abx", c, ErrGrammarMismatch)
	noMatch(t, "abcde", c, ErrGrammarMismatch)
}

func Test_ChoiceInSeq(t *testing.T) {
	c := Seq(Choice(Char('a'), Char('b')), Char('c'))
	match(t, "ac", c, "(a c)")
	match(t, "bc", c, "(b c)")
	noMatch(t, "cc", c, ErrGrammarMismatch)
	noMatch(t, "ab", c, ErrGrammarMismatch)

	match(t, "xby", Seq(Char('x'), Choice(Char('a'), Char('b')), Char('y')), "(x b y)")
	match(t, "bd", Seq(Choice(Char('a'), Char('b')), Choice(Char('c'), Char('d'))), "(b d)")
}

func Test_ChoiceNested(t *testing.T) {
	c := Choice(Choice(Char('a'), Char('b')), Choice(Char('c'), Char('d')))
	for _, s := range []string{"a", "b", "c", "d"} {
		match(t, s, c, s)
	}
	noMatch(t, "e", c, ErrGrammarMismatch)
}

// Both branches push a 'b' node before one dies; the survivor must not see the
// other's stack.
func Test_ChoiceForkIsolation(t *testing.T) {
	c := Seq(Char('a'), Choice(Seq(Char('b'), Char('c')), Seq(Char('b'), Char('d'))))
	match(t, "abc", c, "(a (b c))")
	match(t, "abd", c, "(a (b d))")
	noMatch(t, "abe", c, ErrGrammarMismatch)
}

// The leftmost matching alternative wins.
func Test_ChoicePriority(t *testing.T) {
	match(t, "a", Choice(Rule("x", Char('a')), Rule("y", Char('a'))), "x(a)")
	match(t, "a", Choice(Rule("y", Char('a')), Rule("x", Char('a'))), "y(a)")
	match(t, "ab", Choice(
		Seq(Rule("x", Char('a')), Char('b')),
		Seq(Rule("y", Char('a')), Char('b')),
	), "(x(a) b)")
}

// Leftmost priority must hold for alternatives forked within an earlier
// alternative, not just between siblings.
func Test_ChoicePriorityNested(t *testing.T) {
	x, y, z := Rule("x", Char('a')), Rule("y", Char('a')), Rule("z", Char('a'))
	match(t, "a", Choice(Choice(x, y), z), "x(a)")
	match(t, "a", Choice(Choice(Char('b'), y), x), "y(a)")
	match(t, "a", Choice(z, Choice(x, y)), "z(a)")
}

// Forks share the return stack; each must still return to its own call site.
func Test_ChoiceSharedRule(t *testing.T) {
	ab := Rule("ab", Text("ab"))
	c := Choice(Seq(ab, Char('c')), Seq(ab, Char('d')))
	match(t, "abc", c, `(ab("ab") c)`)
	match(t, "abd", c, `(ab("ab") d)`)
	noMatch(t, "abe", c, ErrGrammarMismatch)
}

func Test_ChoiceInRule(t *testing.T) {
	sign := Rule("sign", Choice(Char('+'), Char('-')))
	match(t, "+1", Seq(sign, Char('1')), "(sign(+) 1)")
	match(t, "-1", Seq(sign, Char('1')), "(sign(-) 1)")
}

func Test_ChoiceErrorOffset(t *testing.T) {
	_, err := run(t, "abx", Choice(Text("abc"), Text("abd")))
	want := `grammar mismatch: unexpected 'x' at offset 2`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func Test_ChoiceRecursive(t *testing.T) {
	var brackets Compiler
	brackets = Rule("p", Char('['), Choice(Lazy(&brackets), Char('x')), Char(']'))
	match(t, "[x]", brackets, "p([ x ])")
	match(t, "[[x]]", brackets, "p([ p([ x ]) ])")
	match(t, "[[[x]]]", brackets, "p([ p([ p([ x ]) ]) ])")

	noMatch(t, "[[x]", brackets, ErrUnexpectedEOI)
	noMatch(t, "[x]]", brackets, ErrGrammarMismatch)
	noMatch(t, "[]", brackets, ErrGrammarMismatch)
}

func Test_ChoiceRightRecursive(t *testing.T) {
	var as Compiler
	as = Rule("as", Char('a'), Choice(Lazy(&as), Char('.')))
	match(t, "a.", as, "as(a .)")
	match(t, "aaa.", as, "as(a as(a as(a .)))")

	long := strings.Repeat("a", 200) + "."
	if n, err := run(t, long, as); err != nil || depth(n) != 200 {
		t.Errorf("long input: depth %d, err %v; want depth 200", depth(n), err)
	}
}

func Test_ChoiceRecursiveExpression(t *testing.T) {
	num := Rule("num", Range('0', '9'))
	var expr Compiler
	expr = Rule("expr", num, Choice(Seq(Choice(Char('+'), Char('-')), Lazy(&expr)), Char(';')))
	match(t, "1;", expr, "expr(num(1) ;)")
	match(t, "1+2;", expr, "expr(num(1) (+ expr(num(2) ;)))")
	match(t, "1-2+3;", expr, "expr(num(1) (- expr(num(2) (+ expr(num(3) ;)))))")

	noMatch(t, "1+;", expr, ErrGrammarMismatch)
	noMatch(t, "1+2", expr, ErrUnexpectedEOI)
}

func Test_ChoiceLazyRecursive(t *testing.T) {
	var list Compiler
	list = Rule("list", Char('a'), Choice(Seq(Char(','), Lazy(&list)), Char('.')))
	match(t, "a,a.", list, "list(a (, list(a .)))")
}

// A call reached without reading a rune counts in any alternative, not just
// the first.
func Test_ChoiceLeftRecursion(t *testing.T) {
	var e1, e2, e3, a Compiler
	e1 = Rule("e", Choice(Seq(Lazy(&e1), Char('+')), Char('1')))
	e2 = Rule("e", Choice(Char('1'), Seq(Lazy(&e2), Char('+'))))
	e3 = Rule("e", Choice(Char('0'), Choice(Char('1'), Lazy(&e3))))
	a = Rule("a", Choice(Char('1'), Rule("b", Lazy(&a), Char('+'))))
	for _, c := range []Compiler{e1, e2, e3, a} {
		noMatch(t, "1", c, ErrLeftRecursion)
	}
}

// Calling rules that read a rune first is fine through Choice too.
func Test_ChoiceLeadingCallAllowed(t *testing.T) {
	x, y := Rule("x", Char('a')), Rule("y", Char('b'))
	match(t, "b", Rule("e", Choice(x, y)), "e(y(b))")
}

// Each Choice(a, a) doubles the threads; dedup must collapse them at the next
// consumption so the count stays constant rather than reaching 2^n.
func Test_DedupAmbiguous(t *testing.T) {
	parts := make([]Compiler, 40)
	for i := range parts {
		parts[i] = Choice(Char('a'), Char('a'))
	}
	_, v, err := runVM(t, strings.Repeat("a", 40), Seq(parts[0], parts[1:]...))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.peak > 2 {
		t.Errorf("peak threads = %d, want <= 2", v.peak)
	}
}

// Threads that call the same rule from the same site get separately pushed
// but equal return stacks; interning must make them compare equal.
func Test_DedupAcrossCalls(t *testing.T) {
	x := Rule("x", Char('b'))
	parts := make([]Compiler, 40)
	for i := range parts {
		parts[i] = Seq(Choice(Char('a'), Char('a')), x)
	}
	_, v, err := runVM(t, strings.Repeat("ab", 40), Seq(parts[0], parts[1:]...))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.peak > 2 {
		t.Errorf("peak threads = %d, want <= 2", v.peak)
	}
}

// Ambiguity inside a recursive rule: both threads make the recursive call from
// the same site, so each level's return stacks persist while the next level
// forks. Without interning the stacks differ by pointer and threads double
// per level.
func Test_DedupInsideRecursion(t *testing.T) {
	var as Compiler
	as = Rule("as", Choice(Char('a'), Char('a')), Choice(Lazy(&as), Char('.')))
	_, v, err := runVM(t, strings.Repeat("a", 40)+".", as)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.peak > 2 {
		t.Errorf("peak threads = %d, want <= 2", v.peak)
	}
}

// Both threads reach the same state on 'b'; the leftmost alternative's tree
// must be the one kept.
func Test_DedupKeepsPriority(t *testing.T) {
	x, y := Rule("x", Char('a')), Rule("y", Char('a'))
	match(t, "ab", Seq(Choice(x, y), Char('b')), "(x(a) b)")
	match(t, "ab", Seq(Choice(y, x), Char('b')), "(y(a) b)")
	match(t, "aab", Seq(Choice(x, y), Choice(y, x), Char('b')), "(x(a) y(a) b)")
}

// Same pc inside a shared rule but different return addresses: distinct
// futures that must not be merged.
func Test_DedupDistinctCallSites(t *testing.T) {
	r := Rule("r", Char('a'))
	c := Choice(Seq(r, Char('b')), Seq(r, Char('c')))
	match(t, "ab", c, "(r(a) b)")
	match(t, "ac", c, "(r(a) c)")
}

// Same pc at different recursion depths must not be merged either.
func Test_DedupDistinctDepths(t *testing.T) {
	var brackets Compiler
	brackets = Rule("p", Char('['), Choice(Lazy(&brackets), Char('x')), Char(']'))
	match(t, "[[x]]", Seq(brackets), "(p([ p([ x ]) ]))")
	match(t, "[x][[x]]", Seq(brackets, brackets), "(p([ x ]) p([ p([ x ]) ]))")
}

func Test_FrameInterning(t *testing.T) {
	v := newVM(&program{})
	a, b := v.frame(5, nil), v.frame(5, nil)
	if a != b {
		t.Error("equal frames not interned")
	}
	if v.frame(6, nil) == a {
		t.Error("different pc shares a frame")
	}
	if v.frame(7, a) != v.frame(7, b) {
		t.Error("equal stacks not interned")
	}
	if v.frame(7, v.frame(6, nil)) == v.frame(7, a) {
		t.Error("different stacks share a frame")
	}
}

func Test_OptionalCompile(t *testing.T) {
	compiles(t, Optional(Char('a')),
		opMark,
		opSplit, 4, 6,
		opChar, 'a',
		opOptional)
	compiles(t, Seq(Char('x'), Optional(Char('a'))),
		opChar, 'x',
		opMark,
		opSplit, 6, 8,
		opChar, 'a',
		opOptional,
		opSeq, 2)
}

func Test_ManyCompile(t *testing.T) {
	compiles(t, Many(Char('a')),
		opMark,
		opSplit, 4, 8,
		opChar, 'a',
		opJmp, 1,
		opMany)
	compiles(t, Seq(Char('x'), Many(Char('a'))),
		opChar, 'x',
		opMark,
		opSplit, 6, 10,
		opChar, 'a',
		opJmp, 3,
		opMany,
		opSeq, 2)
}

func Test_Optional(t *testing.T) {
	match(t, "", Optional(Char('a')), "?()")
	match(t, "a", Optional(Char('a')), "?(a)")
	match(t, "ab", Optional(Text("ab")), `?("ab")`)
	noMatch(t, "b", Optional(Char('a')), ErrGrammarMismatch)
	noMatch(t, "aa", Optional(Char('a')), ErrGrammarMismatch)

	c := Seq(Optional(Char('a')), Char('b'))
	match(t, "b", c, "(?() b)")
	match(t, "ab", c, "(?(a) b)")
	noMatch(t, "a", c, ErrUnexpectedEOI)
	noMatch(t, "aab", c, ErrGrammarMismatch)
	noMatch(t, "", c, ErrUnexpectedEOI)

	match(t, "ac", Seq(Char('a'), Optional(Char('b')), Char('c')), "(a ?() c)")
	match(t, "abc", Seq(Char('a'), Optional(Char('b')), Char('c')), "(a ?(b) c)")
}

func Test_Many(t *testing.T) {
	match(t, "", Many(Char('a')), "*()")
	match(t, "a", Many(Char('a')), "*(a)")
	match(t, "aaa", Many(Char('a')), "*(a a a)")
	noMatch(t, "b", Many(Char('a')), ErrGrammarMismatch)
	noMatch(t, "aab", Many(Char('a')), ErrGrammarMismatch)

	c := Seq(Many(Char('a')), Char('b'))
	match(t, "b", c, "(*() b)")
	match(t, "aaab", c, "(*(a a a) b)")
	noMatch(t, "aaa", c, ErrUnexpectedEOI)
	noMatch(t, "aac", c, ErrGrammarMismatch)
}

func Test_ManyOfComposites(t *testing.T) {
	match(t, "abba", Many(Choice(Char('a'), Char('b'))), "*(a b b a)")
	match(t, "abab", Many(Text("ab")), `*("ab" "ab")`)
	match(t, "a1b2", Many(Seq(Range('a', 'z'), Range('0', '9'))), "*((a 1) (b 2))")
	match(t, "xx", Many(Rule("x", Char('x'))), "*(x(x) x(x))")
	noMatch(t, "aba", Many(Text("ab")), ErrUnexpectedEOI)
}

func Test_ManyLong(t *testing.T) {
	n, v, err := runVM(t, strings.Repeat("a", 1000), Many(Char('a')))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(n.Children) != 1000 {
		t.Errorf("children = %d, want 1000", len(n.Children))
	}
	if v.peak > 1 {
		t.Errorf("peak threads = %d, want 1", v.peak)
	}
}

// Optional prefers matching, and Many prefers another repetition, so the
// first construct takes everything it can.
func Test_Greedy(t *testing.T) {
	match(t, "a", Seq(Optional(Char('a')), Optional(Char('a'))), "(?(a) ?())")
	match(t, "aa", Seq(Many(Char('a')), Optional(Char('a'))), "(*(a a) ?())")
	match(t, "aa", Seq(Many(Char('a')), Many(Char('a'))), "(*(a a) *())")
	match(t, "aab", Seq(Many(Char('a')), Many(Char('a')), Char('b')), "(*(a a) *() b)")
}

// Each Optional and Many pushes its own mark, so nested collectors must take
// only their own nodes.
func Test_OptionalManyNested(t *testing.T) {
	match(t, "", Optional(Optional(Char('a'))), "?(?())")
	match(t, "a", Optional(Optional(Char('a'))), "?(?(a))")
	match(t, "abab", Many(Seq(Char('a'), Optional(Char('b')))), "*((a ?(b)) (a ?(b)))")
	match(t, "aab", Many(Seq(Char('a'), Optional(Char('b')))), "*((a ?()) (a ?(b)))")
	match(t, "a,aa", Seq(Many(Char('a')), Char(','), Many(Char('a'))), "(*(a) , *(a a))")
	match(t, "xaay", Seq(Char('x'), Many(Char('a')), Char('y')), "(x *(a a) y)")
}

// A body that can match nothing would loop forever without consuming; the VM
// must end the loop at the revisited split.
func Test_ManyNullableBody(t *testing.T) {
	match(t, "", Many(Optional(Char('a'))), "*()")
	match(t, "aa", Many(Optional(Char('a'))), "*(?(a) ?(a))")
	match(t, "", Many(Many(Char('a'))), "*()")
	match(t, "aaa", Many(Many(Char('a'))), "*(*(a a a))")
	match(t, "b", Seq(Many(Many(Char('a'))), Char('b')), "(*() b)")
	noMatch(t, "ab", Many(Many(Char('a'))), ErrGrammarMismatch)
}

func Test_ManyRecursive(t *testing.T) {
	var list Compiler
	list = Rule("list", Char('['), Many(Choice(Char('x'), Lazy(&list))), Char(']'))
	match(t, "[]", list, "list([ *() ])")
	match(t, "[x]", list, "list([ *(x) ])")
	match(t, "[x[x]x]", list, "list([ *(x list([ *(x) ]) x) ])")
	match(t, "[[][]]", list, "list([ *(list([ *() ]) list([ *() ])) ])")
	noMatch(t, "[x", list, ErrUnexpectedEOI)
	noMatch(t, "[x]]", list, ErrGrammarMismatch)
}

func Test_OptionalRecursive(t *testing.T) {
	var r Compiler
	r = Rule("r", Char('x'), Optional(Lazy(&r)))
	match(t, "x", r, "r(x ?())")
	match(t, "xxx", r, "r(x ?(r(x ?(r(x ?())))))")
}

func Test_NullableRule(t *testing.T) {
	n := Rule("n", Optional(Char('a')))
	match(t, "", n, "n(?())")
	match(t, "b", Seq(n, Char('b')), "(n(?()) b)")
	match(t, "", Rule("m", Many(Char('a'))), "m(*())")
}

func Test_ManyDedup(t *testing.T) {
	_, v, err := runVM(t, strings.Repeat("a", 40), Many(Choice(Char('a'), Char('a'))))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.peak > 2 {
		t.Errorf("peak threads = %d, want <= 2", v.peak)
	}
}

func Test_ManyErrorOffset(t *testing.T) {
	_, err := run(t, "aac", Seq(Many(Char('a')), Char('b')))
	want := `grammar mismatch: unexpected 'c' at offset 2`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

// A nullable prefix does not protect a call from being left recursive.
func Test_NullableLeftRecursion(t *testing.T) {
	n := Rule("n", Optional(Char('x')))
	m := Rule("m", Many(Char('x')))
	var r1, r2, r3, r4, r5, r6 Compiler
	r1 = Rule("r", Optional(Char('x')), Lazy(&r1), Char('a'))
	r2 = Rule("r", Many(Char('x')), Lazy(&r2))
	r3 = Rule("r", Many(Lazy(&r3)), Char('a'))
	r4 = Rule("r", Optional(Lazy(&r4)), Char('a'))
	r5 = Rule("r", n, Lazy(&r5))
	r6 = Rule("r", m, n, Lazy(&r6))
	for _, c := range []Compiler{r1, r2, r3, r4, r5, r6} {
		noMatch(t, "a", c, ErrLeftRecursion)
	}
}

// A nullable rule followed by a consuming construct is safe, and so is a call
// placed after a consuming construct.
func Test_NullableNotLeftRecursive(t *testing.T) {
	n := Rule("n", Optional(Char('x')))
	var r Compiler
	r = Rule("r", n, Char('a'), Optional(Lazy(&r)))
	match(t, "xa", r, "r(n(?(x)) a ?())")
	match(t, "aa", r, "r(n(?()) a ?(r(n(?()) a ?())))")
}

func Test_OneOrMoreCompile(t *testing.T) {
	compiles(t, OneOrMore(Char('a')),
		opMark,
		opChar, 'a',
		opSplit, 1, 6,
		opMany)
	compiles(t, Seq(Char('x'), OneOrMore(Char('a'))),
		opChar, 'x',
		opMark,
		opChar, 'a',
		opSplit, 3, 8,
		opMany,
		opSeq, 2)
}

func Test_OneOrMore(t *testing.T) {
	match(t, "a", OneOrMore(Char('a')), "*(a)")
	match(t, "aaa", OneOrMore(Char('a')), "*(a a a)")
	noMatch(t, "", OneOrMore(Char('a')), ErrUnexpectedEOI)
	noMatch(t, "b", OneOrMore(Char('a')), ErrGrammarMismatch)
	noMatch(t, "aab", OneOrMore(Char('a')), ErrGrammarMismatch)

	c := Seq(OneOrMore(Char('a')), Char('b'))
	match(t, "ab", c, "(*(a) b)")
	match(t, "aaab", c, "(*(a a a) b)")
	noMatch(t, "b", c, ErrGrammarMismatch)
	noMatch(t, "aaa", c, ErrUnexpectedEOI)
}

func Test_OneOrMoreOfComposites(t *testing.T) {
	match(t, "abba", OneOrMore(Choice(Char('a'), Char('b'))), "*(a b b a)")
	match(t, "abab", OneOrMore(Text("ab")), `*("ab" "ab")`)
	match(t, "a1b2", OneOrMore(Seq(Range('a', 'z'), Range('0', '9'))), "*((a 1) (b 2))")
	match(t, "xx", OneOrMore(Rule("x", Char('x'))), "*(x(x) x(x))")
	noMatch(t, "aba", OneOrMore(Text("ab")), ErrUnexpectedEOI)
}

func Test_OneOrMoreLong(t *testing.T) {
	n, v, err := runVM(t, strings.Repeat("a", 1000), OneOrMore(Char('a')))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(n.Children) != 1000 {
		t.Errorf("children = %d, want 1000", len(n.Children))
	}
	if v.peak > 1 {
		t.Errorf("peak threads = %d, want 1", v.peak)
	}
}

func Test_OneOrMoreGreedy(t *testing.T) {
	match(t, "aa", Seq(OneOrMore(Char('a')), Optional(Char('a'))), "(*(a a) ?())")
	match(t, "aa", Seq(OneOrMore(Char('a')), Many(Char('a'))), "(*(a a) *())")
	match(t, "aa", Seq(OneOrMore(Char('a')), OneOrMore(Char('a'))), "(*(a) *(a))")
	noMatch(t, "a", Seq(OneOrMore(Char('a')), OneOrMore(Char('a'))), ErrUnexpectedEOI)
}

func Test_OneOrMoreNested(t *testing.T) {
	match(t, "aab", OneOrMore(Seq(Char('a'), Optional(Char('b')))), "*((a ?()) (a ?(b)))")
	match(t, "aaa", OneOrMore(OneOrMore(Char('a'))), "*(*(a a a))")
	match(t, "a,aa", Seq(OneOrMore(Char('a')), Char(','), OneOrMore(Char('a'))), "(*(a) , *(a a))")
}

// A body that can match nothing makes OneOrMore itself nullable; the loop
// must still end at the revisited split.
func Test_OneOrMoreNullableBody(t *testing.T) {
	match(t, "", OneOrMore(Optional(Char('a'))), "*(?())")
	match(t, "aa", OneOrMore(Optional(Char('a'))), "*(?(a) ?(a))")
	match(t, "", OneOrMore(Many(Char('a'))), "*(*())")
	match(t, "b", Seq(OneOrMore(Many(Char('a'))), Char('b')), "(*(*()) b)")
	match(t, "", Rule("n", OneOrMore(Optional(Char('x')))), "n(*(?()))")
}

func Test_OneOrMoreRecursive(t *testing.T) {
	var list Compiler
	list = Rule("list", Char('['), OneOrMore(Choice(Char('x'), Lazy(&list))), Char(']'))
	match(t, "[x]", list, "list([ *(x) ])")
	match(t, "[x[x]x]", list, "list([ *(x list([ *(x) ]) x) ])")
	noMatch(t, "[]", list, ErrGrammarMismatch)
	noMatch(t, "[x[]]", list, ErrGrammarMismatch)
}

func Test_OneOrMoreDedup(t *testing.T) {
	_, v, err := runVM(t, strings.Repeat("a", 40), OneOrMore(Choice(Char('a'), Char('a'))))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.peak > 2 {
		t.Errorf("peak threads = %d, want <= 2", v.peak)
	}
}

// The body always runs once, so a call it reaches without reading a rune is
// reached without reading a rune by the enclosing rule too; nullability of the
// body carries through.
func Test_OneOrMoreLeftRecursion(t *testing.T) {
	var r1, r2, r3 Compiler
	r1 = Rule("r", OneOrMore(Lazy(&r1)), Char('a'))
	r2 = Rule("r", OneOrMore(Optional(Char('x'))), Lazy(&r2))
	noMatch(t, "a", r1, ErrLeftRecursion)
	noMatch(t, "a", r2, ErrLeftRecursion)

	r3 = Rule("r", OneOrMore(Char('x')), Optional(Lazy(&r3)))
	match(t, "x", r3, "r(*(x) ?())")
	match(t, "xx", r3, "r(*(x x) ?())")
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
	if s.Lexeme != "" {
		t.Errorf("seq lexeme = %q, want empty", s.Lexeme)
	}
	for i, c := range s.Children {
		if c.Children != nil {
			t.Errorf("leaf %d has children: %v", i, c.Children)
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
	a, b, c := &Node{Lexeme: "a"}, &Node{Lexeme: "b"}, &Node{Lexeme: "c"}
	m := &machine{}
	m.push(a)
	m.push(b)
	m.push(c)

	if got := m.pop(); got != c {
		t.Fatalf("pop = %v, want c", got)
	}
	got := m.popn(2)
	if !slices.Equal(got, []*Node{a, b}) {
		t.Fatalf("popn(2) = %v, want [a b] in push order", got)
	}
	if len(m.stack) != 0 {
		t.Fatalf("stack len = %d, want 0", len(m.stack))
	}
}

// popn's result becomes a sequence node's children; a later push must not
// overwrite it.
func Test_MachinePopnAliasing(t *testing.T) {
	a, b, c := &Node{Lexeme: "a"}, &Node{Lexeme: "b"}, &Node{Lexeme: "c"}
	m := &machine{stack: make([]*Node, 0, 4)}
	m.push(a)
	m.push(b)
	children := m.popn(2)
	m.push(c)

	if !slices.Equal(children, []*Node{a, b}) {
		t.Errorf("popped states overwritten by push: %v", children)
	}
}

func compiles(t *testing.T, c Compiler, want ...int32) {
	t.Helper()
	var p program
	c(&p)
	if !slices.Equal(p.code, want) {
		t.Errorf("program = %v, want %v", p.code, want)
	}
}

// Nesting depth of rule nodes along the last child.
func depth(n *Node) int {
	d := 0
	for n != nil && n.Kind == KindRule {
		d++
		n = n.Children[len(n.Children)-1]
	}
	return d
}

// Compares the fully linked program, including rule bodies after opEof.
func links(t *testing.T, c Compiler, want ...int32) *program {
	t.Helper()
	p, err := compile(c)
	if err != nil {
		t.Errorf("compile: %v", err)
		return nil
	}
	if !slices.Equal(p.code, want) {
		t.Errorf("program = %v, want %v", p.code, want)
	}
	return p
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
	if hasMark(got) {
		t.Errorf("%q: mark leaked into tree: %s", s, show(got))
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

// Exposes the VM so tests can inspect its instrumentation.
func runVM(t *testing.T, s string, c Compiler) (*Node, *vm, error) {
	t.Helper()
	p, err := compile(c)
	if err != nil {
		return nil, nil, err
	}
	v := newVM(p)
	n, err := v.run([]rune(s))
	return n, v, err
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

// Renders chars as their lexeme, text as a quoted lexeme, and interior nodes
// as a parenthesised list prefixed by the rule name, "?" for optional or "*"
// for many.
func show(s *Node) string {
	switch s.Kind {
	case KindChar:
		return s.Lexeme
	case KindText:
		return `"` + s.Lexeme + `"`
	}
	prefix := map[string]string{KindRule: s.Name, KindOptional: "?", KindMany: "*"}[s.Kind]
	parts := make([]string, len(s.Children))
	for i, c := range s.Children {
		parts[i] = show(c)
	}
	return prefix + "(" + strings.Join(parts, " ") + ")"
}

// Marks are internal to the VM and must be consumed before a tree is returned.
func hasMark(n *Node) bool {
	if n.Kind == nkMark {
		return true
	}
	return slices.ContainsFunc(n.Children, hasMark)
}
