# A tree as a source

> **Status: conversation. Nothing here is built.** This is a design reached by
> talking it through, written down so it is not lost, and it is not a contract.
> Where it states a decision, that is a decision taken in conversation and
> nothing more. `sources.md` is what a source is today and `ordering.md` is the
> comparison rules; both of those are built and this leans on them.

A tree is a sequence. That is the whole of the idea, and everything else follows
from taking it seriously.

## Why it is a source and not a view

The obvious place for expansion is the view: which nodes are open is surely
about how somebody is LOOKING at the data rather than about the data. That was
the first answer here and it is the wrong one.

`AmendedSource` already does the thing that answer says cannot be done. `Add`,
`Replace` and `Delete` are not questions — they are a caller reaching in and
changing what the sequence holds, with everyone reading it told by invalidation.
Expansion is the same shape: **a hierarchical filter**, whose marks the caller
sets, whose changes are `Added` and `Removed` for the rows that appeared and
went. That is the discipline already here, not an exception carved out of it.

So a `TreeSource` wraps a source and presents the rows that are VISIBLE.

## What it produces, and what that buys

A flat sequence, in pre-order, of the rows that are currently visible. Each
carries its depth, its path, and whether it can be expanded — three things a
tree knows and a record need not.

Everything downstream then works unchanged, and this is the point of the whole
design rather than a happy accident:

| | |
|---|---|
| `RecordCount` | how many rows are visible — a scrollbar's scale |
| `Scope.From` | a position in the flattened sequence — a thumb dragged |
| `Complete.First` | where an answer began — the calibration |
| `CachedSource` over it | runs of a tree, cached and invalidated like any other |

And it makes a tree VIEW a list view that draws indentation from a field and a
twisty from another. The alternative — a view that holds a cursor into each
expanded node and does its own flattening — needs every one of the four rows
above built again, per view, and gets none of the caching.

## Expansion is marks, and expanding everything is a state

Expanding everything must not mean iterating everything. It is a STATE.

A node's effective state is the nearest mark at or above it, and the root
default is closed. Three marks:

| | |
|---|---|
| `open` | this node's children show. Theirs follow their own marks. |
| `openAll` | this node and everything beneath, until a deeper mark says otherwise |
| `closed` | this node. Nothing beneath a closed node is reachable to be marked. |

Two rules keep the set small, and they are the same rule twice:

**A mark wipes the subtree it governs.** Closing a node discards every mark
beneath it; so does marking one `openAll`. Every mark under a new one was an
exception to a state that no longer applies.

**Setting a node to what it would inherit removes its mark rather than adding
one.** Opening a node inside an `openAll` region is not "add an open mark", it
is "drop the closed mark" — and the inherited `openAll` resumes beneath it.

So what is held is bounded by what somebody has TOUCHED since the enclosing
change, not by the size of the tree. Expanding a million rows is one mark.

It is the selection's shape again — *everything, except these named ones* — made
hierarchical. Two mechanisms with one idea behind them is worth having on
purpose, and both places should say so.

## What a child is

A **child type** is a source and a way of deriving a `Spec` from the parent
record. Most of them are one predicate:

| | | |
|---|---|---|
| its parent by key | `eq parent <the parent's key>` | an adjacency list |
| its container | `eq location <the parent's own path>` | a row that says where it LIVES |
| everything under it | `starts location <the parent's own path>` | the subtree, deliberately |

All three are expressible with what a filter already has: `OpEq` and `OpStarts`
exist. Nothing new is needed to SAY what a child is.

The second is the one to reach for, and it turns on **what the field holds**.
The name here is `location` rather than `path` on purpose, because `path` is the
word that caused this design's worst misreading: it can mean where a row LIVES
or it can mean the row's own full address, and the two behave completely
differently. A field holding the CONTAINER makes children one equality — every
row whose location is this row's own path. A field holding the row's own full
address does not, because no operator can take the parent off the front of it,
and the parent's address is not written down anywhere to compare against.

Something filesystem-shaped, two fields, nothing derived:

| `location` | `name` | its own path |
|---|---|---|
| `/` | `usr` | `/usr/` |
| `/usr/` | `local` | `/usr/local/` |
| `/usr/local/` | `bin` | `/usr/local/bin/` |
| `/usr/local/` | `share` | `/usr/local/share/` |

The children of `/usr/local/` are `eq location "/usr/local/"` — the two rows,
one equality, no depth field and no prefix arithmetic. Everything beneath it is
`starts location "/usr/local/"`, which is the same criterion with the other
operator. So `eq` gives children and `starts` gives descendants, which is the
shallow and deep distinction expressed in the criterion rather than added beside
it. A caller wanting the subtree in one question asks for it; one wanting a level
asks for that.

The field is called whatever the data calls it — `directory`, `container`,
`folder`, `thread` — and the tree is told which name it is. `location` is this
document's example and not a reserved word.

A type naming the same source as the top level makes a hierarchy nested inside
one body of records, by those same rules all the way down. A type naming a
different source grafts one body of records under another.

There is a **default child type**, and a row may carry a field naming a
different one — or naming none, which makes it a leaf whatever its siblings do.
That is what lets one tree mix kinds: a host whose children are applications,
an application whose children are windows, read out of three different places.

**A child type is a Go function from a record to a Spec, not a small language.**
Two or three constructors cover the shapes above. A language for saying this
belongs with bundles authoring trees, which is a real thing to want later and a
parser to write when something needs it rather than now.

## The path is not the identity

**A record keeps its own identity and the tree does not rewrite it.** A path is
a different thing that stands beside it, and conflating the two was the first
mistake this design made.

They are separated in the data all the time. A file's identity may be an inode
and its path `/usr/local/bin`. A message's identity is a message id and its
place in a thread is somewhere else entirely. A row's key may be a database
primary key while what makes it a child is a `parent` column. None of those
paths are keys and none of those keys are paths.

So the path is a **field**, got one of three ways:

| | |
|---|---|
| **its container, plus its name** | two fields — `location` and `name` in the table above — saying where it LIVES and what it is called, and its own path is the two joined |
| **read whole** | the record carries its own full path, materialised, in one field |
| **derived** | the tree builds it as it descends: the parent's path, the delimiter, and this row's segment — a field, defaulting to the record's key |

**The first is the best of the three**, and not only because it makes children
one equality. A row's own path can be worked out from the ROW ALONE — its
container and its name — without having descended to it. So a tree can say where
a row belongs before it has walked there, which is what a deep filter needs and
what jumping straight to a node needs.

The second suits data that already knows its whole address. The third suits an
adjacency list, where the hierarchy is edges and nothing has written a path down
at all — and there the path is only knowable by descending, which is the cost of
that shape.

**The delimiter is the caller's to choose.** `/` for something filesystem-like,
`.` for a namespace, `::`, whatever the data uses — and where the tree DERIVES a
path, choosing a delimiter the segments do not contain is what keeps the path
unambiguous. That is the answer to the ambiguity an earlier draft of this
worried about at length: it was worried about paths built out of record keys
that may contain slashes, and once the delimiter is chosen for the data the
question does not arise. A segment containing the delimiter makes a path that
means two things, and it is the caller's to avoid, exactly as a duplicate key is.

Marks are keyed by the path, which is now a string the caller's own delimiter
makes unambiguous. Nothing needs escaping and nothing needs length-prefixing.

### What identifies a row of the flattened sequence

The record's own identity, unchanged — which is unique across a tree nested
inside ONE source, because a source's keys are unique.

It is not unique where a tree grafts a second source under rows of the first:
two sources may each hold a record keyed `3`. Nor where the same record hangs
under two parents, which is a graph drawn as a tree — then one identity stands
at two positions, and a reader asking where it stands has two answers.

Neither is solved here. Where either is wanted, the path is what tells the rows
apart and the tree is told to identify rows by path instead. That is a choice a
caller makes knowing why, rather than a cost every tree pays: a path is longer
than a key, and a tree of one source never needs it.

## Filtering, shallow and deep

Two ways, and a caller says which.

**Shallow** filters the rows at each level independently. A parent that does not
match is gone and its children with it.

**Deep** keeps a parent that does not match where a descendant does, so that
what matched can be reached. The same rule applies at every level.

**A deep filter is eager, and cannot be otherwise.** "This parent has no
matching descendant" is a claim about everything beneath it, and there is no way
to make it without looking. So a deep filter over a source that holds its
records is one pass and fine; over a source that must be ASKED it means fetching
the whole tree, which is the thing lazy reading exists to avoid.

So: deep filtering is for records in hand. A source that must be asked filters
shallow, or is walked and told what that costs. This is a rule to state rather
than a limit to discover.

## What it costs

The same split as everywhere else here.

**Over records in hand** a `TreeSource` buckets them by parent once, and a
node's children are a slice. Counts are exact, positions are exact, and the
flattened order is built once and rebuilt when a mark moves.

**Over records that must be asked for** it is a question per open node, counts
are `AtLeast` while anything expanded is unexplored, and the thumb is a floor
that keeps off the bottom of its track. All of which the list already handles.

The per-open-node cost does not vanish. It moves — out of every view and into
one place where it can be solved once.

### What a mark change costs

Opening a node makes rows appear, which is `Added`, and `openAll` over a large
subtree makes a great many appear at once. Whether that is one notice or
thousands looked like a question this design would have to answer. It is two
questions, and invalidation already answers one of them.

**The order half is answered.** `Added`'s extent names the two records the new
one fell BETWEEN, and what it costs is the claim across that place — cut, or an
open one pulled back. A thousand rows appearing under one parent all fall in the
same place, so they are a thousand notices with one extent, which is the densest
case of the coalescing rule `live-data-negotiation.md` already states: points
merge into a range when the gap between them is small against the span they
cover. A reader loses its claim across that one point and keeps everything
before and after, whether one row arrived or a thousand.

**The count half is not.** `recount` is `was.Add(1)`, hard, because `Added`
names no record — the other three reasons carry `len(named)` and scale with it,
and `Added` has nothing to scale with. So an `openAll` really does cost a
thousand notices, for arithmetic and for nothing else.

**The fix is a number on `Added`, not a fifth reason.** A count on the notice —
this many appeared, between these two — is to `Added` exactly what the named
list is to the other three, and `RecordCount.Add` already takes an `n`. Nothing
about the four reasons is short of what a tree needs; one field is.

## Decisions taken

1. A tree is a `Source`, wrapping a source, presenting the visible rows flat.
2. Expansion is marks over paths, with the two wiping rules above.
3. Marks live on the SOURCE. Two views sharing a `TreeSource` share expansion,
   which is sometimes exactly right; a view wanting its own wraps its own, which
   is cheap because wrapping is all it is. On the data set instead, two data
   sets over one spec would disagree about what the sequence contains, and "the
   same three name the same sequence" is load-bearing.
4. A child type is a function from record to `Spec`, with constructors.
5. A record keeps its identity; the PATH is a separate field — its container
   plus its name, read whole, or derived by descending — with a delimiter the
   caller chooses, and field names the caller gives. Identifying rows by path
   instead is available for a tree that grafts sources or repeats a record, and
   is not the default.
6. Deep filtering is eager and is for records in hand.
7. **Expandability is a field.** A source that knows says so, and one that does
   not leaves it undefined, which reads as a leaf. Over records in hand a tree
   can work it out from its own buckets and the field is a shortcut; over
   records that must be asked it is the only way to draw a twisty without
   opening every visible row. A field that may hold a NUMBER says how many,
   which also bounds the unknown extent of an `openAll` one level at a time --
   so `undefined` or `false` is a leaf, `true` is children of unknown number,
   and a number is that many.
8. **A sort naming a field a record has not got is not refused.** It reads as
   `undefined`, which is a value with a rank, so those rows gather at the bottom
   of that level and are separated by the levels after it and by the identity
   that settles the rest. This is what serval already does — `supported` refuses
   an unknown collation and nothing else — and it matters for a tree because a
   grafted child source need not carry the field the sort names.
9. **Pre-order is built, not sorted.** It does not fall out of `CompareLevels`:
   that stops where the shorter run ends and returns 0, so `[a]` and `[a, b]`
   compare EQUAL rather than parent-before-child. A tuple per level cannot
   express a varying-depth path either. `TreeSource` orders its own sequence.

## Open questions

- **Cycles.** A child type keyed on a prefix can make a node its own descendant.
  The path is in hand, so refusing to open a node already on its own path is
  cheap — but some file managers deliberately allow it, so whether this is a
  rule or a setting is not settled.
- **Whether one tree can take a path one way from some rows and another way from
  others** — a graft whose second source says where its rows live under a first
  that only has edges. What the fields are CALLED is settled: the caller names
  them, and `location` and `name` are this document's example rather than a
  reserved spelling.

## The risk worth naming

This would be the first thing serval holds that is about **attention** rather
than about content. An amendment says a record is different; a mark says
somebody is looking at a part of the sequence. The `AmendedSource` precedent
covers the mechanism — caller-set state, changes told by invalidation — and it
is a good precedent, but the two are not quite the same kind of statement.

If that distinction ever starts to matter, it will matter here first, and the
symptom to watch for is a second reader wanting its own marks over records it
wants to share. Decision 3 is the escape hatch: wrapping is cheap, so the answer
is another `TreeSource`, not a second kind of state.
