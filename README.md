# Tokeniser

This project implements a programmable tokeniser in Go. It also contains a tutorial
on the tokeniser's design and implementation. The tokeniser compiles and runs a 
state machine generated from a grammar defined using Go functions.

See the [Wiki](https://github.com/james-thorpe/tokeniser/wiki) for the tutorial and full documentation.

The tutorial component of the project has 2 main branches:

1. The `machine` branch contains the code to compile a grammar to a state machine and then execute the 
state machine to validate that an input is grammatically correct.
1. The `tokens` branch contains the code associated with token extraction.

