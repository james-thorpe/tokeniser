# parser

A parser library for Go built from small combinators. You describe a grammar with
functions such as `Seq`, `Choice` and `Many`. The parser compiles the grammar to
bytecode, then runs it on a virtual machine that reads the input one rune at a
time and follows every alternative in parallel.

```go
number := Rule("number", OneOrMore(Range('0', '9')))

// list refers to item, which is assigned after it, so it reads the variable
// through Lazy when the grammar is compiled.
var item Compiler

list := Rule("list",
	Char('['),
	Optional(Seq(Lazy(&item), Many(Seq(Char(','), Lazy(&item))))),
	Char(']'),
)
item = Rule("item", Choice(number, list))

tree, err := Run(item, []rune("[1,[2,3]]"))
```

- [Grammar](#grammar)
- [Parse tree](#parse-tree)
- [Matching](#matching)
- [Recursion](#recursion)
- [Errors](#errors)
- [Internals](#internals)
- [Limitations](#limitations)

## Grammar

Each combinator returns a `Compiler`. You combine compilers into a grammar and
pass the top-level one to `Run`.

| Combinator | Matches | Produces |
|---|---|---|
| `Char(r)` | the rune `r` | `char` node |
| `Range(lo, hi)` | one rune from `lo` to `hi`, inclusive | `char` node |
| `Text(s)` | the literal string `s` | `text` node |
| `Seq(a, b, ...)` | each part, in order | `seq` node, one child per part |
| `Choice(a, b, ...)` | exactly one of the alternatives | the chosen alternative's node |
| `Optional(p)` | `p`, or nothing | `optional` node with 0 or 1 children |
| `Many(p)` | `p` zero or more times | `many` node, one child per match |
| `OneOrMore(p)` | `p` one or more times | `many` node, one child per match |
| `Rule(name, a, b, ...)` | each part, in order, as a named rule | `rule` node, one child per part |
| `Lazy(&v)` | whatever the variable `v` holds when the grammar is compiled | that compiler's node |

A rule is a value like any other compiler. Assign it to a variable and pass
that variable into later rules. Use `Lazy` to refer to a rule that isn't
assigned yet; see [Recursion](#recursion).

`Seq`, `Choice` and `Rule` need at least one part. The Go function signatures
enforce this.

Some arguments are rejected straight away, with a panic when the combinator is
called rather than later at `Run`:

- `Text("")` panics with `ErrEmptyText`.
- `Lazy(nil)` panics with `ErrUnresolvedLazy`.
- `Rule("", ...)` panics with `ErrEmptyName`.

## Parse tree

`Run` returns the root `*Node` of the parse tree.

| Field | Meaning |
|---|---|
| `Kind` | one of the `Kind` constants below |
| `Name` | the rule name; set only on `KindRule` nodes |
| `Lexeme` | the matched text; set only on `KindChar` and `KindText` nodes |
| `Children` | sub-trees; set only on `KindSeq`, `KindRule`, `KindOptional` and `KindMany` nodes |

| Constant | Value | Produced by |
|---|---|---|
| `KindChar` | `"char"` | `Char`, `Range` |
| `KindText` | `"text"` | `Text` |
| `KindSeq` | `"seq"` | `Seq` |
| `KindRule` | `"rule"` | `Rule` |
| `KindOptional` | `"optional"` | `Optional` |
| `KindMany` | `"many"` | `Many`, `OneOrMore` |

`KindChar` and `KindText` nodes are leaves and always have a lexeme. `Choice`
and `Lazy` add no node of their own, so the node you get is the one from the
alternative or compiler that matched.

Parsing `[1,[2,3]]` with the grammar above produces:

```
rule item
└── rule list
    ├── char "["
    ├── optional
    │   └── seq
    │       ├── rule item
    │       │   └── rule number
    │       │       └── many
    │       │           └── char "1"
    │       └── many
    │           └── seq
    │               ├── char ","
    │               └── rule item
    │                   └── rule list ...
    └── char "]"
```

## Matching

- **The whole input must match.** If any input is left over once the grammar is
  complete, `Run` returns `ErrGrammarMismatch` at the first extra rune.
- **The leftmost alternative wins.** When several alternatives of a `Choice`
  match the same input, `Run` returns the tree from the first one listed.
- **Repetition is greedy.** `Optional` tries its body before skipping it, and
  `Many` and `OneOrMore` try another repetition before stopping. For example,
  `Seq(Many(Char('a')), Optional(Char('a')))` on `aa` gives `Many` both runes
  and leaves `Optional` empty.
- **Offsets count runes,** not bytes, in all error messages.
- **Every alternative is followed in parallel.** The parser never backtracks.
  Each rune is read once, and every alternative still alive moves forward
  together. Alternatives can be different lengths. For example,
  `Choice(Text("ab"), Text("abc"))` accepts both `ab` and `abc`.

## Recursion

A rule's body is compiled once, however many times or however recursively the
rule is used. Each use of the rule becomes a call to that one body.

A Go variable can't be used before it's assigned, so a rule that refers to
itself, or to a rule assigned after it, declares the variable first and reads
it through `Lazy`, passing a pointer to the variable. `Lazy` reads the variable
when the grammar is compiled, by which point it has been assigned:

```go
var parens Compiler
parens = Rule("p", Char('('), Optional(Lazy(&parens)), Char(')'))
```

Mutual recursion works the same way. Declare the later rule's variable first,
as `item` is in the example at the top.

Recursion must pass through a `Rule`. A use of a rule compiles to a single
call, so expansion stops there. Recursion without a rule would expand forever,
so it fails with `ErrRecursionWithoutRule`:

```go
var s Compiler
s = Seq(Char('a'), Optional(Lazy(&s))) // ErrRecursionWithoutRule
```

A rule is identified by the value `Rule` returns, not by its name. So you can
pass one rule value to as many places as you like, but two different `Rule`
calls with the same name are an error (`ErrDuplicateRule`), even if their
bodies are identical. Names label nodes in the tree, so two rules sharing one
would be indistinguishable there.

**Left recursion is rejected.** A rule is left-recursive if it can reach a call
to itself without reading any input, either directly or through other rules.
For example, `expr = expr '+' term` is left-recursive. So is a rule that only
reads optional input before calling itself, such as `r = 'x'? r`. Write such
grammars with repetition instead, e.g. `expr = term ('+' term)*`.

## Errors

`Run` returns two kinds of error. Every error wraps one of these values, so you
can test for it with `errors.Is`.

**Grammar errors** are found when the grammar is compiled, before any input is
read:

| Error | Cause |
|---|---|
| `ErrDuplicateRule` | two different `Rule`s share a name |
| `ErrUnresolvedLazy` | a `Lazy` variable was still nil, i.e. never assigned |
| `ErrRecursionWithoutRule` | a `Lazy` leads back to itself without passing through a `Rule` |
| `ErrLeftRecursion` | a rule can call itself without reading any input |

**Input errors** are found while matching:

| Error | Cause | Example message |
|---|---|---|
| `ErrGrammarMismatch` | a rune that no surviving alternative accepts | `grammar mismatch: unexpected ']' at offset 3` |
| `ErrUnexpectedEOI` | the input ended before the grammar was complete | `unexpected end of input at offset 2` |

Input errors report the first point where every alternative had failed.

`ErrNonCharInText` means `Text` was given something other than characters to
join. The public combinators can't produce this; it only happens with
hand-built bytecode.

## Internals

### Compiling

A `Compiler` is a function that appends bytecode to a `program`:

1. The grammar's top-level compiler emits the main program, which ends in
   `opEof`.
2. The body of each rule used is then compiled after the main program. A body
   can use further rules, which are added to the list and compiled in turn.
3. Any error recorded while compiling is returned: a duplicate rule name, an
   unresolved `Lazy`, or recursion without a rule.
4. It works out which rules can match without reading input. A rule's answer
   depends on the rules it calls, so this repeats until nothing changes.
5. It looks for left recursion: a cycle of calls that reads no input. It walks
   each rule body, steps over calls to rules that can match nothing, and
   follows both sides of every split.

Rules are kept in a table on the program, not in the bytecode. That keeps
every instruction a fixed size, and the instructions that need a rule (`opCall`
and `opRule`) refer to it by its number in the table.

### Bytecode

The program is a flat `[]int32`. Jump targets are absolute positions in it.

| Op | Operands | Effect |
|---|---|---|
| `opChar` | `r` | read `r`, push a `char` node |
| `opRange` | `lo hi` | read one rune in `lo..hi`, push a `char` node |
| `opText` | `n` | pop `n` char nodes, push one `text` node |
| `opSeq` | `n` | pop `n` nodes, push a `seq` node |
| `opRule` | `n id` | pop `n` nodes, push a `rule` node named after rule `id` |
| `opCall` | `id` | push the return address, jump to rule `id`'s body |
| `opRet` | | pop the return address and jump to it |
| `opSplit` | `x y` | fork: this machine continues at `x`, a new one starts at `y` |
| `opJmp` | `x` | jump to `x` |
| `opMark` | | push a marker node |
| `opOptional` | | pop back to the marker, push an `optional` node |
| `opMany` | | pop back to the marker, push a `many` node |
| `opEof` | | accept if the input has ended |

Each combinator's doc comment in `parser.go` shows the exact bytecode it
produces.

Every construct leaves exactly one node on the stack. That's how `opSeq` and
`opRule` know how many nodes to pop. `Optional`, `Many` and `OneOrMore` don't
know their child count in advance, so they push a marker first and collect
everything above it when they finish.

### Virtual machine

The VM runs many lightweight machines (threads) side by side and moves them all
forward one rune at a time. Each machine has:

- a program counter;
- a stack of nodes, from which it builds the tree;
- a stack of return addresses, for calls.

For each rune:

1. Each live machine runs until it reads the rune, fails, or reaches `opEof` at
   the end of input.
2. `opSplit` forks a machine. The fork gets its own copy of the node stack and
   shares the return stack. Forks run most-recent-first, so machines reach
   `opEof` in leftmost-alternative order and the first to accept wins.
3. Machines that read the rune go into the list for the next rune. If none did,
   the input doesn't match at this rune.

Three rules stop the number of machines and the running time from blowing up:

- **Duplicates are dropped.** What a machine does next depends only on its
  program counter and return stack. Its node stack only affects the tree it
  builds. So once one machine has read a rune into a given position and return
  stack, any later machine reaching the same state is dropped. Machines are
  checked in priority order, so the one kept is always the higher-priority
  alternative. This keeps ambiguous grammars from doubling the machine count at
  every ambiguity.
- **Return stacks are shared.** When a call pushes a return address, it reuses
  any existing return stack with the same contents. Two machines with equal
  return stacks then hold the same pointer, so the duplicate check is a single
  pointer comparison.
- **Loops that read nothing stop.** If a machine reaches the same `opSplit`
  twice in one step with the same return stack, it is dropped. Every loop in
  the bytecode goes through a split, so this ends repetitions whose body
  matched nothing, e.g. `Many(Optional(x))`. It also merges machines that meet
  at a split.

## Limitations

- `Run` appends an end-of-input marker to the slice it's given. If the slice
  has spare capacity, this overwrites the element just past its length.
