package dfa

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

var (
	ErrUnexpectedEOI   = errors.New("unexpected end of input")
	ErrGrammarMismatch = errors.New("input does not match grammar")
)

// disjointRange is an output range of disjointRanges and the input states whose range covers it.
type disjointRange struct {
	runeRange
	states NFAStates
}

// disjointRanges splits the ranges of states at every boundary so that no two output ranges
// overlap and each output range lies entirely inside or entirely outside every input range.
// Each output carries the states covering it. Gaps not covered by any state are omitted and
// output is sorted by low.
// eg [a-m] [k-z] -> [a-j]{1} [k-m]{1,2} [n-z]{2}
func disjointRanges(states NFAStates) []disjointRange {
	// sweep line: a state becomes active at low and inactive one past high
	opens := map[rune]NFAStates{}
	closes := map[rune]NFAStates{}
	for _, s := range states {
		opens[s.low] = append(opens[s.low], s)
		closes[s.high+1] = append(closes[s.high+1], s)
	}
	// boundaries where one state closes as another opens are kept so adjacent inputs
	// (eg [a-c] [d-f]) stay separate
	bounds := slices.Concat(slices.Collect(maps.Keys(opens)), slices.Collect(maps.Keys(closes)))
	slices.Sort(bounds)
	bounds = slices.Compact(bounds)
	result := []disjointRange{}
	active := NFAStates{}
	for i := 0; i < len(bounds)-1; i++ {
		b := bounds[i]
		active = slices.DeleteFunc(active, func(s *NFAState) bool { return slices.Contains(closes[b], s) })
		active = append(active, opens[b]...)
		if len(active) > 0 {
			// cloned since active is mutated by later boundaries
			result = append(result, disjointRange{runeRange{b, bounds[i+1] - 1}, slices.Clone(active)})
		}
	}
	return result
}

// DFAState is a state of the DFA built by CompileDFA. Ranges live on the transitions rather than
// the states, so each DFA state corresponds to exactly one set of NFA states.
type DFAState struct {
	id          int
	nfaStates   NFAStates    // NFA states that may match the next rune, excluding the terminal
	transitions []transition // sorted by low and pairwise disjoint
	accept      bool         // input may end in this state
	kind        int          // token kind accepted when accept is true
}

// transition is a DFA edge taken on any rune in runeRange.
type transition struct {
	runeRange
	next *DFAState
}

// DFAMetadata captured while building the DFA. It is used to graph and inspect the state machine.
type DFAMetadata struct {
	states []*DFAState // all states, indexed by DFAState.id; states[0] is the start state
}

// CompileDFA compiles the whole grammar as a single token and builds a DFA from it.
// Returns all DFA states; states[0] is the start state.
func CompileDFA(compiler Compiler) *DFAMetadata {
	return buildDFA(CompileNFA(Token(0, compiler)))
}

// buildDFA builds a DFA from nfa, rooted at nfa.states[0] (subset construction). Each DFA state
// represents the set of NFA states that may match the next rune. Token terminals are folded into
// DFAState.accept and kind, so the DFA has no transitions for them.
func buildDFA(nfa *NFAMetadata) *DFAMetadata {
	dfaStates := []*DFAState{}
	index := map[string]*DFAState{}
	isTerminal := func(s *NFAState) bool { return s.terminal }
	add := func(set NFAStates) *DFAState {
		// keyed on the full set so terminals distinguish accepting states
		key := setKey(set)
		if state, ok := index[key]; ok {
			return state
		}
		state := &DFAState{id: len(dfaStates), nfaStates: slices.DeleteFunc(slices.Clone(set), isTerminal)}
		// set is sorted by id and earlier tokens get lower ids, so the first terminal has priority
		if i := slices.IndexFunc(set, isTerminal); i >= 0 {
			state.accept, state.kind = true, set[i].kind
		}
		index[key] = state
		dfaStates = append(dfaStates, state)
		return state
	}
	add(followUnion(NFAStates{nfa.states[0]}))
	// dfaStates doubles as the work queue: states appended by add() are processed in turn
	for i := 0; i < len(dfaStates); i++ {
		for _, d := range disjointRanges(dfaStates[i].nfaStates) {
			addTransition(dfaStates[i], d.runeRange, add(followUnion(d.states)))
		}
	}
	return &DFAMetadata{states: dfaStates}
}

// addTransition appends a transition, merging it into the previous one when both lead to the
// same state over contiguous ranges. Ranges must be added in ascending order.
func addTransition(state *DFAState, r runeRange, next *DFAState) {
	last := len(state.transitions) - 1
	if last >= 0 && state.transitions[last].next == next && state.transitions[last].high+1 == r.low {
		state.transitions[last].high = r.high
		return
	}
	state.transitions = append(state.transitions, transition{r, next})
}

// followUnion returns the union of followStates for the input state (sorted by id so equal sets
// produce equal setKey keys)
func followUnion(states NFAStates) NFAStates {
	next := NFAStates{}
	for _, state := range states {
		next = append(next, state.followStates...)
	}
	slices.SortFunc(next, func(a, b *NFAState) int { return a.id - b.id })
	return slices.Compact(next)
}

// setKey creates a unique key for a set of states sorted by id.
func setKey(states NFAStates) string {
	ids := make([]int, len(states))
	for i, state := range states {
		ids[i] = state.id
	}
	return fmt.Sprint(ids)
}

// Run compiles the grammar to an NFA, converts it to a DFA and matches input against the DFA.
func Run(compiler Compiler, input []rune) error {
	return runDFA(CompileDFA(compiler).states[0], input)
}

// runDFA executes a DFA from CompileDFA. Only one state is live at a time and the graph is not
// mutated, so a DFA can be reused across runs.
func runDFA(start *DFAState, input []rune) error {
	state := start
	for _, r := range input {
		var next *DFAState
		// transitions are disjoint so the first match is the only match
		for _, t := range state.transitions {
			if t.contains(r) {
				next = t.next
				break
			}
		}
		if next == nil {
			return ErrGrammarMismatch
		}
		state = next
	}
	if !state.accept {
		return ErrUnexpectedEOI
	}
	return nil
}
