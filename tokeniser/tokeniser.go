package tokeniser

import (
	"errors"
	"io"
	"slices"
	"strings"
)

type Token struct {
	Id    int    // Id of token
	Name  string // Name of token
	Pos   int    // position of the start of token (in runes)
	Value []rune // the Value of token
}

type Tokens []*Token

// Matcher returns true to indicate that a transition is valid for the rune r
type Matcher func(r rune) bool

type State struct {
	id          int      // id of state (used to debug and graph state machine)
	description string   // text description of matcher (used to debug and graph state machine)
	nextStates  []*State // possible next states depending on input
	match       Matcher  // if true this state matches the input
	pos         int      // track pos of state in input stream to eliminate ambiguous grammars
	token       *Token   // token this state belongs to
}

type States = []*State

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
			token:       token}
		connect(prevTail, States{state})
		meta.states = append(meta.states, state)
		return States{state}
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
	return func(meta *Metadata, token *Token, prevTail States) States {
		for _, compiler := range compilers {
			prevTail = convertToCompiler(compiler)(meta, token, prevTail) // tail = prevTail for each iteration
		}
		return prevTail
	}
}

func Optional(compilers ...any) Compiler {
	return func(meta *Metadata, token *Token, prevTail States) States {
		tail := Seq(compilers...)(meta, token, prevTail)
		return append(prevTail, tail...) // add prevTail to tail to create bypass path
	}
}

func OneOrMore(compilers ...any) Compiler {
	return func(meta *Metadata, token *Token, prevTail States) States {
		firstHeadIx := len(prevTail[0].nextStates) // identify no. of pre-existing transitions
		tail := Seq(compilers...)(meta, token, prevTail)
		head := prevTail[0].nextStates[firstHeadIx:] // get new transitions as the head of this fragment
		connect(tail, head)                          // loop back to the head
		return tail
	}
}

func ZeroOrMore(compilers ...any) Compiler {
	return Optional(OneOrMore(compilers...))
}

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

func Define(tokenName string, compilers ...any) Compiler {
	return func(meta *Metadata, token *Token, prevTail States) States {
		token = &Token{Name: tokenName, Id: len(meta.tokens)}
		meta.tokens = append(meta.tokens, token)
		tail := Seq(compilers...)(meta, token, prevTail)
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

func Compile(compiler Compiler) (start *State, end *State, meta *Metadata) {
	token := &Token{Id: 0, Name: ""}
	meta = &Metadata{states: States{}, tokens: Tokens{token}}
	prevTail := Any()(meta, token, States{})
	tail := Seq(compiler, rune(-1))(meta, token, prevTail)
	return prevTail[0], tail[0], meta // prevTail and tail are guaranteed to only have one state
}

func Run(compiler Compiler, reader io.RuneReader) (tokens Tokens, err error) {
	start, end, _ := Compile(compiler)
	running := make(States, 0, 8) // states that are being matched against the current rune
	pending := make(States, 0, 8) // states that will be matched against the next rune
	tokens = make(Tokens, 0, 8)
	running = append(running, start) // set start state
	// run state machine
	pos := 0
	r, _, rerr := reader.ReadRune()
	for r != -1 {
		if rerr == io.EOF {
			r = rune(-1)
		}
		pos++
		for _, state := range running { // for each running state .
			for _, nextState := range state.nextStates {
				if state.pos != pos && nextState.match(r) {

					// if token changes between state and nextState then process tokens
					if state.token != nextState.token {
						// return state.token if not the "default" token
						if state.token.Id != 0 {
							tokens = append(tokens, copyToken(state.token))
						}
						// record start of next token position
						nextState.token.Pos = pos
					}
					// if nextState == end then we must also be at the end of the input (r == -1)
					// so we are all done.
					if nextState == end {
						return tokens, nil
					}
					// update token value if nextState.token not the "default" token
					if nextState.token.Id != 0 {
						nextState.token.Value = append(nextState.token.Value, r)
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
		running, pending = pending, running
		pending = pending[:0]
		r, _, rerr = reader.ReadRune()
	}
	return nil, rerr // won't ever get here
}

func copyToken(token *Token) *Token {
	t := &Token{Id: token.Id, Name: token.Name, Pos: token.Pos, Value: token.Value}
	token.Value = nil
	token.Pos = 0
	return t
}
