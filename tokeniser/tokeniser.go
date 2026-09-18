package tokeniser

import (
	"errors"
	"io"
	"slices"
	"strings"
)

// Matcher returns true to indicate that a transition is valid for the rune r
type Matcher func(r rune) bool

type State struct {
	id          int      // id of state (used to debug and graph state machine)
	description string   // text description of matcher (used to debug and graph state machine)
	nextStates  []*State // possible outbound state nextStates
	match       Matcher  // if true the state machine can transition to this state
	pos         int      // track pos of state in input stream
}

type States = []*State

type Compiler func(meta *Metadata, prevTail States) States

// Metadata captured during the compilation process. It is used to graph the state machines.
type Metadata struct {
	states States // list of all states that have been created (doubles as State.id generator)
}

var ErrUnexpectedEOI = errors.New("unexpected end of input")
var ErrGrammarMismatch = errors.New("input does not match grammar")

func Match(matcher Matcher, description string) Compiler {
	return func(meta *Metadata, prevTail States) States {
		state := &State{
			match:       matcher,
			nextStates:  States{},
			id:          len(meta.states),
			description: description}
		states := States{state}
		// connect previous states to this state
		connect(prevTail, states)
		// update metadata
		meta.states = append(meta.states, state)
		return states
	}
}

// MatchRune matches an input rune
func MatchRune(rn rune) Compiler {
	return Match(func(r rune) bool { return r == rn }, string(rn))
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
	return Match(func(r rune) bool { return true }, "∀")
}

// OneOf returns true if an input rune is found in a string
func OneOf(options string) Compiler {
	return Match(func(r rune) bool { return strings.ContainsRune(options, r) }, options)
}

// Range returns true if an input rune is between low and high inclusive
func Range(low, high rune) Compiler {
	return Match(func(r rune) bool { return r >= low && r <= high }, string(low)+"-"+string(high))
}

func Seq(compilers ...any) Compiler {
	return func(meta *Metadata, prevTail States) States {
		for _, compiler := range compilers {
			prevTail = convertToCompiler(compiler)(meta, prevTail) // tail = prevTail for each iteration
		}
		return prevTail
	}
}

func Optional(compilers ...any) Compiler {
	return func(meta *Metadata, prevTail States) States {
		tail := Seq(compilers...)(meta, prevTail)
		return append(prevTail, tail...) // returning prevTail creates a bypass path
	}
}

func OneOrMore(compilers ...any) Compiler {
	return func(meta *Metadata, prevTail States) States {
		firstHeadIx := len(prevTail[0].nextStates) // identify no. of pre-existing transitions
		tail := Seq(compilers...)(meta, prevTail)
		head := prevTail[0].nextStates[firstHeadIx:] // get new transitions as the head of this fragment
		connect(tail, head)                          // loop back to the head
		return tail
	}
}

func ZeroOrMore(compilers ...any) Compiler {
	return Optional(OneOrMore(compilers...))
}

func Alt(alternatives ...any) Compiler {
	return func(meta *Metadata, prevTail States) States {
		tail := States{}
		for _, alt := range alternatives {
			altTail := convertToCompiler(alt)(meta, prevTail)
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
// to the source state.nextStates slice. We ensure that there are no duplicate states.
func connect(sourceStates States, destStates States) {
	for _, source := range sourceStates {
		for _, dest := range destStates {
			// add dest to source transitions if not already added
			if !slices.Contains(source.nextStates, dest) {
				source.nextStates = append(source.nextStates, dest)
			}
		}
	}
}

func compile(compiler Compiler) (start *State, end *State, meta *Metadata) {
	meta = &Metadata{states: States{}}
	// append a start and end state to the grammar.  This makes Run() simpler
	tail := Seq(Any(), compiler, rune(-1))(meta, States{})
	return meta.states[0], tail[0], meta //
}

func Run(compiler Compiler, reader io.RuneReader) (err error) {
	start, end, _ := compile(compiler)
	running := make(States, 0, 8)    // states that are being matched against the current rune
	pending := make(States, 0, 8)    // states that will be matched against the next rune
	running = append(running, start) // set start state
	// run state machine
	pos := 0
	r, _, rerr := reader.ReadRune()
	for r != -1 {
		if rerr == io.EOF {
			r = rune(-1)
		}
		pos++
		// for each running state ...
		for _, state := range running {
			// transition to nextState if possible
			for _, nextState := range state.nextStates {
				// if nextState is at this position then it is already pending so we can skip it
				// otherwise if NextState matches input add to pending
				if nextState.pos != pos && nextState.match(r) {
					nextState.pos = pos
					// if nextState == end then we must also be at the end of the input (r == -1)
					// so we are all done.
					if nextState == end {
						return nil
					}
					// add nextState to pending so it becomes a running state for the next rune
					pending = append(pending, nextState)
				}
			}
		}
		// if r == -1 then we are at the end of the file but not the end of the grammar
		if r == -1 {
			return ErrUnexpectedEOI
		}
		// no valid pending states ... so exit with error
		if len(pending) == 0 {
			return ErrGrammarMismatch
		}
		// swap running and pending states and clear the pending state for next iteration
		running, pending = pending, running
		pending = pending[:0]
		r, _, rerr = reader.ReadRune() // get next rune
	}
	// otherwise return the reader error
	return rerr
}
