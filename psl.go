package serval

// Reading a PSL list as records.
//
// A PSL list holds two independent collections: an ordered sequence of items,
// and a keyed map that sits outside that sequence entirely. Both are records
// here. An item's key is its index and a keyed member's key is its name, and
// because an integer ranks below a string in the comparison core, the two
// spaces fall into one total order with no rule of their own: the items first,
// in their order, then the names.
//
// This is a flat reading. A `_bundle` member is a record like any other, and
// nothing here follows an include, applies an amendment or resolves a hash --
// that is a layer above this one, and it is built on this rather than into it.
// That layer is designed and unbuilt; the design is KittyTK's
// docs/data-sources-and-bundles.md, and where it comes to live is a question
// for whoever builds it.
//
// What it is for is access: a scope of a sorted, filtered sequence, found
// without walking the records that precede it. The sequence is ordered once per
// spec, and a scope is a binary search for the boundary and a walk forward as
// far as the scope is long -- so scrolling to the end of a large list costs
// what scrolling to the start of it costs.

import (
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/phroun/pawscript"
)

// orderingsKept is how many orderings of one source are held at once. Each is a
// column header somebody clicked, and clicking back is the next thing they do,
// so the previous few are worth keeping and an unbounded pile of them is not.
const orderingsKept = 4

// The two names the Whole reading gives a record's own key and value.
//
// These are fields, and only fields. A record read this way carries `key`
// because the document gave it one and somebody may want to sort or show it --
// not because this end works by it. What identifies the record travels beside
// the bag, and a document is free to hold a member called `key` of its own
// without the two ever meaning the same thing.
const (
	KeyField   = "key"
	ValueField = "value"
)

// A Reading is how a record's contents are named. Both are useful and neither
// is a subset of the other's behaviour, so it is said once, when the source is
// made, rather than guessed per record.
type Reading int

const (
	// Whole exposes the record entire. `key` is the document's own key for it
	// and `value` is its value, and every member wears a dot: `.size` is the
	// member called size, `.0` is the item at position 0.
	//
	// Nothing can shadow anything, so a member called `key` -- which the bundle
	// format's own `_bundle: (key: "figaro")` has -- is `.key` and is reachable
	// like any other. It is the reading for data whose records are not all the
	// same shape, and for anything that has to survive a round trip.
	//
	// `key` here is a field. It is what the document called this record, handy
	// to sort or show; what this end identifies the record by travels beside
	// the bag and is nobody's field.
	Whole Reading = iota

	// Members exposes the members alone, under their own names: `size`, not
	// `.size`. It is the shorter reading, and it suits a source whose records
	// are all lists of named fields -- which is most of them.
	//
	// It is deliberately not complete: the record's own key and value have no
	// name here, and positions have none either, a bare `0` being the number it
	// spells rather than a name. A member called `key` is a
	// member like any other and is reached and sent as one -- the point of this
	// reading is the document at its face value, and nothing in the bag is an
	// identity for it to collide with.
	Members
)

// A PSLSource is a data source backed by one parsed PSL list.
//
// It is read-only and its records do not move, so everything computed from them
// stays true: an ordering is built once per spec and reused for every scope
// drawn from it.
type PSLSource struct {
	recs    []pslRecord
	reading Reading

	mu     sync.Mutex
	cache  map[string]*ordering
	recent []string // cache keys, oldest first
}

// ParsePSLSource reads PSL text and presents it as a data source.
func ParsePSLSource(text string, reading Reading) (*PSLSource, error) {
	n, err := pawscript.ParsePSL(text)
	if err != nil {
		return nil, err
	}
	return NewPSLSource(n, reading), nil
}

// NewPSLSource presents an already-parsed PSL list as a data source: its items as
// records keyed by index, its keyed members as records keyed by name.
func NewPSLSource(n *pawscript.PSLNode, reading Reading) *PSLSource {
	p := &PSLSource{reading: reading, cache: map[string]*ordering{}}
	p.recs = make([]pslRecord, 0, n.Len()+len(n.Map()))
	for i := 0; i < n.Len(); i++ {
		v, _ := n.Item(i)
		p.recs = append(p.recs, pslRecord{reading: reading, key: NewInt(int64(i)), value: v})
	}
	for _, k := range sortedKeys(n) {
		v, _ := n.Get(k)
		p.recs = append(p.recs, pslRecord{reading: reading, key: NewText(k), value: v})
	}
	return p
}

// Len is how many records the source holds, before any filter.
func (p *PSLSource) Len() int { return len(p.recs) }

// Open states a sequence over the records: one filter, one sort.
func (p *PSLSource) Open(spec *Spec) (DataSet, error) {
	if spec == nil {
		spec = &Spec{}
	}
	if err := supported(spec.Sort, p.reading); err != nil {
		return nil, err
	}
	return &pslDataSet{src: p, spec: spec, ord: p.order(spec)}, nil
}

// supported refuses a sort this source cannot produce exactly. A collation it
// does not carry, or a field this reading cannot reach, would yield an order
// that is nearly right, which is worse than a refusal: a refusal is
// recoverable and says what is wrong.
func supported(levels []SortLevel, reading Reading) error {
	for _, l := range levels {
		switch l.Collation {
		case "", CollateExact, CollateFold, CollateNatural:
		default:
			return fmt.Errorf("sort %s: no collation called %q", l.Field, l.Collation)
		}
	}
	return nil
}

// A pslRecord is one record: its key, and the PSL value it stands for.
type pslRecord struct {
	reading Reading
	key     *Value
	value   any // *pawscript.PSLNode for a list, the value itself otherwise
}

// Field is one of the record's values by name, as this source's reading names
// them: `key`, `value` and `.member` under Whole, and `member` alone under
// Members.
func (r pslRecord) Field(name string) *Value {
	node, isList := r.value.(*pawscript.PSLNode)
	if r.reading == Members {
		if !isList {
			return nil
		}
		// A member called `key` is a member. Nothing here is the record's
		// identity, so there is nothing for it to collide with and no reason
		// to hide it -- the point of this reading is the document at its face
		// value.
		if v, ok := node.Get(name); ok {
			return pslValue(v)
		}
		return nil
	}

	switch {
	case name == KeyField:
		return r.key
	case name == ValueField:
		if isList {
			// A list is its members. There is no value beside them, and a
			// record that answered with the whole list here would carry it
			// twice -- once under this name and once member by member.
			return nil
		}
		return pslValue(r.value)
	case len(name) < 2 || name[0] != '.':
		return nil
	}
	if !isList {
		return nil
	}
	if i, digits := itemIndex(name); digits {
		if v, ok := node.Item(i); ok {
			return pslValue(v)
		}
		return nil
	}
	if v, ok := node.Get(name[1:]); ok {
		return pslValue(v)
	}
	return nil
}

// fields is everything the record carries, once.
//
// Under Whole that is `key`, `value`, and a list's members each wearing a dot.
// Under Members it is the keyed members alone, under their own names: a
// record's positions have no name to go out under.
//
// Nothing is dropped either way. A member called `key` is a member like the
// rest, because the record's identity is not in this bag at all and there is
// nothing for it to shadow.
func (r pslRecord) fields() Record {
	node, isList := r.value.(*pawscript.PSLNode)
	if r.reading == Members {
		if !isList {
			return nil
		}
		out := make(Record, 0, len(node.Map()))
		for _, k := range sortedKeys(node) {
			v, _ := node.Get(k)
			out = append(out, &Field{Name: k, Value: pslValue(v)})
		}
		return out
	}

	if !isList {
		return Record{
			{Name: KeyField, Value: r.key},
			{Name: ValueField, Value: pslValue(r.value)},
		}
	}
	out := make(Record, 0, node.Len()+len(node.Map())+1)
	out = append(out, &Field{Name: KeyField, Value: r.key})
	for i := 0; i < node.Len(); i++ {
		v, _ := node.Item(i)
		out = append(out, &Field{Name: itemName(i), Value: pslValue(v)})
	}
	for _, k := range sortedKeys(node) {
		v, _ := node.Get(k)
		out = append(out, &Field{Name: memberName(k), Value: pslValue(v)})
	}
	return out
}

// pslValue is a PSL value as one of ours.
//
// A nested list has no order of its own, so it takes the unordered rank, and it
// crosses as a block of its own members -- the same shape a record's fields
// take, which is what it is. Its contents are written the Whole way whatever
// the source's reading, because a position inside it has no other spelling.
func pslValue(v any) *Value {
	switch x := v.(type) {
	case nil:
		return NewNil()
	case bool:
		if x {
			return NewBool(true)
		}
		return NewBool(false)
	case int64:
		return NewInt(x)
	case int:
		return NewInt(int64(x))
	case float64:
		return NewFloat(x)
	case string:
		return NewText(x)
	case pawscript.Symbol:
		// PSL writes an absent value as the bare word `undefined`, which is a
		// value and not a name: a member present with nothing under it. So it
		// is read as nothing, and a filter asking about undefined finds it.
		//
		// That is a fact about PSL and is decided here, where PSL is read.
		// Anywhere past this a symbol is only ever a name.
		if x == "undefined" {
			return nil
		}
		// Every other bare word is a symbol, which ranks between a number and
		// text: a name, compared exactly and under no collation. So
		// `kind: text` and `kind: "text"` reach a filter as the two different
		// questions they were written as, and neither turns into the other.
		return NewSymbol(string(x))
	case *pawscript.PSLNode:
		return NewList(pslRecord{reading: Whole, value: x}.fields())
	}
	return NewText(fmt.Sprintf("%v", v))
}

// itemName and memberName are how a record's own contents are written: a dot,
// and then the position or the name.
func itemName(i int) string      { return "." + strconv.Itoa(i) }
func memberName(k string) string { return "." + k }

// itemIndex reads a member name that addresses a position rather than a key:
// after the dot, ASCII digits and nothing else. A run of digits too long to be
// an index is still one, and is simply past the end of every list there could
// be.
func itemIndex(name string) (int, bool) {
	digits := name[1:]
	if digits == "" {
		return 0, false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, false
		}
	}
	i, err := strconv.Atoi(digits)
	if err != nil {
		return -1, true
	}
	return i, true
}

// sortedKeys is a node's keyed members in a settled order. Go's map iteration
// is deliberately unordered, and a record's fields have to come out the same
// way twice; PSL's own serializer sorts them for the same reason.
func sortedKeys(n *pawscript.PSLNode) []string {
	m := n.Map()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- the ordering -------------------------------------------------------

// An ordering is the sequence one spec names: which records are in it, and in
// what order.
//
// The sort tuples are kept beside the rows because they are what the sort and
// every later binary search compare. Extracting a field is a map lookup and a
// conversion; doing it once per record rather than once per comparison is the
// difference between a sort that reads the data n log n times and one that
// reads it once.
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
func (p *PSLSource) order(spec *Spec) *ordering {
	key := dataSetKey(spec)
	p.mu.Lock()
	defer p.mu.Unlock()
	if o := p.cache[key]; o != nil {
		return o
	}

	// The filter runs first, so a record that is not in the sequence is never
	// sorted and never has its sort fields read.
	o := &ordering{levels: ordering1(spec)}
	for i := range p.recs {
		if !Match(p.recs[i].key, p.recs[i], spec.Filter) {
			continue
		}
		o.rows = append(o.rows, i)
		o.tuples = append(o.tuples, tupleOf(p.recs[i], spec.Sort))
	}
	// The record's identity is the last level and no two records share one, so
	// no two tuples are equal and there is nothing for stability to settle.
	sort.Sort(o)

	o.at = make(map[string]int, len(o.rows))
	for i, row := range o.rows {
		o.at[Key(p.recs[row].key)] = i
	}

	p.cache[key] = o
	p.recent = append(p.recent, key)
	for len(p.recent) > orderingsKept {
		delete(p.cache, p.recent[0])
		p.recent = p.recent[1:]
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
func tupleOf(rec pslRecord, levels []SortLevel) []*Value {
	out := make([]*Value, 0, len(levels)+1)
	for _, l := range levels {
		out = append(out, rec.Field(l.Field))
	}
	return append(out, rec.key)
}

// --- the data set ------------------------------------------------------

type pslDataSet struct {
	src  *PSLSource
	spec *Spec
	ord  *ordering
}

// Close lets the data set go. The ordering stays in the source's cache until
// something newer pushes it out, because the records it orders have not moved.
func (v *pslDataSet) Close() { v.ord = nil }

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
func (v *pslDataSet) Read(s *Scope, out Sink) error {
	o := v.ord
	if o == nil {
		return fmt.Errorf("this data set has been closed")
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

	done := Complete{}
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
		rec := v.src.recs[o.rows[i]]
		bag, has, whole := v.fields(rec)
		var err error
		if whole {
			err = out.Record(rec.key, bag)
		} else {
			err = out.Subset(rec.key, bag, has)
		}
		if err != nil {
			return err
		}
		last = rec.key
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
func (v *pslDataSet) fields(rec pslRecord) (Record, Totals, bool) {
	bag := rec.fields()
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
func (v *pslDataSet) without(bag Record) Record {
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
