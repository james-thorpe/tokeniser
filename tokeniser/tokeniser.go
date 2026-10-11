package tokeniser

import (
	"errors"
	"io"
	"iter"
	"slices"
	"strings"
)

type Tokens []*Token
type Token struct {
	Id      int    // Id of token
	Name    string // Name of token
	Pos     int    // absolute position of the start of token (in runes, 0-based)
	Line    int    // line of the start of token ('\n' delimits lines)
	LinePos int    // position within the line of the start of a token (in runes, 1-based)
	Value   string // the Value of token
	Error   error  // if not nil, the token reports an error at its position instead of matched input
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

type Compiler func(machine *StateMachine, token *Token, prevTail states) states

var ErrUnexpectedEOI = errors.New("unexpected end of input")
var ErrGrammarMismatch = errors.New("input does not match grammar")

func Match(matcher Matcher, description string) Compiler {
	return func(machine *StateMachine, token *Token, prevTail states) states {
		state := &state{
			id:          len(machine.states),
			description: description,
			nextStates:  states{},
			match:       matcher,
			pos:         -1,
			token:       token,
			internal:    false,
		}
		connect(prevTail, states{state}, token)
		machine.states = append(machine.states, state)
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
		// an empty string matches nothing, so leave prevTail unchanged (no states created)
		return func(machine *StateMachine, token *Token, prevTail states) states { return prevTail }
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
	return func(machine *StateMachine, token *Token, prevTail states) states {
		for _, compiler := range compilers {
			prevTail = convertToCompiler(compiler)(machine, token, prevTail) // tail = prevTail for each iteration
		}
		return prevTail
	}
}

// Optional is a Seq with a bypass path that allows the state machine to skip over the sequence.
// At least one compiler is required.
func Optional[T CompilerTypes](first T, rest ...T) Compiler {
	return func(machine *StateMachine, token *Token, prevTail states) states {
		tail := Seq(first, rest...)(machine, token, prevTail)
		return slices.Concat(prevTail, tail) // add prevTail to tail to create bypass path (cannot use append)
	}
}

// OneOrMore creates a sequence of compilers that optionally loops back to the head.
// At least one compiler is required.
func OneOrMore[T CompilerTypes](first T, rest ...T) Compiler {
	return func(machine *StateMachine, token *Token, prevTail states) states {
		firstHeadIx := len(prevTail[0].nextStates) // identify no. of pre-existing transitions
		tail := Seq(first, rest...)(machine, token, prevTail)
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
	return func(machine *StateMachine, token *Token, prevTail states) states {
		tail := states{}
		for _, alt := range rest {
			altTail := convertToCompiler(alt)(machine, token, prevTail)
			tail = append(tail, altTail...)
		}
		return tail
	}
}

// Define associates a token with all states within the scope of the definition.
// At least one compiler is required.
func Define[T CompilerTypes](tokenName string, first T, rest ...T) Compiler {
	return func(machine *StateMachine, token *Token, prevTail states) states {
		deftoken := &Token{Name: tokenName, Id: len(machine.tokens)}
		machine.tokens = append(machine.tokens, deftoken)
		tail := Seq(first, rest...)(machine, deftoken, prevTail)
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
	machine := &StateMachine{states: states{}, tokens: Tokens{token}}
	// Add a start and end state to the grammar.  This reduces the conditional logic in Run().
	tail := Seq(Any(), compiler, MatchRune(-1))(machine, token, states{})
	machine.start, machine.end = machine.states[0], tail[0]
	return machine
}

// tokenReader wraps an io.RuneReader and converts io.EOF into rune -1 which simplifies the Run() logic.
// tokenReader tracks the position of each rune by absolute position and by line
// The line tracking is a convenience for text editor review of the input
type tokenReader struct {
	reader  io.RuneReader
	pos     int  // position of the rune in the input (in runes, 0-based)
	line    int  // line of the rune (1-based)
	linePos int  // position of the rune within its line (in runes, 1-based)
	newLine bool // the previous rune was '\n', so the next rune starts a new line
}

func wrapRuneReader(reader io.RuneReader) *tokenReader {
	return &tokenReader{reader: reader, pos: -1, line: 1, linePos: 0}
}

func (tr *tokenReader) readRune() (r rune, err error) {
	r, _, err = tr.reader.ReadRune()
	// the end of input (-1) also has a position, one after the last rune
	tr.pos++
	if tr.newLine {
		tr.line++
		tr.linePos = 1
	} else {
		tr.linePos++
	}
	if err == io.EOF {
		return -1, nil
	}
	tr.newLine = r == '\n'
	return r, err
}

// Run returns a sequence that tokenises input using the grammar compiled into the StateMachine.
// If the input cannot be tokenised, an error token containing the error and its position is returned
func (machine *StateMachine) Run(input io.RuneReader) iter.Seq[*Token] {
	return func(yield func(*Token) bool) {
		machine.reset()
		machine.stale = true
		running := make(states, 0, 8)            // states that are being matched against the current rune
		pending := make(states, 0, 8)            // states that will be matched against the next rune
		buffer := make([]rune, 0, 64)            // runes read last token output
		running = append(running, machine.start) // set start state

		reader := wrapRuneReader(input)
		r, readErr := reader.readRune()
		for {
			if readErr != nil {
				yield(errorToken(reader, readErr))
				return
			}
			for _, state := range running {
				tokenReturned := false
				for _, nextState := range state.nextStates {
					if nextState.pos != reader.pos && nextState.match(r) {
						nextState.pos = reader.pos
						// if token changes between state, or we are transitioning on an extrinsic transition,
						// then we have finished a token and are starting a new one.
						if state.token != nextState.token || !nextState.internal {
							// add previous token to tokens if not the "default" token
							if state.token.Id != 0 {
								state.token.Value = string(buffer) // exclude r, which starts the next token
								if !yield(copyToken(state.token)) {
									return
								}
								pending = pending[:0] // clear pending state
							}
							// start next token
							buffer = buffer[:0]
							startToken(reader, nextState.token)
						}
						// if nextState == end then we must also be at the end of the input (r == -1)
						// so we are all done.
						if nextState == machine.end {
							return
						}
						pending = append(pending, nextState)
					}
					if tokenReturned {
						break
					}
				}
			}
			buffer = append(buffer, r)
			// if r == -1 then we are at the end of the file but not the end of the grammar
			if r == -1 {
				yield(errorToken(reader, ErrUnexpectedEOI))
				return
			}
			// no valid pending states ... so exit with error
			if len(pending) == 0 {
				yield(errorToken(reader, ErrGrammarMismatch))
				return
			}
			// swap running and pending states and clear the pending state for next iteration
			running, pending = pending, running
			pending = pending[:0]
			r, readErr = reader.readRune()
		}
	}
}

// Reset clears the running state left behind by a previous Run to allow reuse of the state machine.
func (machine *StateMachine) reset() {
	if machine.stale {
		for _, state := range machine.states {
			state.pos = -1
		}
		for _, token := range machine.tokens {
			token.Pos, token.Line, token.LinePos = 0, 0, 0
			token.Value = ""
		}
		machine.stale = false
	}
}

// startToken records the position of the rune most recently read as the start of token
func startToken(tr *tokenReader, token *Token) {
	token.Pos, token.Line, token.LinePos = tr.pos, tr.line, tr.linePos
}

// errorToken creates a token reporting err at the position of the rune most recently read
func errorToken(tr *tokenReader, err error) *Token {
	token := &Token{Error: err}
	startToken(tr, token)
	return token
}

func copyToken(token *Token) *Token {
	return &Token{
		Id:      token.Id,
		Name:    token.Name,
		Pos:     token.Pos,
		Line:    token.Line,
		LinePos: token.LinePos,
		Value:   token.Value,
		Error:   token.Error,
	}
}
