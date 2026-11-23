# ADR 0001: Per-step machine deduplication

## Status

Accepted

## Context

`Run` in `parser/parser.go` runs every candidate parse at the same time, one
machine per candidate. All machines advance together, one rune at a time.
Within a step, machines are processed in priority order. Each machine follows
its preferred path all the way before the machine created by its fork runs.

Without a limit on how many machines can exist, two problems occur.

### Unbounded forking on empty-matching loops

A loop whose body can match empty input forks forever without consuming a
rune. `ZeroOrMore(Optional('a'))` compiles to:

```
0: opFork 9        ; exit the loop
2: opFork 7        ; skip the optional 'a'
4: opChar 'a' 'a'
7: opFork 2        ; back-edge
9: opEof
```

On input `"b"`, the machine at pc 2 queues a fork at pc 7. That fork takes the
back-edge to pc 2 and queues another fork at pc 7, and so on. The step never
ends.

### Exponential machine growth

`OneOrMore(Choice('a', 'a'))` on `"aaaa"` matches both alternatives on every
rune. Both resulting machines reach the same back-edge fork, and each forks
again. Without deduplication, the machine count reaches 2ⁿ after n runes.

## Decision

Each pc is entered at most once per step. `entered[pc]` records the step
(rune index + 1) at which that pc was last entered. A machine that arrives at
a pc already entered during the current step is dropped.

This is correct while a machine's future depends only on its pc. Arrivals at a
pc occur in priority order, so the first arrival has the highest priority. A
later arrival at the same pc can produce only the same matches with lower
priority, so dropping it never changes the result.

Deduplication also bounds the work done per step to O(len(prog)).

## Consequences

### Rule calls

With `opCall` and `opRet`, a machine's future depends on both its pc and its
chain of return frames. Deduplication by pc alone then drops valid parses.

For rule `R = 'a'`, grammar `Choice(Seq(call R, 'b'), Seq(call R, 'c'))` and
input `"ac"`:

1. During step 0, the first alternative reaches R's `opChar 'a'` with a frame
   that returns to `'b'`.
2. The second alternative reaches the same pc with a frame that returns to
   `'c'`. That machine is dropped.
3. During step 1, only the machine expecting `'b'` remains, and the parse fails
   with `ErrGrammarMismatch`.

The same loss occurs at the pc of a rule's `opRet` whenever two callers return
during the same step.

Correct deduplication when rules are called requires:

- **A key of (pc, frame).** Frames must be reused, looked up by
  (return pc, parent frame), so that equivalent call stacks share one frame
  object. Without that reuse, identical stacks never compare equal and growth
  becomes exponential again.
- **An explicit left-recursion guard.** Each recursive call creates a new
  frame, so a key of (pc, frame) never repeats under left recursion and
  forking becomes unbounded. Each frame records the step at which it was
  created. `opCall` walks the parent chain and drops the machine when it finds
  a frame with the same return pc and the same step. Such a frame is a call
  site already entered at the current input position.

### Left recursion

Deduplication by pc alone stops left recursion from forking forever. The
recursive `opCall` reaches its own pc again during the same step and is
dropped. A left-recursive rule therefore fails to match rather than hanging.
