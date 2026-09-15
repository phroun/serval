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
// # What can be answered
//
// A hit needs two things: a run that covers the stretch, and every record in it
// known well enough for the fields the query asked for. Both are settled in
// cache.go.
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
			// their records hold and share nothing about where they stand.
			source: c.key,
			set:    c.key + "\x00" + dataSetKey(spec),
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

// Read answers one scope out of what is held, or asks the child and files what
// comes back.
func (s *cachedSet) Read(sc *Scope, out Sink) error {
	if out == nil {
		return fmt.Errorf("a scope needs somewhere to put the answer")
	}
	if sc == nil {
		sc = &Scope{}
	}

	if recs, done, ok := hot.serve(s.ds, s.want, sc); ok {
		// Said before the records, which is where it can be acted on.
		out.Ordered()
		for _, r := range recs {
			fields := r.fields
			entire := r.entire()
			if entire && len(s.spec.Exclude) > 0 {
				// What this query does not want comes off here rather than
				// being held twice: the cache keeps the record, and each
				// sequence over it takes what it asked for.
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
		out.Done(done)
		return nil
	}
	return s.child.Read(sc, &filing{set: s, scope: sc, out: out, most: hot.mostPlaces()})
}

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
