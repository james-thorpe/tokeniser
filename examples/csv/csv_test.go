package csv

import (
	"errors"
	"os"
	"strings"
	"testing"

	. "tokeniser/tokeniser"
)

func printTokens(input string, delimiter rune) (string, error) {
	var output strings.Builder
	err := Print(&output, strings.NewReader(input), delimiter)
	return output.String(), err
}

func Test_Print(t *testing.T) {
	inputs := []struct {
		input     string
		delimiter rune
		expected  string
	}{
		{"", ',', ""},
		{"a", ',', "field: \"a\"\n"},
		{"a,b\r\nc", ',', `field: "a"
delimiter: ","
field: "b"
newline: "\r\n"
field: "c"
`},
		// empty fields and records produce no tokens
		{",a,\n\n", ',', `delimiter: ","
field: "a"
delimiter: ","
newline: "\n"
newline: "\n"
`},
		// spaces are part of a field
		{" a ", ',', "field: \" a \"\n"},
		// quoted fields keep their quotes, and can contain delimiters, newlines and escaped quotes
		{"\"a,\nb\",\"say \"\"hi\"\"\"", ',', `quoted: "\"a,\nb\""
delimiter: ","
quoted: "\"say \"\"hi\"\"\""
`},
		// other delimiters
		{"a;b,c", ';', `field: "a"
delimiter: ";"
field: "b,c"
`},
		{"é😀\"x\"", '😀', `field: "é"
delimiter: "😀"
quoted: "\"x\""
`},
	}
	for _, in := range inputs {
		output, err := printTokens(in.input, in.delimiter)
		if err != nil {
			t.Errorf("%q: expected no error, got %v", in.input, err)
		}
		if output != in.expected {
			t.Errorf("%q: expected output\n%s\ngot\n%s", in.input, in.expected, output)
		}
	}
}

func Test_Errors(t *testing.T) {
	inputs := []struct {
		input   string
		err     error  // error wrapped by the returned error
		message string // the returned error message
		output  string // tokens printed before the error
	}{
		// the end of input inside a quoted field
		{`a,"b`, ErrUnexpectedEOI, `csv: line 1, position 5: unexpected end of input while processing quoted, expected "quoted text" or "\""`,
			"field: \"a\"\ndelimiter: \",\"\n"},
		// a quote is not allowed in an unquoted field, or after a quoted field
		{"a\nb\"c", ErrGrammarMismatch, `csv: line 2, position 2: unexpected rune '"' while processing field, expected "text", ",", "\r", "\n" or end of input`,
			"field: \"a\"\nnewline: \"\\n\"\n"},
		{`"a"b`, ErrGrammarMismatch, `csv: line 1, position 4: unexpected rune 'b' while processing quoted, expected "\"", ",", "\r", "\n" or end of input`,
			""},
		// \r must be followed by \n
		{"a\rb", ErrGrammarMismatch, `csv: line 1, position 3: unexpected rune 'b' while processing newline, expected "\n"`,
			"field: \"a\"\n"},
	}
	for _, in := range inputs {
		output, err := printTokens(in.input, ',')
		if !errors.Is(err, in.err) {
			t.Errorf("%q: expected error %v, got %v", in.input, in.err, err)
			continue
		}
		if err.Error() != in.message {
			t.Errorf("%q: expected error message %s, got %s", in.input, in.message, err)
		}
		if output != in.output {
			t.Errorf("%q: expected output before the error\n%s\ngot\n%s", in.input, in.output, output)
		}
	}
}

func Test_InvalidDelimiter(t *testing.T) {
	for _, delimiter := range []rune{'"', '\r', '\n', -1} {
		if _, err := printTokens("a", delimiter); !errors.Is(err, ErrInvalidDelimiter) {
			t.Errorf("%q: expected error %v, got %v", delimiter, ErrInvalidDelimiter, err)
		}
	}
}

func ExamplePrint() {
	input := "name;quote\nAda;\"Hello; \"\"World\"\"\"\n"
	if err := Print(os.Stdout, strings.NewReader(input), ';'); err != nil {
		panic(err)
	}
	// Output:
	// field: "name"
	// delimiter: ";"
	// field: "quote"
	// newline: "\n"
	// field: "Ada"
	// delimiter: ";"
	// quoted: "\"Hello; \"\"World\"\"\""
	// newline: "\n"
}
