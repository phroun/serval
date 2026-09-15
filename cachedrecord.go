package serval

// What is known about one record, kept where it belongs: with its SOURCE.
//
// A record's values are knowledge about the source it came from, and not about
// any sequence over it. Sort the same files by name and then by size and the
// order is a different order -- but record 7 is the same record, carrying the
// same fields, in both. So the values are held once per source, and every data
// set drawing on that source reads the one copy.
//
// That is what stops a sequence forking. A run used to carry the fields its own
// answer happened to ask for, so the same stretch fetched for `fields={ name }`
// and for `fields={ name; size }` was two runs holding two copies of every
// record, neither able to answer the other's question, and neither willing to
// join the other. Now a run carries no fields at all -- it is an ORDER and
// nothing else -- and what each record holds is one entry in a table the whole
// process shares.
//
// # Knowledge only grows
//
// A second answer about a record already known is ADDED to what is known rather
// than put in its place. A field already here keeps the value it has, because
// nothing yet decides that a held value is stale and the answer that is already
// filed is as good as the one that just arrived; a field this answer brought
// and the last one did not is new knowledge, and is kept. A record that ever
// arrived WHOLE is whole from then on, and answers every question about itself
// without being asked again.
//
// Which is also what makes a top-up possible later: a query that wants one
// field more than is known can ask for that field alone and file the answer
// here, instead of fetching every record entire to learn one thing.
//
// # What a shared record costs
//
// Once. There is one copy of the fields whatever points at it, so there is one
// charge, and a second data set sequencing the same records pays only for the
// places -- which is what makes a second order over one body of records cheap
// rather than twice the price.
//
// The charge sits on the FIRST place that pointed there, and moves to another
// when that one goes. That keeps one book rather than two: what every run says
// it holds adds up to exactly what the cache says it holds, and eviction gives
// back exactly what filing took.

// A cachedRecord is what the cache knows about one record of one source.
type cachedRecord struct {
	// src is the source these fields came from. Two data sets over one source
	// share this; a data set over another source has its own.
	src string
	id  *Value

	// fields is what is known, and whole says it is all of it.
	fields Record
	whole  bool

	// cost is what these fields cost to hold, kept in step with them: it is
	// what every place pointing here is charged, so it moves when they grow.
	cost int

	// gen is the generation of the source this was fetched at.
	//
	// Nothing reads it yet. It is what invalidation will compare against to
	// know whether what is known predates a change, and it is here from the
	// start because adding it afterwards leaves every record already held with
	// a generation nobody can work out.
	gen uint64

	// refs is the places in sequences that point here -- one per data set
	// sequencing this record. A record with none is knowledge nothing is
	// reading, and is not kept.
	//
	// The FIRST of them carries what the record costs, and hands it on when it
	// goes. Any of them would do; the first is the one there is always exactly
	// one of.
	refs []*entry
}

// newRecord is one record as an answer gave it.
func newRecord(src string, id *Value, fields Record, whole bool, gen uint64) *cachedRecord {
	return &cachedRecord{
		src: src, id: id, fields: fields, whole: whole, gen: gen,
		cost: costOfFields(fields),
	}
}

// answers reports whether what is known about this record answers a query
// wanting these fields.
//
// A record that arrived whole answers anything. Otherwise every field wanted
// has to be here: a query naming a field this record has not got is a question
// nothing held can answer, however many of its other fields are known.
//
// Wanting NOTHING in particular is wanting the record entire, which only a
// whole record is.
func (r *cachedRecord) answers(wanted Record) bool {
	if r == nil {
		return false
	}
	if r.whole {
		return true
	}
	if wanted == nil {
		return false
	}
	for _, w := range wanted {
		if !r.fields.Has(w.Name) {
			return false
		}
	}
	return true
}

// learn adds what an answer said to what is known, and gives back what that
// added to the cost.
//
// Nothing already known is overwritten. A record that was already whole learns
// nothing, there being nothing left to learn.
func (r *cachedRecord) learn(fields Record, whole bool, gen uint64) int {
	if r.whole {
		return 0
	}
	before := r.cost
	for _, f := range fields {
		if r.fields.Has(f.Name) {
			continue
		}
		r.fields = append(r.fields, f)
	}
	if whole {
		r.whole = true
	}
	r.gen = gen
	r.cost = costOfFields(r.fields)
	return r.cost - before
}

// The estimated cost of the parts a record is made of.
//
// These are for deciding when to evict, not for reporting memory, so what
// matters is that they are CONSISTENT rather than exact: what a record costs is
// worked out from what it holds, and eviction subtracts what insertion added.
// The total then cannot drift however wrong the estimate is, and being wrong
// only makes a byte limit nominal.
//
// The numbers are the real struct sizes rather than guesses -- a Value is
// 72 bytes, a Field 24, an entry 96 -- rounded up for the allocator's size
// class and the pointers that reach them. 0_cachedscope_test.go holds them to
// within a factor of the heap they model, and fails when the shape of what is
// held changes.
const (
	entryOverhead  = 112 // an entry's own struct, its two links and its slot
	recordOverhead = 96  // a cachedRecord's own struct, its slot and its ref
	fieldOverhead  = 48  // one Field and the pointer to it
	valueOverhead  = 80  // a Value beyond whatever it carries
)

// costOfFields is what one record's values cost to hold.
func costOfFields(fields Record) int {
	n := recordOverhead
	for _, f := range fields {
		n += fieldOverhead + len(f.Name) + costOfValue(f.Value)
	}
	return n
}

// costOfValue is what one value costs, following a list into its members.
func costOfValue(v *Value) int {
	if v == nil {
		return 0
	}
	n := valueOverhead
	switch v.Kind {
	case SymbolValue, TextValue, BytesValue:
		n += len(v.Str)
	case ListValue:
		for _, f := range v.List {
			n += fieldOverhead + len(f.Name) + costOfValue(f.Value)
		}
	}
	return n
}

// keyed spells a lookup key: a name, a zero byte, and the identity's own Key.
//
// The name is a data set for the table of places and a source for the table of
// records, which is the whole difference between them: where a record STANDS
// depends on the sort and the filter, and what it HOLDS does not.
//
// Key and not some spelling of the value: a table keyed by how a value is
// WRITTEN depends on a grammar it has nothing to do with, and moves every key
// it holds the day that grammar changes.
func keyed(name string, id *Value) string {
	b := make([]byte, 0, len(name)+24)
	b = append(append(b, name...), 0)
	return string(AppendKey(b, id))
}
