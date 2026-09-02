package tokeniser

import (
	"errors"
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

type States []*State
type State struct {
	id          int     // id of state (used to debug and graph state machine)
	description string  // text description of matcher (used to debug and graph state machine)
	nextStates  States  // possible next states depending on input
	match       Matcher // if true this state matches the input
	pos         int     // track pos of state in input stream to eliminate duplicate running states
	token       *Token  // token this state belongs to
	internal    bool    // indicates an internal path from another state in the same token
}

type Compiler func(meta *Metadata, token *Token, prevTail States) States

// Metadata captured during the compilation process.
type Metadata struct {
	states States // list of all states that have been created (doubles as State.id generator)
	tokens Tokens // list of all tokens that have been created during compilation
}

var ErrUnexpectedEOI = errors.New("unexpected end of input")
var ErrGrammarMismatch = errors.New("input does not match grammar")

func Match(matcher Matcher, description string) Compiler {
	return func(meta *Metadata, token *Token, prevTail States) States {
		state := &State{
			id:          len(meta.states),
			description: description,
			nextStates:  States{},
			match:       matcher,
			pos:         -1,
			token:       token,
			internal:    false,
		}
		connect(prevTail, States{state}, token)
		meta.states = append(meta.states, state)
		return States{state}
	}
}

// MatchRune matches an input rune
func MatchRune(rn rune) Compiler {
	return Match(func(r rune) bool { return r == rn }, string(rn))
}

// MatchString matches a string by creating a sequence of MatchRune compilers
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

// Seq creates a sequence of compilers by passing compiler n as prevTail parameter to compiler n+1
func Seq(compilers ...any) Compiler {
	return func(meta *Metadata, token *Token, prevTail States) States {
		for _, compiler := range compilers {
			prevTail = convertToCompiler(compiler)(meta, token, prevTail) // tail = prevTail for each iteration
		}
		return prevTail
	}
}

// Optional is a Seq with a bypass path that allows the state machine to skip over the sequence
func Optional(compilers ...any) Compiler {
	return func(meta *Metadata, token *Token, prevTail States) States {
		tail := Seq(compilers...)(meta, token, prevTail)
		return slices.Concat(prevTail, tail) // add prevTail to tail to create bypass path (cannot use append)
	}
}

// OneOrMore creates a sequence of compilers that optionally loops back to the head
func OneOrMore(compilers ...any) Compiler {
	return func(meta *Metadata, token *Token, prevTail States) States {
		firstHeadIx := len(prevTail[0].nextStates) // identify no. of pre-existing transitions
		tail := Seq(compilers...)(meta, token, prevTail)
		head := prevTail[0].nextStates[firstHeadIx:] // get new transitions as the head of this fragment
		connect(tail, head, token)                   // loop back to the head
		return tail
	}
}

// ZeroOrMore is just optional one-or-more
func ZeroOrMore(compilers ...any) Compiler {
	return Optional(OneOrMore(compilers...))
}

// Alt creates a series of alternate transition paths that will be concurrently evaluated by the state machine
func Alt(alternatives ...any) Compiler {
	return func(meta *Metadata, token *Token, prevTail States) States {
		tail := States{}
		for _, alt := range alternatives {
			altTail := convertToCompiler(alt)(meta, token, prevTail)
			tail = append(tail, altTail...)
		}
		return tail
	}
}

// Define associates a token with all states within the scope of the definition
func Define(tokenName string, compilers ...any) Compiler {
	return func(meta *Metadata, token *Token, prevTail States) States {
		deftoken := &Token{Name: tokenName, Id: len(meta.tokens)}
		meta.tokens = append(meta.tokens, deftoken)
		tail := Seq(compilers...)(meta, deftoken, prevTail)
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

// connect each source state to each destination state by creating transitions from source
// to destination states.  Ensure:
// 1. There are no duplicate transitions
// 2. Internal loops take precedence over extrinsic ones
// Internal transitions are identified by source.token == dest.token == token
func connect(sourceStates States, destStates States, token *Token) {
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

func Compile(compiler Compiler) (start *State, end *State, meta *Metadata) {
	// Create default token with Id = 0.
	token := &Token{Id: 0, Name: ""}
	meta = &Metadata{states: States{}, tokens: Tokens{token}}
	// Add a start and end state to the grammar.  This reduces the conditional logic in Run().
	tail := Seq(Any(), compiler, rune(-1))(meta, token, States{})
	return meta.states[0], tail[0], meta
}

// Run tokenises input using the grammar described by compiler.
func Run(compiler Compiler, input []rune) (tokens Tokens, err error) {
	start, end, _ := Compile(compiler)
	input = append(input, -1)     // simplification for tutorial - don't do this in production
	running := make(States, 0, 8) // states that are being matched against the current rune
	pending := make(States, 0, 8) // states that will be matched against the next rune
	tokens = make(Tokens, 0, 8)
	running = append(running, start) // set start state

	for pos, r := range input {
		for _, state := range running { // for each running state .
			for _, nextState := range state.nextStates {
				if nextState.pos != pos && nextState.match(r) {
					nextState.pos = pos
					// if token changes between state, or we are looping backward on an extrinsic transition
					// then we have finished a token and started a new one.
					if state.token != nextState.token || !nextState.internal {
						// add previous token to tokens if not the "default" token
						if state.token.Id != 0 {
							state.token.Value = input[state.token.Pos:pos]
							tokens = append(tokens, copyToken(state.token))
							// if we have just processed the final state then return the tokens.
							if nextState == end {
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
					if nextState == end {
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
	}
	return nil, nil
}

func copyToken(token *Token) *Token {
	t := &Token{Id: token.Id, Name: token.Name, Pos: token.Pos, Value: token.Value}
	token.Value = nil
	token.Pos = 0
	return t
}
