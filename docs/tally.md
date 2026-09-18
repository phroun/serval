# A tally

> **Status: conversation. Nothing here is built.** This is a design reached by
> talking it through, written down so it is not lost, and it is not a contract.
> `docs/sources.md` is what a source is today and `docs/trees.md` is the tree
> design this was thought up in service of.

How many records does this filter admit, **for each value of this field**? One
question, one answer, and a short list of large counts instead of a thousand
questions asked one at a time.

The want that produced it is a tree drawing a page of twisties. Thirty visible
rows, each needing to know whether it has children and how many: thirty counts,
which over a source that must be asked is thirty questions for thirty numbers.
Tallying the child spec's filter by the parent field is one question that answers
all thirty, and answers them exactly.

## It is `RecordCount`, partitioned

That is the whole of the architectural claim, and it is why this fits rather than
being bolted on.

A count already belongs to the **filter** and not to the order — sorting the same
records cannot make there be more or fewer, which is why the cache keys a count
by `source` and `FilterKey(spec.Filter)` with the sort left out entirely. A tally
is that same claim, partitioned by one field's value. So it keys by the same two
things and the field name, and **the sort is ignored**: a tally over the by-name
and the by-size orders of one filter is one answer and one cache entry, exactly
as a count is.

**Groups are told apart by identity, not by spelling.** `Key` and `Equal` already
decide whether two values are the same value, so `3` and `3.0` are two groups
because they are two numbers, and `3` and `"3"` are two groups because one is a
number and one is text. Nothing new decides this and nothing may.

**`undefined` is a group.** A field a record has not got comes back as
`undefined` rather than being left out, which is a guarantee this library already
makes everywhere else — so the records missing the field are a group like any
other, countable and nameable, rather than a silence to be inferred from an
arithmetic that does not add up.

## Three claims, and the vocabulary already covers them

A tally makes more claims than it looks like it makes, and the second is the one
that would get fudged.

| | |
|---|---|
| **each group's count** | a `RecordCount` — `Exactly` from records in hand, `AtLeast` from a source that has read part of a sequence |
| **whether those are ALL the groups** | a `RecordCount` of the GROUPS — `Exactly(3)` means these three and no more, `AtLeast(3)` means at least these |
| **how many groups were asked for** | a scope, because a tally is a sequence |

The second is separate from the first and must be. A source that has seen parents
`3`, `7` and `9` can honestly report those three with honest counts while having
no idea whether there is a fourth. Rolling that into the per-group counts would
lose it: three exact counts and no statement about completeness reads as a
complete answer, which is the silent wrong answer this library refuses everywhere
else.

## A tally is a sequence, so it is a source

One record per distinct value, carrying the value and the count. Which means it
needs no vocabulary of its own for any of the things a caller will want:

| the want | what it is |
|---|---|
| a short list of large counts | `sort={ count desc } count=20` |
| the biggest parent | the same, with `count=1` |
| groups over a threshold | a filter, over the tally's own sequence |
| how many distinct values are there | `RecordCount` of the tally |
| the groups' identities | the field values themselves, unique by `Key`, so nothing is synthesised |

**Cardinality is the hang, and a scope is the answer.** Tallying a timestamp
gives one group per record. Because a tally is a sequence, a caller bounds it the
way it bounds anything — `Scope.Count` — and the group count says there were
more. A source is never asked to materialise an unbounded tally, and a caller
that asks for twenty groups of a million gets twenty groups and a floor.

So the shape is two pieces, which is the `Counting` pattern again:

- **`Tallying`**, an optional interface on the data set, asked with a free
  `TallyOf(set, field)` that answers "cannot" for anything that is not one. The
  least a source can do stays one method.
- **A source over that**, so the result gets scopes, sorting, filtering, caching
  and `Complete` without any of them being written twice.

## What it buys

**A page of twisties in one question.** This is the case it was invented for, and
`trees.md` decision 7 leans on it: expandability is a field, or a count, and a
count per visible row is only affordable if a page of them is one question.

**`openAll` sized before it is opened.** How big each subtree is, for every node
at a level, in one answer — which is what lets a tree bound the unknown extent of
an expand-all one level at a time rather than discovering it by walking.

**It degrades at a graft the way everything else does.** A tally answers one
source and one field, which covers the nested-single-source and adjacency-list
shapes — the common ones. A graft breaks it, and then it is one tally per grafted
SOURCE rather than one count per row, which is the same degradation the readings
have and in the same place.

## What it costs

**Getting a tally is cheap; keeping one exact is not.** This is the honest
finding and it points at a posture rather than at a mechanism.

`Added` names no record, so nothing says which group the new record joined —
which knocks that group, and therefore the whole tally, down to a floor.
`Altered` naming the tallied field moves a record between two groups it also
cannot name. `Removed` and `Replaced` name records, so where the tally's field is
known for them the arithmetic works, and where it is not it does not.

**So: do not maintain a tally through invalidation. Drop it and re-ask.** It is a
cheap question by construction — that being the entire point of it — and a wrong
count on a twisty is worse than a second question. Maintaining one would mean
either holding every record's value for the tallied field, which is a cache
nobody asked for, or accepting floors that decay to uselessness after a few
notices.

**Refuse rather than silently walk.** Over records in hand the fallback is a pass
over records already here, which is fine and exact. Over a source that must be
asked and cannot tally, walking everything to count it is precisely what the
caller was avoiding — so `TallyOf` says it cannot, and the caller falls back to
whatever it had before. A tally that quietly became a full scan would be worse
than no tally at all, because it would be fast in testing and ruinous in use.

## Decisions taken

1. A tally is `RecordCount` partitioned by a field's value, keyed by the source,
   the filter and the field. **The sort is ignored**, because a count belongs to
   the filter.
2. Groups are told apart by `Key` and `Equal`, so `3`, `3.0` and `"3"` are three
   groups. `undefined` is a group like any other.
3. Three claims, three existing shapes: a `RecordCount` per group, a
   `RecordCount` of the groups for whether that is all of them, and a scope for
   how many were asked for.
4. `Tallying` is an optional interface asked with `TallyOf`, on the `Counting`
   and `CountOf` model, and a tally is also a Source so that scopes, sorting,
   filtering and caching come for free.
5. A tally is not maintained through invalidation. It is dropped and re-asked.
6. A data set that cannot tally says so. It does not walk the sequence to
   produce one.

## Open questions

- **The name.** `Tally` reads right in the `Counting` / `CountOf` family and
  avoids `GroupBy`, which is SQL's word for SQL's clause where this is a count
  over a membership. Not strongly held.
- **Whether a tally can ever be maintained rather than re-asked**, which comes
  back to whether `Added` could name the record that appeared. It names none
  today for a good reason — nobody holds it — but a tally is the first thing here
  that would rather know.
- **Several fields at once**, which is a cross-tabulation and is not wanted by
  anything yet. Deferred deliberately: one field covers the tree, and the second
  field can be added when something needs it rather than designed for now.
