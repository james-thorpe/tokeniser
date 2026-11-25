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

/*
Decimal as defined in Go spec: https://go.dev/ref/spec#decimal_digit

	decimal_digit     = "0" … "9"
	decimal_digits    = decimal_digit { [ "_" ] decimal_digit }
	decimal_lit       = "0" | ( "1" … "9" ) [ [ "_" ] decimal_digits ]
*/
var decimal = Alt('0', Seq(Range('1', '9'), ZeroOrMore(Optional('_'), Range('0', '9'))))

var (
	digits   = Seq(Range('0', '9'), ZeroOrMore(Optional('_'), Range('0', '9')))
	integer  = Alt('0', Seq(Range('1', '9'), Optional(Optional('_'), digits)))
	exponent = Seq(Alt('e', 'E'), Optional(Alt('+', '-')), digits)
	float    = Seq(integer, Optional('.', digits), Optional(exponent))
)

func Test_MatchRune(t *testing.T) {
	test(t, MatchRune('a'), "a")
	testE(t, MatchRune('a'), ErrGrammarMismatch, "b")
	testE(t, MatchRune('a'), ErrUnexpectedEOI, "")
}

func Test_Seq(t *testing.T) {
	test(t, Seq('a'), "a")
	testE(t, Seq('a'), ErrGrammarMismatch, "b")
	testE(t, Seq('a'), ErrUnexpectedEOI, "")

	// many parameters
	test(t, Seq('a', 'b', 'c'), "abc")
	testE(t, Seq('a', 'b', 'c'), ErrGrammarMismatch, "abd", "abcd")
	testE(t, Seq('a', 'b', 'c'), ErrUnexpectedEOI, "ab")
}

func Test_String(t *testing.T) {
	test(t, MatchString("ab"), "ab")
	testE(t, MatchString("ab"), ErrGrammarMismatch, "abc")
	testE(t, MatchString("ab"), ErrUnexpectedEOI, "a")

	// multi-byte runes
	test(t, MatchString("héllo"), "héllo")
	test(t, MatchString("日本"), "日本")
	testE(t, MatchString("日本"), ErrUnexpectedEOI, "日")
	testE(t, MatchString("日本"), ErrGrammarMismatch, "日日")
}

func Test_Range(t *testing.T) {
	test(t, Range('a', 'c'), "a", "b", "c") // bounds are inclusive
	testE(t, Range('a', 'c'), ErrGrammarMismatch, "`", "d")
	testE(t, Range('a', 'c'), ErrUnexpectedEOI, "")
	test(t, Range('x', 'x'), "x") // single rune range

	defer func() {
		if recover() == nil {
			t.Errorf("Range('z', 'a'): expected panic for low > high")
		}
	}()
	Range('z', 'a')
}

func Test_Any(t *testing.T) {
	test(t, Any(), "a", "\x00", string(utf8.MaxRune), "日")
	testE(t, Any(), ErrUnexpectedEOI, "")
	testE(t, Any(), ErrGrammarMismatch, "ab")
	test(t, Seq('a', Any(), 'c'), "abc")
	test(t, Seq(Any(), 'x'), "ax", "日x", "xx")
	testE(t, Seq(Any(), 'x'), ErrUnexpectedEOI, "x", "a")
}

func Test_Optional(t *testing.T) {
	test(t, Optional('a'), "", "a")
	test(t, Optional("abc"), "abc")
	test(t, Seq(Optional('a'), 'b'), "ab", "b")
	testE(t, Seq(Optional('a'), 'b'), ErrGrammarMismatch, "aab", "c", "ac")
	testE(t, Seq(Optional('a'), 'b'), ErrUnexpectedEOI, "", "a")
}

func Test_OneOrMore(t *testing.T) {
	testE(t, OneOrMore('a'), ErrUnexpectedEOI, "")
	test(t, OneOrMore('a'), "a", "aaa")
	test(t, OneOrMore("abc"), "abc", "abcabc")
	testE(t, OneOrMore("abc"), ErrUnexpectedEOI, "abcab")
	testE(t, OneOrMore("abc"), ErrGrammarMismatch, "abd")
	test(t, Seq(OneOrMore('a'), 'b'), "ab", "aaab")
	testE(t, Seq(OneOrMore('a'), 'b'), ErrGrammarMismatch, "b", "c", "ac")
	testE(t, Seq(OneOrMore('a'), 'b'), ErrUnexpectedEOI, "a")
}

func Test_ZeroOrMore(t *testing.T) {
	test(t, ZeroOrMore('a'), "", "a", "aaa")
	test(t, Seq(ZeroOrMore('a'), 'b'), "ab", "b", "aaab")
	test(t, ZeroOrMore("abc"), "abcabc")
	testE(t, ZeroOrMore("abc"), ErrGrammarMismatch, "abcabd")
	testE(t, Seq(ZeroOrMore('a'), 'b'), ErrGrammarMismatch, "c", "ac")
	testE(t, Seq(ZeroOrMore('a'), 'b'), ErrUnexpectedEOI, "a")

	// (a|b)*ab
	ab := Seq(ZeroOrMore(Alt('a', 'b')), 'a', 'b')
	test(t, ab, "ab", "aab", "abab", "bbab")
	testE(t, ab, ErrUnexpectedEOI, "", "a", "b", "aba", "abb")
	testE(t, ab, ErrGrammarMismatch, "abc")
}

func Test_Alt(t *testing.T) {
	test(t, Alt("abc", "def"), "abc", "def")
	test(t, Alt("aaa", "aaaa"), "aaa", "aaaa") // one alternative is a prefix of the other
	test(t, Seq(Alt("abc", "def"), 'g'), "abcg", "defg")
	testE(t, Alt("abc", "def"), ErrUnexpectedEOI, "")
	testE(t, Alt("abc", "def"), ErrGrammarMismatch, "abd")

	// common prefix
	prefix := Alt("abc", "abd", "ab")
	test(t, prefix, "ab", "abc", "abd")
	testE(t, prefix, ErrUnexpectedEOI, "", "a")
	testE(t, prefix, ErrGrammarMismatch, "abe", "abcd")

	// overlapping ranges
	overlap := Seq(Alt(Range('a', 'm'), Range('k', 'z')), '!')
	test(t, overlap, "a!", "l!", "z!")
	testE(t, overlap, ErrGrammarMismatch, "0!", "ll!")
	testE(t, overlap, ErrUnexpectedEOI, "l")
}

func Test_Number(t *testing.T) {
	test(t, decimal, "0", "1", "123", "1_234")
	testE(t, decimal, ErrGrammarMismatch, "00", "1__234") // multiple consecutive underscores not allowed
	testE(t, decimal, ErrUnexpectedEOI, "", "1_")         // underscore must be followed by number
	// printGraph(GraphNFA, decimal)
	// printGraph(GraphDFA, decimal)
}

func Test_Float(t *testing.T) {
	// integer with optional fraction and exponent
	test(t, float, "0", "1", "10", "0.5", "3.14159", "1.5e3", "6.022e23", "1E-7", "1e+10", "0e0")

	// numeric separators; leading zeros are allowed in the fraction and exponent
	test(t, float, "1_000", "1_000.000_1", "0.000_001", "1e1_0", "1.0e007")

	// incomplete input: "." and separators must be followed by digits
	testE(t, float, ErrUnexpectedEOI, "", "0.", "1.", "1e", "1e-", "1.5e", "1_", "1.5_")

	// an integer part is required
	testE(t, float, ErrGrammarMismatch, ".", ".5", ".5e-3")

	// "." must be followed by digits, not the exponent
	testE(t, float, ErrGrammarMismatch, "1.e5", "1.E-7")

	// leading zeros: legacy octal-like literals and a separator after a leading 0
	testE(t, float, ErrGrammarMismatch, "00", "01", "072.40", "0_1")

	// separators must sit between two digits
	testE(t, float, ErrGrammarMismatch, "1__0", "1_.5", "1._5", "1_e5", "1e_5")

	// other invalid input; a sign, hex prefix or BigInt suffix is not part of a decimal literal
	testE(t, float, ErrGrammarMismatch, "1..5", "1.5.5", "1e+-5", "1e3.5", "e5", "+1", "0x10", "1n")
	// printGraph(GraphDFA, float)
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

func Test_Classes(t *testing.T) {
	meta := CompileNFA(Token(0, Alt(Range('a', 'm'), Range('k', 'z'))))
	want := []runeRange{{'a', 'j'}, {'k', 'm'}, {'n', 'z'}}
	if !slices.Equal(meta.classes, want) {
		t.Fatalf("got classes %v, want %v", meta.classes, want)
	}
	// states[0] is the root; states[1] and [2] are the ranges
	for _, w := range []struct{ id, first, last int }{{1, 0, 1}, {2, 1, 2}} {
		s := meta.states[w.id]
		if s.firstClass != w.first || s.lastClass != w.last {
			t.Errorf("state %d: got classes %d-%d, want %d-%d", w.id, s.firstClass, s.lastClass, w.first, w.last)
		}
	}
}

// GraphNFA outputs the NFA built from compiler in GraphViz 'dot' format.
// The root and terminal states are double circles; edges are labelled with the target's range.
func GraphNFA(writer io.Writer, compiler Compiler) {
	meta := CompileNFA(Token(0, compiler))

	fmt.Fprint(writer, graphHeader)
	for _, state := range meta.states {
		shape := "circle"
		if state == meta.states[0] || state.terminal {
			shape = "doublecircle"
		}
		fmt.Fprintf(writer, "  %d [shape=\"%s\"];\n", state.id, shape)
	}
	for _, state := range meta.states {
		for _, next := range state.followStates {
			label := rangeLabel(next.runeRange)
			if next.terminal {
				label = "δ"
			}
			fmt.Fprintf(writer, "  %d -> %d [arrowsize=0.7, label=\"%s\"];\n", state.id, next.id, label)
		}
	}
	fmt.Fprint(writer, graphFooter)
}

func test(t *testing.T, exp Compiler, inputs ...string) {
	t.Helper()
	testE(t, exp, nil, inputs...)
}

// testE expects Run to return want for every input; nil expects a match.
func testE(t *testing.T, exp Compiler, want error, inputs ...string) {
	t.Helper()
	for _, input := range inputs {
		check(t, fmt.Sprintf("%q", input), Run(exp, []rune(input)), want)
	}
}

func check(t *testing.T, input string, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Errorf("%s: got %v, want %v", input, got, want)
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

const (
	graphHeader = "digraph {\n  rankdir=LR;\n  node [fixedsize=true];\n"
	graphFooter = "}\n"
)
