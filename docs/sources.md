# Sources, and what a format loads into one

> **Status: built.** Records read out of a document or a delimited file,
> composed under names of their own, amended with replacements and deletions,
> and drawn from a scope at a time.
>
> `ordering.md` is the comparison and membership rules a source computes its
> sequence by, and the README is the model in brief. Nothing here says how any
> of it is written down: a `Spec` arrives as a `Spec`, and how one is spelled on
> a wire or in a file is the business of whatever brought it in.

Records are sometimes here and sometimes somewhere that has to be asked. It is
the same question either way — this filter, this sort, this scope — so `Source`
is one interface with a body of static data behind it or something across a
connection that will answer in its own time.

```go
type Source interface{ Open(spec *Spec) (DataSet, error) }

type DataSet interface {
    Read(s *Scope, out Sink) error
    Close()
}

type Sink interface {
    Ordered()
    Record(id *Value, fields Record) error
    Subset(id *Value, fields Record, has Totals) error
    Done(c Complete)
}
```

`Record` is the record entire and `Subset` is the fields that were asked for.
A source says whichever is true of what it sent: a `ListSource` says `Record`
where nothing narrowed the record and `Subset` where a field list or an
exclusion did, and a source relaying somebody else's records passes on whatever
that end claimed, making no claim of its own.

A subset also says how many members the record HAS — `Totals`, counted by name
and by position — which is what lets a later question about a field it left out
be answered without asking again. A `ListSource` counts what it holds.

**Nothing waits.** `Read` asks for a scope and returns; the records reach the
sink as they are produced — at once for a `ListSource`, whose records are here,
and as they arrive for a source that had to ask. The error is for a request that
could not be started, never for one that has not finished.

Four kinds are behind the interface, and a fifth kind is a fifth implementation
of it:

| | |
|---|---|
| `ListSource` | records here, already in memory |
| `AmendedSource` | any other kind, with replacements and deletions held over it |
| `ComposedSource` | several other kinds at once, their records under names of their own |
| `CachedSource` | any other kind, answering out of what the process already holds |
| a source of your own | records that have to be asked for, answered as they arrive |

The first holds records. The rest wrap, and answer the scope themselves out of
what their children send: an amended source asks its one child the same question
and merges what it holds of its own into the answer, and a composed source asks
all of its children and interleaves theirs. Any of them can stand in front of
any kind, including each other.

**A format is a LOADER, not a source.** `ListSource` is the engine every body of
records in memory runs on — the filter, the ordering built once per spec, the
scope walked out of it, the exact count — and what a format does is turn its own
text into rows for it. So the formats differ in what they read and in nothing
after that:

| | |
|---|---|
| `ParsePSLSource` | a PSL list, under one of the two readings below |
| `ParseDelimited` | CSV, TSV, or whatever a `Delimited` says |

A row owes the engine three things and no more: what identifies it, what one of
its fields holds, and what it holds altogether. `NewRow` is a key and a bag,
which is all a flat format needs; the PSL reading has a row of its own because
it computes its fields only when asked, reaching into a nested list at the
moment somebody names a position inside it.

`ParseDelimited` also hands back a report. A cell that contradicts its column,
or a second row claiming an identity the first already has, is a complaint and
not a refusal: the cell is left undefined or the row is dropped, the load
finishes, and the caller reads the report or ignores it. Nothing about one bad
row costs the rest of the file.

A **data set** is what a query names, seen from the end that holds the records.
It does not change: a different sort or a different filter is a different data
set, opened alongside the one it replaces and closed after it, which is what
keeps the source in use while the reader moves across.

## Several sources at once

A composed source holds a sequence of named **includes** and answers out of all
of them. Every record reaches the outer sequence under a key of its own — the
include's name, a slash, and the child's key:

```
left/0   left/1   left/note   right/0   right/1
```

**Nothing shadows anything here.** Two includes keyed the same way both keep
every record, because the name in front of the key is what tells them apart. No
two records of a composition can share a key at all.

Shadowing is what an AMENDMENT does, and it is a separate mechanism on purpose.
An amendment names one record of one child and states what stands in its place;
a composition names a whole source and keeps everything in it. So a layer stack
is a composition with an amendment over it, and the two nest either way round —
one include may be an `AmendedSource`, or an `AmendedSource` may wrap the whole
composition.

**The order is the include's name, then the child's key as the child itself
orders it** — so `many/10` follows `many/9` rather than sitting between
`many/1` and `many/2`, which is where comparing the composed key as text would
put it. Within one include the name is constant, so the outer order and the
child's own order are the same sequence.

That is what lets the answer stream. Each include delivers into a queue of its
own, and a record leaves its queue as soon as no include can still produce one
before it — which is when every include that has not finished is holding at
least one. **So what is buffered is how far the includes have drifted out of
step with each other, never the answer itself.** One record from each is the
floor, and the wait ends the moment the slowest of them speaks.

Order is claimed only where every include promised it. One that would not
leaves the merge nothing to merge on, so the records go out as they arrive, the
answer says nothing about order, and it does not stop at the shortfall either —
cutting an unordered answer at some arbitrary record would drop ones that
belong in the scope, and a superset is always allowed where a gap is not.

**The watermark is the lowest of the includes', not the highest.** Complete up
to a point means every one of them is complete up to it, so the one that swept
least far holds the claim back for all of them — and it can be no further than
the last record that actually went out.

### Identity, and the field called `key`

A composed source's identity is the include's name, a slash, and the include's
own identity. It settles what the sort leaves equal, as two levels — the name,
then the child's — so that within one include the outer order and the child's
own order are the same sequence.

**No include is ever shown an identity of this source's making.** An identity
means something only where it was made, so a scope resuming after `left/7` asks
each include from the last record *it* gave — which this source noted as it
handed that record on, and which is the same place in the merged sequence. An
include orders its own records by its own identity once the named levels are
spent, so the level this source adds is one it has anyway. Which way round it
goes is said with `Reversed`, which names no field — and `Reversed` is the
*only* thing that turns it over, because identity is not something a sort can
name.

**`key` is a field like any other.** Whatever an include exposes under that
name is its own business: a `Whole` PSL reading puts its own key there as a
convenience for sorting and display, a `Members` reading has whatever member
was called that, and a source that has to be asked has whatever it sends. A
sort or a filter naming `key` goes down to every include untouched and asks
each of them about *its* field. It says nothing about the identity this source
made.

**`OpID` is how identity is asked about.** It matches against a set of them, the
way `in` matches a field against a set of values, and it names no field:

```
{ id "left/1" "left/note" }
```

An identity says which include made it, so the includes none of them name are
never opened at all:

| the filter | `left` | `right` |
|---|---|---|
| `id "left/1"` | asked `id "1" 1` | *not opened* |
| `id "left/1" "right/0"` | asked `id "1" 1` | asked `id "0" 0` |
| `id "nobody/1"` | *not opened* | *not opened* |
| `eq key 1` | asked `eq key 1` | asked `eq key 1` |

It goes down as a set over both spellings of the text, because which of a
number, a name and a string an include identifies its records by is its own
business and the text between the slashes says nothing about it. A question
narrow enough to miss would lose the record, and that is the one thing that
cannot happen. And because the split takes the *first* slash, it composes:
`id "nested/deep/1"` sheds one name per layer and reaches the innermost source
as `id "1" 1`.

What cannot be put in an include's terms is **dropped on the way down and
settled here** instead, where the identity is in hand. So an include is always
asked a question that admits at least every record the outer filter does.
Reading the answer here is three-valued: `id` answers yes or no, a predicate on
any field answers *nothing at all* — the include was asked that one and applied
it already — and a record is dropped only on a definite no. Saying nothing is
not saying no, which is what keeps a negation over an ordinary field from
taking out a record the include had just vouched for.

An include is also asked for **the fields this source sorts by**, on top of
whatever the query asked for. The merge reads a record's sort values back out
of the fields it was sent, so a sort on a field the query did not ask for would
arrive as undefined for every record — and the merge would trust each include's
arrival order over an order it could not see.

One thing it refuses: an **include name holding a slash**, which is what tells
a name from an identity.

## A PSL list: two spaces, one sequence

A PSL list holds two independent collections: an ordered sequence of items, and
a keyed map outside that sequence entirely. **Both are records.** An item's key
is its index and a keyed member's key is its name, and because an integer ranks
below a string in the comparison core, the two fall into one total order with no
rule of their own — the items first, in their order, then the names.

```
(
  ("README.md", size: 2048),
  ("build.sh", size: 310),
  notes: "a bare string, with no members at all",
  _bundle: (key: "figaro", author: "Jeffrey R. Day")
)
```

Four records, keyed `0`, `1`, `"notes"` and `"_bundle"`. The reading is flat:
`_bundle` is a record like any other here, because it is a key in a PSL list and
only a layer above this one knows to look at it. `NewPSLSourceExcept` is how
that layer keeps such a member out of the records without this one having to
know what it means.

## Naming what is in a record

Records are not all the same shape. Some are lists of named fields, some carry a
leading label, and some are a bare string with no members at all. Two readings
name their contents, and the source is told which when it is made.

**`Whole`** exposes the record entire.

| | |
|---|---|
| `key` | its key: the index it stands at, or the name it is filed under |
| `value` | its value entire, whatever kind that is |
| `.size` | the member called size |
| `.0` | the item at position 0 |

Nothing can shadow anything. A member called `key` is `.key`, and the record's
key is `key`. Within the dot, digits mean a position and anything else means a
name.

**`Members`** exposes the members alone, under their own names: `size`, not
`.size`. It is the shorter reading and it suits a source whose records are all
lists of named fields, which is most of them. It is deliberately not complete:
the record's key and its value cannot be named, positions cannot be named at
all, and a member called `key` is neither reachable nor sent.

**A leading dot is part of the name**, not a syntax this library reads. A field
is a string wherever one is written — a sort level, a filter predicate, a field
bag — and `.size` is that string. A source whose records carry their own
contents needs the distinction; one that does not need it never writes a dot.

```go
Sort: []SortLevel{{Field: "key", Level: Level{Descending: true}},
                  {Field: ".name", Level: Level{Collation: "natural"}}}
```

## What a record's values can be

| in the PSL | as a value |
|---|---|
| a string | a string |
| a whole number | an integer, exact, however large |
| a fractional number | a float |
| `true`, `false`, `nil` | the words |
| a bare word | a symbol |
| a nested list | its own members, which is the unordered rank |
| absent | `undefined`, which is a guarantee that the record has not got it rather than a silence — and not counted in the totals |

A nested list's contents are read the `Whole` way whatever the reading, because
a position inside one has no other spelling.

**A bare word is a symbol.** PSL writes `kind: text` and `kind: "text"`
differently and so does this, so the two reach a filter as the two different
questions they were written as: `eq .kind text` asks about the identifier and
`eq .kind "text"` asks about the four characters. `Key` and `Equal` owe nothing
to any grammar — text and bytes are different kinds, and `3` and `3.0` are
different numbers.

## Records that are not uniform

Most of what a mixed source needs is already in the comparison core, and needs
nothing added:

- **A field one record has and another has not** is `undefined`, which is a
  value with a rank rather than an error, so `eq .thumbnail undefined` answers
  for it. `has` and `lacks` ask the same question of any field, including one
  holding a list, which cannot be compared at all.
- **A field that holds a different type per record** is grouped by rank —
  `undefined < nil < false < true < number < symbol < string < bytes <
  unordered` — and `lt` and `gt` still answer.
- **A text predicate against a value that is not text** is `false`, not an
  error.
- **A field holding a nested list** cannot be compared against anything, so
  every comparison naming one is `false`. `has .tags` and `lacks .tags` ask
  whether the field is there, whatever it holds.

One consequence is worth knowing before it surprises somebody: **`undefined`
sits below every number**, so a filter of `lt .size 1000` holds every record
that has no size at all. A filter that means *has a size, and it is under a
thousand* is two predicates and says both.

## Drawing a scope

Stating the sequence runs the filter over every record and sorts what survives,
once. A scope is then a binary search for the boundary and a walk forward as
far as the scope is long, so reaching the last screenful of a long list costs
what reaching the first one costs.

The sort tuples are kept beside the rows rather than recomputed, because
extracting a field is a map lookup and a conversion, and a sort that did it per
comparison would read the data `n log n` times instead of once. Orderings are
cached on the spec that names them, so two data sets over one sequence share
the work and going back to a column somebody clicked before is free.

A scope emits every record in `(After..Until]` and then carries on past `Until`
only while it is still short of `Count` — which is what the far end has to merge
against. What ends it is a watermark, or `exhausted` where the sequence ran out.

## What it costs

Measured on 100,000 records, each a list of three named members, over 200
iterations (`0_psl_bench_test.go`):

| | |
|---|---|
| reading the file | **2.5 s** — `pawscript.ParsePSL`, once |
| stating the sequence | **58 ms** — one filter pass and one sort, once per spec |
| a scope of 30 at the start | **14.0 µs** |
| a scope of 30 at the end | **21.8 µs** |

The last two are the pair that matters: **a scope at the far end of a hundred
thousand records costs about half again what one at the near end costs** — not
three thousand times more, which is what walking to the boundary would have
cost. What separates them is the seventeen comparisons the search makes, each
of them a natural collation over a string, and they are the whole of the
difference between the two ends of the sequence.

The parse dominates everything else by a factor of forty, and none of it is
here — it is PawScript reading the text. A source large enough for that to
matter is a source worth holding parsed rather than re-read.
