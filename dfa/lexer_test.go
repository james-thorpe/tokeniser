package dfa

import (
	"errors"
	"slices"
	"testing"
	"unicode/utf8"
)

const (
	kIf = iota
	kIdent
	kNum
	kPlus
	kDot
	kLParen
	kRParen
	kString
)

var (
	letter     = Alt(Range('a', 'z'), Range('A', 'Z'), '_')
	ident      = Seq(letter, ZeroOrMore(Alt(letter, Range('0', '9'))))
	whitespace = OneOrMore(Alt(' ', '\t', '\n'))
	notQuote   = Alt(Range(0, '"'-1), Range('"'+1, utf8.MaxRune))
	str        = Seq('"', ZeroOrMore(notQuote), '"')
)

func newTestLexer() *Lexer {
	return NewLexer(
		Token(kIf, "if"), // before ident so the keyword wins on equal length
		Token(kIdent, ident),
		Token(kNum, float),
		Token(kPlus, '+'),
		Token(kDot, '.'),
		Token(kLParen, '('),
		Token(kRParen, ')'),
		Token(kString, str),
		Skip(whitespace),
	)
}

func Test_LexerTokens(t *testing.T) {
	testTokens(t, newTestLexer(), `if x1 + 1.5e3 ("a b")`, nil,
		Lexeme{kIf, "if", 0},
		Lexeme{kIdent, "x1", 3},
		Lexeme{kPlus, "+", 6},
		Lexeme{kNum, "1.5e3", 8},
		Lexeme{kLParen, "(", 14},
		Lexeme{kString, `"a b"`, 15},
		Lexeme{kRParen, ")", 20},
	)
	testTokens(t, newTestLexer(), "", nil)
	testTokens(t, newTestLexer(), " \t\n", nil)
}

func Test_LexerLongestMatch(t *testing.T) {
	// "iffy" is longer as an identifier than the keyword "if"
	testTokens(t, newTestLexer(), "iffy", nil, Lexeme{kIdent, "iffy", 0})
	// "1." is not a number, so the lexer backs up to the last accepting position after "1"
	testTokens(t, newTestLexer(), "1.x", nil, Lexeme{kNum, "1", 0}, Lexeme{kDot, ".", 1}, Lexeme{kIdent, "x", 2})
	// "1e" and "1e+" are incomplete numbers, so the lexer backs up two runes to "1"
	testTokens(t, newTestLexer(), "1e+", nil, Lexeme{kNum, "1", 0}, Lexeme{kIdent, "e", 1}, Lexeme{kPlus, "+", 2})
}

func Test_LexerPriority(t *testing.T) {
	// with ident listed first, it wins over the keyword on equal length
	lexer := NewLexer(Token(kIdent, ident), Token(kIf, "if"), Skip(whitespace))
	testTokens(t, lexer, "if", nil, Lexeme{kIdent, "if", 0})
}

func Test_LexerErrors(t *testing.T) {
	// unknown rune: tokens read so far are returned with the error
	testTokens(t, newTestLexer(), "x $", ErrGrammarMismatch, Lexeme{kIdent, "x", 0})
	// unterminated string: input ends part way through a token
	testTokens(t, newTestLexer(), `x "abc`, ErrUnexpectedEOI, Lexeme{kIdent, "x", 0})

	_, err := newTestLexer().Tokens([]rune("x $"))
	if err == nil || err.Error() != "input does not match grammar at 2" {
		t.Errorf("x $: got %v, want position 2 in error", err)
	}
}

func Test_LexerEmptyToken(t *testing.T) {
	// a token matching the empty string never matches without input, so the lexer cannot stall
	lexer := NewLexer(Token(kIdent, Optional('a')))
	testTokens(t, lexer, "aa", nil, Lexeme{kIdent, "a", 0}, Lexeme{kIdent, "a", 1})
	testTokens(t, lexer, "b", ErrGrammarMismatch)
}

func Test_LexerNext(t *testing.T) {
	input := []rune("if x")
	token, pos, err := newTestLexer().Next(input, 2)
	if err != nil || pos != 3 || token != (Lexeme{SkipKind, " ", 2}) {
		t.Errorf("Next at 2: got %v, %d, %v", token, pos, err)
	}
}

// testTokens checks that lexing input returns want and an error matching wantErr.
func testTokens(t *testing.T, lexer *Lexer, input string, wantErr error, want ...Lexeme) {
	t.Helper()
	got, err := lexer.Tokens([]rune(input))
	if !errors.Is(err, wantErr) {
		t.Errorf("%q: got error %v, want %v", input, err, wantErr)
	}
	if !slices.Equal(got, want) && !(len(got) == 0 && len(want) == 0) {
		t.Errorf("%q: got %v, want %v", input, got, want)
	}
}
