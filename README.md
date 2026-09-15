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
- **Order and values are two caches.** Where a record *stands* belongs to a data
  set — a source, a sort and a filter. What it *holds* belongs to the source,
  and is kept once there for every data set drawing on it. Each has its own
  room and its own eviction, and neither keeps the other alive: close one sort
  and open another and the values are all still here, while a run whose records
  have been evicted still knows what comes after what — which is what a top-up
  is asked against. So a second sort costs places and not fields, and a stretch
  already fetched for one field is topped up with another rather than fetched
  again.
- **Identity is not a spelling.** `Key` and `Equal` decide whether two values
  are the same value, and they owe nothing to any grammar: text and bytes are
  different kinds, 3 and 3.0 are different numbers, and floats compare by their
  bits so that a key is reflexive.

## Use

```go
import "github.com/phroun/serval"

src := serval.NewPSLSource(node, serval.Whole)

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
    Subset(id *serval.Value, f serval.Record) error  // only what was asked for
    Done(c serval.Complete)
}
```

## Test

```
go test ./...
```

## License

MIT — see [LICENSE](LICENSE).
