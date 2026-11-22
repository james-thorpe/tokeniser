# parser

## Result tree

`Run` returns a tree of `*node` values with two kinds:

| Kind      | Meaning                                             |
|-----------|-----------------------------------------------------|
| `nkChars` | Leaf. `lexeme` holds the matched characters.        |
| `nkSeq`   | Interior. `children` holds the nodes it collected.  |

The examples below render chars nodes as their lexeme and seq nodes as a
parenthesised, space-separated list of children.

### Nodes contributed by each element

| Element                  | Contribution                                                 |
|--------------------------|--------------------------------------------------------------|
| `Char`, `Range`, `Text`  | Characters, merged into the adjacent chars node (see below). |
| `Seq`                    | Exactly one seq node holding its elements' nodes.            |
| `Optional`               | Its elements' nodes when matched, nothing when skipped.      |
| `ZeroOrMore`             | Its elements' nodes once per iteration, nothing for zero.    |
| `OneOrMore`              | Its elements' nodes once per iteration; at least one.        |

`Optional`, `ZeroOrMore` and `OneOrMore` never add a node of their own. Their
elements' nodes become children of the enclosing `Seq`; wrap the elements in a
`Seq` to keep them grouped.

| Grammar                                  | Input  | Tree            |
|------------------------------------------|--------|-----------------|
| `Seq('x', Optional('a'), 'y')`           | `xy`   | `(xy)`          |
| `Seq('x', ZeroOrMore(Seq('a')), 'y')`    | `xaay` | `(x (a) (a) y)` |
| `Seq('x', Optional('a', Seq('b')), 'y')` | `xaby` | `(xa (b) y)`    |
| `Seq(Optional('a'))`                     | (empty)| `()`            |

### Character merging

A matched character is appended to the chars node on top of the stack if
there is one; otherwise a new chars node is pushed. Adjacent characters
therefore form a single leaf regardless of which elements matched them. Only a
`Seq` boundary separates them.

| Grammar                         | Input   | Tree        |
|---------------------------------|---------|-------------|
| `Text("abc")`                   | `abc`   | `abc`       |
| `Seq('a', Range('0', '9'), 'b')`| `a5b`   | `(a5b)`     |
| `Seq("ab", Optional("cd"), 'e')`| `abcde` | `(abcde)`   |
| `Seq('a', Seq('b'), 'c')`       | `abc`   | `(a (b) c)` |

### Root

At end of input the root is built from whatever nodes the top-level element
left on the stack:

| Nodes left | Root returned                                      |
|------------|----------------------------------------------------|
| none       | `nil`, with a `nil` error: a successful empty match |
| one        | that node, unwrapped                               |
| several    | a new seq node holding them in match order         |

Only a top-level `Optional` or `ZeroOrMore` can leave no nodes, and only
those and `OneOrMore` can leave several.

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

| Grammar                                  | Input | Tree        |
|------------------------------------------|-------|-------------|
| `Seq(ZeroOrMore(Seq('a')), Optional('a'))` | `aa`  | `((a) (a))` |
| `Seq(Optional(Seq('a')), 'a')`           | `aa`  | `((a) a)`   |

## Errors

| Error                | Cause                                                     |
|----------------------|-----------------------------------------------------------|
| `ErrGrammarMismatch` | No parse accepts the character at the reported offset.    |
| `ErrUnexpectedEOI`   | Input ended while every remaining parse needed more.      |
| `ErrEmptyText`       | Panic at construction: `Text("")`, or `""` passed as an element. |

Offsets count runes, not bytes. Passing an unsupported element type (anything
other than a `Compiler`, `string` or `rune`) also panics at construction.
