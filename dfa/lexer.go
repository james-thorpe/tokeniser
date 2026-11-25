package dfa

import "fmt"

// SkipKind is the token kind of Skip tokens, which Lexer.Tokens discards.
const SkipKind = -1

// Skip matches the sequence compilers as a token that Lexer.Tokens discards, eg whitespace.
func Skip(compilers ...any) Compiler {
	return Token(SkipKind, compilers...)
}

// Lexeme is a token read from the input.
type Lexeme struct {
	Kind int
	Text string
	Pos  int // rune offset of the first rune in the input
}

// Lexer splits input into tokens using a DFA built from every token's grammar.
type Lexer struct {
	start *DFAState
}

// NewLexer builds a lexer from tokens, each created with Token or Skip. When tokens match the
// same text, the one listed first wins.
func NewLexer(tokens ...Compiler) *Lexer {
	alternatives := make([]any, len(tokens))
	for i, token := range tokens {
		alternatives[i] = token
	}
	return &Lexer{start: buildDFA(CompileNFA(Alt(alternatives...))).states[0]}
}

// Next reads the longest token starting at pos and returns it with the position after it.
// Tokens that match the empty string are ignored so the lexer always advances.
func (l *Lexer) Next(input []rune, pos int) (Lexeme, int, error) {
	state := l.start
	end, kind := -1, 0
	for i := pos; i < len(input) && state != nil; i++ {
		state = step(state, input[i])
		if state != nil && state.accept {
			end, kind = i+1, state.kind
		}
	}
	switch {
	case end >= 0:
		return Lexeme{Kind: kind, Text: string(input[pos:end]), Pos: pos}, end, nil
	case state != nil:
		// input ran out part way through a token
		return Lexeme{}, pos, fmt.Errorf("%w at %d", ErrUnexpectedEOI, pos)
	default:
		return Lexeme{}, pos, fmt.Errorf("%w at %d", ErrGrammarMismatch, pos)
	}
}

// Tokens splits the whole input into tokens, dropping Skip tokens. On error it returns the tokens
// read so far.
func (l *Lexer) Tokens(input []rune) ([]Lexeme, error) {
	tokens := []Lexeme{}
	for pos := 0; pos < len(input); {
		token, next, err := l.Next(input, pos)
		if err != nil {
			return tokens, err
		}
		if token.Kind != SkipKind {
			tokens = append(tokens, token)
		}
		pos = next
	}
	return tokens, nil
}

// step returns the state reached from s on rune r, or nil if there is no transition.
func step(s *DFAState, r rune) *DFAState {
	for _, t := range s.transitions {
		if t.contains(r) {
			return t.next
		}
	}
	return nil
}
