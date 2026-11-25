package dfa

import (
	"fmt"
	"io"
	"testing"
)

func Test_RuneInput(t *testing.T) {
	// only the runes in the slice are input, even when the array behind it is longer
	check(t, "ab of abc", Run(MatchString("ab"), []rune("abc")[:2]), nil)
	// nil is the empty input
	check(t, "nil", Run(Optional('a'), nil), nil)
	check(t, "nil", Run(MatchRune('a'), nil), ErrUnexpectedEOI)
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
