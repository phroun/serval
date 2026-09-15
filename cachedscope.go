package serval

// Runs of records the cache holds, and what it costs to hold them.
//
// A **cached scope** is a stretch of one data set that is guaranteed complete
// between its two ends: every record of the sequence falling between them is
// here, in order, with nothing missing. That is the same claim a watermark
// makes, which is why a scope's answer and a cache entry are the same object --
// what came back from one question is what can be handed to the next.
//
// Both ends are optional, and their absence is a stronger claim rather than
// ignorance. No beginning means from the start of the sequence; no end means to
// the end of it. So a source that ignores every hint, sends everything and says
// `exhausted` leaves one cached scope with neither end: the whole sequence,
// held entire, never asked for again.
//
// **The records are a doubly linked list, and the order is the data set's own.**
// A data set is a source, a sort and a filter, which is already an order, so
// there is no second one to invent and no ordinal to keep -- an entry's place is
// where its own values put it. Linked rather than laid out in order because of
// what happens to a run when the data changes underneath it: an insert splices
// one node in and cuts one link, and a merge joins one tail to one head, where
// an array would copy. Doubly linked because a scope can be read backwards, and
// because trimming the cold end off the back would otherwise walk the whole run
// to find the node before the last.
//
// Nothing is searched. An entry is found by identity in one lookup, and serving
// a scope is walking that many links from it -- forwards or backwards -- ending
// at the run's own end, which is exactly where the guarantee ends.
//
// **A run holds no fields.** An entry is a PLACE -- which record stands here,
// and what stands either side of it -- and what that record holds is kept once
// per source, in the table cachedrecord.go describes. So a run is an order and
// nothing else, two runs of one sequence join wherever their ends meet, and the
// fields a query asked for change nothing about the shape of what is held.

// A cachedScopeID names one run. Entries carry it so that a record found by
// identity says which guarantee it falls under without walking to find out.
type cachedScopeID uint64

// An entry is one record's PLACE in a run: which record stands here, and what
// stands either side of it.
type entry struct {
	scope      cachedScopeID
	prev, next *entry

	id *Value

	// cost is what this place costs to hold: its own struct, its two links and
	// its identity, and nothing else. What the record standing here HOLDS is
	// the flesh cache's to count -- see cachedrecord.go -- and this run neither
	// pays for it nor keeps it alive.
	//
	// It is worked out once and never moves, so eviction gives back exactly
	// what filing took however wrong the estimate behind it is.
	cost int

	// hit is the tick this entry was last handed to somebody, and zero for one
	// that never has been.
	//
	// A tick rather than a clock: it only has to order, it is compared far more
	// often than it is set, and a monotonic counter cannot go backwards when the
	// machine's time does. It is per entry rather than per run so that a cold
	// end can be trimmed off a warm one instead of dropping the lot.
	hit uint64

	// warm says this record has been handed out more than once.
	//
	// Once is no evidence: a walk that reads a million records and never looks
	// back touches every one of them exactly once, and each is the most recently
	// used thing in the cache the moment it lands. Twice is evidence, and it is
	// what divides the two segments -- see cache.go.
	//
	// It is the PLACE that is warm and not the record: what has proved itself
	// here is a reader coming back to this stretch of this sequence. What the
	// record standing here has proved is the flesh cache's own reckoning, kept
	// separately because the two are evicted separately.
	warm bool
}

// newEntry is one place in a run: which record stands here, and what it costs
// to remember that.
func newEntry(id *Value) *entry {
	return &entry{id: id, cost: entryOverhead + costOfValue(id)}
}

// A cachedScope is a run of one data set's records, guaranteed complete between
// its two ends.
type cachedScope struct {
	id cachedScopeID

	// set is the data set these records belong to: a source, a sort and a
	// filter. Records of two sequences are never in one run, because "complete
	// between its ends" is a claim about an order and they have different ones.
	set string

	// begin is the record this run is guaranteed FROM, exclusive, and end the
	// one it is guaranteed TO, inclusive. Nil begin is the start of the sequence
	// and nil end is the end of it -- both claims rather than gaps.
	begin, end *Value

	head, tail *entry
	n          int
	cost       int

	// cold is what the run's UNWARM records cost -- the part of it that is
	// still on probation. A run streaming in from a source is entirely cold,
	// which is how a scan is told from a working set that happens to be large.
	cold int
}

// Len is how many records the run holds, and Cost what it costs to hold them.
func (s *cachedScope) Len() int  { return s.n }
func (s *cachedScope) Cost() int { return s.cost }

// Cold is what the run's records cost that have not proved themselves.
func (s *cachedScope) Cold() int { return s.cold }

// warmUp and coolDown move one record between the segments, keeping the run's
// share of each right. What they cost the cache as a whole is the cache's own
// bookkeeping; this is only the run's part of it.
func (s *cachedScope) warmUp(e *entry) {
	if !e.warm {
		e.warm = true
		s.cold -= e.cost
	}
}

func (s *cachedScope) coolDown(e *entry) {
	if e.warm {
		e.warm = false
		s.cold += e.cost
	}
}

// pushBack and pushFront add one entry at an end, which is what answering a
// scope just past one of them amounts to.
func (s *cachedScope) pushBack(e *entry) {
	e.scope, e.prev, e.next = s.id, s.tail, nil
	if s.tail != nil {
		s.tail.next = e
	} else {
		s.head = e
	}
	s.tail = e
	s.n++
	s.cost += e.cost
	if !e.warm {
		s.cold += e.cost
	}
}

func (s *cachedScope) pushFront(e *entry) {
	e.scope, e.prev, e.next = s.id, nil, s.head
	if s.head != nil {
		s.head.prev = e
	} else {
		s.tail = e
	}
	s.head = e
	s.n++
	s.cost += e.cost
	if !e.warm {
		s.cold += e.cost
	}
}

// unlink takes one entry out and gives back what that freed.
//
// It says nothing about the guarantee. Taking a record out of the MIDDLE of a
// run breaks the claim across it, so whoever does that splits the run as well;
// taking one off an end is what trimming does, and it moves the end with it.
func (s *cachedScope) unlink(e *entry) int {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		s.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		s.tail = e.prev
	}
	e.prev, e.next = nil, nil
	s.n--
	s.cost -= e.cost
	if !e.warm {
		s.cold -= e.cost
	}
	return e.cost
}

// splitAfter cuts the run in two just past one entry, which is what an insert
// there does to it.
//
// The guarantee holds on both sides of the cut and not across it, so what comes
// back is two runs holding the same records between them: this one now ends at
// `at`, and the new one begins there. Nothing is copied and nothing is
// re-queried -- one link is broken, and the shorter side is relabelled, because
// an entry says which guarantee it falls under and half of them now fall under
// another.
//
// Nil comes back where there is nothing past `at` to make a run of.
func (s *cachedScope) splitAfter(at *entry, id cachedScopeID) *cachedScope {
	if at == nil || at.next == nil {
		return nil
	}
	rest := at.next
	at.next, rest.prev = nil, nil

	left := &cachedScope{
		set: s.set, begin: s.begin, end: at.id,
		head: s.head, tail: at,
	}
	right := &cachedScope{
		set: s.set, begin: at.id, end: s.end,
		head: rest, tail: s.tail,
	}
	for e := left.head; e != nil; e = e.next {
		left.n++
		left.cost += e.cost
		if !e.warm {
			left.cold += e.cost
		}
	}
	right.n, right.cost = s.n-left.n, s.cost-left.cost
	right.cold = s.cold - left.cold

	// The id is opaque, so it stays with whichever side is longer and the
	// shorter one is relabelled. Either would be correct; this is the cheaper.
	keep, move := left, right
	if right.n > left.n {
		keep, move = right, left
	}
	keep.id, move.id = s.id, id
	for e := move.head; e != nil; e = e.next {
		e.scope = move.id
	}

	*s = *left
	return right
}

// merge takes another run into this one where they meet.
//
// They meet where this one's guarantee ends exactly at the other's beginning, so
// that between them nothing is unaccounted for. It is what a split undoes, and
// what two scopes answered back to back amount to: one link is made, and the
// other run's entries are relabelled.
//
// Which fields either answer asked for has nothing to say about it. A run is an
// order, and two stretches of one order meet or they do not; what each record
// holds is the source's business, and is the same copy whichever answer brought
// it.
func (s *cachedScope) merge(other *cachedScope) bool {
	if s.set != other.set || s.end == nil || other.begin == nil {
		return false
	}
	if !Equal(s.end, other.begin) {
		return false
	}
	for e := other.head; e != nil; e = e.next {
		e.scope = s.id
	}
	if other.head != nil {
		if s.tail != nil {
			s.tail.next = other.head
			other.head.prev = s.tail
		} else {
			s.head = other.head
		}
		s.tail = other.tail
	}
	s.end = other.end
	s.n += other.n
	s.cost += other.cost
	s.cold += other.cold
	return true
}

// trimFront and trimBack take an end off, giving back what that freed.
//
// Trimming keeps a run true: dropping records from an end and moving that end in
// with them leaves the claim between the ends exactly as good as it was. Taking
// something out of the MIDDLE would not -- that is a split, and it costs a run
// rather than freeing one.
func (s *cachedScope) trimFront(n int) int {
	freed := 0
	for i := 0; i < n && s.head != nil; i++ {
		e := s.head
		s.begin = e.id
		freed += s.unlink(e)
	}
	return freed
}

func (s *cachedScope) trimBack(n int) int {
	freed := 0
	for i := 0; i < n && s.tail != nil; i++ {
		e := s.tail
		if e.prev != nil {
			s.end = e.prev.id
		} else {
			s.end = s.begin
		}
		freed += s.unlink(e)
	}
	return freed
}

// serve walks the run from one entry, the way a scope reads it.
//
// Forwards it starts at the entry past `from`; backwards, at the one before it.
// Either way it stops at the run's own end, which is where the guarantee stops,
// so nothing has to check whether it has walked out of what was promised.
//
// `from` is nil for a scope starting at the run's own beginning -- or, read
// backwards, at its end.
func (s *cachedScope) serve(from *entry, count int, back bool) []*entry {
	var at *entry
	switch {
	case from == nil && back:
		at = s.tail
	case from == nil:
		at = s.head
	case back:
		at = from.prev
	default:
		at = from.next
	}
	out := make([]*entry, 0, count)
	for ; at != nil && len(out) < count; at = at.along(back) {
		out = append(out, at)
	}
	return out
}

// along is the next entry in the direction a walk is going.
func (e *entry) along(back bool) *entry {
	if back {
		return e.prev
	}
	return e.next
}

// coldest is when each end was last read, which is what says which end of a run
// is worth trimming.
func (s *cachedScope) coldest() (front, back uint64) {
	if s.head == nil {
		return 0, 0
	}
	return s.head.hit, s.tail.hit
}
