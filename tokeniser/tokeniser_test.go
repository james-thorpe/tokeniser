package tokeniser

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
)

func Test_MatchNoInput(t *testing.T) {
	testE(t, "", MatchRune('a'), ErrUnexpectedEOI)
}

func Test_MatchRune(t *testing.T) {
	test(t, "a", MatchRune('a'))
	testE(t, "b", MatchRune('a'), nil)
}

func Test_Seq(t *testing.T) {
	test(t, "a", Seq('a'))
	testE(t, "b", Seq('a'), nil)
	testE(t, "", Seq('a'), ErrUnexpectedEOI)

	// many parameters
	test(t, "abc", Seq('a', 'b', 'c'))
	testE(t, "abd", Seq('a', 'b', 'c'), nil)
	testE(t, "ab", Seq('a', 'b', 'c'), ErrUnexpectedEOI)
	testE(t, "abcd", Seq('a', 'b', 'c'), nil)
}

func Test_String(t *testing.T) {
	test(t, "ab", MatchString("ab"))
	testE(t, "abc", MatchString("ab"), ErrGrammarMismatch)
	testE(t, "a", MatchString("ab"), ErrUnexpectedEOI)
}

func Test_Optional(t *testing.T) {
	test(t, "", Optional('a'))
	test(t, "a", Optional('a'))
	test(t, "abc", Optional("abc"))
	test(t, "ab", Seq(Optional('a'), 'b'))
	test(t, "b", Seq(Optional('a'), 'b'))

	testE(t, "aab", Seq(Optional('a'), 'b'), nil)
	testE(t, "c", Seq(Optional('a'), 'b'), nil)
	testE(t, "ac", Seq(Optional('a'), 'b'), nil)
	testE(t, "a", Seq(Optional('a'), 'b'), ErrUnexpectedEOI)
}

func Test_OneOrMore(t *testing.T) {
	testE(t, "", OneOrMore('a'), ErrUnexpectedEOI)
	test(t, "a", OneOrMore('a'))
	test(t, "aaa", OneOrMore('a'))
	test(t, "abc", OneOrMore("abc"))
	test(t, "abcabc", OneOrMore("abc"))
	test(t, "ab", Seq(OneOrMore('a'), 'b'))
	testE(t, "b", Seq(OneOrMore('a'), 'b'), nil)
	test(t, "aaab", Seq(OneOrMore('a'), 'b'))
	testE(t, "c", Seq(OneOrMore('a'), 'b'), nil)
	testE(t, "ac", Seq(OneOrMore('a'), 'b'), nil)
	testE(t, "a", Seq(OneOrMore('a'), 'b'), ErrUnexpectedEOI)
}

func Test_ZeroOrMore(t *testing.T) {
	test(t, "", ZeroOrMore('a'))
	test(t, "a", ZeroOrMore('a'))
	test(t, "aaa", ZeroOrMore('a'))
	test(t, "ab", Seq(ZeroOrMore('a'), 'b'))
	test(t, "b", Seq(ZeroOrMore('a'), 'b'))
	test(t, "aaab", Seq(ZeroOrMore('a'), 'b'))
	test(t, "abcabc", ZeroOrMore("abc"))
	testE(t, "abcabd", ZeroOrMore("abc"), nil)
	testE(t, "c", Seq(ZeroOrMore('a'), 'b'), nil)
	testE(t, "ac", Seq(ZeroOrMore('a'), 'b'), nil)
	testE(t, "a", Seq(ZeroOrMore('a'), 'b'), ErrUnexpectedEOI)
}

func Test_Alt(t *testing.T) {
	test(t, "abc", Alt("abc", "def"))
	test(t, "def", Alt("abc", "def"))
	test(t, "aaa", Alt("aaa", "aaaa"))           // subset test
	test(t, "aaaa", Alt("aaa", "aaaa"))          // superset test
	test(t, "abcg", Seq(Alt("abc", "def"), 'g')) // superset test
	test(t, "defg", Seq(Alt("abc", "def"), 'g')) // superset test
	testE(t, "", Alt("abc", "def"), nil)
	testE(t, "abd", Alt("abc", "def"), nil)
}

func Test_Number(t *testing.T) {
	/*
	   Decimal as defined in Go spec: https://go.dev/ref/spec#decimal_digit

	   decimal_digit     = "0" … "9"
	   decimal_digits    = decimal_digit { [ "_" ] decimal_digit }
	   decimal_lit       = "0" | ( "1" … "9" ) [ [ "_" ] decimal_digits ]
	*/
	decimal := Alt('0', Seq(Range('1', '9'), ZeroOrMore(Optional('_'), Range('0', '9'))))
	test(t, "0", decimal)
	testE(t, "00", decimal, ErrGrammarMismatch)
	test(t, "1", decimal)
	test(t, "123", decimal)
	test(t, "1_234", decimal)
	testE(t, "1__234", decimal, ErrGrammarMismatch) // multiple consecutive underscores not allowed
	testE(t, "1_", decimal, ErrUnexpectedEOI)       // underscore must be followed by number
	// graph(decimal)
}

func Test_BasicOps(t *testing.T) {
	/*
	  String subset based on go grammar  https://go.dev/ref/spec#byte_value
	*/
	ops := Seq(Alt("abc", "abd"), OneOrMore("e", Optional('f'), 'g'))
	graph(ops)
}

func Test_Define(t *testing.T) {
	number := Define("number", OneOrMore(Range('0', '9')))
	mul := Seq(number, OneOrMore(Define("mul", '*'), number))
	testToken(t, "1*2*3", mul, []string{"1", "*", "2", "*", "3"})

}

func test(t *testing.T, input string, exp Compiler) Tokens {
	reader := strings.NewReader(input)
	tokens, err := Run(exp, reader)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
		t.Log(string(debug.Stack()))
	}
	return tokens
}

func testE(t *testing.T, input string, exp Compiler, expectedErr error) {
	reader := strings.NewReader(input)
	_, err := Run(exp, reader)
	if err == nil {
		t.Errorf("expected error, got nil")
		t.Log(string(debug.Stack()))
	}
	if expectedErr != nil && !errors.Is(err, expectedErr) {
		t.Errorf("expected error: %v", expectedErr)
		t.Log(string(debug.Stack()))
	}
}

func testToken(t *testing.T, input string, exp Compiler, values []string) {
	reader := strings.NewReader(input)
	tokens, err := Run(exp, reader)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
		t.Log(string(debug.Stack()))
	}
	if len(tokens) != len(values) {
		t.Errorf("expected %d tokens, got %d", len(values), len(tokens))
	}
	for i, token := range tokens {
		if string(token.Value) != values[i] {
			t.Errorf("token %d: expected %s, got %s", i, values[i], string(token.Value))
		}
	}
}

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
		for _, transition := range state.nextStates {
			if transition == end {
				_, _ = fmt.Fprintf(writer, "  %d -> %d [arrowsize=0.7, label=\"δ\"];\n", state.id, transition.id)
			} else {
				_, _ = fmt.Fprintf(writer, "  %d -> %d [arrowsize=0.7, label=%s];\n", state.id, transition.id, strconv.Quote(transition.description))
			}
		}
	}
	_, _ = fmt.Fprintf(writer, "}\n")
}
