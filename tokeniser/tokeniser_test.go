package tokeniser

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------------------------------------------------
// Matchers
// ---------------------------------------------------------------------------------------------------------------------

func Test_MatchRune(t *testing.T) {
	testInputs(t, MatchRune('a'), ok("a"), mismatch("b"), eoi(""))
}

func Test_MatchString(t *testing.T) {
	testInputs(t, MatchString("ab"), ok("ab"), mismatch("abc"), eoi("a"))
}

func Test_Range(t *testing.T) {
	// bounds are inclusive
	testInputs(t, Range('b', 'd'), mismatch("a"), ok("b"), ok("c"), ok("d"), mismatch("e"), eoi(""))
	testInputs(t, Range('x', 'x'), ok("x"))                // single rune range
	testInputs(t, Range('d', 'b'), mismatch("c"))          // inverted range matches nothing
	testInputs(t, Range('α', 'ω'), ok("λ"), mismatch("a")) // non-ASCII range
}

func Test_OneOf(t *testing.T) {
	testInputs(t, OneOf("abc"), ok("a"), ok("c"), mismatch("d"), eoi(""),
		mismatch("ab")) // matches a single rune only
	testInputs(t, OneOf("aé😀"), ok("é"), ok("😀"))
	testInputs(t, OneOf(""), mismatch("a"), eoi("")) // an empty set never matches
	testInputs(t, OneOrMore(OneOf("abc")), ok("cab"))
}

func Test_Any(t *testing.T) {
	// Any must consume a rune, and the end of input is not a rune
	testInputs(t, Any(), ok("a"), ok("é"), eoi(""), mismatch("ab"))
	testInputs(t, Seq('a', Any()), ok("ab"), eoi("a"))
	testInputs(t, ZeroOrMore(Any()), ok(""), ok("abc"))
	testInputs(t, OneOrMore(Any()), eoi(""))
}

func Test_Match(t *testing.T) {
	vowel := Match(func(r rune) bool { return strings.ContainsRune("aeiou", r) }, "vowel")
	testInputs(t, vowel, ok("a"), mismatch("b"), eoi(""))
	testInputs(t, OneOrMore(vowel), ok("aei"))
	testInputs(t, Seq('b', vowel, 'b'), ok("bab"))
}

func Test_Unicode(t *testing.T) {
	testInputs(t, MatchString("héllo"), ok("héllo"), mismatch("hello"))
	testInputs(t, Seq('日', "本語"), ok("日本語"))
	testInputs(t, Seq('a', rune(0), 'b'), ok("a\x00b")) // NUL is an ordinary rune

	// invalid UTF-8 is read as U+FFFD
	testInputs(t, Any(), ok("\xff"))
	testInputs(t, MatchRune('�'), ok("\xff"))
	testInputs(t, MatchRune('a'), mismatch("\xff"))

	// token values and positions are in runes not bytes
	testTokens(t, "héllo", Define("word", "héllo"), tok{"word", 0, "héllo"})
	testTokens(t, "aλμ", Seq('a', Define("greek", OneOrMore(Range('α', 'ω')))), tok{"greek", 1, "λμ"})
	testTokens(t, "é😀x", Seq('é', Define("emoji", '😀'), Define("x", 'x')), tok{"emoji", 1, "😀"}, tok{"x", 2, "x"})
}

// ---------------------------------------------------------------------------------------------------------------------
// Combinators
// ---------------------------------------------------------------------------------------------------------------------

func Test_Seq(t *testing.T) {
	testInputs(t, Seq('a'), ok("a"), mismatch("b"), eoi(""))
	testInputs(t, Seq('a', 'b', 'c'), ok("abc"), mismatch("abd"), eoi("ab"),
		mismatch("abcd")) // trailing input
}

func Test_Optional(t *testing.T) {
	testInputs(t, Optional('a'), ok(""), ok("a"))
	testInputs(t, Optional("abc"), ok("abc"))
	testInputs(t, Seq(Optional('a'), 'b'), ok("ab"), ok("b"), mismatch("aab"), mismatch("c"), mismatch("ac"), eoi("a"))

	// nested
	testInputs(t, Optional(Optional('a')), ok(""), ok("a"))
	testInputs(t, Optional(Optional('a'), 'b'), ok("b"), ok("ab"), eoi("a"))
}

func Test_OneOrMore(t *testing.T) {
	testInputs(t, OneOrMore('a'), eoi(""), ok("a"), ok("aaa"))
	testInputs(t, OneOrMore("abc"), ok("abc"), ok("abcabc"))
	testInputs(t, Seq(OneOrMore('a'), 'b'), ok("ab"), ok("aaab"), mismatch("b"), mismatch("c"), mismatch("ac"), eoi("a"))
}

func Test_ZeroOrMore(t *testing.T) {
	testInputs(t, ZeroOrMore('a'), ok(""), ok("a"), ok("aaa"))
	testInputs(t, ZeroOrMore("abc"), ok("abcabc"), mismatch("abcabd"), eoi("abcab"))
	testInputs(t, Seq(ZeroOrMore('a'), 'b'), ok("ab"), ok("b"), ok("aaab"), mismatch("c"), mismatch("ac"), eoi("a"))
}

func Test_Alt(t *testing.T) {
	testInputs(t, Alt("abc", "def"), ok("abc"), ok("def"), eoi(""), eoi("ab"), mismatch("abd"), mismatch("abcdef"))
	testInputs(t, Alt("aaa", "aaaa"), ok("aaa"), ok("aaaa")) // one alternative is a prefix of the other
	testInputs(t, Seq(Alt("abc", "def"), 'g'), ok("abcg"), ok("defg"))

	// single and nested
	testInputs(t, Alt('a'), ok("a"))
	testInputs(t, Alt('a', Alt('b', 'c')), ok("a"), ok("c"), mismatch("d"))
}

func Test_CompilerTypes(t *testing.T) {
	// compilers, strings and runes can be mixed freely
	var r = 'c' // rune is an alias of int32
	testInputs(t, Seq(MatchRune('a'), "b", r), ok("abc"))
	testInputs(t, Alt(MatchString("abc"), "def", 'g'), ok("abc"), ok("g"))

	// anything else is a coding error
	testPanic(t, "int", Seq(97))
	testPanic(t, "byte", Seq(byte('a')))
	testPanic(t, "nil", Seq(nil))
	testPanic(t, "[]rune", Seq([]rune("a")))
	testPanic(t, "int in Alt", Alt('a', 98))
	// a func literal is not a Compiler until it is converted to one
	testPanic(t, "func literal", Seq(func(meta *Metadata, token *Token, prevTail States) States { return prevTail }))
}

func Test_Empty(t *testing.T) {
	// compilers with nothing to match accept the empty input and nothing else
	for _, exp := range []Compiler{MatchString(""), Seq(), Optional(), OneOrMore(), ZeroOrMore()} {
		testInputs(t, exp, ok(""), mismatch("a"))
	}
	// and are transparent inside a sequence
	testInputs(t, Seq('a', Seq(), MatchString(""), Optional(), OneOrMore(), ZeroOrMore(), 'b'), ok("ab"))

	// a definition with no states never produces a token
	testTokens(t, "", Define("empty"))
	testTokens(t, "a", Seq(Define("empty"), 'a'))
}

func Test_Combinations(t *testing.T) {
	// loop over alternatives
	testInputs(t, OneOrMore(Alt("ab", "cd")), ok("ab"), ok("abcdab"), eoi("abc"), mismatch("abd"))

	// loop with an optional head
	testInputs(t, OneOrMore(Optional('a'), 'b'), ok("b"), ok("abbab"), mismatch("aab"), eoi("aba"))

	// nested loops
	testInputs(t, OneOrMore(OneOrMore('a')), ok("aaa"))
	testInputs(t, ZeroOrMore(ZeroOrMore('a')), ok(""), ok("aaa"))
	testInputs(t, OneOrMore(OneOrMore('a'), OneOrMore('b')), ok("abab"), ok("aabbbab"), eoi("aba"))

	// loop that follows more than one state
	testInputs(t, Seq(Alt('a', 'b'), OneOrMore('c')), ok("ac"), ok("bcc"), eoi("b"))
	testInputs(t, Seq(Optional('a'), OneOrMore('b')), ok("bb"), ok("abb"), eoi("a"))
	testInputs(t, Seq(Alt('a', 'b'), ZeroOrMore('c')), ok("a"), ok("bccc"))

	// optional alternatives
	testInputs(t, Alt(Optional('a'), Optional('b')), ok(""), ok("a"), ok("b"), mismatch("ab"))

	// consecutive optionals following more than one state
	testInputs(t, Seq(Alt('a', 'b', 'c'), Optional('d'), Optional('e'), 'f'),
		ok("cf"), ok("adf"), ok("bef"), ok("cdef"), mismatch("aedf"))
	testInputs(t, Seq(Alt('a', 'b', 'c'), Alt(Optional('d'), Optional('e')), 'f'),
		ok("af"), ok("adf"), ok("aef"), mismatch("adef"))
}

func Test_Ambiguous(t *testing.T) {
	// a loop followed by what it loops on
	testInputs(t, Seq(ZeroOrMore('a'), 'a'), ok("a"), ok("aaa"), eoi(""))
	testInputs(t, Seq(ZeroOrMore('a'), "ab"), ok("ab"), ok("aaab"), eoi("aaa"))

	// an optional followed by the rune it matches
	testInputs(t, Seq(Optional('a'), 'a'), ok("a"), ok("aa"), mismatch("aaa"))

	// identical and shared-prefix alternatives
	testInputs(t, Alt('a', 'a'), ok("a"))
	testInputs(t, Alt("ab", "ac"), ok("ab"), ok("ac"), mismatch("ad"))
}

func Test_Number(t *testing.T) {
	/*
	   Decimal as defined in Go spec: https://go.dev/ref/spec#decimal_digit

	   decimal_digit     = "0" … "9"
	   decimal_digits    = decimal_digit { [ "_" ] decimal_digit }
	   decimal_lit       = "0" | ( "1" … "9" ) [ [ "_" ] decimal_digits ]
	*/
	decimal := Alt('0', Seq(Range('1', '9'), ZeroOrMore(Optional('_'), Range('0', '9'))))
	testInputs(t, decimal, ok("0"), ok("1"), ok("123"), ok("1_234"),
		mismatch("00"),
		mismatch("1__234"), // multiple consecutive underscores not allowed
		eoi("1_"))          // underscore must be followed by a digit
}

// ---------------------------------------------------------------------------------------------------------------------
// Tokens
// ---------------------------------------------------------------------------------------------------------------------

func Test_TokenFields(t *testing.T) {
	// name, position (0-based, in runes) and value of each token
	testTokens(t, "12*345", calc(), tok{"number", 0, "12"}, tok{"mul", 2, "*"}, tok{"number", 3, "345"})
	testTokens(t, "7-8", calc(), tok{"number", 0, "7"}, tok{"sub", 1, "-"}, tok{"number", 2, "8"})

	// a grammar without definitions produces no tokens
	testTokens(t, "abc", MatchString("abc"))
	testTokens(t, "", Optional('a'))
}

func Test_TokenIds(t *testing.T) {
	number := Define("number", OneOrMore(Range('0', '9')))
	add := Define("add", '+')
	tokens, err := run(t, "1+2", Seq(number, add, number))
	if err != nil || len(tokens) != 3 {
		t.Fatalf("expected 3 tokens and no error, got %d tokens and %v", len(tokens), err)
	}
	if tokens[0].Id == 0 || tokens[1].Id == 0 || tokens[2].Id == 0 {
		t.Errorf("expected non-zero ids (0 is the default token), got %d, %d, %d", tokens[0].Id, tokens[1].Id, tokens[2].Id)
	}
	if tokens[0].Id == tokens[1].Id {
		t.Errorf("expected different ids for different definitions, got %d for both", tokens[0].Id)
	}
	// each use of a definition in a grammar is a separate token with its own id
	if tokens[0].Id == tokens[2].Id {
		t.Errorf("expected different ids for each use of the number definition, got %d for both", tokens[0].Id)
	}
}

func Test_TokenGaps(t *testing.T) {
	number := Define("number", OneOrMore(Range('0', '9')))
	// runes outside a definition are matched but do not appear in any token
	testTokens(t, "1 2", Seq(number, ' ', number), tok{"number", 0, "1"}, tok{"number", 2, "2"})
	testTokens(t, " 1", Seq(' ', number), tok{"number", 1, "1"})
	testTokens(t, "1 ", Seq(number, ' '), tok{"number", 0, "1"})
	testTokens(t, "(12)", Seq('(', number, ')'), tok{"number", 1, "12"})
	testTokens(t, "12,345", Seq(number, ZeroOrMore(',', number)), tok{"number", 0, "12"}, tok{"number", 3, "345"})
}

func Test_TokenOptional(t *testing.T) {
	sign := Define("sign", '-')
	number := Define("number", OneOrMore(Range('0', '9')))
	testTokens(t, "-12", Seq(Optional(sign), number), tok{"sign", 0, "-"}, tok{"number", 1, "12"})
	testTokens(t, "12", Seq(Optional(sign), number), tok{"number", 0, "12"})
	testTokens(t, "12", Seq(number, Optional(sign)), tok{"number", 0, "12"})
	testTokens(t, "12-", Seq(number, Optional(sign)), tok{"number", 0, "12"}, tok{"sign", 2, "-"})
	testInputs(t, Seq(Optional(sign), number), mismatch("--12"))
}

func Test_TokenNested(t *testing.T) {
	// an inner definition splits the outer definition around it
	exp := Define("outer", 'a', Define("inner", 'b'), 'c')
	testTokens(t, "abc", exp, tok{"outer", 0, "a"}, tok{"inner", 1, "b"}, tok{"outer", 2, "c"})
}

func Test_TokenLoop(t *testing.T) {
	// the usual shape of a tokeniser: a loop over alternative tokens with untokenised whitespace
	testTokens(t, "", lexer())
	testTokens(t, "12", lexer(), tok{"number", 0, "12"})
	testTokens(t, "12+3*4", lexer(),
		tok{"number", 0, "12"}, tok{"add", 2, "+"}, tok{"number", 3, "3"}, tok{"mul", 4, "*"}, tok{"number", 5, "4"})
	testTokens(t, "12 + abc", lexer(), tok{"number", 0, "12"}, tok{"add", 3, "+"}, tok{"ident", 5, "abc"})
	testTokens(t, "  x  ", lexer(), tok{"ident", 2, "x"})
	testTokens(t, "12ab", lexer(), tok{"number", 0, "12"}, tok{"ident", 2, "ab"})
	testInputs(t, lexer(), mismatch("12 $ 3"))
}

func Test_TokenRepeat(t *testing.T) {
	// each pass through a definition is a separate token, even when the same definition matches twice in a row
	testTokens(t, "1**2", lexer(), tok{"number", 0, "1"}, tok{"mul", 1, "*"}, tok{"mul", 2, "*"}, tok{"number", 3, "2"})
	testTokens(t, "++", lexer(), tok{"add", 0, "+"}, tok{"add", 1, "+"})
	digit := Define("digit", Range('0', '9'))
	testTokens(t, "123", OneOrMore(digit), tok{"digit", 0, "1"}, tok{"digit", 1, "2"}, tok{"digit", 2, "3"})
	testTokens(t, "12", Seq(digit, digit), tok{"digit", 0, "1"}, tok{"digit", 1, "2"})
}

func Test_TokenSharedPrefix(t *testing.T) {
	// alternatives within a token that start with the same rune
	exp := Define("n", Alt("ab", "ac"))
	testTokens(t, "ab", exp, tok{"n", 0, "ab"})
	testTokens(t, "ac", exp, tok{"n", 0, "ac"})
	exp = Define("n", 'x', Alt("ab", "ac"))
	testTokens(t, "xab", exp, tok{"n", 0, "xab"})
	testTokens(t, "xac", exp, tok{"n", 0, "xac"})
}

func Test_TokenAmbiguous(t *testing.T) {
	// each input rune appears once in the token value, however many paths match it
	testTokens(t, "aaa", Define("t", ZeroOrMore('a'), 'a'), tok{"t", 0, "aaa"})
	testTokens(t, "xab", Define("t", 'x', Optional('a'), Optional('a'), 'b'), tok{"t", 0, "xab"})
	testTokens(t, "xa", Define("t", 'x', Alt('a', 'a')), tok{"t", 0, "xa"})
}

func Test_TokenLongestMatch(t *testing.T) {
	eq := Define("eq", "=")
	eqeq := Define("eqeq", "==")
	// one token is a prefix of another: the order of the alternatives must not matter
	testTokens(t, "=", Alt(eq, eqeq), tok{"eq", 0, "="})
	testTokens(t, "==", Alt(eq, eqeq), tok{"eqeq", 0, "=="})
	testTokens(t, "=", Alt(eqeq, eq), tok{"eq", 0, "="})
	testTokens(t, "==", Alt(eqeq, eq), tok{"eqeq", 0, "=="})

	// precedence test: reserved word vs identifier
	keyword := Define("if", "if")
	ident := Define("ident", OneOrMore(Range('a', 'z')))
	testTokens(t, "if", Alt(keyword, ident), tok{"if", 0, "if"})
	testTokens(t, "if", Alt(ident, keyword), tok{"ident", 0, "if"})
	testTokens(t, "iffy", Alt(keyword, ident), tok{"ident", 0, "iffy"})
	testTokens(t, "in", Alt(keyword, ident), tok{"ident", 0, "in"})
	testTokens(t, "x", Alt(keyword, ident), tok{"ident", 0, "x"})
}

func Test_TokenBoundary(t *testing.T) {
	// a token that starts with the same rune as the untokenised rune before it
	testTokens(t, "aaa", Seq('a', Define("as", OneOrMore('a'))), tok{"as", 1, "aa"})

	// adjacent tokens that accept the same rune: every input rune belongs to exactly one token
	exp := Seq(Define("a", OneOrMore('x')), Define("b", OneOrMore('x')))
	testTokens(t, "xx", exp, tok{"a", 0, "x"}, tok{"b", 1, "x"})
	testTokens(t, "xxx", exp, tok{"a", 0, "x"}, tok{"b", 1, "xx"})
	testInputs(t, exp, eoi("x"))

	// where the boundary falls for "xxx" is a design choice, but the values must add up to the input
	tokens, err := run(t, "xxx", exp)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tokens) != 2 || tokens[0].Name != "a" || tokens[1].Name != "b" {
		t.Fatalf("expected tokens a and b, got %v", newToks(tokens))
	}
	if value := string(tokens[0].Value) + string(tokens[1].Value); value != "xxx" {
		t.Errorf("expected token values to add up to %q, got %v", "xxx", newToks(tokens))
	}
	if expected := len(tokens[0].Value); tokens[1].Pos != expected {
		t.Errorf("expected token b at position %d, got %v", expected, newToks(tokens))
	}
}

func Test_TokenEndOfInput(t *testing.T) {
	// a token that runs to the end of the input must not include the end-of-input marker in its value
	notNewline := Match(func(r rune) bool { return r != '\n' }, "!\\n")
	comment := Define("comment", "//", ZeroOrMore(notNewline))
	testTokens(t, "//hi", comment, tok{"comment", 0, "//hi"})
	testTokens(t, "//", comment, tok{"comment", 0, "//"})
	testTokens(t, "//hi\n", Seq(comment, '\n'), tok{"comment", 0, "//hi"})
	testTokens(t, "ab", Define("all", ZeroOrMore(Any())), tok{"all", 0, "ab"})
	testTokens(t, "ab", Define("all", OneOrMore(Any())), tok{"all", 0, "ab"})
}

// ---------------------------------------------------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------------------------------------------------

func Test_ErrorTokens(t *testing.T) {
	// tokens completed before a mismatch are returned with the error
	tokens, err := run(t, "12*x", calc())
	if !errors.Is(err, ErrGrammarMismatch) {
		t.Errorf("expected error: %v, got %v", ErrGrammarMismatch, err)
	}
	if len(tokens) == 0 {
		t.Errorf("expected the completed number token to be returned with the error")
	} else if first := newTok(tokens[0]); first != (tok{"number", 0, "12"}) {
		t.Errorf("expected first token %v, got %v", tok{"number", 0, "12"}, first)
	}
	testInputs(t, calc(), eoi("12*"))
}

// ---------------------------------------------------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------------------------------------------------

// result is the expected result of running a grammar against an input
type result struct {
	input string
	err   error // nil when the input matches the grammar
}

// ok expects input to match the grammar
func ok(input string) result { return result{input, nil} }

// mismatch expects input to contain a rune that the grammar does not allow
func mismatch(input string) result { return result{input, ErrGrammarMismatch} }

// eoi expects input to end before the grammar does
func eoi(input string) result { return result{input, ErrUnexpectedEOI} }

// testInputs runs the grammar against the input of each result and checks that it returns the expected error, or none
func testInputs(t *testing.T, exp Compiler, results ...result) {
	t.Helper()
	for _, expected := range results {
		_, err := run(t, expected.input, exp)
		if !errors.Is(err, expected.err) {
			want := "no error"
			if expected.err != nil {
				want = expected.err.Error()
			}
			t.Errorf("%q: expected %s, got %v", expected.input, want, err)
		}
	}
}

// testTokens checks the name, position and value of every token produced
func testTokens(t *testing.T, input string, exp Compiler, expected ...tok) {
	t.Helper()
	tokens, err := run(t, input, exp)
	if err != nil {
		t.Errorf("%q: expected no error, got %v", input, err)
		return
	}
	if got, want := fmt.Sprint(newToks(tokens)), fmt.Sprint(expected); got != want {
		t.Errorf("%q: expected tokens %s, got %s", input, want, got)
	}
}

// testPanic expects the grammar to be rejected as a coding error when it is compiled
func testPanic(t *testing.T, description string, exp Compiler) {
	t.Helper()
	defer func() {
		if r := recover(); r != "unable to convert to compiler" {
			t.Errorf("%s: expected panic \"unable to convert to compiler\", got %v", description, r)
		}
	}()
	_, _ = Run(exp, []rune("a"))
}

// run the grammar, reporting a panic as a test failure rather than aborting the whole test run
func run(t *testing.T, input string, exp Compiler) (tokens Tokens, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%q: unexpected panic: %v", input, r)
			tokens, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()
	return Run(exp, []rune(input))
}

// tok is the expected name, position and value of a token
type tok struct {
	name  string
	pos   int
	value string
}

func (k tok) String() string {
	return fmt.Sprintf("%s@%d=%q", k.name, k.pos, k.value)
}

func newTok(token *Token) tok {
	return tok{token.Name, token.Pos, string(token.Value)}
}

func newToks(tokens Tokens) []tok {
	toks := make([]tok, len(tokens))
	for i, token := range tokens {
		toks[i] = newTok(token)
	}
	return toks
}

// calc returns a grammar of a number, an operator and a number
func calc() Compiler {
	number := Define("number", OneOrMore(Range('0', '9')))
	mul := Define("mul", '*')
	div := Define("div", '/')
	add := Define("add", '+')
	sub := Define("sub", '-')
	return Seq(number, Alt(mul, div, add, sub), number)
}

// lexer returns a small tokeniser: any number of numbers, identifiers and operators separated by optional spaces
func lexer() Compiler {
	number := Define("number", OneOrMore(Range('0', '9')))
	ident := Define("ident", OneOrMore(Range('a', 'z')))
	mul := Define("mul", '*')
	add := Define("add", '+')
	space := OneOrMore(' ')
	return ZeroOrMore(Alt(number, ident, mul, add, space))
}

// ---------------------------------------------------------------------------------------------------------------------
// Debugging: call graph(exp) in a test to print a grammar's state machine, and paste the output into a GraphViz viewer
// ---------------------------------------------------------------------------------------------------------------------

func graph(exp Compiler) {
	var builder strings.Builder
	Graph(&builder, exp)
	println(builder.String())
}

// Graph output state machine in GraphViz 'dot' format
func Graph(writer io.Writer, compiler Compiler) {
	start, end, meta := Compile(compiler)
	// group states by token id
	tokenStates := map[int]States{}
	for _, state := range meta.states {
		tokenStates[state.token.Id] = append(tokenStates[state.token.Id], state)
	}

	_, _ = fmt.Fprintf(writer, "digraph {\n  rankdir=LR;\n  node [fixedsize=true];\n")
	// render the nodes
	for _, token := range meta.tokens {
		if token.Id > 0 {
			_, _ = fmt.Fprintf(writer,
				"subgraph cluster_%d {\n  label=\"%s\";\n  color=blue;\n  fontcolor=blue;\n",
				token.Id, token.Name)
		}
		states := tokenStates[token.Id]
		for _, state := range states {
			if state == start || state == end {
				_, _ = fmt.Fprintf(writer, "  %d [shape=\"doublecircle\"];\n", state.id)
			} else {
				_, _ = fmt.Fprintf(writer, "  %d [shape=\"circle\"];\n", state.id)
			}
		}
		if token.Id > 0 {
			_, _ = fmt.Fprintf(writer, "}\n")
		}
	}
	// render the transitions
	for _, state := range meta.states {
		for _, nextState := range state.nextStates {
			colour := "blue"
			if state.token != nextState.token || !nextState.internal {
				colour = "red"
			}
			if nextState == end {
				_, _ = fmt.Fprintf(writer, "  %d -> %d [arrowsize=0.7, color=%s, label=\"δ\"];\n", state.id, nextState.id, colour)
			} else {
				_, _ = fmt.Fprintf(writer, "  %d -> %d [arrowsize=0.7, color=%s, label=\"%s\"];\n", state.id, nextState.id, colour, nextState.description)
			}
		}
	}
	_, _ = fmt.Fprintf(writer, "}\n")
}
