package serval

// The cache of answered scopes, and which of them gives way when the room runs
// out.
//
// One cache for the whole process, holding runs of records belonging to many
// data sets. A scope's answer and a cached run are the same object -- complete
// between two ends -- so holding an answer is filing it, and answering from the
// cache is walking one that is already here. Nothing is re-derived: a run that
// meets another joins it, and a run asked for part of itself hands back a slice
// of its own links.
//
// # Two caches, because there are two different things here
//
// Where a record STANDS belongs to a data set: a source, a sort and a filter
// decide it between them, and change it between them. What a record HOLDS
// belongs to the SOURCE: sort the same files by name and then by size and
// record 7 carries the same fields in both.
//
// So they are two caches and not two tables. This one is the SKELETON -- runs
// of places, with no fields in them at all. cachedrecord.go is the FLESH, one
// entry per source and identity, read by every data set drawing on that source.
// Each has its own room, its own books and its own eviction, and neither keeps
// the other alive:
//
//   - The flesh outlives the order. Close one sort and open another and the new
//     order is new, while every value it needs is still here.
//   - The order outlives the flesh. A run whose records have been evicted still
//     knows what comes after what, which is what lets the fields be asked for
//     again for exactly those records rather than the stretch being walked from
//     the start.
//
// A place therefore holds an identity and not a pointer, and what is known
// about the record standing there is looked up when it is wanted.
//
// One run per stretch of a sequence follows from that, and it is what the split
// was for: a stretch fetched for `fields={ name }` and the same stretch fetched
// for `fields={ name; size }` used to be two runs that could not answer each
// other's question and would not join. Now they are one run, and the second
// answer teaches the flesh what the first one did not know.
//
// # Keeping the hot records against a cold flood
//
// The thing an LRU cannot do is survive a scan. Recency is only a proxy for
// worth, and a walk that reads a million records and never looks back
// manufactures recency for free: every one of those records is the most
// recently used thing in the cache the moment it lands, and between them they
// push out everything that had actually proved itself. The records that were
// being used lose to records nothing ever wanted twice.
//
// So there are two segments, which is the standard answer (segmented LRU, or
// 2Q):
//
//   - Every PLACE arrives on probation. A record handed out once is no evidence
//     of anything -- a scan touches every record it passes exactly once.
//   - A place handed out a SECOND time is warm, and moves to the protected
//     segment. The place and not the record: what has proved itself HERE is a
//     reader coming back to this stretch of this sequence. What the record has
//     proved is the flesh cache's own reckoning, on its own segments.
//   - Eviction always takes from probation. Not preferentially: entirely. A
//     place that has proved itself is only ever touched once there is no
//     probationary place left anywhere in the cache to take, and it is that
//     rule a flood cannot get around, however many records it pours in.
//
// Two caps keep either segment from swallowing the cache:
//
//   - The protected segment is held under warmShare of the limit, so that what
//     has proved itself cannot fill the cache and leave no room to prove
//     anything else.
//   - One run's PROBATIONARY part is held under coldShare of the limit, and
//     that is the flood defence proper. A streaming answer is entirely cold, so
//     a run pouring in eats its own far end rather than the rest of the cache
//     once it passes the cap. It costs a large scan nothing it was going to use
//     anyway -- it never looks back -- and it costs everything else nothing at
//     all.
//
// A frequency sketch on top of this (TinyLFU: admit a record only if it scores
// better than the record it would evict, counting what has been ASKED for
// rather than what is held) is the stronger answer, and is what to reach for if
// measurement says these two are not enough. It is deliberately not here yet.
//
// # What decides that something held is wrong
//
// Nothing here does. A source SAYS so, and invalidate.go is what saying so
// costs: the values forgotten for the source, the order forgotten per sequence,
// or neither. Nothing is polled, nothing expires and no generation is compared
// -- a cache nobody tells holds what it holds, which is right for a source that
// does not change and is the source's problem for one that does.
//
// Which is also why the cache refuses to hold an answer whose records already
// stand somewhere in this sequence. Two answers putting one record in two
// places are two orders, and nothing here can tell which of them is right; the
// one already filed is kept, and whoever knows the order changed says so. What
// those records HOLD is not refused the same way -- knowledge only grows, and a
// second answer about a record already known adds to it.

import (
	"sync"
)

// defaultCacheLimit is what the cache holds until somebody says otherwise. A
// nominal figure in the units cachedrecord.go counts, which are close enough to
// bytes to reason in.
const defaultCacheLimit = 64 << 20

// The shares things are held to, as numerator and denominator so that they read
// as what they are.
const (
	warmShare, warmOf = 4, 5 // the protected segment: four fifths
	coldShare, coldOf = 1, 2 // one run's probationary part: one half

	// boneShare is what the ORDER gets of a cache's room, the values getting
	// the rest. A quarter, because a place is a fraction of what the record
	// standing in it costs -- an identity and two links against however many
	// fields -- so a quarter of the room buys a great deal of sequence, and a
	// long sequence is what a reader scrolling has and what re-asking for is
	// most expensive.
	boneShare, boneOf = 1, 4
)

// hot is the process's cache. Global because a data set is global: two parts of
// a program reading the same source, sort and filter are reading the same
// sequence, and there is no reason for them to fetch it twice.
var hot = newCache(defaultCacheLimit)

// A dataSet names the two things a cached answer belongs to at once: the
// sequence it is a stretch of, and the source its records came from.
//
// They are different keys because they key different things. Two data sets over
// one source put its records in two different orders and share every one of
// their values; two sources have nothing in common however alike their
// sequences look.
type dataSet struct {
	source string // whose records these are
	set    string // which sequence of them: source, sort and filter together
}

type cache struct {
	mu sync.Mutex

	limit int // what it will hold
	cost  int // what it is holding
	warm  int // and how much of that is protected

	// tick orders hand-outs. See entry.hit.
	tick uint64

	// next names the run made next. Ids are opaque and never reused.
	next cachedScopeID

	runs map[cachedScopeID]*cachedScope
	sets map[string][]*cachedScope

	// at finds a record's PLACE by data set and identity in one lookup. One
	// place per data set: a record stands somewhere in a sequence or it does
	// not, and a run never overlaps another of the same sequence.
	at map[string]*entry

	// flesh is what those records hold, which is a cache of its own with its
	// own room and its own eviction. Nothing here points into it and nothing
	// there points back: a place holds an identity, and what is known about
	// that record is looked up when it is wanted. See cachedrecord.go.
	//
	// It is guarded by this lock, the two being reached together on every
	// answer.
	flesh *recordCache
}

func newCache(limit int) *cache {
	return &cache{
		limit: limit * boneShare / boneOf,
		next:  1, // zero names no run, so a half-built one cannot be mistaken for one

		runs:  map[cachedScopeID]*cachedScope{},
		sets:  map[string][]*cachedScope{},
		at:    map[string]*entry{},
		flesh: newRecordCache(limit - limit*boneShare/boneOf),
	}
}

// Cost is what the cache is holding, and Limit what it will hold -- both halves
// of it together, which is the figure anyone sizing it cares about.
func (c *cache) Cost() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cost + c.flesh.cost
}

func (c *cache) Limit() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limit + c.flesh.limit
}

// SetLimit fixes how much the cache holds, evicting down to it at once. The
// share between the order and the values is the same one a new cache is built
// with.
func (c *cache) SetLimit(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = n * boneShare / boneOf
	c.room(0)
	c.flesh.setLimit(n - c.limit)
}

// mostPlaces is how many places one run may hold, at a nominal cost each.
//
// It is what stops an answer being assembled whole in memory before the cache
// gets a chance to say it was never entitled to the room: an answer past this
// many is cut down as it arrives rather than afterwards. Nominal because a
// place costs whatever its identity does, and the point is a bound rather than
// a measurement -- what actually fits is settled when the run is filed.
func (c *cache) mostPlaces() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n := c.limit * coldShare / coldOf / (entryOverhead + valueOverhead); n > 1 {
		return n
	}
	return 1 // a cache with room for nothing still sees one record at a time
}

// --- answering -----------------------------------------------------------

// serve answers a scope from what is held, or says it cannot.
//
// It answers only in FULL, and in two senses. The WALK has to have got where it
// was going: a run holding the first half of what was asked for is no use,
// because the query is asked once and answered once, so half an answer would
// have to be stitched to a fetch for the rest and the fetch has to happen
// either way. And every record the walk passed has to answer for the fields
// wanted, because a stretch that knows `name` for all of them and `size` for
// most is not an answer to a question about size.
//
// So a hit is a walk that reached the count, reached `until`, or reached the
// end of the sequence, over records that are all known well enough. Falling off
// the end of the RUN, which is where its guarantee stops and not where the
// records do, is a miss -- and so is meeting a record that has not been asked
// about in enough detail yet.
func (c *cache) serve(ds dataSet, wanted Record, sc *Scope) ([]*cachedRecord, Complete, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	run, from := c.find(ds, sc)
	if run == nil {
		return nil, Complete{}, false
	}

	at := from
	switch {
	case at != nil:
		at = at.along(sc.Reversed)
	case sc.Reversed:
		at = run.tail
	default:
		at = run.head
	}

	var done Complete
	out := make([]*cachedRecord, 0, sc.Count)
	walked := make([]*entry, 0, sc.Count)
	for at != nil && (sc.Count <= 0 || len(out) < sc.Count) {
		if sc.Until != nil && Equal(at.id, sc.Until) {
			done.Stop = StopJoined
			break
		}
		r := c.flesh.get(ds.source, at.id)
		if !r.answers(wanted) {
			// The order is known and what stands here is not known well enough
			// -- or is not known at all, the flesh having been let go of while
			// the order was kept. Which is the shape a top-up will take: the
			// places are already right, and only these records need asking
			// about.
			return nil, Complete{}, false
		}
		out = append(out, r)
		walked = append(walked, at)
		at = at.along(sc.Reversed)
	}

	switch {
	case done.Stop == StopJoined:
	case sc.Count > 0 && len(out) == sc.Count:
		done.Stop = StopFilled
	case run.beyond(sc.Reversed) == nil:
		// The walk ran out inside a run whose far end is the sequence's own, so
		// there is nothing past it. No watermark: nothing to be complete up to.
		done.Stop = StopExhausted
		c.handed(run, walked, out)
		return out, done, true
	default:
		return nil, Complete{}, false // past the guarantee, not past the records
	}
	if len(out) > 0 {
		done.Watermark = out[len(out)-1].id
	}
	c.handed(run, walked, out)
	return out, done, true
}

// find is the run a scope reads and the entry it starts from, or nil for a
// scope no held run can answer.
//
// It says nothing about what those records hold: a run is an order, and whether
// what stands in it is known well enough is settled record by record as the
// walk passes them.
func (c *cache) find(ds dataSet, sc *Scope) (*cachedScope, *entry) {
	// No start named: the scope begins at the sequence's own beginning, or read
	// backwards, at its own end -- which is the run claiming that end.
	if sc.After == nil {
		for _, s := range c.sets[ds.set] {
			if s.beyond(!sc.Reversed) == nil {
				return s, nil
			}
		}
		return nil, nil
	}
	// The record stands somewhere in this sequence, and its run carries on past
	// it.
	if e := c.at[keyed(ds.set, sc.After)]; e != nil {
		if s := c.runs[e.scope]; s != nil {
			if e.along(sc.Reversed) != nil || s.beyond(sc.Reversed) == nil {
				return s, e
			}
		}
	}
	// Or it does not stand here, or stands right at the edge of its own run's
	// guarantee -- and a run guaranteed FROM this record starts in exactly the
	// place a scope past it does. Reading backwards, one guaranteed TO it ends
	// there.
	for _, s := range c.sets[ds.set] {
		if edge := s.beyond(!sc.Reversed); edge != nil && Equal(edge, sc.After) {
			return s, nil
		}
	}
	return nil, nil
}

// beyond is the end a walk in this direction runs out at: nil says the
// sequence's own end is there, so there is nothing past it.
func (s *cachedScope) beyond(back bool) *Value {
	if back {
		return s.begin
	}
	return s.end
}

// handed marks what was given out, which is what decides what is worth keeping.
// A thing given out a second time has proved something a thing given out once
// has not.
//
// Both halves, each on its own reckoning: the places here, and the records in
// the cache that holds them. They are the same walk and they part company at
// once -- a stretch read twice in one order has proved its places, while the
// records standing in it may have been read a dozen times through other orders
// or not at all.
func (c *cache) handed(run *cachedScope, es []*entry, rs []*cachedRecord) {
	for _, e := range es {
		c.tick++
		if e.hit != 0 && !e.warm {
			run.warmUp(e)
			c.warm += e.cost
		}
		e.hit = c.tick
	}
	c.capWarm()
	for _, r := range rs {
		c.flesh.handed(r)
	}
}

// --- filing --------------------------------------------------------------

// hold files an answered scope.
//
// The scope says what the run is guaranteed between: it starts past `after`,
// and it reaches the watermark -- or, where the answer was `exhausted`, the end
// of the sequence itself, which is the stronger claim and is spelled as no end
// at all. Read backwards the two ends swap, the records arriving furthest-first.
//
// What the answer says its records HOLD is always taken: knowledge only grows,
// and the flesh cache takes it whether or not anything is placed. What the
// answer says about ORDER is refused for an empty answer, for a refusal, and
// where those records already stand somewhere in this sequence.
func (c *cache) hold(ds dataSet, sc *Scope, recs []*cachedRecord, done Complete) {
	if done.Error != "" || len(recs) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// The values first, and unconditionally. Two answers about one record of
	// one source do not contradict each other -- what has stopped being true is
	// forgotten rather than replaced -- so what this one knows is added to what
	// is known however the placement below goes.
	for _, r := range recs {
		c.flesh.learn(ds.source, r)
	}

	// An answer whose records already stand somewhere in this sequence is not
	// placed again -- with nothing yet to decide which ORDER is right, the one
	// already filed is kept. Nor is one that names a record twice, which is two
	// places for one record and no order in which both are right.
	//
	// Which is what a top-up is. Ask a stretch for `fields={ name }`, then the
	// same stretch for `fields={ name; size }`: the second answer says nothing
	// new about where anything stands, and everything new about what it holds
	// -- and the values above have already taken it.
	twice := make(map[string]bool, len(recs))
	for _, r := range recs {
		k := keyed(ds.set, r.id)
		if c.at[k] != nil || twice[k] {
			return
		}
		twice[k] = true
	}

	run := &cachedScope{set: ds.set}
	if sc.Reversed {
		run.begin, run.end = done.Watermark, sc.After
		for _, r := range recs {
			run.pushFront(newEntry(r.id))
		}
	} else {
		run.begin, run.end = sc.After, done.Watermark
		for _, r := range recs {
			run.pushBack(newEntry(r.id))
		}
	}

	// A flood is cut down to its share BEFORE anything is asked to give way for
	// it, or one enormous answer would evict the whole cache on its way in and
	// only then discover it was never entitled to the room. What is dropped is
	// the front of the WALK: records arrive in walk order, so the reader is at
	// the back of what just came and will not be asking for the front of it
	// again. Which end of the run that is depends on which way the walk went.
	//
	// It is the ORDER being cut down here and not the values. The flesh has its
	// own room and its own defence against the same flood, on its own segments:
	// what a long answer pours in there is cold, and cold is what gives way.
	for max := c.limit * coldShare / coldOf; run.cold > max && run.n > 0; {
		if sc.Reversed {
			run.trimBack(1)
		} else {
			run.trimFront(1)
		}
	}
	if run.n == 0 {
		return // a cache too small to hold one place of it holds none
	}

	c.room(run.cost)
	c.cost += run.cost
	for _, e := range run.all() {
		c.at[keyed(ds.set, e.id)] = e
	}

	// Joined to whatever it meets, in either direction, so that scrolling leaves
	// one run rather than a run per screenful.
	if before := c.endingAt(ds.set, run.begin, run); before != nil && before.merge(run) {
		run = before
	} else {
		run.id = c.next
		c.next++
		for _, e := range run.all() {
			e.scope = run.id
		}
		c.runs[run.id] = run
		c.sets[ds.set] = append(c.sets[ds.set], run)
	}
	if after := c.beginningAt(ds.set, run.end, run); after != nil && run.merge(after) {
		c.forget(after)
	}
	c.capCold(run, sc.Reversed)
}

// endingAt and beginningAt are the runs `not` would meet. Runs of one data set
// are few -- each is a stretch somebody is reading -- so they are looked
// through rather than indexed.
//
// `not` is the run doing the asking, and is never the answer. A run that met
// itself would relabel its own entries, take its own count and cost into itself
// twice, and then be forgotten as the half that was absorbed -- so it is worth
// a pointer compare to make impossible, even though nothing reaches it today: a
// run in the table always spans at least one record, and one that does has a
// beginning and an end naming different records. Insurance, not a live path,
// which is why no test kills it.
func (c *cache) endingAt(set string, at *Value, not *cachedScope) *cachedScope {
	if at == nil {
		return nil
	}
	for _, s := range c.sets[set] {
		if s != not && Equal(s.end, at) {
			return s
		}
	}
	return nil
}

func (c *cache) beginningAt(set string, at *Value, not *cachedScope) *cachedScope {
	if at == nil {
		return nil
	}
	for _, s := range c.sets[set] {
		if s != not && Equal(s.begin, at) {
			return s
		}
	}
	return nil
}

// all is the run's entries, taken before anything relinks them.
func (s *cachedScope) all() []*entry {
	out := make([]*entry, 0, s.n)
	for e := s.head; e != nil; e = e.next {
		out = append(out, e)
	}
	return out
}

// --- making room ---------------------------------------------------------

// room evicts until n more will fit.
//
// Probation first and entirely: every unwarm place in the cache goes before a
// warm one is touched, which is the rule a flood cannot get around. Only when
// there is nothing left on probation anywhere does the coldest protected end
// give way, and a cache of nothing but proven places would otherwise wedge --
// full, unable to evict, and unable to hold anything new.
//
// Each pass drops one place, so this terminates whatever is held; it gives up
// only when the cache is empty and n still does not fit, which is a limit
// smaller than one place. What the record standing there HOLDS is untouched: it
// is the flesh cache's, evicted on its own terms, and a record nothing places
// any more is knowledge that is still true.
func (c *cache) room(n int) {
	for c.cost+n > c.limit {
		s, _, front := c.coldestEnd(false)
		if s == nil {
			s, _, front = c.coldestEnd(true)
		}
		if s == nil {
			return
		}
		c.drop(s, front)
	}
}

// capWarm holds the protected segment under its share, so that places which
// have proved themselves cannot fill the cache and leave nothing on probation
// with a chance to prove anything.
func (c *cache) capWarm() {
	for c.warm > c.limit*warmShare/warmOf {
		s, e, _ := c.coldestEnd(true)
		if s == nil {
			return // warm records, but none of them at an end to cool
		}
		s.coolDown(e)
		c.warm -= e.cost
	}
}

// capCold holds ONE run's probationary part under its share, which is the flood
// defence: an answer streaming in is entirely cold, so past the cap it eats its
// own far end instead of the rest of the cache. Places that have been read more
// than once are not charged against the cap and are never taken for it.
//
// Which end is the far one is the direction of the answer that just arrived,
// and not which end was read longest ago -- records that have just come in have
// been read NEVER, which sorts them coldest of all, and they are exactly the
// ones the reader is sitting on. A reader scrolling down is at the back of the
// run and will not be asking for the front of it again.
func (c *cache) capCold(s *cachedScope, back bool) {
	for s.cold > c.limit*coldShare/coldOf {
		e := s.head
		if back {
			e = s.tail
		}
		if e == nil || e.warm {
			return // nothing left, or what is there has proved itself
		}
		c.drop(s, !back)
	}
}

// coldestEnd is the least recently handed place at the end of any run, in the
// segment asked for. Only ends: taking a place out of the middle of a run would
// cost a run rather than freeing one, which is why the flesh cache -- where
// there is no run to break -- keeps lists instead.
//
// Every run is looked at, which is a walk of the runs per place evicted. Runs
// are few and places are many, so that is cheap; an eviction heap would be
// worth it only once it is not.
func (c *cache) coldestEnd(warm bool) (best *cachedScope, at *entry, front bool) {
	consider := func(s *cachedScope, e *entry, f bool) {
		if e == nil || e.warm != warm {
			return
		}
		if at == nil || e.hit < at.hit {
			best, at, front = s, e, f
		}
	}
	for _, s := range c.runs {
		consider(s, s.head, true)
		consider(s, s.tail, false)
	}
	return best, at, front
}

// drop takes one place off an end of a run, moving the run's end in with it so
// that what it still claims stays true. What that record HOLDS stays where it
// is: the flesh is another cache, and knowledge nothing places is still true.
func (c *cache) drop(s *cachedScope, front bool) {
	e := s.tail
	if front {
		e = s.head
	}
	if e == nil {
		return
	}
	delete(c.at, keyed(s.set, e.id))
	if e.warm {
		s.coolDown(e) // it costs the protected segment nothing once it is gone
		c.warm -= e.cost
	}
	if front {
		c.cost -= s.trimFront(1)
	} else {
		c.cost -= s.trimBack(1)
	}
	if s.n == 0 {
		c.forget(s)
	}
}

// forget drops an emptied or merged-away run.
func (c *cache) forget(s *cachedScope) {
	delete(c.runs, s.id)
	kept := c.sets[s.set][:0]
	for _, held := range c.sets[s.set] {
		if held != s {
			kept = append(kept, held)
		}
	}
	if len(kept) == 0 {
		delete(c.sets, s.set)
	} else {
		c.sets[s.set] = kept
	}
}
