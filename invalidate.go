package serval

// Being told that what is held is no longer true.
//
// A notice says WHERE and WHY, and the why is what decides what it costs. A
// bare "forget this" throws away the one thing that settles whether the order
// moved or only the values did -- and those are two caches with two prices.
//
// **Invalidation causes forgetting, not traffic.** Nothing here issues a fill.
// What is no longer true is let go of, the guarantee is pulled back to what is
// still true, and whether a replacement is ever asked for is somebody else's
// decision: a stretch scrolled past an hour ago may never be read again.
//
// # One notice, one sequence
//
// A stretch is a stretch OF AN ORDER. "Between these two records" means nothing
// without one, and a stretch of the by-name sequence is not a stretch of the
// by-size sequence over the same records. So a notice is answered against one
// sequence, which is also how it was stated: coverage is per sequence, and
// whoever answers a coverage statement answers it per statement.
//
// What that notice costs, though, is not per sequence. A field that DECIDES one
// sequence is decoration in another -- `.size` changing moves a record in the
// by-size order and repaints it in the by-name one -- so the same notice is a
// split here and nothing at all there. Which is the two caches earning their
// keep: the values are forgotten once, for the source, and the order is
// forgotten per sequence, or not at all.
//
// # What each kind costs
//
//	| notice   | the values             | the order                            |
//	|----------|------------------------|--------------------------------------|
//	| Added    | nothing is known of it | the claim across that place is cut, or an open one pulled back |
//	| Removed  | forgotten              | UNLINKED, and the claim survives      |
//	| Replaced | forgotten              | the run goes: it may have moved       |
//	| Altered  | those fields forgotten | the run goes where a named field decides, and nothing where none does |
//
// **A deletion is cheaper than a move**, and it is worth knowing why. A record
// taken out of the MIDDLE of a run normally breaks the claim across it -- which
// is why making room only ever takes from an end: the record is still in the
// sequence, so a run that had dropped it from the middle would be claiming
// across a gap. A record that has LEFT the sequence is different: everything
// still in the sequence between the run's ends is still here, so the claim is
// as true as it was and the run stays whole. The same operation on the links,
// told apart by the reason.
//
// **Everything else is conservative.** A record that may have moved takes its
// run with it, rather than the run being cut around it: a cut would leave a
// boundary anchored to a record that is no longer where it says, and a boundary
// that lies is worse than a run that is gone. Over-invalidating is always safe
// and under-invalidating never is, so that is the direction to be wrong in.
//
// # And how many there are
//
// A notice moves the COUNT as well, and moves it once for every sort over that
// filter: how many records there are is a fact about membership, and sorting
// cannot make there be more or fewer of them. Added is one more, Removed is one
// fewer, and anything that leaves membership in doubt -- Replaced, or Altered
// naming a field the FILTER tests -- lowers the floor and gives up the
// exactness rather than throwing the figure away.
//
// Which is the finer of the two questions a field is asked. `Decides` is the
// ORDER's: could a change to this move a record, or take it out? `Filters` is
// the COUNT's: could it take one out? A sort field answers the first and not
// the second, because moving a record somewhere else in a sequence leaves just
// as many records in it.
//
// And it is why counting needs no deltas of its own. The statement that would
// have carried a delta is the notice, and the notice was being sent anyway.
//
// # A stretch that cannot be walked
//
// One record is always answered exactly, and so is a stretch this cache holds
// end to end: what falls between two identities is the SEQUENCE's answer, and a
// run is that answer written down. A stretch whose first record is not placed
// here, or whose run stops before the last, names records that cannot be named
// -- and quietly answering for the ones that can be would leave the rest held
// and wrong.
//
// So it is answered bluntly instead: the whole sequence's order goes, and every
// record held of that source is forgotten or unlearned. It is a real price, and
// it is the source's to avoid by naming stretches the reader is actually
// holding -- which is what Covers is for.

// A Change is why what is held is no longer true.
type Change int

const (
	// Added: a record appeared. It names no record anyone holds, so the extent
	// names the two it fell BETWEEN -- First the one it falls after, Last the
	// one it falls before -- either of which may be absent where it fell past
	// an end.
	Added Change = iota

	// Removed: a record has left the sequence altogether.
	Removed

	// Replaced: a record is still there and everything about it may be
	// different, which is the same as every field having changed.
	Replaced

	// Altered: the named fields may have changed. MAY, not did -- a source that
	// cannot tell exactly which is welcome to name more than changed, or to say
	// Replaced and be done. Over-stating costs a refill; under-stating is a
	// wrong answer nobody finds out about.
	Altered
)

func (c Change) String() string {
	switch c {
	case Added:
		return "added"
	case Removed:
		return "removed"
	case Replaced:
		return "replaced"
	case Altered:
		return "altered"
	}
	return "?"
}

// A Notice is one thing a source says has happened to what is held.
//
// The extent is by identity, the way everything here is. One record is First
// with no Last; a stretch is both, and is read in the order of the sequence the
// notice is answered against. Neither end is the whole of it -- every record
// the notice is answered against -- which is the widest thing a source can say
// and the most expensive.
type Notice struct {
	Extent
	Change Change

	// Fields are the ones that may have changed, for Altered. A name that
	// decides the sequence costs the order; one that does not costs those
	// records' values and nothing structural. Naming none says the source
	// cannot tell, which is every field.
	Fields []string
}

// Stale tells this wrapper that something it may be holding is no longer true.
//
// The descriptor names the sequence the extent is stated in, and a nil one says the
// notice is about records and not about anyone's order -- which is what a
// record known to the source but placed in no live sequence needs, there being
// no stretch to name it in.
//
// Nothing is fetched. What is no longer true is let go of.
func (c *CachedSource) Stale(descriptor *DataSetDescriptor, n Notice) {
	var ds dataSet
	var roles Roles
	if descriptor != nil {
		ds = dataSet{
			source:  c.key,
			set:     c.key + "\x00" + dataSetKey(descriptor),
			members: c.key + "\x00" + FilterKey(descriptor.Filter),
		}
		roles = descriptor.Roles()
	} else {
		ds = dataSet{source: c.key}
	}
	hot.stale(ds, roles, n)
}

// stale is the two forgettings: the values, which belong to the source, and the
// order, which belongs to one sequence.
func (c *cache) stale(ds dataSet, roles Roles, n Notice) {
	c.mu.Lock()
	defer c.mu.Unlock()

	named, exact := c.named(ds, n)

	// The values first, because the order is what says which records the extent
	// covered and trimming it would lose them.
	switch n.Change {
	case Removed, Replaced:
		for _, r := range c.reach(ds.source, named, exact) {
			c.flesh.drop(r)
		}
	case Altered:
		for _, r := range c.reach(ds.source, named, exact) {
			c.flesh.unlearn(r, n.Fields)
		}
	}

	if ds.set == "" {
		// A notice about records, with no sequence to answer it against --
		// which is also why it moves no COUNT. It names no filter, so it says
		// nothing about whether the record it names was ever a member of
		// anything, and guessing either way would be a figure nobody could
		// check. A source that changes what is IN a sequence says so against
		// that sequence.
		//
		// Nothing below would find anything keyed to no sequence anyway, so as
		// a guard this says what is meant rather than stopping anything --
		// which is why no test kills it.
		return
	}
	c.reorder(ds, roles, n, named, exact)
	c.recount(ds, roles, n, named, exact)
}

// recount is what the notice costs the figure for how many records there are.
//
// It is answered against the MEMBERSHIP rather than the sequence -- the same
// notice against the by-name and the by-size orders of one filter is one change
// to one count, because sorting cannot make there be more or fewer records.
//
// A notice that names no sequence never reaches here at all, and the reason it
// moves no count is with the guard that turns it back.
func (c *cache) recount(ds dataSet, roles Roles, n Notice, named []*Value, exact bool) {
	was, held := c.counts[ds.members]
	if !held {
		return // nothing stated, so nothing to correct
	}
	if n.Change == Added {
		// Stated against this sequence, so it is in it.
		c.counts[ds.members] = was.Add(1)
		return
	}
	if n.Change == Altered && !filtersAny(roles, n.Fields) {
		return // a field the filter does not test cannot change who is in
	}
	if !exact {
		// A stretch that could not be walked names an unknown number of
		// records, so what it costs the figure is unknown too.
		delete(c.counts, ds.members)
		return
	}
	if n.Change == Removed {
		c.counts[ds.members] = was.Take(len(named)) // certainly gone
	} else {
		c.counts[ds.members] = was.Doubt(len(named)) // in or out, nobody said
	}
}

// filtersAny reports whether any of these fields is one the filter tests.
//
// No fields named is every field, for the same reason it is everywhere here: a
// source that cannot say which has said it cannot be precise, and the safe
// reading is that membership may have moved.
func filtersAny(roles Roles, fields []string) bool {
	if len(fields) == 0 {
		return true
	}
	for _, f := range fields {
		if roles.Filters(f) {
			return true
		}
	}
	return false
}

// named is the records the notice covers, in the order the sequence puts them,
// and whether that is exactly them.
//
// One identity names itself, whether or not anything holds it. A stretch is
// walked, which is the only way to say which records fall between two: they are
// identities and not positions, and what stands between them is the sequence's
// answer rather than anybody's arithmetic.
//
// A stretch this cache cannot walk end to end comes back INEXACT, and so does
// one stated against no sequence at all: the caller then takes it as everything,
// which is blunt and safe. Added names no record either way -- the extent is
// where it fell, not what it is.
func (c *cache) named(ds dataSet, n Notice) ([]*Value, bool) {
	if n.Change == Added {
		return nil, true
	}
	if n.First == nil {
		// No beginning is the sequence's own beginning, and nothing here can
		// walk from there.
		return nil, false
	}
	if n.Last == nil || Equal(n.First, n.Last) {
		return []*Value{n.First}, true
	}
	if ds.set == "" {
		return nil, false // no order, so nothing to say what falls between
	}
	e := c.at[keyed(ds.set, n.First)]
	if e == nil {
		return nil, false // nothing places the record the stretch starts at
	}
	out := []*Value{}
	for ; e != nil; e = e.next {
		out = append(out, e.id)
		if Equal(e.id, n.Last) {
			return out, true
		}
	}
	return nil, false // the run stopped before the far end
}

// reach is the records whose values a notice touches: the ones it names and
// this cache holds, or every record of the source where it named a stretch that
// could not be walked.
func (c *cache) reach(src string, named []*Value, exact bool) []*cachedRecord {
	if !exact {
		return c.flesh.everything(src)
	}
	out := make([]*cachedRecord, 0, len(named))
	for _, id := range named {
		if r := c.flesh.get(src, id); r != nil {
			out = append(out, r)
		}
	}
	return out
}

// reorder is what the notice costs this sequence's runs.
func (c *cache) reorder(ds dataSet, roles Roles, n Notice, named []*Value, exact bool) {
	if n.Change == Added {
		c.cutAt(ds, n.First)
		return
	}
	if n.Change == Altered && !decidesAny(roles, n.Fields) {
		return // the values moved and nothing else did
	}
	if !exact {
		c.forgetSet(ds.set)
		return
	}
	for _, id := range named {
		if n.Change == Removed {
			c.lift(ds, id)
		} else {
			c.forgetRunOf(ds, id)
		}
	}
}

// decidesAny reports whether any of these fields decides this sequence.
//
// No fields named is every field: a source that says Altered and names none has
// said it cannot be more precise, and the safe reading of that is that the
// order may have moved.
func decidesAny(roles Roles, fields []string) bool {
	if len(fields) == 0 {
		return true
	}
	for _, f := range fields {
		if roles.Decides(f) {
			return true
		}
	}
	return false
}

// cutAt takes back a run's claim across the place a record appeared.
//
// The run said everything between its ends was here, and something is now
// between them that is not. So the claim is cut there: what came before it is
// still whole, and so is what came after. A run that does not hold the record
// the addition fell after never claimed that far, which is the log case --
// appending past a watermark costs the reader no places at all.
func (c *cache) cutAt(ds dataSet, after *Value) {
	if after == nil {
		// It fell before everything, so a run claiming to start at the
		// sequence's own beginning claimed too much. Nothing else is touched: a
		// record that is now the first cannot have landed inside a run that
		// begins somewhere else.
		for _, s := range c.runsOf(ds.set) {
			c.openBegin(s)
		}
		return
	}
	e := c.at[keyed(ds.set, after)]
	switch {
	case e == nil:
		// It fell after a record this sequence does not place, so where it fell
		// cannot be told. A run closed at both ends is safe whatever the answer
		// -- it claims what stands between two records it holds, and this fell
		// after one it does not -- and only the OPEN claims are at risk.
		for _, s := range c.runsOf(ds.set) {
			c.openEnd(s)
			c.openBegin(s)
		}
	case e.next == nil:
		// It fell past the last record of the run holding that place, so only a
		// run claiming to reach the sequence's own end claimed that far: this
		// one, and any other whose end this place may lie past.
		for _, s := range c.runsOf(ds.set) {
			c.openEnd(s)
		}
	default:
		s := c.runs[e.scope]
		if s == nil {
			return
		}
		if rest := s.splitAfter(e, c.next); rest != nil {
			c.next++
			c.runs[rest.id] = rest
			c.runs[s.id] = s
			c.sets[ds.set] = append(c.sets[ds.set], rest)
		}
	}
}

// openEnd and openBegin take back a run's open claims, so that it stops saying
// there is nothing beyond.
//
// The END costs nothing. It is INCLUSIVE, so naming the last record the run
// holds keeps every place and gives up only the promise that the sequence stops
// there.
//
// The BEGINNING costs one place. It is EXCLUSIVE -- the record it names is the
// one the run starts PAST -- so the only record it could name without lying is
// the one the run would have to give up in order to name it.
func (c *cache) openEnd(s *cachedScope) {
	if s.end == nil && s.tail != nil {
		s.end = s.tail.id
	}
}

func (c *cache) openBegin(s *cachedScope) {
	if s.begin == nil && s.head != nil {
		c.drop(s, true)
	}
}

// runsOf is a sequence's runs, taken as a copy: what is done to them here drops
// runs, and dropping one rewrites the list being walked.
func (c *cache) runsOf(set string) []*cachedScope {
	return append([]*cachedScope(nil), c.sets[set]...)
}

// lift takes a record that has LEFT the sequence out of its place.
//
// The run's claim survives it: everything still in the sequence between its
// ends is still here. That is what makes a deletion cheaper than a move, and it
// is the reason a notice says which it was.
//
// A record standing at one of the run's own boundaries is the exception. Those
// name where the guarantee starts and stops, and one anchored to a record that
// is no longer in the sequence is a boundary nothing can be matched against --
// so the run goes rather than being left pointing at a ghost.
func (c *cache) lift(ds dataSet, id *Value) {
	e := c.at[keyed(ds.set, id)]
	if e == nil {
		// It stands nowhere here, but a run's boundary may still name it.
		for _, s := range c.runsOf(ds.set) {
			if Equal(s.begin, id) || Equal(s.end, id) {
				c.evict(s)
			}
		}
		return
	}
	s := c.runs[e.scope]
	if s == nil {
		return
	}
	if Equal(s.begin, id) || Equal(s.end, id) {
		c.evict(s)
		return
	}
	delete(c.at, keyed(ds.set, id))
	if e.warm {
		s.coolDown(e) // it costs the protected segment nothing once it is gone
		c.warm -= e.cost
	}
	c.cost -= s.unlink(e)
	if s.n == 0 {
		c.forget(s)
	}
}

// forgetRunOf drops the whole run a record stands in.
//
// Conservative on purpose. A record that may have MOVED cannot be cut around:
// the pieces either side would be anchored to a record that is no longer where
// they say it is, and a boundary that lies is worse than a run that is gone.
// Over-invalidating is always safe; under-invalidating never is.
func (c *cache) forgetRunOf(ds dataSet, id *Value) {
	e := c.at[keyed(ds.set, id)]
	if e == nil {
		return
	}
	if s := c.runs[e.scope]; s != nil {
		c.evict(s)
	}
}

// forgetSet drops every run of one sequence, which is what a notice too vague to
// name records costs the order.
func (c *cache) forgetSet(set string) {
	for _, s := range c.runsOf(set) {
		c.evict(s)
	}
}

// evict drops a run entire, which is its own front given way to over and over.
//
// Written as the eviction it is rather than as a second way of doing the same
// thing: making room takes a place off an end, and a run given up is every
// place taken off one end. So the lookup, the two segments' books and the
// emptied run are all somebody else's problem, already solved and already
// tested.
//
// The last place taken is what forgets the run, which is where a run emptied
// any other way is forgotten too. What those records HOLD is untouched either
// way: the flesh is another cache, and knowledge nothing places is still true.
func (c *cache) evict(s *cachedScope) {
	for s.n > 0 {
		c.drop(s, true)
	}
}
