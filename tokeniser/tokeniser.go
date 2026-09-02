package tokeniser

import (
	"errors"
	"io"
	"slices"
	"strings"
)

type Tokens []*Token
type Token struct {
	Id    int    // Id of token
	Name  string // Name of token
	Pos   int    // position of the start of token (in runes, 0-based)
	Value []rune // the Value of token
}

// Matcher returns true to indicate that a transition is valid for the rune r
type Matcher func(r rune) bool

type states []*state
type state struct {
	id          int     // id of state (used to debug and graph state machine)
	description string  // text description of matcher (used to debug and graph state machine)
	nextStates  states  // possible next states depending on input
	match       Matcher // if true this state matches the input
	pos         int     // track pos of state in input stream to eliminate duplicate running states
	token       *Token  // token this state belongs to
	internal    bool    // indicates an internal path from another state in the same token
}

type Compiler func(sm *StateMachine, token *Token, prevTail states) states

var ErrUnexpectedEOI = errors.New("unexpected end of input")
var ErrGrammarMismatch = errors.New("input does not match grammar")

func Match(matcher Matcher, description string) Compiler {
	return func(sm *StateMachine, token *Token, prevTail states) states {
		state := &state{
			id:          len(sm.states),
			description: description,
			nextStates:  states{},
			match:       matcher,
			pos:         -1,
			token:       token,
			internal:    false,
		}
		connect(prevTail, states{state}, token)
		sm.states = append(sm.states, state)
		return states{state}
	}
}

// MatchRune matches an input rune
func MatchRune(rn rune) Compiler {
	return Match(func(r rune) bool { return r == rn }, string(rn))
}

// MatchString matches a string by creating a sequence of MatchRune compilers
func MatchString(text string) Compiler {
	runes := []rune(text)
	if len(runes) == 0 {
		// an empty string matches nothing, so leave prevTail unchanged
		return func(sm *StateMachine, token *Token, prevTail states) states { return prevTail }
	}
	children := make([]Compiler, len(runes))
	for i, r := range runes {
		children[i] = MatchRune(r)
	}
	return Seq(children[0], children[1:]...)
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

// Seq creates a sequence of compilers by passing compiler n as prevTail parameter to compiler n+1.
// At least one compiler is required.
func Seq[T CompilerTypes](first T, rest ...T) Compiler {
	compilers := append([]T{first}, rest...)
	return func(sm *StateMachine, token *Token, prevTail states) states {
		for _, compiler := range compilers {
			prevTail = convertToCompiler(compiler)(sm, token, prevTail) // tail = prevTail for each iteration
		}
		return prevTail
	}
}

// Optional is a Seq with a bypass path that allows the state machine to skip over the sequence.
// At least one compiler is required.
func Optional[T CompilerTypes](first T, rest ...T) Compiler {
	return func(sm *StateMachine, token *Token, prevTail states) states {
		tail := Seq(first, rest...)(sm, token, prevTail)
		return slices.Concat(prevTail, tail) // add prevTail to tail to create bypass path (cannot use append)
	}
}

// OneOrMore creates a sequence of compilers that optionally loops back to the head.
// At least one compiler is required.
func OneOrMore[T CompilerTypes](first T, rest ...T) Compiler {
	return func(sm *StateMachine, token *Token, prevTail states) states {
		firstHeadIx := len(prevTail[0].nextStates) // identify no. of pre-existing transitions
		tail := Seq(first, rest...)(sm, token, prevTail)
		head := prevTail[0].nextStates[firstHeadIx:] // get new transitions as the head of this fragment
		connect(tail, head, token)                   // loop back to the head
		return tail
	}
}

// ZeroOrMore is just optional one-or-more.
// At least one compiler is required.
func ZeroOrMore[T CompilerTypes](first T, rest ...T) Compiler {
	return Optional(OneOrMore(first, rest...))
}

// Alt creates a series of alternate transition paths that will be concurrently evaluated by the state machine.
// At least one alternative is required.
func Alt[T CompilerTypes](first T, rest ...T) Compiler {
	rest = append([]T{first}, rest...)
	return func(sm *StateMachine, token *Token, prevTail states) states {
		tail := states{}
		for _, alt := range rest {
			altTail := convertToCompiler(alt)(sm, token, prevTail)
			tail = append(tail, altTail...)
		}
		return tail
	}
}

// Define associates a token with all states within the scope of the definition.
// At least one compiler is required.
func Define[T CompilerTypes](tokenName string, first T, rest ...T) Compiler {
	return func(sm *StateMachine, token *Token, prevTail states) states {
		deftoken := &Token{Name: tokenName, Id: len(sm.tokens)}
		sm.tokens = append(sm.tokens, deftoken)
		tail := Seq(first, rest...)(sm, deftoken, prevTail)
		return tail
	}
}

// CompilerTypes is anything that can be converted to a Compiler: a Compiler, a string (see MatchString)
// or a rune (see MatchRune).
type CompilerTypes interface {
	Compiler | string | rune
}

func convertToCompiler[T CompilerTypes](obj T) Compiler {
	switch v := any(obj).(type) {
	case Compiler:
		return v
	case string:
		return MatchString(v)
	case rune:
		return MatchRune(v)
	}
	return nil
}

// connect each source state to each destination state by creating transitions from source
// to destination states.  Ensure:
// 1. There are no duplicate transitions
// 2. Internal loops take precedence over extrinsic ones
// Internal transitions are identified by source.token == dest.token == token
func connect(sourceStates states, destStates states, token *Token) {
	for _, source := range sourceStates {
		for _, dest := range destStates {
			if !slices.Contains(source.nextStates, dest) {
				source.nextStates = append(source.nextStates, dest)
			}
			// flag internal transition
			if source.token == dest.token && source.token == token {
				dest.internal = true
			}
		}
	}
}

// StateMachine is a compiled grammar.
type StateMachine struct {
	start  *state // state the machine starts in (matches any rune)
	end    *state // final state (matches the end of input)
	states states // all states in the machine, indexed by state.id (doubles as state.id generator during compilation)
	tokens Tokens // all tokens defined in the grammar, indexed by Token.Id (doubles as Token.Id generator during compilation)
	stale  bool   // indicates that a reset is required
}

func Compile(compiler Compiler) *StateMachine {
	// Create default token with Id = 0.
	token := &Token{Id: 0, Name: ""}
	sm := &StateMachine{states: states{}, tokens: Tokens{token}}
	// Add a start and end state to the grammar.  This reduces the conditional logic in Run().
	tail := Seq(Any(), compiler, MatchRune(-1))(sm, token, states{})
	sm.start, sm.end = sm.states[0], tail[0]
	return sm
}

// tokenReader wraps an io.RuneReader and converts io.EOF into rune -1, which is how the end of
// input is signalled to the state machine.
type tokenReader struct {
	io.RuneReader
}

func (e tokenReader) ReadRune() (r rune, size int, err error) {
	r, size, err = e.RuneReader.ReadRune()
	if err == io.EOF {
		return -1, 0, nil
	}
	return r, size, err
}

// Reset clears the matching state left behind by a previous Run.
func (sm *StateMachine) reset() {
	if sm.stale {
		for _, state := range sm.states {
			state.pos = -1
		}
		for _, token := range sm.tokens {
			token.Pos = 0
			token.Value = nil
		}
		sm.stale = false
	}
}

// Run tokenises input using the grammar compiled into sm.  Run is not safe for concurrent use
// on the same StateMachine.
func (sm *StateMachine) Run(input io.RuneReader) (tokens Tokens, err error) {
	sm.reset()
	sm.stale = true
	running := make(states, 0, 8) // states that are being matched against the current rune
	pending := make(states, 0, 8) // states that will be matched against the next rune
	buffer := make([]rune, 0, 64) // runes read since bufferPos, used to build token values
	bufferPos := 0                // position in the input of buffer[0]
	tokens = make(Tokens, 0, 8)
	running = append(running, sm.start) // set start state

	input = tokenReader{input}

	for pos := 0; ; pos++ {
		r, _, readErr := input.ReadRune()
		if readErr != nil {
			return nil, readErr
		}
		buffer = append(buffer, r)
		for _, state := range running {
			for _, nextState := range state.nextStates {
				if nextState.pos != pos && nextState.match(r) {
					nextState.pos = pos
					// if token changes between state, or we are looping backward on an extrinsic transition,
					// then we have finished a token and started a new one.
					if state.token != nextState.token || !nextState.internal {
						// add previous token to tokens if not the "default" token
						if state.token.Id != 0 {
							// full slice expression stops appends to Value overwriting the buffer
							from, to := state.token.Pos-bufferPos, pos-bufferPos
							state.token.Value = buffer[from:to:to]
							tokens = append(tokens, CopyToken(state.token))
							// no running state can refer to input before pos so discard it
							buffer = buffer[to:]
							bufferPos = pos
							// if we have just processed the final state then return the tokens.
							if nextState == sm.end {
								return tokens, nil
							}
							nextState.token.Pos = pos
							// make nextState the only pending state to enforce token precedence
							// don't bother processing further states
							pending = append(pending[:0], nextState)
							goto processPending
						}
						// single character loops have state == nextState, so we cannot update nextState.token.pos
						// until end of token processing is complete
						nextState.token.Pos = pos
					}
					// if nextState == end then we must also be at the end of the input (r == -1)
					// so we are all done.
					if nextState == sm.end {
						return tokens, nil
					}
					pending = append(pending, nextState)
				}
			}
		}
		// if r == -1 then we are at the end of the file but not the end of the grammar
		if r == -1 {
			return nil, ErrUnexpectedEOI
		}
		// no valid pending states ... so exit with error
		if len(pending) == 0 {
			return tokens, ErrGrammarMismatch
		}
		// swap running and pending states and clear the pending state for next iteration
	processPending:
		running, pending = pending, running
		pending = pending[:0]
		if r == -1 {
			return nil, nil
		}
	}
}

func CopyToken(token *Token) *Token {
	t := &Token{Id: token.Id, Name: token.Name, Pos: token.Pos, Value: token.Value}
	token.Value = nil
	token.Pos = 0
	return t
}
