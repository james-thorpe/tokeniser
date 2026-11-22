# parser

## Result tree

`Run` returns a tree of `*Node` values. The parser produces two kinds itself;
`Token` produces leaves of a caller-supplied kind:

| Kind      | Meaning                                             |
|-----------|-----------------------------------------------------|
| `NkChars` | Leaf. `Lexeme` holds the matched characters.        |
| `NkSeq`   | Interior. `Children` holds the nodes it collected.  |

Kinds are `int32`. The parser's own kinds are negative; zero and positive
values are reserved for user-defined kinds.

The examples below render leaf nodes as their lexeme and seq nodes as a
parenthesised, space-separated list of children. `K` stands for any kind.

### Nodes contributed by each element

| Element                  | Contribution                                                 |
|--------------------------|--------------------------------------------------------------|
| `Char`, `Range`, `Text`  | Characters, merged into the adjacent chars node (see below). |
| `Seq`                    | Exactly one seq node holding its elements' nodes.            |
| `Token`                  | Exactly one leaf of the given kind holding every character matched within. |
| `Optional`               | Its elements' nodes when matched, nothing when skipped.      |
| `ZeroOrMore`             | Its elements' nodes once per iteration, nothing for zero.    |
| `OneOrMore`              | Its elements' nodes once per iteration; at least one.        |
| `Choice`                 | The chosen alternative's nodes.                              |

`Optional`, `ZeroOrMore`, `OneOrMore` and `Choice` never add a node of their
own. Their elements' nodes become children of the enclosing `Seq`; wrap the
elements in a `Seq` to keep them grouped.

`Optional`, `ZeroOrMore` and `OneOrMore` treat multiple arguments as a group
that matches together. `Choice` treats each argument as a separate
alternative; group an alternative of several elements with `Seq`.

| Grammar                                  | Input  | Tree            |
|------------------------------------------|--------|-----------------|
| `Seq('x', Optional('a'), 'y')`           | `xy`   | `(xy)`          |
| `Seq('x', ZeroOrMore(Seq('a')), 'y')`    | `xaay` | `(x (a) (a) y)` |
| `Seq('x', Optional('a', Seq('b')), 'y')` | `xaby` | `(xa (b) y)`    |
| `Seq(Optional('a'))`                     | (empty)| `()`            |
| `Seq('x', Choice('a', Seq('b')), 'y')`   | `xby`  | `(x (b) y)`     |

### Character merging

A matched character is appended to the chars node on top of the stack if
there is one; otherwise a new chars node is pushed. Adjacent characters
therefore form a single leaf regardless of which elements matched them. Only a
`Seq` or `Token` boundary separates them.

| Grammar                         | Input   | Tree        |
|---------------------------------|---------|-------------|
| `Text("abc")`                   | `abc`   | `abc`       |
| `Seq('a', Range('0', '9'), 'b')`| `a5b`   | `(a5b)`     |
| `Seq("ab", Optional("cd"), 'e')`| `abcde` | `(abcde)`   |
| `Seq('a', Seq('b'), 'c')`       | `abc`   | `(a (b) c)` |
| `Seq('a', Token(K, 'b'), 'c')`  | `abc`   | `(a b c)`   |

### Tokens

`Token(kind, ...)` matches its elements as `Seq` does, then replaces
everything they produced with a single leaf node of `kind`. Its lexeme is the
concatenation, in match order, of every character matched beneath it, however
deeply nested in `Seq` or `Token` nodes. A token that matches nothing yields a
node with an empty lexeme.

A token node is sealed: characters matched after it start a new chars node
rather than extending it, so adjacent tokens stay distinct. This holds even
when `kind` is `NkChars`.

| Grammar                                         | Input  | Tree       |
|-------------------------------------------------|--------|------------|
| `Token(K, 'a', Seq('b'), 'c')`                  | `abc`  | `abc`      |
| `Seq('x', Token(K, Seq('a'), Seq('b')), 'y')`   | `xaby` | `(x ab y)` |
| `Seq(Token(K, 'a'), Token(K, 'b'))`             | `ab`   | `(a b)`    |

### Root

At end of input the root is built from whatever nodes the top-level element
left on the stack:

| Nodes left | Root returned                                      |
|------------|----------------------------------------------------|
| none       | `nil`, with a `nil` error: a successful empty match |
| one        | that node, unwrapped                               |
| several    | a new seq node holding them in match order         |

Only a top-level `Optional` or `ZeroOrMore` can leave no nodes, and only
those and `OneOrMore` can leave several. A top-level `Choice` leaves whatever
its chosen alternative does.

| Grammar                   | Input  | Root        |
|---------------------------|--------|-------------|
| `Optional('a')`           | (empty)| `nil`       |
| `ZeroOrMore('a')`         | `aaa`  | `aaa`       |
| `ZeroOrMore(Seq('a'))`    | `a`    | `(a)`       |
| `ZeroOrMore(Seq('a'))`    | `aa`   | `((a) (a))` |
| `Optional('a', Seq('b'))` | `ab`   | `(a (b))`   |

The shape of the root under a top-level `Optional`, `ZeroOrMore` or
`OneOrMore` therefore depends on the input: one iteration of
`ZeroOrMore(Seq('a'))` returns the inner seq node directly, while two return a
new seq node wrapping both. Wrap the grammar in `Seq` for a root whose shape
does not vary.

## Matching

`Optional`, `ZeroOrMore` and `OneOrMore` are greedy: when several parses of
the input succeed, the one that matches elements earlier and more often is
returned.

| Grammar                                    | Input | Tree        |
|--------------------------------------------|-------|-------------|
| `Seq(ZeroOrMore(Seq('a')), Optional('a'))` | `aa`  | `((a) (a))` |
| `Seq(Optional(Seq('a')), 'a')`             | `aa`  | `((a) a)`   |

`Choice` is ordered: when several alternatives lead to a successful parse, the
earliest is returned. Alternatives run in parallel, so one that matches a
longer prefix does not prevent a later one from succeeding if the longer match
cannot complete.

| Grammar                          | Input | Tree       |
|----------------------------------|-------|------------|
| `Choice(Seq('a'), 'a')`          | `a`   | `(a)`      |
| `Choice('a', Seq('a'))`          | `a`   | `a`        |
| `Seq(Choice(Seq("ab"), 'a'), 'b')` | `ab`  | `(ab)`     |
| `Seq(Choice(Seq("ab"), 'a'), 'b')` | `abb` | `((ab) b)` |

## Errors

| Error                | Cause                                                     |
|----------------------|-----------------------------------------------------------|
| `ErrGrammarMismatch` | No parse accepts the character at the reported offset.    |
| `ErrUnexpectedEOI`   | Input ended while every remaining parse needed more.      |
| `ErrEmptyText`       | Panic at construction: `Text("")`, or `""` passed as an element. |

Offsets count runes, not bytes. Passing an unsupported element type (anything
other than a `Compiler`, `string` or `rune`) also panics at construction.
