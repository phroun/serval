package serval

// Answering out of what is already held.
//
// A CachedSource stands in front of another source. A scope it can answer from
// what the process already holds it answers at once, without the source below
// being told anything about it; one it cannot it passes down, and files the
// answer on its way back so that the next scope over the same ground is the
// first kind.
//
// **It is a wrapper and not a policy.** Caching is a thing a caller asks for by
// wrapping, the way amending and composing are, so a source that should not be
// cached simply is not wrapped -- and one that is already a cache is not
// wrapped twice by accident. It composes with the other two in either order,
// all three being Sources.
//
// # One cache, one quota
//
// There is ONE cache for the process, and every wrapper answers out of it. Ten
// CachedSources are ten wrappers over one body of room: what they hold is
// counted together against one limit, they give way to each other under it on
// the same terms, and CacheLimit says what that limit is for all of them at
// once. A wrapper does not carry a quota of its own and cannot be given one.
//
// What each wrapper does get is its own NAME in that shared cache, so nothing
// of one is ever taken for a record of another. The name is the wrapper itself
// rather than anything the caller writes down: two wrappers over what is really
// one body of records do not share it, because nothing here could check that
// claim and a wrong one would hand out another source's records. So a source is
// wrapped ONCE and the wrapper is what gets kept.
//
// # What can be answered, and the two ways of missing
//
// A hit needs two things: a run that covers the stretch, and every record in it
// known well enough for the fields the query asked for. Both are settled in
// cache.go -- and they fail differently, which is worth more than it sounds.
//
// A walk that falls off the end of a run does not know what comes NEXT, and
// nothing short of reading the stretch will tell it. But a walk that got where
// it was going over records that are not known well enough knows the order
// perfectly: which record follows which, with nothing missing between. Only the
// values are short, and there are two ways to answer that.
//
// **Ask about those records.** An identity filter over exactly the ones that
// fell short, for exactly the fields wanted -- the narrow question the order
// makes possible. The stretch is not walked again and nothing already known is
// re-sent. That is the top-up, and it is what the two caches were split for.
//
// **Or hand the order over.** A sink with somewhere to put a PLACE gets the
// stretch at once: results for the records it can be given, places for the
// rest, and the order settled before the scope is done. Nothing is fetched, and
// what is worth asking for next is the reader's to decide. See Placing.
//
// Which of the two happens is the sink's doing and nobody negotiates it: one
// that takes places gets them, one that does not gets the values found for it.
//
// # What a query asks for
//
// A query that names FIELDS is answered wherever every one of them is settled:
// carried, carried as an absence, or settled by the record's totals. A query
// that names none is asking for whole records, and is answered only by records
// known entire -- which a run of subsets that between them cover a record makes
// it, so this is far from the rare case it sounds.
//
// A query that says what it does NOT want is answered the same way and then
// narrowed here, the excluded members being dropped on the way out. It cannot
// be answered out of a record known only in part: what such a query wants is
// everything else, and how much else there is is exactly what a record known in
// part does not say.

import (
	"fmt"
	"strconv"
	"sync/atomic"
)

// CacheLimit is how much the process holds, and SetCacheLimit changes it,
// evicting down to the new figure at once. It is one figure for every
// CachedSource there is.
func CacheLimit() int     { return hot.Limit() }
func SetCacheLimit(n int) { hot.SetLimit(n) }

// CacheCost is how much of that is in use.
func CacheCost() int { return hot.Cost() }

// wrappers names each CachedSource apart from every other one.
var wrappers atomic.Uint64

// A CachedSource answers out of the process's cache wherever it can, and asks
// the source it wraps wherever it cannot.
type CachedSource struct {
	child Source
	key   string
}

// NewCachedSource wraps a source.
//
// Wrap once and keep the wrapper: it is the name under which everything it
// learns is filed, so a second wrapper over the same source starts knowing
// nothing.
func NewCachedSource(child Source) *CachedSource {
	return &CachedSource{
		child: child,
		key:   "s" + strconv.FormatUint(wrappers.Add(1), 36),
	}
}

// Open states a sequence, and opens the same one on the child.
//
// The child is opened whether or not anything is ever asked of it. Opening is
// where a source REFUSES a sequence it cannot produce exactly, and a refusal
// that only arrived once the cache happened to miss would be a refusal that
// depended on what was in memory.
func (c *CachedSource) Open(spec *Spec) (DataSet, error) {
	if spec == nil {
		spec = &Spec{}
	}
	child, err := c.child.Open(spec)
	if err != nil {
		return nil, err
	}
	return &cachedSet{
		src:   c,
		spec:  spec,
		child: child,
		ds: dataSet{
			// The records are the wrapper's, and every sequence over them is a
			// sequence of the wrapper's: two sorts of one source share what
			// their records hold and share nothing about where they stand --
			// and share how many of them there are, which the filter decides
			// and the sort cannot.
			source:  c.key,
			set:     c.key + "\x00" + dataSetKey(spec),
			members: c.key + "\x00" + FilterKey(spec.Filter),
		},
		want: spec.Fields,
	}, nil
}

type cachedSet struct {
	src   *CachedSource
	spec  *Spec
	child DataSet
	ds    dataSet

	// want is what a held record has to carry for this sequence's questions to
	// be answered out of it. Empty is the whole record.
	want Record
}

// Close lets this sequence go, and the child's with it. What was learned stays:
// it belongs to the source and to the sequence, and neither has gone anywhere.
func (s *cachedSet) Close() { s.child.Close() }

// RecordCount is how many records this sequence has.
//
// What is held is asked first, and an exact figure there ends it -- either a
// source said so, or a run covers the sequence end to end, and neither gets
// better for asking again. Otherwise the child is asked and what it says is
// filed, so that the next sequence over this filter has it without asking: a
// re-sort costs a new order and not a new count.
func (s *cachedSet) RecordCount() RecordCount {
	held := hot.recordCount(s.ds)
	if held.Exact {
		return held
	}
	if from := CountOf(s.child); says(from, held) {
		hot.learnCount(s.ds, from)
		return hot.recordCount(s.ds)
	}
	return held
}

// Read answers one scope out of what is held, or asks the child and files what
// comes back.
func (s *cachedSet) Read(sc *Scope, out Sink) error {
	if out == nil {
		return fmt.Errorf("a scope needs somewhere to put the answer")
	}
	if sc == nil {
		sc = &Scope{}
	}

	// Two misses, and only one of them is about the order. A walk that fell off
	// the end of a run does not know what comes next, and nothing but reading
	// the stretch will tell it. A walk that got where it was going over records
	// that are not known well enough knows the ORDER perfectly -- so it is
	// answered by asking about those records, or by handing the order over and
	// letting whoever asked decide.
	if got, ok := hot.serve(s.ds, s.want, sc); ok {
		if got.whole {
			return s.replay(got, out, nil)
		}
		if p := placing(out); p != nil {
			return s.replay(got, out, p)
		}
		// Nowhere to put a place, so the values are found rather than skipped.
		// Whether that worked is the cache's to say and not the child's: what
		// came back was filed on its way past, so the question is simply
		// whether the stretch answers now.
		s.topUp(got.short(s.want))
		if again, ok := hot.serve(s.ds, s.want, sc); ok && again.whole {
			return s.replay(again, out, nil)
		}
	}
	return s.child.Read(sc, &filing{set: s, scope: sc, out: out, most: hot.mostPlaces()})
}

// replay hands a held answer to the sink.
//
// A record known well enough goes out as a result, and where `places` is there
// one that is not goes out as a place -- its position, and whatever is known,
// which may be nothing. The order's own completion is said only where a place
// went out: with none, the scope's `Done` settles the order at the end like any
// other answer, and saying it twice would say it twice.
func (s *cachedSet) replay(got *serving, out Sink, places Placing) error {
	out.Ordered() // before the records, which is where it can be acted on
	placed := false
	for i, e := range got.at {
		r := got.held[i]
		if places != nil && !r.answers(s.want) {
			placed = true
			if err := places.Place(e.id, s.known(r)); err != nil {
				return err
			}
			continue
		}
		fields := r.fields
		entire := r.entire()
		if entire && len(s.spec.Exclude) > 0 {
			// What this query does not want comes off here rather than being
			// held twice: the cache keeps the record, and each sequence over it
			// takes what it asked for.
			fields = without(fields, s.spec.Exclude)
			entire = false
		}
		var err error
		if entire {
			err = out.Record(r.id, fields)
		} else {
			err = out.Subset(r.id, fields, r.has)
		}
		if err != nil {
			return err
		}
	}
	if placed {
		places.Placed(got.done)
	}
	out.Done(got.done)
	return nil
}

// known is what a place carries: everything this cache knows of the record,
// less whatever the query asked to leave out.
//
// Everything, and not just what was asked for. A place with nothing in it is
// still a place and still worth sending -- and one carrying the fields that
// DECIDE the sequence is one a merging source can put somewhere, which a place
// carrying only what this query happened to name might not be.
func (s *cachedSet) known(r *cachedRecord) Record {
	if r == nil {
		return nil
	}
	if len(s.spec.Exclude) == 0 {
		return r.fields
	}
	return without(r.fields, s.spec.Exclude)
}

// topUp asks the child about exactly the records whose values fell short.
//
// Which is the narrow question the order makes possible: these identities,
// these fields. The stretch is not walked again and nothing already known is
// re-sent, because where those records STAND is not in question -- only what
// they hold.
//
// It is an optimisation and fails quietly, saying nothing about how it went: a
// child that will not answer an identity filter, or answers one short, simply
// leaves the stretch still short, and the caller finds that out by asking the
// cache rather than by being told here.
//
// Whatever DID come back is filed either way, a refusal included. Two answers
// about one record of one source do not contradict each other, so a record the
// child managed to send before it gave up is a record worth keeping.
func (s *cachedSet) topUp(short []*Value) {
	if len(short) == 0 {
		// Nothing reaches here with nothing to ask about -- a stretch that is
		// not whole has at least one record short -- so this says what is meant
		// rather than stopping anything, and no test kills it.
		return
	}
	v, err := s.src.child.Open(&Spec{
		Sort:    s.spec.Sort,
		Filter:  &Filter{Op: OpID, Values: short},
		Fields:  s.spec.Fields,
		Exclude: s.spec.Exclude,
	})
	if err != nil {
		return // it will not answer questions in that shape
	}
	defer v.Close()

	got := &topping{}
	if err := v.Read(&Scope{Count: len(short)}, got); err != nil {
		return
	}
	hot.learnValues(s.ds.source, got.kept)
}

// A topping takes a top-up's answer, which is values and nothing else: those
// records are already placed, and the order an identity filter produced them in
// is nobody's.
type topping struct {
	kept []*cachedRecord
}

func (t *topping) Ordered() {}

func (t *topping) Record(id *Value, fields Record) error {
	return t.take(id, fields, Tally(fields))
}

func (t *topping) Subset(id *Value, fields Record, has Totals) error {
	return t.take(id, fields, has)
}

func (t *topping) take(id *Value, fields Record, has Totals) error {
	if id != nil {
		t.kept = append(t.kept,
			newRecord("", id, append(Record(nil), fields...), has, 0))
	}
	return nil
}

func (t *topping) Done(Complete) {}

// A filing is the sink a missed scope is answered into: it hands each record
// on as it arrives and keeps a copy, and files the run when the answer ends.
//
// Nothing waits for it. A source whose records are somewhere else answers in
// its own time, and what reaches the asker reaches it exactly when it would
// have without any of this in the way.
type filing struct {
	set   *cachedSet
	scope *Scope
	out   Sink

	// kept is the run as it is being built, bounded: an answer larger than the
	// cache would ever hold is not assembled in memory first only to be cut
	// down afterwards. What is dropped is the front of the WALK, which is what
	// the cache would drop anyway -- so `from` moves with it, and is where the
	// run really starts past.
	kept []*cachedRecord
	from *Value
	most int

	// unplaceable says this answer cannot be filed at all.
	//
	// A record with no identity cannot be found again and cannot be placed, and
	// an answer that simply left it out would claim there was nothing between
	// its neighbours -- which is the one thing a run says. So the answer goes on
	// to whoever asked for it, as it stands, and nothing is kept of it.
	unplaceable bool
}

func (f *filing) Ordered() { f.out.Ordered() }

func (f *filing) Record(id *Value, fields Record) error {
	// A whole record IS its own totals: what it carries is everything there is.
	f.take(id, fields, Tally(fields))
	return f.out.Record(id, fields)
}

func (f *filing) Subset(id *Value, fields Record, has Totals) error {
	f.take(id, fields, has)
	return f.out.Subset(id, fields, has)
}

// take keeps one record, in a copy of the run the source handed over.
//
// A copy of the RUN and not of the values: the cache holds what it is given for
// as long as it holds it, and a source that reuses its own slice between
// records would otherwise rewrite what is filed. The fields inside are shared,
// values being read-only everywhere here.
func (f *filing) take(id *Value, fields Record, has Totals) {
	if id == nil {
		f.unplaceable = true
		return
	}
	f.kept = append(f.kept,
		newRecord("", id, append(Record(nil), fields...), has, 0))
	for len(f.kept) > f.most {
		// The run now starts past the record that just went, which for a walk
		// read backwards is the record it now stops before: one end either way,
		// and the scope names it the same.
		f.from = f.kept[0].id
		f.kept = f.kept[1:]
	}
}

// Done files the run and then says so, in that order: a sink told an answer has
// ended may ask for the next one at once, and it should find this one here.
func (f *filing) Done(c Complete) {
	if f.unplaceable {
		f.out.Done(c)
		return
	}
	sc := f.scope
	if f.from != nil {
		cut := *sc
		cut.After = f.from
		sc = &cut
	}
	hot.hold(f.set.ds, sc, f.kept, c)
	f.out.Done(c)
}

// without drops the members a query said it did not want. The record it is
// taken out of is the cache's and is left as it was.
func without(fields, excluded Record) Record {
	out := make(Record, 0, len(fields))
	for _, f := range fields {
		if !excluded.Has(f.Name) {
			out = append(out, f)
		}
	}
	return out
}
