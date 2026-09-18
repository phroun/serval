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

| | |
|---|---|
| a field equal to the parent's key | `eq parent <the parent's key>` |
| a field equal to the parent's path | `eq parent <the parent's path>` |
| a field beginning with the parent's path | `starts parent <the parent's path>` |

All three are expressible with what a filter already has: `OpEq` and `OpStarts`
exist. Nothing new is needed to SAY what a child is.

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

So the path is a **field**, got one of two ways:

| | |
|---|---|
| **read** | the record carries its whole path, materialised, in a field the tree is told to read |
| **derived** | the tree builds it as it descends: the parent's path, the delimiter, and this row's SEGMENT — a field, defaulting to the record's key |

The first suits data that already knows where it lives. The second suits an
adjacency list, where the hierarchy is edges and nothing has written a path
down.

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

## Decisions taken

1. A tree is a `Source`, wrapping a source, presenting the visible rows flat.
2. Expansion is marks over paths, with the two wiping rules above.
3. Marks live on the SOURCE. Two views sharing a `TreeSource` share expansion,
   which is sometimes exactly right; a view wanting its own wraps its own, which
   is cheap because wrapping is all it is. On the data set instead, two data
   sets over one spec would disagree about what the sequence contains, and "the
   same three name the same sequence" is load-bearing.
4. A child type is a function from record to `Spec`, with constructors.
5. A record keeps its identity; the PATH is a separate field, read or derived,
   with a delimiter the caller chooses. Identifying rows by path instead is
   available for a tree that grafts sources or repeats a record, and is not the
   default.
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

- **What a mark change costs.** Opening a node is `Added` for each row that
  appeared; `openAll` on a large subtree is a great many. Whether that is one
  notice about the sequence or one per record is the same question
  `live-data-negotiation.md` asks about everything else, and the answer should
  be the same answer.
- **Cycles.** A child type keyed on a prefix can make a node its own descendant.
  The path is in hand, so refusing to open a node already on its own path is
  cheap — but some file managers deliberately allow it, so whether this is a
  rule or a setting is not settled.
- **What a row's path field is called**, and whether the same tree can read a
  path from some rows and derive it for others — a graft whose second source
  materialises paths under a first that does not.

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
