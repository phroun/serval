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
carries its depth.

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

## The path, and the ambiguity in it

A row's identity in a tree is its PATH, not its key: two parents may each have a
child keyed `x`. The path is the parent's path, a separator, the child's key —
the same grammar `ComposedSource` uses, splitting on the first slash so each
layer sheds one name and hands the rest down.

**A path joined with slashes is ambiguous**, because a key may itself contain a
slash: that was decided deliberately, a slash inside a key being what makes the
key an address. So parent `a` with a child keyed `b/c`, and parent `a/b` with a
child keyed `c`, both spell `a/b/c`. `ComposedSource` does not meet this because
it refuses a slash in an include NAME and the ambiguity only bites at a leaf. A
tree has no leaves in that sense: every node can become a parent.

Marks are therefore keyed by a **length-prefixed join** — `1:a|3:b/c` — which is
unambiguous, is still one string, and still prefix-matches for the wiping rule.
The joined-with-slashes form stays what is shown and what an address uses.

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
5. Paths are keyed by a length-prefixed join.
6. Deep filtering is eager and is for records in hand.
7. **Pre-order is built, not sorted.** It does not fall out of `CompareLevels`:
   that stops where the shorter run ends and returns 0, so `[a]` and `[a, b]`
   compare EQUAL rather than parent-before-child. A tuple per level cannot
   express a varying-depth path either. `TreeSource` orders its own sequence.

## Open questions

- **What a mark change costs.** Opening a node is `Added` for each row that
  appeared; `openAll` on a large subtree is a great many. Whether that is one
  notice about the sequence or one per record is the same question
  `live-data-negotiation.md` asks about everything else, and the answer should
  be the same answer.
- **Whether a row's expandability is a field.** Over records in hand the source
  knows it from the buckets. Over records that must be asked it does not, and
  drawing a twisty would mean opening every visible row — so a field the source
  MAY carry, used where it is there. Which field, and whether it is a count or a
  flag, is undecided; a count also bounds the unknown extent of an `openAll`.
- **Cycles.** A child type keyed on a prefix can make a node its own descendant.
  The path is in hand, so refusing to open a node already on its own path is
  cheap — but some file managers deliberately allow it, so whether this is a
  rule or a setting is not settled.
- **Whether a sort passes down.** Sorting a tree by size should surely sort each
  level by size, but a child type naming a different source may not carry the
  field the sort names, and a sequence that cannot be produced exactly is
  refused rather than approximated. What that refusal looks like partway down a
  tree is undiscussed.

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
