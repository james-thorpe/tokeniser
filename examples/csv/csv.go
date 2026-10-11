// Package csv is an example CSV tokeniser built on the tokeniser package.
//
// The format follows RFC 4180, with a configurable delimiter:
//   - fields are separated by the delimiter, and records by "\n" or "\r\n"
//   - quoted fields may contain delimiters and newlines, and a quote is escaped by doubling it ("")
//   - empty fields produce no token
package csv

import (
	"errors"
	"fmt"
	"io"

	. "tokeniser/tokeniser"
)

// token names
const (
	field     = "field"
	quoted    = "quoted"
	delimiter = "delimiter"
	newline   = "newline"
)

var ErrInvalidDelimiter = errors.New("csv: invalid delimiter")

// grammar returns the CSV grammar for the given delimiter
func grammar(delim rune) Compiler {
	// r >= 0 stops the end of input (-1) being matched as part of a field
	unquotedRune := Match(func(r rune) bool {
		return r >= 0 && r != delim && r != '"' && r != '\r' && r != '\n'
	}, "text")
	quotedRune := Match(func(r rune) bool { return r >= 0 && r != '"' }, "quoted text")

	// fields are empty, unquoted or quoted.  Empty fields have no runes, so they produce no token.
	value := Optional(Alt(
		Define(field, OneOrMore(unquotedRune)),
		Define(quoted, Seq(
			MatchRune('"'),
			ZeroOrMore(Alt(quotedRune, MatchString(`""`))),
			MatchRune('"'),
		)),
	))
	// a record is fields separated by delimiters, and records are separated by newlines.  Only a delimiter,
	// newline or the end of input can follow a field, so a quote after a quoted field can only be an escape.
	record := Seq(value, ZeroOrMore(Define(delimiter, delim), value))
	return Seq(record, ZeroOrMore(Define(newline, Alt("\r\n", "\n")), record))
}

// Print tokenises CSV input with fields separated by delimiter, and writes each token to w as its
// name and quoted value, one per line.  The delimiter may not be a quote, '\r' or '\n'.
// If the input is not valid CSV, printing stops and the error is returned with its line and position.
func Print(w io.Writer, input io.RuneReader, delimiter rune) error {
	if delimiter == '"' || delimiter == '\r' || delimiter == '\n' || delimiter < 0 {
		return fmt.Errorf("%w: %q", ErrInvalidDelimiter, delimiter)
	}
	for token := range Compile(grammar(delimiter)).Run(input) {
		if token.Error != nil {
			return fmt.Errorf("csv: line %d, position %d: %w", token.Line, token.LinePos, token.Error)
		}
		if _, err := fmt.Fprintf(w, "%s: %q\n", token.Name, token.Value); err != nil {
			return err
		}
	}
	return nil
}
