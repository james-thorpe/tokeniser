package dfa

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func Test_MatchNoInput(t *testing.T) {
	testE(t, "", MatchRune('a'), ErrUnexpectedEOI)
}

func Test_MatchRune(t *testing.T) {
	test(t, "a", MatchRune('a'))
	testE(t, "b", MatchRune('a'), nil)
}

func Test_Seq(t *testing.T) {
	test(t, "a", Seq('a'))
	testE(t, "b", Seq('a'), nil)
	testE(t, "", Seq('a'), ErrUnexpectedEOI)

	// many parameters
	test(t, "abc", Seq('a', 'b', 'c'))
	testE(t, "abd", Seq('a', 'b', 'c'), nil)
	testE(t, "ab", Seq('a', 'b', 'c'), ErrUnexpectedEOI)
	testE(t, "abcd", Seq('a', 'b', 'c'), nil)
}

func Test_String(t *testing.T) {
	test(t, "ab", MatchString("ab"))
	testE(t, "abc", MatchString("ab"), ErrGrammarMismatch)
	testE(t, "a", MatchString("ab"), ErrUnexpectedEOI)
}

func Test_StringMultiByte(t *testing.T) {
	test(t, "héllo", MatchString("héllo"))
	test(t, "日本", MatchString("日本"))
	testE(t, "日", MatchString("日本"), ErrUnexpectedEOI)
	testE(t, "日日", MatchString("日本"), ErrGrammarMismatch)
}

func Test_Range(t *testing.T) {
	test(t, "a", Range('a', 'c'))
	test(t, "b", Range('a', 'c'))
	test(t, "c", Range('a', 'c')) // bounds are inclusive
	testE(t, "`", Range('a', 'c'), ErrGrammarMismatch)
	testE(t, "d", Range('a', 'c'), ErrGrammarMismatch)
	test(t, "x", Range('x', 'x')) // single rune range
	testE(t, "", Range('a', 'c'), ErrUnexpectedEOI)
}

func Test_Any(t *testing.T) {
	test(t, "a", Any())
	test(t, "\x00", Any())
	test(t, string(utf8.MaxRune), Any())
	test(t, "日", Any())
	testE(t, "", Any(), ErrUnexpectedEOI)
	testE(t, "ab", Any(), ErrGrammarMismatch)
	test(t, "abc", Seq('a', Any(), 'c'))
}

func Test_Optional(t *testing.T) {
	test(t, "", Optional('a'))
	test(t, "a", Optional('a'))
	test(t, "abc", Optional("abc"))
	test(t, "ab", Seq(Optional('a'), 'b'))
	test(t, "b", Seq(Optional('a'), 'b'))

	testE(t, "aab", Seq(Optional('a'), 'b'), nil)
	testE(t, "c", Seq(Optional('a'), 'b'), nil)
	testE(t, "ac", Seq(Optional('a'), 'b'), nil)
	testE(t, "a", Seq(Optional('a'), 'b'), ErrUnexpectedEOI)
}

func Test_OneOrMore(t *testing.T) {
	testE(t, "", OneOrMore('a'), ErrUnexpectedEOI)
	test(t, "a", OneOrMore('a'))
	test(t, "aaa", OneOrMore('a'))
	test(t, "abc", OneOrMore("abc"))
	test(t, "abcabc", OneOrMore("abc"))
	test(t, "ab", Seq(OneOrMore('a'), 'b'))
	testE(t, "b", Seq(OneOrMore('a'), 'b'), nil)
	test(t, "aaab", Seq(OneOrMore('a'), 'b'))
	testE(t, "c", Seq(OneOrMore('a'), 'b'), nil)
	testE(t, "ac", Seq(OneOrMore('a'), 'b'), nil)
	testE(t, "a", Seq(OneOrMore('a'), 'b'), ErrUnexpectedEOI)
}

func Test_ZeroOrMore(t *testing.T) {
	test(t, "", ZeroOrMore('a'))
	test(t, "a", ZeroOrMore('a'))
	test(t, "aaa", ZeroOrMore('a'))
	test(t, "ab", Seq(ZeroOrMore('a'), 'b'))
	test(t, "b", Seq(ZeroOrMore('a'), 'b'))
	test(t, "aaab", Seq(ZeroOrMore('a'), 'b'))
	test(t, "abcabc", ZeroOrMore("abc"))
	testE(t, "abcabd", ZeroOrMore("abc"), nil)
	testE(t, "c", Seq(ZeroOrMore('a'), 'b'), nil)
	testE(t, "ac", Seq(ZeroOrMore('a'), 'b'), nil)
	testE(t, "a", Seq(ZeroOrMore('a'), 'b'), ErrUnexpectedEOI)
}

func Test_Alt(t *testing.T) {
	test(t, "abc", Alt("abc", "def"))
	test(t, "def", Alt("abc", "def"))
	test(t, "aaa", Alt("aaa", "aaaa"))           // subset test
	test(t, "aaaa", Alt("aaa", "aaaa"))          // superset test
	test(t, "abcg", Seq(Alt("abc", "def"), 'g')) // superset test
	test(t, "defg", Seq(Alt("abc", "def"), 'g')) // superset test
	testE(t, "", Alt("abc", "def"), nil)
	testE(t, "abd", Alt("abc", "def"), nil)
}

func Test_Number(t *testing.T) {
	/*
	   Decimal as defined in Go spec: https://go.dev/ref/spec#decimal_digit

	   decimal_digit     = "0" … "9"
	   decimal_digits    = decimal_digit { [ "_" ] decimal_digit }
	   decimal_lit       = "0" | ( "1" … "9" ) [ [ "_" ] decimal_digits ]
	*/
	decimal := Alt('0', Seq(Range('1', '9'), ZeroOrMore(Optional('_'), Range('0', '9'))))
	test(t, "0", decimal)
	testE(t, "00", decimal, ErrGrammarMismatch)
	test(t, "1", decimal)
	test(t, "123", decimal)
	test(t, "1_234", decimal)
	testE(t, "1__234", decimal, ErrGrammarMismatch) // multiple consecutive underscores not allowed
	testE(t, "1_", decimal, ErrUnexpectedEOI)       // underscore must be followed by number
	// printGraph(GraphNFA, decimal)
	// printGraph(GraphDFA, decimal)
}

func Test_Float(t *testing.T) {

	digits := Seq(Range('0', '9'), ZeroOrMore(Optional('_'), Range('0', '9')))
	integer := Alt('0', Seq(Range('1', '9'), Optional(Optional('_'), digits)))
	exponent := Seq(Alt('e', 'E'), Optional(Alt('+', '-')), digits)
	float := Seq(integer, Optional('.', digits), Optional(exponent))

	// integer with optional fraction and exponent
	test(t, "0", float)
	test(t, "1", float)
	test(t, "10", float)
	test(t, "0.5", float)
	test(t, "3.14159", float)
	test(t, "1.5e3", float)
	test(t, "6.022e23", float)
	test(t, "1E-7", float)
	test(t, "1e+10", float)
	test(t, "0e0", float)

	// numeric separators
	test(t, "1_000", float)
	test(t, "1_000.000_1", float)
	test(t, "0.000_001", float)
	test(t, "1e1_0", float)
	test(t, "1.0e007", float) // leading zeros are allowed in the fraction and exponent

	// incomplete input
	testE(t, "", float, ErrUnexpectedEOI)
	testE(t, "0.", float, ErrUnexpectedEOI) // "." must be followed by digits
	testE(t, "1.", float, ErrUnexpectedEOI)
	testE(t, "1e", float, ErrUnexpectedEOI)
	testE(t, "1e-", float, ErrUnexpectedEOI)
	testE(t, "1.5e", float, ErrUnexpectedEOI)
	testE(t, "1_", float, ErrUnexpectedEOI) // separator must be followed by a digit
	testE(t, "1.5_", float, ErrUnexpectedEOI)

	// an integer part is required
	testE(t, ".", float, ErrGrammarMismatch)
	testE(t, ".5", float, ErrGrammarMismatch)
	testE(t, ".5e-3", float, ErrGrammarMismatch)

	// "." must be followed by digits, not the exponent
	testE(t, "1.e5", float, ErrGrammarMismatch)
	testE(t, "1.E-7", float, ErrGrammarMismatch)

	// leading zeros: legacy octal-like literals are a syntax error in strict mode
	testE(t, "00", float, ErrGrammarMismatch)
	testE(t, "01", float, ErrGrammarMismatch)
	testE(t, "072.40", float, ErrGrammarMismatch)
	testE(t, "0_1", float, ErrGrammarMismatch) // separator is not allowed after a leading 0

	// separators must sit between two digits
	testE(t, "1__0", float, ErrGrammarMismatch)
	testE(t, "1_.5", float, ErrGrammarMismatch)
	testE(t, "1._5", float, ErrGrammarMismatch)
	testE(t, "1_e5", float, ErrGrammarMismatch)
	testE(t, "1e_5", float, ErrGrammarMismatch)

	// other invalid input
	testE(t, "1..5", float, ErrGrammarMismatch)
	testE(t, "1.5.5", float, ErrGrammarMismatch)
	testE(t, "1e+-5", float, ErrGrammarMismatch)
	testE(t, "1e3.5", float, ErrGrammarMismatch)
	testE(t, "e5", float, ErrGrammarMismatch)
	testE(t, "+1", float, ErrGrammarMismatch)   // sign is a unary operator, not part of the literal
	testE(t, "0x10", float, ErrGrammarMismatch) // hex is a separate NumericLiteral production
	testE(t, "1n", float, ErrGrammarMismatch)   // BigInt suffix is not a DecimalLiteral
	// printGraph(GraphDFA, float)
}

func Test_RuneInput(t *testing.T) {
	// only the runes in the slice are input, even when the array behind it is longer
	if err := Run(MatchString("ab"), []rune("abc")[:2]); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	// nil is the empty input
	if err := Run(Optional('a'), nil); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if err := Run(MatchRune('a'), nil); !errors.Is(err, ErrUnexpectedEOI) {
		t.Errorf("expected error: %v, got %v", ErrUnexpectedEOI, err)
	}
}

func test(t *testing.T, input string, exp Compiler) {
	t.Helper()
	if err := Run(exp, []rune(input)); err != nil {
		t.Errorf("%q: got %v, want no error", input, err)
	}
}

// testE expects Run to fail with expectedErr, or with any error when expectedErr is nil.
func testE(t *testing.T, input string, exp Compiler, expectedErr error) {
	t.Helper()
	err := Run(exp, []rune(input))
	if err == nil || (expectedErr != nil && !errors.Is(err, expectedErr)) {
		t.Errorf("%q: got %v, want %v", input, err, expectedErr)
	}
}

// rangeLabel renders a rune range as a dot label, escaping characters that would end the label.
func rangeLabel(r runeRange) string {
	escape := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace
	switch {
	case r.low == 0 && r.high == utf8.MaxRune:
		return "∀"
	case r.low == r.high:
		return escape(string(r.low))
	default:
		return escape(string(r.low)) + "-" + escape(string(r.high))
	}
}

// printGraph prints the dot output of graph for exp; used ad hoc while debugging a grammar.
func printGraph(graph func(io.Writer, Compiler), exp Compiler) {
	var builder strings.Builder
	graph(&builder, exp)
	println(builder.String())
}

const graphHeader = "digraph {\n  rankdir=LR;\n  node [fixedsize=true];\n"

// GraphDFA outputs the DFA built from compiler in GraphViz 'dot' format.
// Accepting states are double circles; the start state is marked by an arrow from a point.
func GraphDFA(writer io.Writer, compiler Compiler) {
	states := CompileDFA(compiler).states

	_, _ = fmt.Fprint(writer, graphHeader)
	_, _ = fmt.Fprintf(writer, "  start [shape=\"point\"];\n  start -> 0 [arrowsize=0.7];\n")
	for _, state := range states {
		shape := "circle"
		if state.accept {
			shape = "doublecircle"
		}
		_, _ = fmt.Fprintf(writer, "  %d [shape=\"%s\"];\n", state.id, shape)
	}
	for _, state := range states {
		for _, t := range state.transitions {
			_, _ = fmt.Fprintf(writer, "  %d -> %d [arrowsize=0.7, label=\"%s\"];\n", state.id, t.next.id, rangeLabel(t.runeRange))
		}
	}
	_, _ = fmt.Fprintf(writer, "}\n")
}

func Test_DisjointRanges(t *testing.T) {
	type rr = runeRange
	cases := []struct {
		name string
		in   []rr
		want []rr
	}{
		{"empty", nil, []rr{}},
		{"single", []rr{{'a', 'z'}}, []rr{{'a', 'z'}}},
		{"no overlap", []rr{{'x', 'z'}, {'a', 'c'}}, []rr{{'a', 'c'}, {'x', 'z'}}},
		{"adjacent kept separate", []rr{{'a', 'c'}, {'d', 'f'}}, []rr{{'a', 'c'}, {'d', 'f'}}},
		{"partial overlap", []rr{{'a', 'm'}, {'k', 'z'}}, []rr{{'a', 'j'}, {'k', 'm'}, {'n', 'z'}}},
		{"contained", []rr{{'a', 'z'}, {'m', 'p'}}, []rr{{'a', 'l'}, {'m', 'p'}, {'q', 'z'}}},
		{"identical", []rr{{'a', 'c'}, {'a', 'c'}}, []rr{{'a', 'c'}}},
		{"shared low", []rr{{'a', 'c'}, {'a', 'z'}}, []rr{{'a', 'c'}, {'d', 'z'}}},
		{"shared high", []rr{{'a', 'z'}, {'x', 'z'}}, []rr{{'a', 'w'}, {'x', 'z'}}},
		{"touching", []rr{{'a', 'm'}, {'m', 'z'}}, []rr{{'a', 'l'}, {'m', 'm'}, {'n', 'z'}}},
		{"gap omitted", []rr{{'a', 'c'}, {'b', 'd'}, {'x', 'z'}}, []rr{{'a', 'a'}, {'b', 'c'}, {'d', 'd'}, {'x', 'z'}}},
		{"three way", []rr{{'a', 'f'}, {'c', 'h'}, {'e', 'j'}},
			[]rr{{'a', 'b'}, {'c', 'd'}, {'e', 'f'}, {'g', 'h'}, {'i', 'j'}}},
		{"invalid range ignored", []rr{{'z', 'a'}, {'b', 'c'}}, []rr{{'b', 'c'}}},
		{"terminal", []rr{{-1, -1}, {0, utf8.MaxRune}}, []rr{{-1, -1}, {0, utf8.MaxRune}}},
	}
	for _, c := range cases {
		in := statesOf(c.in)
		got := disjointRanges(in)
		ranges := make([]rr, len(got))
		for i, d := range got {
			ranges[i] = d.runeRange
		}
		if !slices.Equal(ranges, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, ranges, c.want)
			continue
		}
		// each output must carry exactly the inputs that contain it, in any order
		for _, d := range got {
			want := slices.DeleteFunc(slices.Clone(in), func(s *NFAState) bool { return s.low > d.low || d.high > s.high })
			if len(d.states) != len(want) || slices.ContainsFunc(want, func(s *NFAState) bool { return !slices.Contains(d.states, s) }) {
				t.Errorf("%s %v: got %d covering states, want %d", c.name, d.runeRange, len(d.states), len(want))
			}
		}
	}
}

// statesOf wraps ranges as bare NFAStates for testing range-only helpers.
func statesOf(ranges []runeRange) NFAStates {
	states := make(NFAStates, len(ranges))
	for i, r := range ranges {
		states[i] = &NFAState{id: i, runeRange: r}
	}
	return states
}

func Test_NFAToDFA(t *testing.T) {
	type tc struct {
		input string
		want  error
	}
	grammars := []struct {
		name  string
		c     Compiler
		cases []tc
	}{
		{"(a|b)*ab", Seq(ZeroOrMore(Alt('a', 'b')), 'a', 'b'), []tc{
			{"", ErrUnexpectedEOI}, {"a", ErrUnexpectedEOI}, {"b", ErrUnexpectedEOI}, {"ab", nil}, {"aab", nil},
			{"abab", nil}, {"bbab", nil}, {"aba", ErrUnexpectedEOI}, {"abc", ErrGrammarMismatch}, {"abb", ErrUnexpectedEOI}}},
		{"common prefix", Alt("abc", "abd", "ab"), []tc{
			{"", ErrUnexpectedEOI}, {"a", ErrUnexpectedEOI}, {"ab", nil}, {"abc", nil}, {"abd", nil},
			{"abe", ErrGrammarMismatch}, {"abcd", ErrGrammarMismatch}}},
		{"overlapping ranges", Seq(Alt(Range('a', 'm'), Range('k', 'z')), '!'), []tc{
			{"a!", nil}, {"l!", nil}, {"z!", nil}, {"0!", ErrGrammarMismatch}, {"l", ErrUnexpectedEOI},
			{"ll!", ErrGrammarMismatch}}},
		{"any", Seq(Any(), 'x'), []tc{
			{"ax", nil}, {"日x", nil}, {"xx", nil}, {"x", ErrUnexpectedEOI}, {"a", ErrUnexpectedEOI}}},
		{"optional", Seq(Optional('a'), 'b'), []tc{
			{"", ErrUnexpectedEOI}, {"a", ErrUnexpectedEOI}, {"b", nil}, {"ab", nil}, {"aab", ErrGrammarMismatch}}},
		{"one or more", OneOrMore("abc"), []tc{
			{"", ErrUnexpectedEOI}, {"abc", nil}, {"abcabc", nil}, {"abcab", ErrUnexpectedEOI}, {"abd", ErrGrammarMismatch}}},
		{"decimal", Alt('0', Seq(Range('1', '9'), ZeroOrMore(Optional('_'), Range('0', '9')))), []tc{
			{"0", nil}, {"00", ErrGrammarMismatch}, {"1", nil}, {"123", nil}, {"1_234", nil},
			{"1__234", ErrGrammarMismatch}, {"1_", ErrUnexpectedEOI}, {"", ErrUnexpectedEOI}}},
	}
	for _, g := range grammars {
		dfa := CompileDFA(g.c)
		assertDeterministic(t, g.name, dfa.states)
		// one DFA serves every input since runDFA does not mutate it
		for _, c := range g.cases {
			if got := runDFA(dfa.states[0], []rune(c.input)); got != c.want {
				t.Errorf("%s %q: got %v, want %v", g.name, c.input, got, c.want)
			}
		}
	}
}

func Test_NFAToDFAShape(t *testing.T) {
	// (a|b)*ab: A = start, B = seen a, C = seen ab (accepting)
	states := CompileDFA(Seq(ZeroOrMore(Alt('a', 'b')), 'a', 'b')).states
	if len(states) != 3 {
		t.Fatalf("(a|b)*ab: got %d states, want 3", len(states))
	}
	a := states[0]
	b := step(a, 'a')
	c := step(b, 'b')
	want := []struct {
		from, onA, onB *DFAState
		accept         bool
	}{
		{a, b, a, false},
		{b, b, c, false},
		{c, b, a, true},
	}
	for _, w := range want {
		if step(w.from, 'a') != w.onA || step(w.from, 'b') != w.onB || w.from.accept != w.accept {
			t.Errorf("(a|b)*ab: state %d has wrong transitions or accept", w.from.id)
		}
	}

	// contiguous ranges to the same state merge into a single transition
	got := CompileDFA(Alt('a', 'b', 'c')).states[0].transitions
	if len(got) != 1 || got[0].runeRange != (runeRange{'a', 'c'}) {
		t.Errorf("Alt('a','b','c'): got transitions %v, want single a-c", got)
	}
}

// assertDeterministic checks that every DFA state's transitions are sorted and share no rune.
func assertDeterministic(t *testing.T, name string, states []*DFAState) {
	for _, state := range states {
		for i := 1; i < len(state.transitions); i++ {
			prev, cur := state.transitions[i-1], state.transitions[i]
			if cur.low <= prev.high {
				t.Errorf("%s: state %d has overlapping or unsorted transitions %c-%c and %c-%c",
					name, state.id, prev.low, prev.high, cur.low, cur.high)
			}
		}
	}
}

// step returns the state reached from s on rune r, or nil if there is no transition.
func step(s *DFAState, r rune) *DFAState {
	for _, t := range s.transitions {
		if r >= t.low && r <= t.high {
			return t.next
		}
	}
	return nil
}

// GraphNFA outputs the NFA built from compiler in GraphViz 'dot' format.
// The root and terminal states are double circles; edges are labelled with the target's range.
func GraphNFA(writer io.Writer, compiler Compiler) {
	start, end, meta := CompileNFA(compiler)

	_, _ = fmt.Fprint(writer, graphHeader)
	for _, state := range meta.states {
		shape := "circle"
		if state == start || state == end {
			shape = "doublecircle"
		}
		_, _ = fmt.Fprintf(writer, "  %d [shape=\"%s\"];\n", state.id, shape)
	}
	for _, state := range meta.states {
		for _, next := range state.followStates {
			label := rangeLabel(next.runeRange)
			if next == end {
				label = "δ"
			}
			_, _ = fmt.Fprintf(writer, "  %d -> %d [arrowsize=0.7, label=\"%s\"];\n", state.id, next.id, label)
		}
	}
	_, _ = fmt.Fprintf(writer, "}\n")
}
