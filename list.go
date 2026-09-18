package serval

// Records already in memory, whatever read them.
//
// A source that holds its records is one shape however they arrived: a PSL
// list, a delimited file, or anything a later loader learns to read. What each
// format does is turn its text into ROWS; what happens after that -- the
// filter, the ordering, the scopes drawn out of it -- is the same work and is
// here, once.
//
// The engine asks three things of a row and nothing else: what identifies it,
// what one of its fields holds, and what it holds altogether. A format that can
// answer those is a source, and the formats differ only in how they answer.
//
// It is read-only and its records do not move, so everything computed from them
// stays true: an ordering is built once per spec and reused for every scope
// drawn from it.

import (
	"fmt"
	"sort"
	"sync"
)

// A Row is one record of a source that holds its records.
//
// Field comes from Subject, which is what a filter tests: a field the row has
// not got reads as nil, which is `undefined` rather than an error.
//
// Fields is the row entire. A format that computes its fields lazily -- the PSL
// reading does, reaching into a nested list only when asked -- pays for this
// one when a scope carries the whole record, and pays for nothing when the
// query named the fields it wanted.
type Row interface {
	Subject
	Key() *Value
	Fields() Record
}

// A plainRow is a row whose fields are already in our own shape: the common
// case for a format whose records are flat.
type plainRow struct {
	key    *Value
	fields Record
}

// NewRow is one record for a source built from rows: an identity, and the
// fields it carries.
func NewRow(key *Value, fields Record) Row {
	return plainRow{key: key, fields: fields}
}

func (r plainRow) Key() *Value              { return r.key }
func (r plainRow) Fields() Record           { return r.fields }
func (r plainRow) Field(name string) *Value { return r.fields.Get(name) }

// ListSource is a body of records held here, presented as a source.
type ListSource struct {
	rows []Row

	mu     sync.Mutex
	cache  map[string]*ordering
	recent []string // cache keys, oldest first
}

// NewListSource presents rows already in memory as a source. The rows are the
// source's from here: it does not copy them and it does not change them.
func NewListSource(rows []Row) *ListSource {
	return &ListSource{rows: rows, cache: map[string]*ordering{}}
}

// Len is how many records the source holds, before any filter.
func (l *ListSource) Len() int { return len(l.rows) }

// Open states a sequence over the records: one filter, one sort.
func (l *ListSource) Open(spec *Spec) (DataSet, error) {
	if spec == nil {
		spec = &Spec{}
	}
	if err := supported(spec.Sort); err != nil {
		return nil, err
	}
	return &listDataSet{src: l, spec: spec, ord: l.order(spec)}, nil
}

// supported refuses a sort this source cannot produce exactly: a collation it
// does not carry would yield an order that is nearly right, which is worse than
// a refusal, a refusal being recoverable and saying what is wrong.
//
// **A field it cannot reach is not among them.** A field a record has not got
// reads as `undefined`, which is a value with a rank rather than an absence, so
// the level it names still orders every record -- those without it together at
// the bottom of that level, separated by whatever levels come after and by the
// identity that settles the rest. That is an exact order over what was asked
// for, not an approximation of one, so there is nothing to refuse.
func supported(levels []SortLevel) error {
	for _, l := range levels {
		switch l.Collation {
		case "", CollateExact, CollateFold, CollateNatural:
		default:
			return fmt.Errorf("sort %s: no collation called %q", l.Field, l.Collation)
		}
	}
	return nil
}

type ordering struct {
	rows   []int
	tuples [][]*Value
	levels []Level

	// at is where each record stands, by identity. A scope names its ends by
	// identity rather than by position, so this is what turns `after=` into
	// somewhere to start walking -- and it is built once, with the ordering,
	// because every scope of this sequence needs it.
	at map[string]int
}

// index is where a record stands in this sequence, and false for one that is
// not in it. A record can fail to be here by having been filtered out as
// easily as by not existing.
func (o *ordering) index(id *Value) (int, bool) {
	i, ok := o.at[Key(id)]
	return i, ok
}

func (o *ordering) Len() int { return len(o.rows) }
func (o *ordering) Swap(i, j int) {
	o.rows[i], o.rows[j] = o.rows[j], o.rows[i]
	o.tuples[i], o.tuples[j] = o.tuples[j], o.tuples[i]
}
func (o *ordering) Less(i, j int) bool {
	return CompareLevels(o.tuples[i], o.tuples[j], o.levels) < 0
}

// order is the sequence a spec names, built if it has not been built already.
//
// Two data sets over the same sequence share one, and so does one opened
// again on an order somebody had before -- which is the same click that
// produced it the first time.
func (l *ListSource) order(spec *Spec) *ordering {
	key := dataSetKey(spec)
	l.mu.Lock()
	defer l.mu.Unlock()
	if o := l.cache[key]; o != nil {
		return o
	}

	// The filter runs first, so a record that is not in the sequence is never
	// sorted and never has its sort fields read.
	o := &ordering{levels: ordering1(spec)}
	for i := range l.rows {
		if !Match(l.rows[i].Key(), l.rows[i], spec.Filter) {
			continue
		}
		o.rows = append(o.rows, i)
		o.tuples = append(o.tuples, tupleOf(l.rows[i], spec.Sort))
	}
	// The record's identity is the last level and no two records share one, so
	// no two tuples are equal and there is nothing for stability to settle.
	sort.Sort(o)

	o.at = make(map[string]int, len(o.rows))
	for i, row := range o.rows {
		o.at[Key(l.rows[row].Key())] = i
	}

	l.cache[key] = o
	l.recent = append(l.recent, key)
	for len(l.recent) > orderingsKept {
		delete(l.cache, l.recent[0])
		l.recent = l.recent[1:]
	}
	return o
}

// ordering1 is what the sequence compares positions by: a level per sort
// level, and then the record's identity, which settles what the sort leaves
// equal.
func ordering1(spec *Spec) []Level {
	return append(Levels(spec.Sort), Level{})
}

// dataSetKey names a data set: this source, this sort, this filter. Those three
// decide which records are in the sequence and where each one stands, and two
// queries naming the same three are reading the same body of data -- so
// whatever was worked out for one of them holds for the other.
//
// The fields a query asks for are not among them: they change what a scope
// carries, not which records are in it or where. Neither is the direction it is
// read in -- one prepared ordering is walked either way, which is the whole
// reason `reversed` belongs to the scope and not to the sequence.
//
// The source itself is not in the string because the table it keys is the
// source's own.
func dataSetKey(spec *Spec) string {
	return SortKey(spec.Sort) + "\x00" + FilterKey(spec.Filter)
}

// tupleOf is a record's position: the value at each sort level, and then its
// key. Without that last one two records could tie, and "the record after this
// point" would name more than one place.
func tupleOf(r Row, levels []SortLevel) []*Value {
	out := make([]*Value, 0, len(levels)+1)
	for _, lv := range levels {
		out = append(out, r.Field(lv.Field))
	}
	return append(out, r.Key())
}

// --- the data set ------------------------------------------------------

type listDataSet struct {
	src  *ListSource
	spec *Spec
	ord  *ordering
}

// Close lets the data set go. The ordering stays in the source's cache until
// something newer pushes it out, because the records it orders have not moved.
func (v *listDataSet) Close() { v.ord = nil }

// RecordCount is exact and costs nothing. The sequence was ordered when it was
// opened, so the records that passed the filter are already in a slice and how
// many there are is its length. A source that has to fetch its records pays for
// this figure; one that holds them has it whether or not anyone asks.
func (v *listDataSet) RecordCount() RecordCount {
	if v.ord == nil {
		return Unknown() // closed, and its ordering let go of
	}
	return Exactly(v.ord.Len())
}

// Read produces one scope.
//
// Where it starts is a map lookup: the ordering knows where every record of the
// sequence stands, so `after=` becomes an index rather than a walk. From there
// it is a step in one direction or the other, counting.
//
// An `after` this sequence does not hold is refused. It cannot be placed --
// what put a record where it was were that record's own values, and they went
// with it -- and guessing would hand back a run from somewhere the asker did
// not ask about, with nothing to mark it as the wrong place.
//
// A `from` is honoured EXACTLY, the ordering being a slice: the position asked
// for is where the walk begins, clamped to the sequence's ends. This source
// never has to estimate, so the First it reports is always exact.
func (v *listDataSet) Read(s *Scope, out Sink) error {
	o := v.ord
	if o == nil {
		return fmt.Errorf("this data set has been closed")
	}

	if err := bothEnds(s); err != nil {
		out.Done(Complete{Error: err.Error()})
		return nil
	}

	step := 1
	i := 0
	if s.Reversed {
		step = -1
		i = len(o.rows) - 1
	}
	if s.After != nil {
		at, ok := o.index(s.After)
		if !ok {
			out.Done(Complete{Error: fmt.Sprintf(
				"after %s: no record of mine is in this sequence under that identity",
				s.After.String())})
			return nil
		}
		i = at + step
	} else if s.From != 0 {
		// The position is a place in the SEQUENCE, which is the order Total and
		// First are both counted in, and not a place in the walk. So a reversed
		// scope starting from a position starts at that record and walks back
		// from it, rather than that far in from the end.
		i = s.From
		if i < 0 {
			i = 0
		}
		if i >= len(o.rows) {
			i = len(o.rows) - 1
		}
	}

	// An `until` this sequence does not hold is not a refusal. It only says
	// where the asker's own knowledge picks up again, and one that cannot be
	// placed simply never arrives -- the walk runs to its count instead.
	stop := -1
	if s.Until != nil {
		if at, ok := o.index(s.Until); ok {
			stop = at
		}
	}

	// Said before the records, which is where it can be acted on.
	out.Ordered()

	// How long the sequence is, volunteered rather than waited for. This source
	// ordered its records when the sequence was stated, so the figure is already
	// in hand and costs nothing to say -- and beside First it is what a reader
	// needs to draw a scrollbar over records it has never seen.
	done := Complete{Total: Exactly(len(o.rows))}
	last := s.After
	sent := 0
	for ; i >= 0 && i < len(o.rows); i += step {
		if i == stop {
			// The next record is one the asker already holds, so what it holds
			// on this side and what it holds on that are now one run.
			done.Stop = StopJoined
			break
		}
		if sent >= s.Count {
			done.Stop = StopFilled
			break
		}
		if sent == 0 {
			// Where the answer began, said of the record that actually goes out
			// rather than of the place the walk was aimed at -- a scope that
			// ends before its first record began nowhere and says nothing.
			done.First = Exactly(i)
		}
		rec := v.src.rows[o.rows[i]]
		bag, has, whole := v.fields(rec)
		var err error
		if whole {
			err = out.Record(rec.Key(), bag)
		} else {
			err = out.Subset(rec.Key(), bag, has)
		}
		if err != nil {
			return err
		}
		last = rec.Key()
		sent++
	}

	if done.Stop == "" {
		// Walked off the end: nothing more this way, and so no point past the
		// end to be complete up to.
		done.Stop = StopExhausted
	} else {
		done.Watermark = last
	}
	out.Done(done)
	return nil
}

// fields is what one record carries in this scope, how many members the record
// HAS altogether, and whether what goes out is the whole of it.
//
// Whole is the stronger claim and it is only made where it is true: a list of
// fields was asked for, or an exclusion took something out, and what goes out
// is a subset. Narrowing to a list that happens to name everything is still
// answered as a subset, which is the weaker claim and therefore always safe --
// and the totals beside it say so exactly, which is what lets the far end work
// out for itself that it now holds the lot.
//
// **A field the record has not got goes out as undefined** rather than being
// left out. Left out it reads as a field nobody asked about, and the next query
// naming it asks all over again; sent, it is a guarantee that the record has
// not got it. This source holds its records entire, so it always knows which of
// the two it is looking at.
func (v *listDataSet) fields(rec Row) (Record, Totals, bool) {
	bag := rec.Fields()
	has := Tally(bag)

	want := v.spec.Fields
	if len(want) == 0 {
		out := v.without(bag)
		return out, has, len(out) == len(bag)
	}
	out := make(Record, 0, len(want))
	for _, a := range want {
		if v.spec.Exclude.Has(a.Name) {
			continue
		}
		// Present with its value, or present with nothing under it, which says
		// the record has not got it and is an answer rather than a silence.
		out = append(out, &Field{Name: a.Name, Value: rec.Field(a.Name)})
	}
	return out, has, false
}

// without drops the fields the query said it did not want.
func (v *listDataSet) without(bag Record) Record {
	if len(v.spec.Exclude) == 0 {
		return bag
	}
	out := make(Record, 0, len(bag))
	for _, a := range bag {
		if !v.spec.Exclude.Has(a.Name) {
			out = append(out, a)
		}
	}
	return out
}
