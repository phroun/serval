# Ordering and membership

> **Status: settled.** These rules are implemented by `compare.go` and
> `match.go`, and case by case by `0_match_test.go` and — for the ordering —
> by a corpus that the Go, C and Python implementations all answer.

A sequence's records can come from more than one place at once: some held here,
some known only to something else that has to be asked. Neither side can hand
the other its whole set, so each orders and filters its own records and the
results are folded together.

That only works if both compute **the same order** and **the same membership**
from the same descriptor, without conferring. So the rules have to be exact,
small
enough to implement twice, and free of anything whose answer depends on the
machine, the locale or the library version.

Nothing here says how any of it is written down. A descriptor arrives as a
`DataSetDescriptor`; how one gets spelled on a wire or in a file is the business
of whatever brought it in.

## The comparison core

Sorting and filtering are the same comparison rules used two ways. Define
comparison once and both follow.

### Ranks

A value's **kind** decides its rank before anything in it is looked at:

```
undefined < nil < false < true < int|float < symbol < text < bytes < unordered
```

`int` and `float` share one rank and interleave by value, so `2` and `2.0` sort
together rather than into separate groups. (They are still not the *same
value* — see `Equal` — but they have no order between them beyond their
magnitudes.)

The last rank holds every value with no order of its own, which is a list. All
of them compare equal, and the sort's final level settles them. Nothing here
pretends a list has a position it does not have.

**A value that is not there at all is `undefined`**, at the bottom of the order:
a nil `*Value`, or a field the record has not got. That is a value with a rank
rather than an error, which is why presence needs no special comparison.

### Within a rank

| rank | compared by |
|---|---|
| `undefined`, `nil`, `false`, `true` | equal to themselves; the rank is the whole answer |
| `int`, `float` | mathematical value. Integers compare as integers and widen to float only when one side is genuinely a float, so an id or a nanosecond timestamp past 2^53 still compares exactly |
| `symbol` | rune by rune, exact, **no collation**: a symbol is a name, and folding its case or reading digit runs inside it would invent meaning it does not have |
| `text` | rune by rune, under the level's collation |
| `bytes` | unsigned byte comparison |

### Collations

A collation belongs to the text rank alone.

| | |
|---|---|
| `exact` | codepoint order, rune by rune. The default |
| `fold` | ASCII `A`–`Z` mapped to `a`–`z`, every other rune untouched, then `exact` |
| `natural` | runs of ASCII digits compared as numbers, everything else `fold` |

`natural` in full, so two implementations agree: walk both strings together; at
a position where both have a run of ASCII digits, take the maximal run from each
and compare them as numbers with leading zeros ignored; if they are equal in
value, the run with fewer leading zeros sorts first; anywhere else compare one
rune against one rune under `fold`. A run too long to hold in an integer is
compared by its length after leading zeros are stripped, then rune by rune —
which is the same answer without needing arbitrary precision.

**Locale-aware collation is deliberately absent.** Turkish dotless i, umlaut
placement, the Unicode collation algorithm: none of it is reproducible in twenty
lines in every language an implementation might be written in, and a near-miss
does not fail — it produces a wrong fold that nobody notices. Anything needing
its own locale order computes a sort field and sorts on that, which is exact and
needs no agreement at all.

## A sort

An ordered run of levels. Each names a field, and may run descending and — for
text — under a collation.

**A level settles only what the levels above it left equal.** `desc` turns over
its own level's answer and nothing else.

**The caller appends the record's identity as a final level.** Two records equal
on every level a sort names must still have an order, or "the record after this
point" names more than one place.

**Reversed is the exact mirror.** Reading a sequence from its end turns over
every level *including* the identity level the sort does not write — which is
what makes it a mirror rather than a third sequence. `desc` on a named level
turns that one over and leaves the records it ties facing the way they were.

There is no `nulls first` knob: `undefined` sits at the bottom of the rank, and
descending lifts it to the top along with everything else.

## A filter

A tree. Each node is a predicate over one field, a test of a record's identity,
or an `and`/`or`/`not` over other nodes.

| | |
|---|---|
| `eq` `ne` `lt` `le` `gt` `ge` | comparison, by the core above |
| `in` | one field against a set of values |
| `contains` `starts` `ends` | text only, collation-aware |
| `has` `lacks` | whether the record carries the field at all; no value |
| `id` | the record's identity against a set of them; **names no field** |
| `and` `or` `not` | over other nodes |

**The text ops are text's and bytes' alone.** A number, a symbol and a boolean
have no inside for a run of characters to sit in, so a field of any other kind
is `false` rather than being rendered into text to compare. Text and bytes do
not mix either, being different kinds and not one kind written two ways. Bytes
take no collation, being not text; and under a text op `natural` reads as
`fold`, because a digit run's numeric value says whether one string sorts before
another and nothing at all about whether it sits inside it.

**A node with children is an AND wherever one appears**, `not` included, so a
`not` over two predicates negates both together. With one predicate inside,
which is how a negation is nearly always written, the two readings agree. **An
operator given nothing is its own identity**: an empty `and` holds and an empty
`or` does not.

**A symbol and text are different values**, so a filter needs no type
annotations: what the value IS says what is being asked.

**A comparison takes a simple value**, and the last rank is not one. A field
holding a list cannot be compared against anything, so **every comparison naming
one is false**.

That is a rule the filter has and the sort does not. The comparison core calls
all such values equal, which is exactly what a sort needs: they tie, and the
identity settles them. A filter inheriting it would answer *yes* to `eq tags …`
for any record carrying any tags at all, having looked inside nothing — a false
positive, in the direction a filter should never fail. It cannot answer, so it
does not admit.

**`has` and `lacks` are how presence is asked**, and they take no value. They
work whatever the field holds, which is the point: comparing against `undefined`
still answers for a field holding a simple value, but it cannot serve a field
holding a list, and presence is not a comparison.

**`id` is not a field.** A record's identity travels beside its fields rather
than among them, so `id` takes identities and names nothing, and a record that
happens to carry a field called `key` is answering an ordinary question about an
ordinary field. A record with no identity matches no set of them.

## Agreement is settled at open, not discovered later

A sequence's sort and filter are stated once, when it is opened. A source that
cannot honour them exactly — a field it does not have, an op it does not
implement, a collation it does not carry — **refuses to open**.

A refusal is recoverable and says what is wrong. An ordering that is quietly a
little different corrupts every answer after it, and looks like data.
