package dfa

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
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
	//	graph(decimal)
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
	err := Run(exp, []rune(input))
	if err != nil {
		t.Errorf("expected no error, got %v", err)
		t.Log(string(debug.Stack()))
	}
}

func testE(t *testing.T, input string, exp Compiler, expectedErr error) {
	err := Run(exp, []rune(input))
	if err == nil {
		t.Errorf("expected error, got nil")
		t.Log(string(debug.Stack()))
	}
	if expectedErr != nil && !errors.Is(err, expectedErr) {
		t.Errorf("expected error: %v", expectedErr)
		t.Log(string(debug.Stack()))
	}
}

func test_DFA(t *testing.T) {

}

// label renders a state's rune range for graph transitions
func label(s *State) string {
	switch {
	case s.low == 0 && s.high == utf8.MaxRune:
		return "∀"
	case s.low == s.high:
		return string(s.low)
	default:
		return string(s.low) + "-" + string(s.high)
	}
}

func graph(exp Compiler) {
	var builder strings.Builder
	Graph(&builder, exp)
	println(builder.String())
}

// Graph output state machine in GraphViz 'dot' format
func Graph(writer io.Writer, compiler Compiler) {
	start, end, meta := compile(compiler)

	_, _ = fmt.Fprintf(writer, "digraph {\n  rankdir=LR;\n  node [fixedsize=true];\n")
	// render the nodes
	for _, state := range meta.states {
		if state == start || state == end {
			_, _ = fmt.Fprintf(writer, "  %d [shape=\"doublecircle\"];\n", state.id)
		} else {
			_, _ = fmt.Fprintf(writer, "  %d [shape=\"circle\"];\n", state.id)
		}
	}
	// render the transitions
	for _, state := range meta.states {
		for _, transition := range state.nextStates {
			if transition == end {
				_, _ = fmt.Fprintf(writer, "  %d -> %d [arrowsize=0.7, label=\"δ\"];\n", state.id, transition.id)
			} else {
				_, _ = fmt.Fprintf(writer, "  %d -> %d [arrowsize=0.7, label=\"%s\"];\n", state.id, transition.id, label(transition))
			}

		}
	}
	_, _ = fmt.Fprintf(writer, "}\n")
}

func Test_Intersection(t *testing.T) {
	st := func(lo, hi rune) *State { return &State{low: lo, high: hi} }
	cases := []struct {
		a, b      *State
		low, high rune
	}{
		{st('a', 'z'), st('m', 'p'), 'm', 'p'}, // contained
		{st('a', 'm'), st('k', 'z'), 'k', 'm'}, // partial overlap
		{st('k', 'z'), st('a', 'm'), 'k', 'm'}, // order independent
		{st('a', 'm'), st('m', 'z'), 'm', 'm'}, // touching bounds are inclusive
		{st('a', 'c'), st('d', 'f'), -1, -1},   // adjacent but disjoint
		{st('x', 'z'), st('a', 'c'), -1, -1},   // disjoint
		{st('a', 'a'), st('a', 'a'), 'a', 'a'}, // identical single rune
	}
	for _, c := range cases {
		low, high := intersection(c.a, c.b)
		if low != c.low || high != c.high {
			t.Errorf("intersection(%c-%c, %c-%c) = %d,%d; want %d,%d",
				c.a.low, c.a.high, c.b.low, c.b.high, low, high, c.low, c.high)
		}
	}
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
		if got := disjointRanges(c.in); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
