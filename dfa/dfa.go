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

// Matcher returns true to indicate that a transition is valid for the rune r
type Matcher func(r rune) bool

type NFAState struct {
	id           int         // id of state (used to debug and graph state machine)
	followStates []*NFAState // states that can match the rune following this one
	runeRange                // runes this state matches; embedded so low/high are promoted
}

type NFAStates = []*NFAState

type Compiler func(meta *Metadata, prevStates NFAStates) NFAStates

// Metadata captured during the compilation process. It is used to graph the state machines.
type Metadata struct {
	states NFAStates // list of all states that have been created (doubles as NFAState.id generator)
}

var ErrUnexpectedEOI = errors.New("unexpected end of input")
var ErrGrammarMismatch = errors.New("input does not match grammar")

func Match(runeLow rune, runeHigh rune) Compiler {
	return func(meta *Metadata, prevStates NFAStates) NFAStates {
		state := &NFAState{
			followStates: NFAStates{},
			id:           len(meta.states),
			runeRange:    runeRange{runeLow, runeHigh}}
		states := NFAStates{state}
		// connect previous states to this state
		connect(prevStates, states)
		// update metadata
		meta.states = append(meta.states, state)
		return states
	}
}

// MatchRune matches an input rune
func MatchRune(rn rune) Compiler {
	return Match(rn, rn)
}

func MatchString(text string) Compiler {
	runes := []rune(text)
	children := make([]any, len(runes))
	for i, r := range runes {
		children[i] = MatchRune(r)
	}
	return Seq(children...)
}

// Any matches any rune (always returns true)
func Any() Compiler {
	return Match(rune(0), utf8.MaxRune)
}

// Range returns true if an input rune is between low and high inclusive
func Range(low, high rune) Compiler {
	return Match(low, high)
}

func Seq(compilers ...any) Compiler {
	return func(meta *Metadata, prevStates NFAStates) NFAStates {
		for _, compiler := range compilers {
			prevStates = convertToCompiler(compiler)(meta, prevStates) // tail = prevStates for each iteration
		}
		return prevStates
	}
}

func Optional(compilers ...any) Compiler {
	return func(meta *Metadata, prevStates NFAStates) NFAStates {
		tail := Seq(compilers...)(meta, prevStates)
		return append(prevStates, tail...) // returning prevStates creates a bypass path
	}
}

func OneOrMore(compilers ...any) Compiler {
	return func(meta *Metadata, prevStates NFAStates) NFAStates {
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
	return func(meta *Metadata, prevStates NFAStates) NFAStates {
		tail := NFAStates{}
		for _, alt := range alternatives {
			altTail := convertToCompiler(alt)(meta, prevStates)
			tail = append(tail, altTail...)
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

// connect each source state to each destination state by adding the destination states
// to the source state.followStates slice. We ensure that there are no duplicate states.
func connect(sourceStates NFAStates, destStates NFAStates) {
	for _, source := range sourceStates {
		for _, dest := range destStates {
			// add dest to source transitions if not already added
			if !slices.Contains(source.followStates, dest) {
				source.followStates = append(source.followStates, dest)
			}
		}
	}
}

func CompileNFA(compiler Compiler) (start *NFAState, end *NFAState, meta *Metadata) {
	meta = &Metadata{states: NFAStates{}}
	// append a start and end state to the grammar.  This makes Run() simpler
	tail := Seq(Any(), compiler, rune(-1))(meta, NFAStates{})
	return meta.states[0], tail[0], meta //
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

// transition is a DFA edge taken on any rune in runeRange.
type transition struct {
	runeRange
	next *DFAState
}

// DFAState is a state of the DFA built by CompileDFA. Ranges live on the transitions rather than
// the states, so each DFA state corresponds to exactly one set of NFA states.
type DFAState struct {
	id          int
	transitions []transition // sorted by low and pairwise disjoint
	accept      bool         // input may end in this state
}

// CompileDFA compiles the grammar to an NFA and builds a DFA from it, rooted at the NFA's first
// state (subset construction). Each DFA state represents the set of NFA states that may match
// the next rune. The NFA terminal is folded into DFAState.accept, so the DFA has no -1
// transitions. Returns the start state; every other state is reachable from it.
func CompileDFA(compiler Compiler) *DFAState {
	_, nfaEnd, nfa := CompileNFA(compiler)
	dfaStates := []*DFAState{}
	sets := []NFAStates{} // sets[i] is the NFA states that may match the next rune in states[i]
	index := map[string]*DFAState{}
	add := func(followUnionStates NFAStates) *DFAState {
		key := followUnionKey(followUnionStates)
		if state, ok := index[key]; ok {
			return state
		}
		dfaState := &DFAState{id: len(dfaStates), accept: slices.Contains(followUnionStates, nfaEnd)}
		index[key] = dfaState
		dfaStates = append(dfaStates, dfaState)
		sets = append(sets, followUnionStates)
		return dfaState
	}
	add(followUnion(NFAStates{nfa.states[0]}))
	// states doubles as the work queue: states appended by add() are processed in turn
	for i := 0; i < len(dfaStates); i++ {
		next := slices.DeleteFunc(slices.Clone(sets[i]), func(s *NFAState) bool { return s == nfaEnd })
		for _, d := range disjointRanges(next) {
			addTransition(dfaStates[i], d.runeRange, add(followUnion(d.states)))
		}
	}
	return dfaStates[0]
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
func Run(compiler Compiler, input []rune) (err error) {
	return runDFA(CompileDFA(compiler), input)
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
