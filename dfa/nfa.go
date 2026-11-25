package dfa

import (
	"cmp"
	"maps"
	"slices"
	"unicode/utf8"
)

// runeRange is an inclusive range of runes low...high
type runeRange struct {
	low, high rune
}

func (r runeRange) contains(c rune) bool {
	return c >= r.low && c <= r.high
}

// NFAState is a position in the NFA: it matches one rune range and lists the states that may
// match the following rune.
type NFAState struct {
	id           int       // index in NFAMetadata.states; gives states a stable order
	followStates NFAStates // states that can match the rune following this one
	runeRange              // runes this state matches; embedded so low/high are promoted
	terminal     bool      // end of a Token; matches no rune
	kind         int       // token kind when terminal
	// runeRange is exactly NFAMetadata.classes[firstClass..lastClass]; unset for the root and terminals
	firstClass, lastClass int
}

// NFAStates is a set of NFA states.
type NFAStates []*NFAState

// Compiler adds a grammar fragment to the NFA in meta, connecting it after prevStates, and returns
// the states that end the fragment.
type Compiler func(meta *NFAMetadata, prevStates NFAStates) NFAStates

// NFAMetadata captured while compiling the grammar to an NFA, with states[0] as the root.
type NFAMetadata struct {
	states  NFAStates   // list of all states that have been created (doubles as NFAState.id generator)
	classes []runeRange // character classes: disjoint, sorted by low; runes in a class are interchangeable
}

// Range matches an input rune between low and high inclusive
func Range(low, high rune) Compiler {
	if low > high {
		panic("range low is greater than high") // coding error not runtime error
	}
	return func(meta *NFAMetadata, prevStates NFAStates) NFAStates {
		return NFAStates{addState(meta, prevStates, runeRange{low, high})}
	}
}

// Token marks the end of the sequence compilers as a token of kind with a terminal state that
// matches no rune. When several tokens match the same input, the one compiled first wins.
func Token(kind int, compilers ...any) Compiler {
	return func(meta *NFAMetadata, prevStates NFAStates) NFAStates {
		tail := Seq(compilers...)(meta, prevStates)
		end := addState(meta, tail, runeRange{-1, -1})
		end.terminal = true
		end.kind = kind
		return NFAStates{end}
	}
}

// addState creates a state in meta matching r and connects prevStates to it.
func addState(meta *NFAMetadata, prevStates NFAStates, r runeRange) *NFAState {
	state := &NFAState{id: len(meta.states), runeRange: r}
	meta.states = append(meta.states, state)
	connect(prevStates, NFAStates{state})
	return state
}

// MatchRune matches an input rune
func MatchRune(rn rune) Compiler {
	return Range(rn, rn)
}

// MatchString matches the runes of text in order
func MatchString(text string) Compiler {
	return func(meta *NFAMetadata, prevStates NFAStates) NFAStates {
		for _, r := range text {
			prevStates = MatchRune(r)(meta, prevStates)
		}
		return prevStates
	}
}

// Any matches any input rune
func Any() Compiler {
	return Range(0, utf8.MaxRune)
}

// Seq matches each of compilers in order
func Seq(compilers ...any) Compiler {
	return func(meta *NFAMetadata, prevStates NFAStates) NFAStates {
		for _, compiler := range compilers {
			prevStates = convertToCompiler(compiler)(meta, prevStates)
		}
		return prevStates
	}
}

// Optional matches the sequence compilers zero or one times
func Optional(compilers ...any) Compiler {
	return func(meta *NFAMetadata, prevStates NFAStates) NFAStates {
		tail := Seq(compilers...)(meta, prevStates)
		return append(prevStates, tail...) // returning prevStates creates a bypass path
	}
}

// OneOrMore matches the sequence compilers one or more times
func OneOrMore(compilers ...any) Compiler {
	return func(meta *NFAMetadata, prevStates NFAStates) NFAStates {
		// connect gives every state in prevStates the same new followStates, so those added to
		// prevStates[0] are the head of the fragment that the tail loops back to
		firstHeadIx := len(prevStates[0].followStates)
		tail := Seq(compilers...)(meta, prevStates)
		connect(tail, prevStates[0].followStates[firstHeadIx:])
		return tail
	}
}

// ZeroOrMore matches the sequence compilers zero or more times
func ZeroOrMore(compilers ...any) Compiler {
	return Optional(OneOrMore(compilers...))
}

// Alt matches any one of alternatives
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

// CompileNFA compiles the grammar to an NFA rooted at meta.states[0]. Only paths ending in a
// Token's terminal state are accepted.
func CompileNFA(compiler Compiler) *NFAMetadata {
	meta := &NFAMetadata{}
	// the root's followStates are the grammar's first runes, giving a single start state
	Seq(Any(), compiler)(meta, nil)
	assignClasses(meta)
	return meta
}

// assignClasses partitions the runes matched by the grammar into character classes. The root and
// terminals are excluded: the root is never matched against a rune and terminals match none.
func assignClasses(meta *NFAMetadata) {
	states := slices.DeleteFunc(slices.Clone(meta.states[1:]), func(s *NFAState) bool { return s.terminal })
	for _, d := range disjointRanges(states) {
		meta.classes = append(meta.classes, d.runeRange)
	}
	// every state range starts and ends on a class boundary, so both searches find exact matches
	for _, s := range states {
		s.firstClass, _ = slices.BinarySearchFunc(meta.classes, s.low, func(c runeRange, r rune) int { return cmp.Compare(c.low, r) })
		s.lastClass, _ = slices.BinarySearchFunc(meta.classes, s.high, func(c runeRange, r rune) int { return cmp.Compare(c.high, r) })
	}
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
