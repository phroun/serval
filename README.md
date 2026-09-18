# serval

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

> **serval** — *Scoped Enquiry over Records, Values And Lists* (a wild cat of the tall grass, which finds what it wants in a field by listening and takes it in one leap)

*If you use this, please support me on ko-fi:  [https://ko-fi.com/jeffday](https://ko-fi.com/F2F61JR2B4)*

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/F2F61JR2B4)

Reading a long sequence of records a screenful at a time, without ever holding
all of it and without asking twice for what you already have. It knows nothing
about terminals, protocols, rendering or documents, and its only dependency
outside the standard library is `github.com/phroun/pawscript`, for reading PSL.

It's a sibling to the rest of the line: **Mew** (text editor), **PurfecTerm**
(terminal emulator), **PawScript** (language), **KittyTK** (UI toolkit),
**khatool** (Hebrew and Arabic text) and **ifitfits** (viewport tiling).

## Model

- **A query is asked once and answered once.** You state a **sequence** — a
  source, a filter, a sort — and that naming never changes: a different filter
  is a different sequence, opened alongside the first and taking its place.
  Then you ask for **scopes** of it. Each scope is a separate question, and
  `Complete` ends the answer rather than the interest.
- **A scope is not a filter and names no field.** It says *start past this
  record, give me this many, stop if you reach that one, and walk backwards if
  I say so*. Both ends are optional and both are **identities, never
  positions** — where a record stands is the business of whoever put it there,
  and a source made of several others hands each of them that one's own
  identity rather than passing one down.
- **An answer says how far it is complete.** A `watermark` means *nothing
  between where you asked from and this record is missing*. Three ways to stop
  — `filled`, `joined`, `exhausted` — because a scope that ran out of records
  and one that reached its count look identical from outside and mean opposite
  things about whether to ask again.
- **Sources compose.** `AmendedSource` lays additions, replacements and
  deletions over another source without touching it. `ComposedSource` reads
  several as one merged sequence, in the merged order, with each include asked
  only what it can answer. Both are Sources, so they nest.
- **Answers are kept.** A cached scope is a run of records *guaranteed complete
  between its two ends* — the same claim a watermark makes, which is why an
  answer and a cache entry are the same object. Scrolling extends a run rather
  than making another; two runs that meet join; a run cut in two by an insert
  is two runs and not a re-query. Eviction is segmented, so a scan of a million
  records cannot flush out the handful you were actually using.
- **Caching is a wrapper.** `NewCachedSource(src)` answers out of what the
  process already holds wherever it can and asks the source below wherever it
  cannot, filing what comes back. It is a Source like the other two, so it
  composes with them in either order — and a source that should not be cached
  simply is not wrapped. There is one cache and one quota for the whole
  process, however many wrappers draw on it; `SetCacheLimit` sizes it.
- **Order and values are two caches.** Where a record *stands* belongs to a data
  set — a source, a sort and a filter. What it *holds* belongs to the source,
  and is kept once there for every data set drawing on it. Each has its own
  room and its own eviction, and neither keeps the other alive: close one sort
  and open another and the values are all still here, while a run whose records
  have been evicted still knows what comes after what — which is what a top-up
  is asked against. So a second sort costs places and not fields, and a stretch
  already fetched for one field is topped up with another rather than fetched
  again.
- **Two ways to miss, answered differently.** Falling off the end of a run is
  not knowing what comes next, and only reading the stretch will tell you. But a
  walk that got where it was going over records that aren't known well enough
  knows the *order* perfectly — so it asks the narrow question that makes
  possible: these identities, these fields, nothing re-walked and nothing
  already known re-sent. A sink that implements `Placing` skips even that and is
  handed the order at once, as results for the records it can have and **places**
  for the rest — a record's position and whatever is known of it, with no claim
  about how much. Places are additional, never substitutional, so a sink that
  does not take them is handed every record it would have been handed anyway.
- **Staleness is told, never decided.** Nothing polls, nothing expires and no
  generation is compared. A source *says* what happened — a record added,
  removed, replaced, or altered in named fields — and the reason is what decides
  the price. A record that has **left** a sequence is unlinked and the run's
  claim survives it; one that may have **moved** takes its run with it. An
  altered field costs the order only in the sequences that field decides, and
  costs the values once, for the source. Nothing is fetched either way:
  invalidation causes forgetting, not traffic.
- **How many there are is a figure, a floor, or nothing.** `RecordCount` is the
  whole sequence's total, and it belongs to the FILTER rather than the order —
  sorting the same records cannot make there be more or fewer — so clicking a
  column header costs a new order and not a new count. A source holding its own
  records has it for free, one that has read part of a sequence has a floor, and
  a notice moves it: `Added` is one more, `Removed` one fewer, and anything
  leaving membership in doubt lowers the floor rather than losing the figure.
  Composing adds the includes' figures; amending asks one question twice —
  does the filter admit the child's version, and does it admit ours — which is
  exact wherever the child's has been seen, and a floor where it has not.
- **What is depended on can be stated.** `Covers(spec)` is the stretches of one
  sequence actually held, read off the runs rather than tracked beside them, and
  `Spec.Roles()` groups a sequence's fields by the part each plays — so a change
  is classified by looking a name up, rather than by evaluating a filter or
  comparing two boundaries.
- **A record says how many members it has.** Every subset carries two counts —
  how many members stand by position, how many by name — so a later question
  can be answered without asking. Three of the first means `0`, `1` and `2` and
  nothing else; eight of the second, eight of which are known, leaves nothing
  for a ninth name to be. A field a record has *not* got comes back as
  `undefined` rather than being left out, which is a guarantee instead of a
  silence. Subsets that between them cover a record leave it known *entire*,
  without anyone deciding to.
- **Identity is not a spelling.** `Key` and `Equal` decide whether two values
  are the same value, and they owe nothing to any grammar: text and bytes are
  different kinds, 3 and 3.0 are different numbers, and floats compare by their
  bits so that a key is reflexive.

## Docs

- [`docs/sources.md`](docs/sources.md) — what a source is, what a format loads
  into one, how several compose under names of their own, and what a scope
  costs at either end of a long sequence.
- [`docs/ordering.md`](docs/ordering.md) — the comparison and membership rules,
  which two holders of one sequence have to compute the same way without
  conferring.
- [`docs/trees.md`](docs/trees.md) — built over records that are here: a tree as
  a source, presenting the rows that are visible flat, so that a scrollbar, a
  thumb drag and a cache work over one without knowing it is one.
- [`docs/census.md`](docs/census.md) — built over records that are here:
  `RecordCount` partitioned by a field's value, so that a page of counts is one
  question rather than a page of questions.

## Use

```go
import "github.com/phroun/serval"

src := serval.NewCachedSource(serval.NewPSLSource(node, serval.Whole))

set, err := src.Open(&serval.Spec{
    Sort: []serval.SortLevel{{Field: "name"}},
})
if err != nil {
    return err
}
defer set.Close()

// One screenful, then the next, from where the first one got to.
err = set.Read(&serval.Scope{Count: 30}, sink)
err = set.Read(&serval.Scope{After: watermark, Count: 30}, sink)
```

A sink takes the records one at a time and is told what ended them:

```go
type Sink interface {
    Ordered()                                  // before the first record, or not at all
    Record(id *serval.Value, f serval.Record) error  // the record entire
    Subset(id *serval.Value, f serval.Record,        // only what was asked for,
           has serval.Totals) error                  // out of this many members
    Done(c serval.Complete)
}
```

## Test

```
go test ./...
```

## License

MIT — see [LICENSE](LICENSE).
