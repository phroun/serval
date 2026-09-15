package serval

// The flesh: what records hold, kept where it belongs -- with the SOURCE.
//
// A record's values are knowledge about the source it came from, and not about
// any sequence over it. Sort the same files by name and then by size and the
// order is a different order -- but record 7 is the same record, carrying the
// same fields, in both. So the values are held once per source, and every data
// set drawing on that source reads the one copy.
//
// # A cache of its own
//
// This is not a table hanging off the runs. It is a SECOND CACHE, with its own
// room, its own books and its own eviction, and what it holds answers to
// nothing in the first one:
//
//   - **Flesh outlives the order.** A record stays known when the run that
//     placed it is evicted, or when the reader closes one sort and opens
//     another. That is the whole point: re-sorting is a new order over records
//     whose values are all still right, and re-fetching them would be work
//     nobody needed.
//   - **The order outlives the flesh.** A run stays when the records standing
//     in it are evicted. What it knows -- which record comes after which, with
//     nothing missing between -- is what lets the fields be asked for again for
//     exactly those records, rather than the stretch being walked from the
//     start.
//   - So neither pins the other, and a place holds no pointer here. It would
//     keep the fields on the heap after this cache had let go of them, which is
//     the one thing a cost that comes off the books is supposed to mean.
//
// Records are found by source and identity in one lookup, and evicted by the
// same two-segment rule the runs use -- probation entirely before protection,
// so that a scan of a million records cannot flush out the handful being read.
// The segments are lists here rather than run ends, because a record can be
// taken from anywhere: dropping one out of the middle costs no sequence,
// there being no sequence in this cache at all.
//
// # Knowledge only grows
//
// A second answer about a record already known is ADDED to what is known rather
// than put in its place. A field already here keeps the value it has, because a
// value that has stopped being true is FORGOTTEN rather than replaced -- see
// invalidate.go -- so a field still standing is one nothing has been said
// against, and the answer already filed is as good as the one that just
// arrived; a field this answer brought and the last one did not is new
// knowledge, and is kept.
//
// Which is what makes a top-up possible: a query wanting one field more than is
// known can ask for that field alone, over the records a run already places,
// and file the answer here.
//
// # Three ways to know an answer
//
// A record answers a question about a field it CARRIES, obviously. It also
// answers one about a field it carries as an ABSENCE -- a name that was asked
// about and is not there, kept as a member with nothing under it so that nobody
// asks again.
//
// And it answers one the TOTALS settle. Every record says how many members it
// has, ordered and named counted apart, whether or not they were all sent. A
// record with three ordered members has `0`, `1` and `2` and nothing else, so
// `3` is absent without anyone being asked. One with eight named members, eight
// of which are here, has nothing left for a ninth name to be. Which is what
// turns "I have seven of eight and you want two more" into one question about
// one field instead of two about two -- and "I have eight of eight" into no
// question at all.
//
// A record whose members are all known is known ENTIRE, and answers anything
// asked about it. That is not a flag a source sets: it is what the two counts
// say, so a run of subsets that between them cover a record leaves it entire
// without anybody deciding to.

// A cachedRecord is what the cache knows about one record of one source.
type cachedRecord struct {
	// src is the source these fields came from. Two data sets over one source
	// share this; a data set over another source has its own.
	src string
	id  *Value

	// fields is what is known: the members that have been seen, and the names
	// that have been ASKED about and are not there, which are kept as absences
	// so that nobody asks again.
	fields Record

	// has is how many members the record has altogether, as its source said,
	// and knows how many of them are here. When knows reaches has the record is
	// known ENTIRE, and answers anything asked about it.
	//
	// Two counts each, because they settle different questions -- see Totals.
	has, knows Totals

	// cost is what these fields cost to hold, kept in step with them.
	cost int

	// gen is the generation of the source this was fetched at.
	//
	// Nothing reads it. A notice forgets what it names rather than comparing
	// ages, so it is not on that path: this is for the question a notice cannot
	// answer, which is whether what is held predates a change nobody was told
	// about. It is here from the start because adding it afterwards leaves
	// every record already held with a generation nobody can work out.
	gen uint64

	// hit is the tick this record was last handed to somebody, and zero for one
	// that never has been. warm says it has been handed out more than once,
	// which is what divides the two segments: once is no evidence, a scan
	// touching every record it passes exactly once.
	hit  uint64
	warm bool

	// prior and next are this record's place in its segment's list, most
	// recently handed at the front.
	prior, next *cachedRecord
}

// newRecord is one record as an answer gave it: what it carries, and how many
// members the record has altogether.
func newRecord(src string, id *Value, fields Record, has Totals, gen uint64) *cachedRecord {
	return &cachedRecord{
		src: src, id: id, fields: fields, gen: gen,
		has: has, knows: Tally(fields),
		cost: costOfFields(fields),
	}
}

// entire reports whether every member the record has is here, which is what
// lets it answer a question naming no fields at all.
func (r *cachedRecord) entire() bool {
	return r.knows.Ordered >= r.has.Ordered && r.knows.Named >= r.has.Named
}

// absent reports whether the record is KNOWN not to carry a name that is not
// among its fields -- which is what the totals are for.
//
// An ordered member is settled by the count alone: a record with three of them
// has `0`, `1` and `2` and nothing else, so `3` and everything past it is
// absent. A named one is settled once as many are known as there are, there
// being nothing left for another name to be.
func (r *cachedRecord) absent(name string) bool {
	if i, ok := MemberIndex(name); ok {
		return i >= r.has.Ordered
	}
	return r.knows.Named >= r.has.Named
}

// answers reports whether what is known about this record answers a query
// wanting these fields.
//
// A record that arrived whole answers anything. Otherwise every field wanted
// has to be here: a query naming a field this record has not got is a question
// nothing held can answer, however many of its other fields are known.
//
// Wanting NOTHING in particular is wanting the record entire. And a record this
// cache has let go of answers nothing, which is the nil case: the order may
// still know where it stood.
func (r *cachedRecord) answers(wanted Record) bool {
	if r == nil {
		return false
	}
	if r.entire() {
		return true
	}
	if len(wanted) == 0 {
		return false
	}
	for _, w := range wanted {
		// Carried, carried as an absence, or settled by the totals. All three
		// are knowing the answer; only the fourth case is a question.
		if !r.fields.Has(w.Name) && !r.absent(w.Name) {
			return false
		}
	}
	return true
}

// learn adds what an answer said to what is known, and gives back what that
// added to the cost.
//
// Nothing already known is overwritten, an absence included: a name already
// filed as not there keeps that answer, two answers about one record of one
// source not contradicting each other, and whatever has stopped being true
// having been forgotten before this arrived. A record already known entire learns nothing, there being nothing left
// to learn.
func (r *cachedRecord) learn(fields Record, has Totals, gen uint64) int {
	if r.entire() {
		return 0
	}
	before := r.cost
	for _, f := range fields {
		if r.fields.Has(f.Name) {
			continue
		}
		r.fields = append(r.fields, f)
	}
	// The newer statement of how many there are stands. A source counting a
	// record it has just read is better placed than one counting it last time,
	// and neither is guessing.
	r.has = has
	r.knows = Tally(r.fields)
	r.gen = gen
	r.cost = costOfFields(r.fields)
	return r.cost - before
}

// --- the cache ------------------------------------------------------------

// A recordCache holds what records are known to carry, one entry per source and
// identity, for every data set drawing on that source.
//
// It is guarded by the lock of the cache that owns it: the two are reached
// together on every answer, so one lock is one lock less to get wrong.
type recordCache struct {
	limit int // what it will hold
	cost  int // what it is holding
	warm  int // and how much of that is protected

	// tick orders hand-outs. A tick rather than a clock: it only has to order,
	// and a monotonic counter cannot go backwards when the machine's time does.
	tick uint64

	at map[string]*cachedRecord

	// The two segments, most recently handed at the front.
	coldFront, coldBack *cachedRecord
	warmFront, warmBack *cachedRecord
}

func newRecordCache(limit int) *recordCache {
	return &recordCache{limit: limit, at: map[string]*cachedRecord{}}
}

// get is what is known about one record, and nil for one this cache has let go
// of or never saw.
func (rc *recordCache) get(src string, id *Value) *cachedRecord {
	return rc.at[keyed(src, id)]
}

// learn files what an answer said about one record.
//
// A record nothing has seen is filed as it stands. One already known keeps what
// it had and takes what this answer brought that it had not. Either way the
// cache is brought back under its limit afterwards rather than before, so that
// what just arrived competes for the room on the same terms as everything else
// -- and may lose, which is what a cache too small for one record does.
func (rc *recordCache) learn(src string, r *cachedRecord) {
	k := keyed(src, r.id)
	if held := rc.at[k]; held != nil {
		d := held.learn(r.fields, r.has, r.gen)
		rc.cost += d
		if held.warm {
			rc.warm += d
		}
		rc.room()
		return
	}
	r.src = src
	rc.at[k] = r
	rc.cost += r.cost
	rc.hook(r)
	rc.room()
}

// handed marks a record as given out, which is what decides what is worth
// keeping. A record given out a second time has proved something a record given
// out once has not.
func (rc *recordCache) handed(r *cachedRecord) {
	rc.tick++
	rc.unhook(r)
	if r.hit != 0 && !r.warm {
		r.warm = true
		rc.warm += r.cost
	}
	r.hit = rc.tick
	rc.hook(r)
	rc.capWarm()
}

// room evicts until what is held is under the limit.
//
// Probation first and entirely: every unproven record goes before a proven one
// is touched, which is the rule a flood cannot get around. Only when there is
// nothing left on probation does the coldest protected record give way, and a
// cache of nothing but proven records would otherwise wedge -- full, unable to
// evict, and unable to hold anything new.
func (rc *recordCache) room() {
	for rc.cost > rc.limit {
		r := rc.coldBack
		if r == nil {
			r = rc.warmBack
		}
		if r == nil {
			return
		}
		rc.drop(r)
	}
}

// capWarm holds the protected segment under its share, so that records which
// have proved themselves cannot fill the cache and leave nothing on probation
// with a chance to prove anything. What is cooled goes to the FRONT of
// probation: it has been read twice, which is more than anything arriving for
// the first time can say.
func (rc *recordCache) capWarm() {
	for rc.warm > rc.limit*warmShare/warmOf {
		r := rc.warmBack
		if r == nil {
			return
		}
		rc.unhook(r)
		r.warm = false
		rc.warm -= r.cost
		rc.hook(r)
	}
}

func (rc *recordCache) drop(r *cachedRecord) {
	rc.unhook(r)
	delete(rc.at, keyed(r.src, r.id))
	rc.cost -= r.cost
	if r.warm {
		rc.warm -= r.cost
	}
}

// --- letting go of part of it ---------------------------------------------

// unlearn forgets what a record was holding under these names.
//
// Which is all an alteration costs the values: the names that MAY have changed
// stop being answered, and everything else about the record is as true as it
// was. A name held as an ABSENCE goes the same way -- "it has not got one" is an
// answer about that name, and an answer about that name is what is no longer
// trusted.
//
// Naming NO fields names every one of them: a source that says a record altered
// and cannot say where has said nothing about it is left to trust.
//
// # Where the totals settle it, the record goes
//
// A record answers about a name it does not carry wherever its own counts settle
// it -- three ordered members are `0`, `1` and `2` and nothing else, and eight
// named members with eight known leave nothing for a ninth name to be. If a name
// that may have changed is still settled that way once what was carried has
// gone, the answer left standing is "it has not got one", and whether it has got
// one is exactly what may have changed.
//
// The counts are what is wrong then, and they cannot be narrowed without being
// invented -- a total is the source's statement and is handed on to whoever asks
// -- so the record is dropped instead. Its place in every order is untouched,
// which is the ordinary asymmetry here.
func (rc *recordCache) unlearn(r *cachedRecord, fields []string) {
	if len(fields) == 0 {
		rc.drop(r)
		return
	}
	was := r.cost
	r.fields = lacking(r.fields, fields)
	r.knows = Tally(r.fields)
	for _, name := range fields {
		if r.absent(name) {
			rc.drop(r) // its cost is still the one the books were charged
			return
		}
	}
	r.cost = costOfFields(r.fields)
	rc.cost += r.cost - was
	if r.warm {
		rc.warm += r.cost - was
	}
}

// lacking is a record without the members these names call, absences included.
func lacking(r Record, names []string) Record {
	out := make(Record, 0, len(r))
	for _, f := range r {
		keep := true
		for _, n := range names {
			if f.Name == n {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, f)
		}
	}
	return out
}

// everything is every record held of one source, taken before anything drops
// them -- which is what a notice too vague to name records costs.
func (rc *recordCache) everything(src string) []*cachedRecord {
	out := make([]*cachedRecord, 0, len(rc.at))
	for _, r := range rc.at {
		if r.src == src {
			out = append(out, r)
		}
	}
	return out
}

// setLimit fixes how much the cache holds, evicting down to it at once.
func (rc *recordCache) setLimit(n int) {
	rc.limit = n
	rc.room()
}

// ends is the front and back of the segment a record belongs to. Which segment
// is the record's own business, so there is one place that decides it.
func (rc *recordCache) ends(r *cachedRecord) (front, back **cachedRecord) {
	if r.warm {
		return &rc.warmFront, &rc.warmBack
	}
	return &rc.coldFront, &rc.coldBack
}

func (rc *recordCache) hook(r *cachedRecord) {
	front, back := rc.ends(r)
	r.prior, r.next = nil, *front
	if *front != nil {
		(*front).prior = r
	} else {
		*back = r
	}
	*front = r
}

func (rc *recordCache) unhook(r *cachedRecord) {
	front, back := rc.ends(r)
	if r.prior != nil {
		r.prior.next = r.next
	} else {
		*front = r.next
	}
	if r.next != nil {
		r.next.prior = r.prior
	} else {
		*back = r.prior
	}
	r.prior, r.next = nil, nil
}

// --- what things cost -----------------------------------------------------

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
	entryOverhead  = 112 // a place's own struct, its two links and its slot
	recordOverhead = 128 // a cachedRecord's own struct, its slot and its links
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
