package dfa

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"unicode/utf8"
)

// runeRange is an inclusive range of runes low...high
type runeRange struct {
	low, high rune
}

type NFAState struct {
	id           int         // index in NFAMetadata.states; orders and keys NFA state sets in CompileDFA
	followStates []*NFAState // states that can match the rune following this one
	runeRange                // runes this state matches; embedded so low/high are promoted
}

type NFAStates = []*NFAState

type Compiler func(meta *NFAMetadata, prevStates NFAStates) NFAStates

// NFAMetadata captured while compiling the grammar to an NFA. CompileDFA reads it to build the DFA,
// with states[0] as the root.
type NFAMetadata struct {
	states NFAStates // list of all states that have been created (doubles as NFAState.id generator)
}

var ErrUnexpectedEOI = errors.New("unexpected end of input")
var ErrGrammarMismatch = errors.New("input does not match grammar")

// Range matches an input rune between low and high inclusive
func Range(low, high rune) Compiler {
	return func(meta *NFAMetadata, prevStates NFAStates) NFAStates {
		state := &NFAState{id: len(meta.states), runeRange: runeRange{low, high}}
		meta.states = append(meta.states, state)
		connect(prevStates, NFAStates{state})
		return NFAStates{state}
	}
}

// MatchRune matches an input rune
func MatchRune(rn rune) Compiler {
	return Range(rn, rn)
}

func MatchString(text string) Compiler {
	runes := []rune(text)
	children := make([]any, len(runes))
	for i, r := range runes {
		children[i] = MatchRune(r)
	}
	return Seq(children...)
}

// Any matches any input rune
func Any() Compiler {
	return Range(0, utf8.MaxRune)
}

func Seq(compilers ...any) Compiler {
	return func(meta *NFAMetadata, prevStates NFAStates) NFAStates {
		for _, compiler := range compilers {
			prevStates = convertToCompiler(compiler)(meta, prevStates) // tail = prevStates for each iteration
		}
		return prevStates
	}
}

func Optional(compilers ...any) Compiler {
	return func(meta *NFAMetadata, prevStates NFAStates) NFAStates {
		tail := Seq(compilers...)(meta, prevStates)
		return append(prevStates, tail...) // returning prevStates creates a bypass path
	}
}

func OneOrMore(compilers ...any) Compiler {
	return func(meta *NFAMetadata, prevStates NFAStates) NFAStates {
		firstHeadIx := len(prevStates[0].followStates) // identify no. of pre-existing transitions
		tail := Seq(compilers...)(meta, prevStates)
		head := prevStates[0].followStates[firstHeadIx:] // get new transitions as the head of this fragment
		connect(tail, head)                              // loop back to the head
		return tail
	}
}

func ZeroOrMore(compilers ...any) Compiler {
	return Optional(OneOrMore(compilers...))
}

func Alt(alternatives ...any) Compiler {
	return func(meta *NFAMetadata, prevStates NFAStates) NFAStates {
		tail := NFAStates{}
		for _, alt := range alternatives {
			tail = append(tail, convertToCompiler(alt)(meta, prevStates)...)
		}
		return tail
	}
}

func convertToCompiler(obj any) Compiler {
	switch v := obj.(type) {
	case Compiler:
		return v
	case string:
		return MatchString(v)
	case rune:
		return MatchRune(v)
	default:
		panic("unable to convert to compiler") // coding error not runtime error
	}
}

// connect adds each destination state to the followStates of each source state, without duplicates.
func connect(sourceStates NFAStates, destStates NFAStates) {
	for _, source := range sourceStates {
		for _, dest := range destStates {
			if !slices.Contains(source.followStates, dest) {
				source.followStates = append(source.followStates, dest)
			}
		}
	}
}

func CompileNFA(compiler Compiler) (start *NFAState, end *NFAState, meta *NFAMetadata) {
	meta = &NFAMetadata{states: NFAStates{}}
	// wrap the grammar in a root and a terminal state so the DFA has a single start and accept test
	tail := Seq(Any(), compiler, rune(-1))(meta, NFAStates{})
	return meta.states[0], tail[0], meta
}

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
		if s.low > s.high {
			continue
		}
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

// CompileDFA compiles the grammar to an NFA and builds a DFA from it, rooted at the NFA's first
// state (subset construction). Each DFA state represents the set of NFA states that may match
// the next rune. The NFA terminal is folded into DFAState.accept, so the DFA has no -1
// transitions. Returns all DFA states; states[0] is the start state.
func CompileDFA(compiler Compiler) *DFAMetadata {
	_, nfaEnd, nfa := CompileNFA(compiler)
	dfaStates := []*DFAState{}
	index := map[string]*DFAState{}
	add := func(set NFAStates) *DFAState {
		// keyed on the full set so the terminal distinguishes accepting states
		key := followUnionKey(set)
		if state, ok := index[key]; ok {
			return state
		}
		state := &DFAState{
			id:        len(dfaStates),
			nfaStates: slices.DeleteFunc(slices.Clone(set), func(s *NFAState) bool { return s == nfaEnd }),
			accept:    slices.Contains(set, nfaEnd),
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
// produce equal followUnionKey keys)
func followUnion(states NFAStates) NFAStates {
	next := NFAStates{}
	for _, state := range states {
		next = append(next, state.followStates...)
	}
	slices.SortFunc(next, func(a, b *NFAState) int { return a.id - b.id })
	return slices.Compact(next)
}

// followUnionKey creates a unique key for the states.
func followUnionKey(states NFAStates) string {
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
			if r >= t.low && r <= t.high {
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
