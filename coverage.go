package serval

// What a reader depends on, and which part of a sequence each field plays in.
//
// A holder of records cannot say what matters to it by naming its filter and
// its sort: the far end would have to understand both, evaluate the filter, and
// know what a collation is. So it states its concerns instead, in two pieces
// that move at completely different rates.
//
// **Roles move rarely.** Which fields decide the sequence and which are only
// shown is settled when the sequence is opened and does not change while it is
// read. **Extents move constantly** -- every scroll.
//
// Kept separable, a scroll costs a pair of boundaries rather than a field list.
// That is the whole reason they are two types rather than one.
//
// # Why the roles are the useful half
//
// Because a change can then be classified by LOOKING A NAME UP, which is a
// thing any holder of records can do in any language over any backend:
//
//	| what changed          | what it costs                                |
//	|-----------------------|----------------------------------------------|
//	| a DETAIL field        | the values of those records, and nothing else |
//	| a FILTER field        | membership -- the record may be in or out now |
//	| a SORT field          | order -- the record may stand somewhere else  |
//	| a record added or gone | the count, at a point in the order           |
//
// Nobody evaluates a filter, compares two boundaries, or learns what a
// collation is. They ask which group a field name is in.
//
// # What is here and what is not
//
// The roles and the extents are here, and what a notice against them costs is
// in invalidate.go. What is NOT here, and is the protocol's half rather than
// this library's: handles with stable ids across revisions, coalescing two
// extents that nearly meet, splitting one region into a visible chunk and a
// bulk chunk, the count as a standing concern, and individually pinned keys.
//
// Nor is the FLESH covered by an extent. An extent says which stretch of an
// ORDER is depended on, which is what membership, positions and the count are
// about; what a record holds is known per source and shared by every sequence
// over it, so a change to one record's values is a statement about that record
// by identity and not about anyone's stretch. Two caches, two channels.

import "sort"

// Roles is the fields of a sequence grouped by the part each one plays.
//
// A field may play two -- sorted and filtered both -- and appears in both
// groups when it does, because it costs both. What it may not do is be a detail
// field as well: a field that decides where a record stands is not decoration,
// however much it is also displayed.
type Roles struct {
	// Sort and Filter are the fields the SEQUENCE is decided by. A change to
	// one of them can move a record, or take it out of the sequence
	// altogether, so it costs membership or order and not merely a repaint.
	Sort   []string
	Filter []string

	// Detail is the rest of what the query asked for: fields that are carried
	// and shown and decide nothing, so a change to one costs those records'
	// values and nothing structural.
	Detail []string

	// Whole says the query asked for whole records, so its detail fields are
	// every field a record has that is not a sort or a filter field -- which
	// cannot be listed here, the records being the source's to shape. Detail is
	// then what was asked to be LEFT OUT, and the answer to "is this a detail
	// field" is yes for anything else.
	Whole bool
}

// Roles groups this spec's fields by the part each plays.
//
// The names are in the order they were first met -- sort levels top down, then
// the filter tree -- and each appears once per group. A stable order matters
// because this is stated over and over as a sequence is read, and a set that
// reshuffles itself makes every statement look like a change.
func (s *Spec) Roles() Roles {
	var r Roles
	if s == nil {
		return r
	}
	for _, l := range s.Sort {
		r.Sort = appendOnce(r.Sort, l.Field)
	}
	r.Filter = appendFilterFields(nil, s.Filter)

	r.Whole = len(s.Fields) == 0
	for _, f := range s.Fields {
		if r.decides(f.Name) || s.Exclude.Has(f.Name) {
			continue
		}
		r.Detail = appendOnce(r.Detail, f.Name)
	}
	if r.Whole {
		// What a whole record's detail fields are is every other field it has,
		// less whatever was asked to be left out. Those are the ones that CAN
		// be named.
		for _, f := range s.Exclude {
			r.Detail = appendOnce(r.Detail, f.Name)
		}
	}
	return r
}

// appendFilterFields collects the fields a filter tree tests, in the order the
// tree names them.
//
// `id` names none: a record's identity travels beside its fields rather than
// among them, and a change to what a record CONTAINS never changes what it is
// called. `has` and `lacks` name one, and a field appearing or going away is
// exactly the membership change they test for.
func appendFilterFields(out []string, f *Filter) []string {
	if f == nil {
		return out
	}
	switch f.Op {
	case OpAnd, OpOr, OpNot:
		for _, c := range f.Children {
			out = appendFilterFields(out, c)
		}
		return out
	case OpID:
		return out
	}
	if f.Field != "" {
		out = appendOnce(out, f.Field)
	}
	return out
}

func appendOnce(out []string, name string) []string {
	for _, n := range out {
		if n == name {
			return out
		}
	}
	return append(out, name)
}

// Decides reports whether a change to this field could move a record or take it
// out of the sequence.
//
// Which is the SKELETON question: a field that decides where a record stands is
// what positions, membership, the count and the watermark are built out of, and
// losing it means the stretch has to be read again from the start. A field that
// decides nothing is flesh, and re-acquiring it is a fill for the rows somebody
// is actually looking at.
func (r Roles) Decides(name string) bool { return r.decides(name) }

func (r Roles) decides(name string) bool {
	for _, n := range r.Sort {
		if n == name {
			return true
		}
	}
	for _, n := range r.Filter {
		if n == name {
			return true
		}
	}
	return false
}

// Shows reports whether this sequence carries the field without being decided
// by it -- so a change to it repaints those records and moves nothing.
//
// A sequence that asked for whole records shows everything it is not decided by
// and has not excluded, which is why this is a question rather than a list.
func (r Roles) Shows(name string) bool {
	if r.decides(name) {
		return false
	}
	if r.Whole {
		for _, n := range r.Detail {
			if n == name {
				return false // excluded: asked for, and asked against
			}
		}
		return true
	}
	for _, n := range r.Detail {
		if n == name {
			return true
		}
	}
	return false
}

// --- extents --------------------------------------------------------------

// An Extent is a stretch of one sequence that is currently depended on.
//
// Named by its ends the way a scope is -- identities, never positions -- and
// both ends INCLUSIVE, both being records that are actually held. A scope's
// ends say where to start and stop walking, which is a different question and
// is why they are exclusive; an extent says what is covered.
type Extent struct {
	First, Last *Value
}

// Covers is what this wrapper currently depends on for one sequence: the
// stretches of it being held.
//
// **The stated coverage is a superset of what is actually depended on**, which
// is the rule the whole arrangement stands on. Stating more is safe -- a
// staleness reported for something already let go of is ignored. Stating less
// is a missed invalidation, which is silent and permanent. These are taken from
// what is held, so they are exactly it, which satisfies the rule with nothing
// to spare and nothing to get wrong.
//
// **In no particular order.** They are stretches of one sequence, but which
// stretch comes before which is a question about the ORDER, and comparing two
// identities needs the records they name. Whoever asked holds that order and
// can sort them; this cannot.
func (c *CachedSource) Covers(spec *Spec) []Extent {
	if spec == nil {
		spec = &Spec{}
	}
	return hot.extents(c.key + "\x00" + dataSetKey(spec))
}

// extents is every run of one sequence, by the records at its ends.
func (c *cache) extents(set string) []Extent {
	c.mu.Lock()
	defer c.mu.Unlock()

	runs := c.sets[set]
	out := make([]Extent, 0, len(runs))
	for _, s := range runs {
		if s.head == nil {
			continue // a run of nothing covers nothing
		}
		out = append(out, Extent{First: s.head.id, Last: s.tail.id})
	}
	// Runs come out of a map, so the same cache would otherwise state the same
	// coverage in a different arrangement each time and every statement would
	// look like a revision. Sorting by the key of the first record is arbitrary
	// -- it is not the sequence's own order -- but it is STABLE, which is the
	// property that matters here.
	sort.Slice(out, func(i, j int) bool {
		return Key(out[i].First) < Key(out[j].First)
	})
	return out
}
