// Package json is an example JSON tokeniser built on the tokeniser package.
//
// The tokens follow RFC 8259:
//   - structural tokens: [ ] { } : ,
//   - strings in double quotes, with escapes (\" \\ \/ \b \f \n \r \t \uXXXX) and no control characters
//   - numbers with an optional minus sign, fraction and exponent, and no leading zeros
//   - the literals true, false and null
//   - whitespace (space, tab, \r and \n) between tokens produces no token
//
// Strings, numbers and literals (scalars) must be separated by structural tokens or whitespace, so input
// such as "truefalse", "01" or `"a""b"` is rejected.
//
// The state machine can only recognise regular languages, so it cannot check that brackets are balanced,
// or that values and separators are in the right order (which needs a parser with a stack).
package json

import (
	"fmt"
	"io"

	. "tokeniser/tokeniser"
)

// token names (structural tokens are named as in RFC 8259)
const (
	beginArray     = "begin-array"
	endArray       = "end-array"
	beginObject    = "begin-object"
	endObject      = "end-object"
	nameSeparator  = "name-separator"
	valueSeparator = "value-separator"
	str            = "string"
	number         = "number"
	trueLiteral    = "true"
	falseLiteral   = "false"
	nullLiteral    = "null"
)

// grammar returns the JSON grammar
func grammar() Compiler {
	digit := Range('0', '9')
	hex := Alt(Range('0', '9'), Range('a', 'f'), Range('A', 'F'))

	// control characters (which include the end of input, -1) must be escaped in a string
	char := Match(func(r rune) bool { return r >= 0x20 && r != '"' && r != '\\' }, "character")
	escape := Seq(MatchRune('\\'), Alt(OneOf(`"\/bfnrt`), Seq(MatchRune('u'), hex, hex, hex, hex)))

	// scalars are strings, numbers and literals.  Each token starts with a different rune.
	scalar := Alt(
		Define(str, MatchRune('"'), ZeroOrMore(Alt(char, escape)), MatchRune('"')),
		Define(number,
			Optional('-'),
			Alt(MatchRune('0'), Seq(Range('1', '9'), ZeroOrMore(digit))),
			Optional(MatchRune('.'), OneOrMore(digit)),
			Optional(OneOf("eE"), Optional(OneOf("+-")), OneOrMore(digit)),
		),
		Define(trueLiteral, "true"),
		Define(falseLiteral, "false"),
		Define(nullLiteral, "null"),
	)
	// separators are structural tokens and whitespace
	separator := Alt(
		Define(beginArray, '['),
		Define(endArray, ']'),
		Define(beginObject, '{'),
		Define(endObject, '}'),
		Define(nameSeparator, ':'),
		Define(valueSeparator, ','),
		OneOrMore(OneOf(" \t\r\n")),
	)
	// every scalar after the first follows a separator, so scalars are always separated.  After a scalar,
	// only a separator or the end of input can follow, so a number cannot be followed by more digits.
	// scalar is used twice, but the first is only reachable at the start of the input, so the state machine
	// never has to choose between them.
	return Seq(Optional(scalar), ZeroOrMore(separator, Optional(scalar)))
}

// Print tokenises JSON input, and writes each token to w as its name and quoted value, one per line.
// If the input is not a valid sequence of JSON tokens, printing stops and the error is returned with its
// line and position.
func Print(w io.Writer, input io.RuneReader) error {
	for token := range Compile(grammar()).Run(input) {
		if token.Error != nil {
			return fmt.Errorf("json: line %d, position %d: %w", token.Line, token.LinePos, token.Error)
		}
		if _, err := fmt.Fprintf(w, "%s: %q\n", token.Name, token.Value); err != nil {
			return err
		}
	}
	return nil
}
