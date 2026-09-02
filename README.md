# Tokeniser

This project implements a programmable tokeniser in Go. It also contains a tutorial
on the tokeniser's design and implementation. The tokeniser compiles and runs a 
state machine generated from a grammar defined using Go functions.

See the [Wiki](https://github.com/james-thorpe/tokeniser/wiki) for the tutorial and full documentation.

The tutorial component of the project has 2 main branches:

1. The `machine` branch contains the code to compile a grammar to a state machine and then execute the 
state machine to validate that an input is grammatically correct.
1. The `tokens` branch contains the code associated with token extraction.

## Example

The grammar for a Go decimal integer taken directly from 
the [Go Specification](https://go.dev/ref/spec#decimal_digit) is:
```
decimal_digit     = "0" … "9"
decimal_digits    = decimal_digit { [ "_" ] decimal_digit }
decimal_lit       = "0" | ( "1" … "9" ) [ [ "_" ] decimal_digits ]
```
The equivalent grammar using Go functions is:
```go
decimal := Alt('0', Seq(Range('1', '9'), ZeroOrMore(Optional('_'), Range('0', '9'))))
```
Compiling the grammar results in a state machine as follows:

![Go Decimal](https://github.com/james-thorpe/tokeniser/wiki/images/go-int.svg)

The state machine for a grammar can be run against an input string to validate that the input is grammatically correct. 
With a little more work, we can extract tokens during the process.
