package json

import (
	"errors"
	"os"
	"strings"
	"testing"

	. "tokeniser/tokeniser"
)

func printTokens(input string) (string, error) {
	var output strings.Builder
	err := Print(&output, strings.NewReader(input))
	return output.String(), err
}

func Test_Print(t *testing.T) {
	inputs := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{" \t\r\n", ""},
		{"[]{}:,", `begin-array: "["
end-array: "]"
begin-object: "{"
end-object: "}"
name-separator: ":"
value-separator: ","
`},
		{"true false null", `true: "true"
false: "false"
null: "null"
`},
		// numbers
		{"0 -0 12 -3.25 1e5 1E+5 2.5e-10", `number: "0"
number: "-0"
number: "12"
number: "-3.25"
number: "1e5"
number: "1E+5"
number: "2.5e-10"
`},
		// strings keep their quotes and escapes
		{`"" "a b" "\"\\\/\b\f\n\r\t" "\u00e9\uD83D" "é😀"`, `string: "\"\""
string: "\"a b\""
string: "\"\\\"\\\\\\/\\b\\f\\n\\r\\t\""
string: "\"\\u00e9\\uD83D\""
string: "\"é😀\""
`},
		// whitespace is only needed between scalars
		{`{"a":[1,true],"b":null}`, `begin-object: "{"
string: "\"a\""
name-separator: ":"
begin-array: "["
number: "1"
value-separator: ","
true: "true"
end-array: "]"
value-separator: ","
string: "\"b\""
name-separator: ":"
null: "null"
end-object: "}"
`},
		// a scalar can follow a structural token directly, with or without whitespace, at the end of input
		{"[1", "begin-array: \"[\"\nnumber: \"1\"\n"},
		{"[ 1", "begin-array: \"[\"\nnumber: \"1\"\n"},
		// the structure is not checked
		{"[1}", `begin-array: "["
number: "1"
end-object: "}"
`},
	}
	for _, in := range inputs {
		output, err := printTokens(in.input)
		if err != nil {
			t.Errorf("%q: expected no error, got %v", in.input, err)
		}
		if output != in.expected {
			t.Errorf("%q: expected output\n%s\ngot\n%s", in.input, in.expected, output)
		}
	}
}

func Test_Errors(t *testing.T) {
	// the separators that can follow a complete scalar
	separators := `, expected "[", "]", "{", "}", ":", ",", "[ \t\r\n]" or end of input`
	inputs := []struct {
		input   string
		err     error  // error wrapped by the returned error
		message string // the returned error message
		output  string // tokens printed before the error
	}{
		// scalars must be separated
		{"[truefalse]", ErrGrammarMismatch, `json: line 1, position 6: unexpected rune 'f' while processing true` + separators,
			"begin-array: \"[\"\n"},
		{`"a""b"`, ErrGrammarMismatch, `json: line 1, position 4: unexpected rune '"' while processing string` + separators,
			""},
		{"1-2", ErrGrammarMismatch, `json: line 1, position 2: unexpected rune '-' while processing number, expected "0-9", ".", "[eE]", "[", "]", "{", "}", ":", ",", "[ \t\r\n]" or end of input`,
			""},
		{"null\"a\"", ErrGrammarMismatch, `json: line 1, position 5: unexpected rune '"' while processing null` + separators,
			""},
		// invalid numbers: a leading zero cannot be followed by more digits
		{"01", ErrGrammarMismatch, `json: line 1, position 2: unexpected rune '1' while processing number, expected ".", "[eE]", "[", "]", "{", "}", ":", ",", "[ \t\r\n]" or end of input`,
			""},
		{"[-00.5]", ErrGrammarMismatch, `json: line 1, position 4: unexpected rune '0' while processing number, expected ".", "[eE]", "[", "]", "{", "}", ":", ",", "[ \t\r\n]" or end of input`,
			"begin-array: \"[\"\n"},
		{"1.", ErrUnexpectedEOI, `json: line 1, position 3: unexpected end of input while processing number, expected "0-9"`,
			""},
		{"-", ErrUnexpectedEOI, `json: line 1, position 2: unexpected end of input while processing number, expected "0" or "1-9"`,
			""},
		{"+1", ErrGrammarMismatch, `json: line 1, position 1: unexpected rune '+', expected "\"", "-", "0", "1-9", "t", "f", "n", "[", "]", "{", "}", ":", ",", "[ \t\r\n]" or end of input`,
			""},
		// invalid strings
		{`"\x"`, ErrGrammarMismatch, `json: line 1, position 3: unexpected rune 'x' while processing string, expected "[\"\\/bfnrt]" or "u"`,
			""},
		{`"\u12g4"`, ErrGrammarMismatch, `json: line 1, position 6: unexpected rune 'g' while processing string, expected "0-9", "a-f" or "A-F"`,
			""},
		{"[\n\"a\nb\"]", ErrGrammarMismatch, `json: line 2, position 3: unexpected rune '\n' while processing string, expected "character", "\\" or "\""`,
			"begin-array: \"[\"\n"},
		{`"abc`, ErrUnexpectedEOI, `json: line 1, position 5: unexpected end of input while processing string, expected "character", "\\" or "\""`,
			""},
		// invalid literals
		{"nul", ErrUnexpectedEOI, `json: line 1, position 4: unexpected end of input while processing null, expected "l"`,
			""},
		{"True", ErrGrammarMismatch, `json: line 1, position 1: unexpected rune 'T', expected "\"", "-", "0", "1-9", "t", "f", "n", "[", "]", "{", "}", ":", ",", "[ \t\r\n]" or end of input`,
			""},
	}
	for _, in := range inputs {
		output, err := printTokens(in.input)
		if !errors.Is(err, in.err) {
			t.Errorf("%q: expected error %v, got %v", in.input, in.err, err)
			continue
		}
		if err.Error() != in.message {
			t.Errorf("%q: expected error message\n%s\ngot\n%s", in.input, in.message, err)
		}
		if output != in.output {
			t.Errorf("%q: expected output before the error\n%s\ngot\n%s", in.input, in.output, output)
		}
	}
}

func ExamplePrint() {
	input := `{"name": "Ada", "born": 1815, "languages": ["English", "French"], "alive": false}`
	if err := Print(os.Stdout, strings.NewReader(input)); err != nil {
		panic(err)
	}
	// Output:
	// begin-object: "{"
	// string: "\"name\""
	// name-separator: ":"
	// string: "\"Ada\""
	// value-separator: ","
	// string: "\"born\""
	// name-separator: ":"
	// number: "1815"
	// value-separator: ","
	// string: "\"languages\""
	// name-separator: ":"
	// begin-array: "["
	// string: "\"English\""
	// value-separator: ","
	// string: "\"French\""
	// end-array: "]"
	// value-separator: ","
	// string: "\"alive\""
	// name-separator: ":"
	// false: "false"
	// end-object: "}"
}
