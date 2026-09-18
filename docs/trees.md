# A tree as a source

> **Status: being built.** `marks.go` is the expansion, `treepath.go` is
> `Standing` — the three readings — and `treechild.go` is `ChildType` and its
> criteria. `TreeSource` itself is not written yet, so the sections about
> flattening, counting and scopes are still conversation. Where building changed
> a decision, it says so and the decision has been rewritten rather than
> annotated. `sources.md` is what a source is, `ordering.md` is the comparison
> rules, and `census.md` is what decision 7 leans on.

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

A node's effective state is the nearest mark at or above it. A node nobody has
spoken about is closed; the ROOT is the exception and shows its children, because
a tree's top level is visible without anybody having said so. Three marks:

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

Building that found it is not `state == inherited`, which is how this was first
written. `open` is not `openAll`, so an exact comparison keeps a mark exactly
where the rule says to drop one. What is being asked is whether the inherited
state ALREADY DOES what is being asked for, and an `openAll` already does what an
`open` asks. Its consequence is real and is named rather than hidden: *open
exactly one level, inside an `openAll` region* is a state the verb cannot
express, because it is spent saying the thing above — which is wanted constantly,
where this has not been wanted at all.

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
| everything under it | that, **or** `starts location <that path, and a delimiter>` | the subtree, deliberately |

All three are expressible with what a filter already has: `OpEq`, `OpStarts` and
`OpOr` exist. Nothing new is needed to SAY what a child is.

**The third is two predicates and not one**, which is not what it looks like and
is what building it found. A direct child's location is the parent's path
EXACTLY, with no delimiter after it — so a prefix test with the delimiter on
misses every child and catches only what is deeper, while one without it drags
`/usr/locally` into `/usr/local`. Descendants are the two halves together, each
doing exactly the job it was described as doing.

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
| `/` | `usr` | `/usr` |
| `/usr` | `local` | `/usr/local` |
| `/usr/local` | `bin` | `/usr/local/bin` |
| `/usr/local` | `share` | `/usr/local/share` |

The children of `/usr/local` are `eq location "/usr/local"` — the two rows, one
equality, no depth field and no prefix arithmetic. Everything beneath it is that
same predicate **or** `starts location "/usr/local/"`, the first half being the
children and the second everything below them. So the shallow and deep
distinction is expressed in the criterion rather than added beside it: a caller
wanting the subtree in one question asks for it, one wanting a level asks for
that, and the deep one is the shallow one with a second half.

The field is called whatever the data calls it — `directory`, `container`,
`folder`, `thread` — and the tree is told which name it is. `location` is this
document's example and not a reserved word.

### Trailing delimiters are not assumed

The table writes `/usr/local` and not `/usr/local/`, and data that writes the
other way is just as common. **The tree assumes neither**, and both places it
could have are places it must not.

**Joining** a container and a name puts the delimiter between them and does not
double one already there. So `/usr` and `local` join as `/usr/local`, `/usr/`
and `local` join as `/usr/local` as well, and the root spelled `/` does not
produce `//usr`. One rule reads both conventions, so nothing has to be declared.

**The prefix question is where it bites, and the tree appends the delimiter
itself.** The deep half of the descendants criterion is `starts location
"/usr/local/"` and NOT `starts location "/usr/local"`, because the second also
matches `/usr/locally` — a sibling whose name begins with the same letters,
dragged into a subtree it has nothing to do with. The delimiter is what makes a
prefix a boundary instead of a spelling, and since the data may not have written
one, appending it is the tree's job.

That appended delimiter is also exactly why the criterion needs its other half:
a direct child's location ends where the parent's path ends, so the bounded
prefix excludes every one of them. Equality needs no such care — `eq` compares
the whole of the field, so the path as the data spells it is right as it stands —
which is what makes the children's predicate reusable as the first half.

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
| **read whole** | the record carries its own full path, materialised, in one field, spelled however it is spelled |
| **derived** | the tree builds it as it descends: the parent's path joined with this row's segment — a field, defaulting to the record's key |

**The first is the best of the three**, and not only because it makes children
one equality. A row's own path can be worked out from the ROW ALONE — its
container and its name — without having descended to it. So a tree can say where
a row belongs before it has walked there, which is what a deep filter needs and
what jumping straight to a node needs.

The second suits data that already knows its whole address. The third suits an
adjacency list, where the hierarchy is edges and nothing has written a path down
at all — and there the path is only knowable by descending, which is the cost of
that shape.

**But the third is the one that always works, and that is worth saying the other
way round: the first two are OPTIMISATIONS.** A tree can always build a path as
it descends, whatever the data is shaped like. What reading it whole or joining a
container and a name buys is knowing a node's position WITHOUT having descended
to it — which is what jumping straight to a node needs and what a deep filter
needs, and which is the only thing the other two buy. Derived is the mechanism;
the others are shortcuts past it.

**Which reading applies is the CHILD TYPE's, not the tree's.** A child type
already names a source, and how a row's position is read is a property of that
source rather than of the tree holding it. Treating it as one setting for the
whole tree was an accident of writing the single-source case down first.

**The delimiter is the caller's to choose.** `/` for something filesystem-like,
`.` for a namespace, `::`, whatever the data uses — and where the tree DERIVES a
path, choosing a delimiter the segments do not contain is what keeps the path
unambiguous. That is the answer to the ambiguity an earlier draft of this
worried about at length: it was worried about paths built out of record keys
that may contain slashes, and once the delimiter is chosen for the data the
question does not arise. A segment containing the delimiter makes a path that
means two things, and it is the caller's to avoid, exactly as a duplicate key is.

**A mark is keyed by whatever names a node's POSITION** — which is the path
where a path is what does that, and the record's own identity where the identity
already does it. The next section is when that holds, and the section after it is
the whole shape of tree where it always does.

Where the key is a path, the caller's own delimiter is what makes it
unambiguous: nothing needs escaping and nothing needs length-prefixing. Joining
and deriving both produce one spelling, so a node has one key; a path READ WHOLE
is whatever the record says, and data that writes `/usr/local` on one row and
`/usr/local/` on another has two names for one node and will hold two marks.
That is a property of the data rather than something the tree can mend, and it is
the cost of the reading that does not build the path itself.

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

**Revisits are the third case**, and they are why this is stated as a condition
rather than as a default. With `Revisits` above nought the same record stands at
two positions on purpose, so its identity no longer names one of them — and
identity-keyed marks then mean that opening one lap opens every lap. Sometimes
that is exactly what somebody wants to see and sometimes it is nonsense, so a
tree that allows revisits AND wants the laps to open independently is a tree that
keys by path. It is the same trade as the other two, arrived at from a different
direction.

## An adjacency list needs no paths at all

A table of `id` and `parent`, integers both, with nothing anywhere that looks
like an address. This is the commonest hierarchy in a database and it wants none
of the machinery above.

| | |
|---|---|
| its children | `eq parent <the parent's id>` |
| the top level | the caller's own spec — `eq parent undefined`, or `0`, or `-1`, or whatever that table means by a root |

That is the whole of the configuration. There is no delimiter to choose, no
field holding an address, no joining and no prefix, and the `starts` criterion
never comes up because descendants are not a string question here.

**The one place the path was load-bearing is the mark key, and the identity
serves.** A record in a pure adjacency list has one parent, so it stands at
exactly one position, so its identity names that position — which is the
condition the section above already states for row identity, asked once and
answering both. Marks are keyed by `id`. Nothing is built, nothing is
concatenated, and no two spellings of one node can arise, because there are no
spellings.

Everything else carries over untouched, and it is worth being explicit that none
of it was secretly about paths:

| | |
|---|---|
| **cycles** | already counted on identity rather than on the path, for reasons of its own |
| **pre-order** | already built rather than sorted, out of the buckets |
| **expandability** | already a field, and a `parent` table often has the child count in one |
| **deep filtering** | already eager and for records in hand — bucket by `parent`, mark upwards from what matched |
| **counts, `From`, `Complete.First`** | already read off the flattened order, which exists either way |

**So a path is a reading, not a requirement.** It is what data shaped like an
address offers a tree, and it buys one real thing — knowing where a row belongs
without having descended to it — which an adjacency list cannot offer and does
not need, because it is in hand or it is walked.

## Mixed descent, and what a graft costs

One tree, several shapes: addresses at the top, an adjacency list beneath,
another source grafted under that. Everything above says how each of them works
on its own. What is left is what happens where they MEET, and most of it is
already decided somewhere else — which is the useful finding, because a graft
turns out to need no mechanism of its own.

**Capability degrades at the boundary, not for the tree.** A reading belongs to
the child type, so a tree that is addresses at the top and edges beneath can
still name a node in its upper reaches without walking to it, and cannot beneath.
The weakest link governs what is possible BELOW it and nothing above. That is the
right answer rather than the convenient one: the alternative is a tree that drops
to its worst source's capability everywhere, which would punish exactly the
mixture this design exists to allow.

**Two things must be qualified by where the graft is, and they are the same
thing twice.** An identity is unique within a source and not across sources, so
both the MARK KEY and the REVISIT COUNT have to say which source they mean. That
is to say they are key-paths — which is why the derived reading is the mechanism
rather than a fallback for odd data.

**Expandability at a graft is a count, not a read.** Decision 7 puts the field on
the parent row, and across a graft that asks a row in one source to know
something about another. Nobody will author that correctly. But the child type
produces a `Spec`, and `CountOf` answers a Spec without reading a record: `Open`,
count, `Close`. So three degrees, in the spirit of blank, placed and filled:

| | |
|---|---|
| the field | where the row can say, which is free |
| a count on the child spec | where the source can count — exact over records in hand |
| unknown | `CountOf` answers `Unknown()` for anything that is not `Counting`, so the twisty is drawn and the answer found on opening |

A count per visible row is a real cost over a source that must be asked, and
`docs/census.md` is how a whole page of them becomes one question.

**A child type may change the SORT, and this is free.** It produces a whole
`Spec`, and a Spec is a source, a filter and a sort. Windows in z-order under
applications in name order needs nothing added. Worth writing down only because
a capability nobody notices gets reinvented.

**Decision 8 was written for this case.** A sort naming a field a grafted source
has not got reads `undefined` rather than being refused — which is what a graft
does to a sort every time, and the reason that decision is load-bearing rather
than a nicety.

**A child-type name nothing is registered under reads as a leaf.** Same posture
as the sort: one bad row must not break a tree, and over-refusing here would let
a single mistyped field empty a view.

**One data set per open node, per graft.** They are small, and closing a node
closes its set — so the two mark-wiping rules release resources as well as
keeping the mark set small, which is the better reason for them.

**What is deliberately NOT per graft:** `Revisits`. The budget says whether the
data is a graph, a tree that is a graph in one arm and not in another is a
distinction nothing has yet wanted, and making it per graft means re-running the
cannot-hang argument per subtree instead of once. One budget, one argument.

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

## Cycles are a small budget

A child type keyed on a prefix can make a node its own descendant. Symlink loops
do it, a `parent` column with a bad row does it, and a graft that points back at
its own top does it on purpose.

**It is a setting, because the two behaviours are both right.** A tree that
refuses says the data is a tree and means it. A tree that allows says the data
is a graph and showing the loop once is how somebody SEES it — which is what
several file managers do deliberately, and the reason this is not a rule.

**`Revisits`, 0 to 3, and 0 is the default.** It is how many times a node may
appear again beneath itself on one root-to-leaf path. Zero refuses, so a node
already standing on its own path is not expandable and draws no twisty. One
shows the loop, which is the point of allowing it at all. The cap is three
because the reason to allow any is to make the cycle visible and nobody has ever
needed a fourth lap to see one; a value above it is clamped rather than refused,
the way a count is.

**Counted on the record's IDENTITY, never on the path.** A cycle appends a
segment each time round — `/a/b/a/b/` — so every lap has a path nobody has seen
before while it is the same record every time. The path is the thing GROWING and
cannot be what notices. This matters most in exactly the case that tempts the
other answer: a tree told to identify its rows by path still counts revisits on
the underlying record, because the tree knows both and only one of them is the
same thing twice.

### Why it cannot hang

The budget makes every path finite. That alone is not enough — a bounded path
can still enclose a colossal number of rows — so it is worth saying which
operations were ever at risk.

**Reading never was.** A scope asks for a count and the tree produces that many
visible rows; a cycle makes the sequence long, not the walk unbounded. Same for
`From`, which is a bounded descent to a position. This falls out of scopes being
what they are and needs nothing added.

**Counting is where it bites, and `AtLeast` is the answer already here.** A tree
that has taken a revisit and has not walked the whole thing reports a floor
rather than a figure, exactly as one over records that must be asked for does.
The thumb then keeps off the bottom of its track, which the list already handles.
A tree does not walk a cycle to the end in order to answer how many rows there
are, because it is allowed not to know.

**Deep filtering is the one that has to be refused**, and it already is by a
rule stated for another reason: a deep filter is eager, so it is for records in
hand. Over records in hand with revisits allowed the eager pass is bounded by the
path rule and may still be large, and that is the caller's to know — it is the
same eagerness, carrying the same warning, and no new one.

## Decisions taken

1. A tree is a `Source`, wrapping a source, presenting the visible rows flat.
2. Expansion is marks, with the two wiping rules above, keyed by whatever names
   a node's position — the record's identity where that already picks out one,
   and the path where it does not.
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
   is not the default. **No trailing delimiter is assumed**: joining does not
   double one already there, and the prefix question has the tree append one,
   because `starts location "/usr/local"` would drag in `/usr/locally` — and
   descendants are consequently TWO predicates, the children's equality or that
   bounded prefix, a direct child's location ending exactly where the parent's
   path ends.
   **And a path is optional entirely** — an adjacency list of `id` and `parent`
   is a tree with no delimiter, no address field and no string built anywhere.
   **The reading belongs to the CHILD TYPE**, because a child type names a source
   and a reading is a property of a source; derived is the mechanism and the
   other two are shortcuts past it, so capability degrades at a graft and not for
   the whole tree.
6. Deep filtering is eager and is for records in hand.
7. **Expandability is a field, or a count.** A source that knows says so, and one
   that does not leaves it undefined. A field that may hold a NUMBER says how
   many, which also bounds the unknown extent of an `openAll` one level at a
   time -- so `undefined` or `false` is a leaf, `true` is children of unknown
   number, and a number is that many. Where the row cannot say — which is every
   graft, a row in one source having no business knowing about another — the
   child type's `Spec` is counted rather than read, and `CountOf` answering
   `Unknown()` is the third degree: draw the twisty and find out on opening. A
   census turns a page of those counts into one question.
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
10. **Cycles are a `Revisits` setting, 0 to 3, defaulting to 0**, counted on the
   record's identity rather than on the path, and clamped rather than refused.
   Nothing hangs, because reading is bounded by the scope and counting is allowed
   to answer `AtLeast`. One budget for the tree, not one per graft.
11. **A graft needs no mechanism of its own.** The reading is the child type's,
   the sort is the child type's, the mark key and the revisit count are qualified
   by which source they mean, an unknown child-type name is a leaf, and
   expandability is a count. Every one of those is a rule stated for another
   reason, holding here.

## Open questions

None outstanding on the tree itself. What is still to settle is in
`docs/census.md`, which the expandability count leans on.

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
