package dfa

import (
	"fmt"
	"io"
	"slices"
	"testing"
	"unicode/utf8"
)

func Test_RuneInput(t *testing.T) {
	// only the runes in the slice are input, even when the array behind it is longer
	check(t, "ab of abc", Run(MatchString("ab"), []rune("abc")[:2]), nil)
	// nil is the empty input
	check(t, "nil", Run(Optional('a'), nil), nil)
	check(t, "nil", Run(MatchRune('a'), nil), ErrUnexpectedEOI)
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

func Test_NFAToDFADeterministic(t *testing.T) {
	grammars := map[string]Compiler{
		"(a|b)*ab":           Seq(ZeroOrMore(Alt('a', 'b')), 'a', 'b'),
		"common prefix":      Alt("abc", "abd", "ab"),
		"overlapping ranges": Seq(Alt(Range('a', 'm'), Range('k', 'z')), '!'),
		"any":                Seq(Any(), 'x'),
		"decimal":            decimal,
		"float":              float,
	}
	for name, g := range grammars {
		assertDeterministic(t, name, CompileDFA(g).states)
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

// GraphDFA outputs the DFA built from compiler in GraphViz 'dot' format.
// Accepting states are double circles; the start state is marked by an arrow from a point.
func GraphDFA(writer io.Writer, compiler Compiler) {
	states := CompileDFA(compiler).states

	fmt.Fprint(writer, graphHeader)
	fmt.Fprint(writer, "  start [shape=\"point\"];\n  start -> 0 [arrowsize=0.7];\n")
	for _, state := range states {
		shape := "circle"
		if state.accept {
			shape = "doublecircle"
		}
		fmt.Fprintf(writer, "  %d [shape=\"%s\"];\n", state.id, shape)
	}
	for _, state := range states {
		for _, t := range state.transitions {
			fmt.Fprintf(writer, "  %d -> %d [arrowsize=0.7, label=\"%s\"];\n", state.id, t.next.id, rangeLabel(t.runeRange))
		}
	}
	fmt.Fprint(writer, graphFooter)
}
