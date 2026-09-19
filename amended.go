package serval

// A source that amends another.
//
// The third kind, and the first that wraps. It holds two different things over
// a child source -- amendments against the child's records, and records of its
// own -- and it answers the query itself: it asks the child the same question,
// and merges what comes back with what it holds.
//
//	a record the child sent that we replace   ours goes out, so the values are right
//	a record the child sent that we deleted   dropped
//	a record of ours the child never sent     ours goes out, if it matches
//	one of ours the child sent after all      THEIRS goes out; ours is a clash
//	anything else the child sent              passed on
//
// **An amendment is a statement about a record of the child's; an addition is
// not.** An amendment names one of the child's and says "mine instead" or
// "gone", and is authoritative for that key: whatever the child says about it
// is suppressed. An addition names nothing of the child's -- it is a record
// this source owns, keyed however its author likes, with no prefix imposed --
// and where the two meet, the child wins.
//
// That is the opposite way round from a replacement, and deliberately. An
// addition's key is the author's to choose and the author's to keep clear; the
// child's records are not this source's to displace by accident. Where a child
// is a ComposedSource every key it hands out carries a slash, so an addition
// without one cannot collide at all -- and adding *into* an include's
// namespace is a legitimate thing to do, for records meant to be moved there
// later.
//
// **Nothing is asked to find a clash out.** It surfaces when the child's copy
// arrives, which is a lookup this source does on every record anyway. On the
// scope where it surfaces, ours has usually already gone and the child's is
// dropped rather than send one identity twice; the clash is written down, and
// every scope after that has ours out and the child's in.
//
// Amendments change at any time. This is a data source, not a query: it
// answers against what it holds when it is asked, and two scopes of one
// data set need not agree.

import (
	"fmt"
	"sort"
	"sync"
)

// rounds is how many times one scope will go back to the child for the
// records its deletions took out. A prediction that was wrong is corrected by
// what the round taught it, so a second is nearly always enough; the cap is
// there because a child that keeps answering short should not be asked forever.
const rounds = 4

// An AmendedSource holds replacements and deletions against a child's records.
type AmendedSource struct {
	// What it says its records ARE, where somebody has said; see treehint.go.
	HintSaid

	child  Source
	notes  *placebook
	orders *orders

	mu    sync.Mutex
	amend map[string]*amendment

	// gen counts changes to what is held.
	//
	// Records do not move, but amendments do: one may be added, forgotten, or
	// have its placement learned between two scopes of one sequence. So the
	// order they are arranged in carries the generation it was built at, and a
	// source amended since arranges them again.
	gen uint64
}

// NewAmendedSource amends the records of a child source. The child is any kind
// -- records here, records an application's, or another amended source.
func NewAmendedSource(child Source) *AmendedSource {
	return &AmendedSource{
		child:  child,
		notes:  newPlacebook(),
		orders: newOrders(),
		amend:  map[string]*amendment{},
	}
}

// An amendment is what is held against one of the child's records.
//
// A deletion carries what was last known of the record it removes. That is not
// the record -- it is gone -- but what it takes to work out whether it would
// have fallen inside a scope, which is how the shortfall it causes is
// predicted rather than discovered.
type amendment struct {
	key     *Value
	fields  Record // the replacement's, addition's or alteration's content; nil for a deletion
	deleted bool
	seen    Record // last known fields of a deleted record

	// altered marks a statement about SOME of a record rather than all of it:
	// `fields` is then the members that change and the rest is the child's. It is
	// the one amendment that does not stand on its own -- there is nothing to send
	// for a key the child never sends -- and the one that cannot move a record,
	// because the child places it and only its contents are touched afterwards.
	altered bool

	// added marks a record of this source's own rather than a statement about
	// one of the child's, and clashed marks one whose key turned out to be the
	// child's after all. A clashed addition never goes out again.
	added   bool
	clashed bool
}

// place is what the amendment is positioned and filtered by.
func (a *amendment) place() Record {
	if a.deleted {
		return a.seen
	}
	return a.fields
}

// Replace says a record now carries these fields, whatever the child holds.
//
// These are the record entire, not a correction to some of it: what goes out
// for this key is exactly what is stated here, and it goes out as a whole
// record.
func (a *AmendedSource) Replace(key *Value, fields Record) {
	if key == nil {
		return
	}
	a.mu.Lock()
	a.amend[Key(key)] = &amendment{key: key, fields: fields}
	a.gen++
	a.mu.Unlock()
	a.notes.ranksGone()
}

// Alter says SOME of a record changes and the rest is the child's.
//
// The one amendment that is not a record entire, and it is what a cell edit is: a
// reader changed one member of one row and said nothing whatever about the others.
// Stating it as a Replace would mean holding the whole record to state it with, and
// a reader that has drawn five columns of a forty-member record has not got one.
//
// Three things follow from it being partial, and they are the reason it is worth
// having rather than a convenience:
//
//   - **It cannot move the record.** The child applies the filter and the sort and
//     places the record; this touches what the record HOLDS afterwards. So a row
//     stays where it was until something asks the question again -- which is what a
//     reader editing a cell wants, the row not leaping away under the cursor.
//   - **It cannot change how many there are.** The child counted it and still does.
//   - **It means nothing for a key the child does not send.** There is no record to
//     alter, so nothing goes out. That is the opposite of Replace, which stands on
//     its own, and it is why an alteration is not a way to add anything.
//
// **Altering accumulates.** Two edits to two columns of one row are two calls, and
// the second must not lose the first -- so the members are merged into whatever is
// held: into an alteration, into a replacement's or an addition's own fields (both
// being records entire, a member of one is well defined), and into nothing at all
// for a key that was deleted, the record being gone.
func (a *AmendedSource) Alter(key *Value, members Record) {
	if key == nil || len(members) == 0 {
		return
	}
	a.mu.Lock()
	am := a.amend[Key(key)]
	switch {
	case am == nil:
		a.amend[Key(key)] = &amendment{key: key, fields: members, altered: true}
	case am.deleted:
		// Gone is gone. Altering a record that is not there says nothing, and
		// resurrecting it under some of its members would invent the rest.
		a.mu.Unlock()
		return
	default:
		am.fields = overlay(am.fields, members)
	}
	// **The generation is bumped only where the arrangement could have changed.**
	// An alteration is not in the arrangement at all -- see buildAmendOrder -- so a
	// held order is still right, and re-arranging every sequence on every keystroke
	// would be paying for nothing. Amending a record that IS arranged moves it, so
	// that case pays.
	if am != nil {
		a.gen++
	}
	a.mu.Unlock()
	if am != nil {
		a.notes.ranksGone()
	}
}

// overlay is a record with some of its members written over, and the rest as they
// were. A member the original has not got is appended, because altering a field a
// record lacks is how a field gets added to it.
func overlay(over, with Record) Record {
	out := make(Record, len(over), len(over)+len(with))
	copy(out, over)
	for _, m := range with {
		replaced := false
		for i, had := range out {
			if had.Name == m.Name {
				out[i], replaced = m, true
				break
			}
		}
		if !replaced {
			out = append(out, m)
		}
	}
	return out
}

// Add says this source holds a record of its own under this key.
//
// It is not a statement about anything the child holds: the key is the
// author's to choose, and keeping it clear of the child's is the author's to
// do. Where it turns out not to be clear, the child's record is the one that
// stands and this one is dropped for good -- the opposite of Replace, which
// displaces whatever the child has.
func (a *AmendedSource) Add(key *Value, fields Record) {
	if key == nil {
		return
	}
	a.mu.Lock()
	a.amend[Key(key)] = &amendment{key: key, fields: fields, added: true}
	a.gen++
	a.mu.Unlock()
	a.notes.ranksGone()
}

// Delete says a record is gone.
//
// Known is what was last seen of it, and may be nil. With it, the scope that
// record would have fallen in is known before the child is asked; without it,
// the shortfall is discovered afterwards and costs a second question -- which
// is also where the fields to remember are learned.
func (a *AmendedSource) Delete(key *Value, known Record) {
	if key == nil {
		return
	}
	a.mu.Lock()
	a.amend[Key(key)] = &amendment{key: key, deleted: true, seen: known}
	a.gen++
	a.mu.Unlock()
	a.notes.ranksGone()
}

// An Amendment is one statement this source holds, as a caller reads it back.
//
// A copy, so that amending afterwards does not change one already handed out.
type Amendment struct {
	// Key is the record this is about.
	Key *Value

	// How is which of the four things has been said about it, in the same words a
	// source uses to ANNOUNCE one: Added, Removed, Replaced, Altered.
	//
	// **They are the same four facts, so they are the same four words.** A
	// `Change` is one of them told to a reader; an amendment is one of them held
	// against a child. Giving the held version four words of its own would mean
	// two vocabularies for one idea, and a reader turning `Delete` into "removed"
	// by hand.
	How Change

	// Fields is the record entire for Replaced and Added, the members that change
	// for Altered, and nil for Removed -- the record being gone, there is nothing
	// of it to state.
	Fields Record

	// Clashed marks an addition whose key turned out to be the child's after all.
	// The child's record stands and this one no longer goes out, so it is here to
	// be seen and dealt with rather than written out as though it were in force.
	Clashed bool
}

// Amendments is every statement this source holds.
//
// **It is here because an amendment is meant to outlive the session that made
// it.** A reader edits a cell, the edit is held here, and somebody eventually has
// to write it to a file or a database -- which is impossible if the only way to
// see one is to read the records back and diff them against a child that has
// meanwhile moved on. So what is held can be asked for.
//
// Ordered by key, which is not the order they were made in and is deliberately
// not: the map holds one statement per key, so the making order has already been
// collapsed, and reporting some order would suggest a history that is not kept. A
// stable one is what a caller writing a file wants.
func (a *AmendedSource) Amendments() []Amendment {
	a.mu.Lock()
	out := make([]Amendment, 0, len(a.amend))
	for _, am := range a.amend {
		out = append(out, Amendment{
			Key:     am.key,
			How:     am.how(),
			Fields:  am.fields,
			Clashed: am.clashed,
		})
	}
	a.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return Key(out[i].Key) < Key(out[j].Key) })
	return out
}

// Amended reports whether anything is held at all, for a caller asking whether
// there is anything to save.
func (a *AmendedSource) Amended() bool { return a.amends() }

// how is which of the four this amendment is.
func (a *amendment) how() Change {
	switch {
	case a.deleted:
		return Removed
	case a.altered:
		return Altered
	case a.added:
		return Added
	}
	return Replaced
}

// Forget drops an amendment, leaving the child's own record to stand.
func (a *AmendedSource) Forget(key *Value) {
	if key == nil {
		return
	}
	a.mu.Lock()
	delete(a.amend, Key(key))
	a.gen++
	a.mu.Unlock()
	a.notes.ranksGone()
}

// learn writes down the child's own version of a record this source amends,
// from a copy the child sent.
//
// For a DELETION that is where the record sat, and the next scope over that
// stretch predicts its shortfall instead of discovering it.
//
// For a REPLACEMENT it changes nothing about placement -- ours stands where its
// own values put it -- and everything about counting. Whether a replacement
// makes the sequence longer, shorter or neither is a question about two
// records, and it cannot be answered while only one of them is here. See
// RecordCount.
//
// An addition shadows nothing: it names no record of the child's, so there is
// nothing of the child's under that key to remember.
func (a *AmendedSource) learn(key *Value, fields Record) {
	a.mu.Lock()
	defer a.mu.Unlock()
	am := a.amend[Key(key)]
	if am == nil || am.added {
		return
	}
	am.seen = fields
	if am.deleted {
		// Not just a note: a deletion with a placement is one this source can
		// rule out of a scope, so it moves from being counted for every scope
		// to standing somewhere in the order.
		a.gen++
	}
}

// Stale says the child's own record under this key may have changed, so what
// was shadowed of it is no longer to be trusted.
//
// This source holds no runs and no values, so it needs no extent and no reason:
// what it keeps of the child is one record per amended key, and either that is
// still right or it is not. Told, never decided -- the same rule as everywhere,
// because a shadow is knowledge like any other and goes stale like any other.
//
// A count that was exact because of a shadow falls back to a floor, and a
// deletion that had a placement goes back to being ruled out of no scope.
func (a *AmendedSource) Stale(key *Value) {
	if key == nil {
		return
	}
	a.mu.Lock()
	if am := a.amend[Key(key)]; am != nil && am.seen != nil {
		am.seen = nil
		a.gen++
	}
	a.mu.Unlock()
	a.notes.ranksGone()
}

// clash writes down that an addition's key is the child's after all, which is
// the one thing about an addition that cannot be known until the child answers.
// From here on the child's record stands and this one does not go out.
func (a *AmendedSource) clash(key *Value) {
	a.mu.Lock()
	if am := a.amend[Key(key)]; am != nil && am.added && !am.clashed {
		am.clashed = true
		a.gen++
	}
	a.mu.Unlock()
	a.notes.ranksGone()
}

// amends reports whether this source holds anything of its own at all.
//
// It is what decides whether a POSITION can be handed down to the child. With
// nothing held, this sequence IS the child's: position n is position n, `from`
// means the same thing to both, and the child's word on where it began is this
// source's word too. That is the case a list reading its own items is in, and it
// is worth having exactly because it is so common.
//
// **With anything held, no.** Not because a replacement moves a record -- it
// does not, standing in for one of the child's under the same key and in the
// same place -- but because this source positions its OWN records against the
// record the scope resumed past, and a scope naming a place names no record to
// position against. Its own cursor would start at the beginning of its own
// order, and a replacement sorting before the place asked for would go out ahead
// of the child's first record: an answer beginning somewhere it said it did not.
//
// So the rule is the conservative one until the merge can place its own records
// against a position, which is a separate piece of work and not this one.
func (a *AmendedSource) amends() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.amend) > 0
}

// lookup is the amendment against one key, and nil where there is none.
func (a *AmendedSource) lookup(key *Value) *amendment {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.amend[Key(key)]
}

// Open states a sequence, and opens the same one on the child.
func (a *AmendedSource) Open(spec *Spec) (DataSet, error) {
	if spec == nil {
		spec = &Spec{}
	}
	child, err := a.child.Open(spec)
	if err != nil {
		return nil, err
	}
	notes := a.notes.of(spec, 2)
	return &amendedSet{
		src:    a,
		spec:   spec,
		child:  child,
		levels: ordering1(spec),
		placed: notes[0],
		resume: notes[1],
	}, nil
}

type amendedSet struct {
	src    *AmendedSource
	spec   *Spec
	child  DataSet
	levels []Level

	// placed is where each record this sequence has handed out stood. The
	// child places `after` for its own records, but this source has to place it
	// too -- it decides which of its own amendments come after it -- and an
	// identity is not a position.
	placed *places

	// resume is where the CHILD stood as each of those records went out.
	//
	// This source has records of its own, so a scope can end on one the child
	// has never heard of -- and handing that identity down would be asking the
	// child to place something that is not its. What goes down instead is the
	// last identity the child itself gave before that record crossed, which is
	// the same place in the merged sequence.
	resume *places
}

// Close lets this sequence go, and the child's with it.
func (s *amendedSet) Close() { s.child.Close() }

// RecordCount is the child's figure, moved by what this source lays over it.
//
// Each amendment is worth -1, 0 or +1, and which of those it is turns on one
// question asked twice: does the filter admit the child's version, and does it
// admit ours? Where both answers are here the figure stays exact. Where the
// child's is not -- nothing of that record has crossed yet, so there is nothing
// shadowed -- the answer is a range, and a range is a floor.
//
//	amendment      shadowed                       not shadowed
//	added          in: +1, out: 0                 the same: it shadows nothing
//	added, clashed 0: the child's record stands    0
//	deleted        was in: -1, was out: 0         floor -1
//	replaced       in-before against in-after      in now: floor +0, out: floor -1
//
// A DELETION of a record the filter never admitted costs nothing at all, which
// is the case a floor alone would have got wrong in the expensive direction: it
// would have said there might be one fewer when there certainly is not.
//
// An unclashed addition is taken at its word. Add says the key is the author's
// to keep clear of the child's, and this believes that exactly as far as the
// read path does -- ours goes out until a clash surfaces, and ours is counted
// until a clash surfaces. An author who collides is wrong by one until the
// child's copy crosses, in the figure and in the records alike.
func (s *amendedSet) RecordCount() RecordCount {
	n := CountOf(s.child)
	f := s.spec.Filter

	s.src.mu.Lock()
	defer s.src.mu.Unlock()
	for _, am := range s.src.amend {
		switch {
		case am.clashed:
			// The child's record stands, and the child has counted it.
		case am.altered:
			// The child's record stands, wearing our members. It is the same
			// record in the same place, so the child's figure is still the
			// figure -- which is the whole of what makes an alteration cheap.
		case am.added:
			if Match(am.key, am.fields, f) {
				n = n.Add(1)
			}
		case am.seen == nil:
			// Nothing of the child's has crossed, so whether it was ever in
			// this sequence is not known here.
			if am.deleted || !Match(am.key, am.fields, f) {
				n = n.Doubt(1) // it may have been in, and is not now
			} else {
				n = n.Doubt(0) // it is in now, and may already have been
			}
		case am.deleted:
			if Match(am.key, am.seen, f) {
				n = n.Take(1)
			}
		default:
			was, now := Match(am.key, am.seen, f), Match(am.key, am.fields, f)
			if was && !now {
				n = n.Take(1)
			} else if now && !was {
				n = n.Add(1)
			}
		}
	}
	return n
}

// Read answers one scope out of the child's records and this source's own.
func (s *amendedSet) Read(sc *Scope, out Sink) error {
	if out == nil {
		return fmt.Errorf("a scope needs somewhere to put the answer")
	}
	if sc == nil {
		sc = &Scope{}
	}

	if err := bothEnds(sc); err != nil {
		out.Done(Complete{Error: err.Error()})
		return nil
	}

	var at []*Value
	if sc.After != nil {
		t, ok := s.placed.get(sc.After)
		if !ok {
			out.Done(Complete{Error: fmt.Sprintf(
				"after %s: this sequence has not placed that record",
				sc.After.String())})
			return nil
		}
		at = t
	}

	// Where the child carries on from. For a record of the child's this is the
	// record itself; for one of ours it is whatever the child last gave before
	// ours went out.
	var from *Value
	if sc.After != nil {
		if r, ok := s.resume.get(sc.After); ok && len(r) > 0 {
			from = r[0]
		}
	}

	// A position goes down only where nothing here moves one. Where something
	// does, the child is not shown a figure that would mean a different place in
	// its sequence than it means in this one.
	asked := sc
	pass := sc.From != 0 && !s.src.amends()
	if sc.From != 0 && !pass {
		stripped := *sc
		stripped.From = 0
		asked = &stripped
	}

	m := &merge{
		set: s, want: asked, out: out, at: at,
		levels: s.levels, childAt: from, step: 1,
		begin: startFrom(asked, s.placed, s.RecordCount()),
	}
	if pass {
		// Where it began is the CHILD's to say, and it says so at the end. So
		// nothing is ranked this round, rather than ranked from a figure guessed
		// before the answer arrived -- and the reader is told the truth once the
		// child has told it.
		m.begin = Unknown()
		m.passed = true
	}
	if sc.Reversed {
		m.step = -1
		// Walking the other way turns every comparison over, this source's
		// own records included: what "before" means is the only thing that
		// changes, and it changes for everyone at once.
		m.levels = Reverse(s.levels)
	}
	m.prepare()

	// The child is asked for the shortfall: the scope, plus what our deletions
	// will take out of its answer, less what we will put in ourselves. Asking
	// for more than that is work nobody reads.
	want := sc.Count + m.slack - m.waiting()
	if want < 0 {
		want = 0
	}
	return m.ask(from, want)
}

// A merge is one scope being answered: this source's own records for it, and
// the child's, going out as one run.
type merge struct {
	set    *amendedSet
	want   *Scope
	out    Sink
	at     []*Value // where the scope starts, nil at the sequence's end
	levels []Level  // the walk's own direction

	// Where this scope stands in what this source holds: the order, arranged
	// once for the sequence, and a cursor into it. The order is shared with
	// every other reader of the sequence, so nothing here writes to it.
	order *amendOrder
	i     int // the next of ours to go out
	step  int // +1 forward, -1 walking the sequence from its end
	slack int // records of the child's this scope will take out

	childAt *Value          // the last identity the child gave
	gone    map[string]bool // additions of ours that have already crossed
	dropped map[string]bool // additions whose key turned out to be the child's

	sent  int
	round int

	// begin is where this answer starts in the sequence, worked out once
	// before any record goes out, and what every record's rank counts from.
	begin RecordCount

	// passed says a position was handed to the child, so where the answer began
	// is the child's to report and this source repeats it. childFirst is what it
	// reported.
	passed      bool
	childFirst  RecordCount
	last        *Value // the identity of the last record that went out
	joined      bool   // the walk reached the record the asker already held
	done        bool
	saidOrdered bool
}

// full reports whether the scope has as many records as it was asked for.
//
// Ours count towards it like anything else. A scope of thirty is thirty
// records whoever they came from, and what is left over is not lost -- it is
// the start of the next one.
func (m *merge) full() bool { return m.sent >= m.want.Count }

// prepare works out what this source has to say about the scope before the
// child is asked anything.
//
// Two things come out of it, and both are a search rather than a walk. Where
// ours start: the first of them past the boundary, which the order is sorted
// for. And what is subtracted: how many of the child's records this source
// takes out of the answer beyond that point, which is a count the order carries
// -- a deletion, and a replacement whose new values no longer match, losing the
// child's record just as surely if the child still holds the old ones.
func (m *merge) prepare() {
	o := m.set.src.order(m.set.spec)
	m.order = o

	// The order is arranged the way the sequence runs, and a scope reading it
	// backwards walks the same arrangement the other way.
	up, down := span(o.outAt, m.at, m.set.levels)
	goneUp, goneDown := span(o.goneAt, m.at, m.set.levels)
	if m.step > 0 {
		m.i = up
		m.slack = len(o.gone) - goneUp
	} else {
		m.i = down - 1
		m.slack = goneDown
	}
	// A deletion nobody has seen the record of cannot be ruled out of any
	// scope, so it is counted for this one whichever way it is read.
	m.slack += o.unplaced
}

// waiting is how many of ours are still to go out, which is what the child is
// asked for fewer of.
func (m *merge) waiting() int {
	if m.step > 0 {
		return len(m.order.out) - m.i
	}
	return m.i + 1
}

// peek is the next of ours and where it stands, and false where there is none.
func (m *merge) peek() (*amendment, []*Value, bool) {
	if m.i < 0 || m.i >= len(m.order.out) {
		return nil, nil, false
	}
	return m.order.out[m.i], m.order.outAt[m.i], true
}

// ask puts the scope to the child, with room for what this source will take
// out of the answer.
//
// The identity goes down untranslated: this source amends the child's records
// rather than renaming them, so the two share one identity space and an `after`
// that means something here means the same thing there.
func (m *merge) ask(after *Value, count int) error {
	m.round++
	next := *m.want
	next.After = after
	next.Count = count
	return m.set.child.Read(&next, m)
}

// Ordered is the child saying its records are in the sequence's order, before
// any of them arrive.
//
// Ours go out in that order too, so what comes out of the merge is ordered
// exactly when what goes into it was -- and whoever is reading learns it in
// time to act on it, which is the whole reason it is said up front.
func (m *merge) Ordered() {
	if !m.saidOrdered {
		m.saidOrdered = true
		m.out.Ordered()
	}
}

// Record and Subset take one of the child's records, entire or in part. Which
// it was goes out unchanged: this source says of a record it passed on exactly
// what the child said of it.
// A whole record is its own totals: what it carries is everything there is. So
// the count below is true and nothing on that path reads it -- emit sends a
// whole record whole -- which is why changing it breaks no test.
func (m *merge) Record(key *Value, fields Record) error {
	return m.theirs(key, fields, Tally(fields), true)
}

func (m *merge) Subset(key *Value, fields Record, has Totals) error {
	return m.theirs(key, fields, has, false)
}

// theirs is one of the child's records reaching the merge.
//
// A key this source amends is the source's to answer: the child's copy is
// dropped, and ours goes out in its own place -- which is wherever the run of
// ours reaches, not wherever the child's copy turned up.
func (m *merge) theirs(key *Value, fields Record, has Totals, whole bool) error {
	// The child gave it, so this is where the child now stands -- whether or
	// not it is passed on, and that is what the next scope resumes it from.
	m.childAt = key

	if am := m.set.src.lookup(key); am != nil {
		if am.deleted {
			// The child still holds it, so this is where we find out where it
			// sat. Next time the shortfall is predicted rather than met.
			m.set.src.learn(key, fields)
			return nil
		}
		if am.altered {
			// **Theirs goes out, wearing our members.** An alteration is not a
			// record of ours standing in the child's place -- it is a correction
			// applied to the child's on the way past, which is why nothing is
			// shadowed, nothing is placed and the record keeps the position the
			// child gave it.
			fields = overlay(fields, am.fields)
		} else if !am.added {
			// A replacement: ours stands in its place. Theirs is shadowed on
			// the way past, because whether ours makes the sequence longer,
			// shorter or neither is a question about both of them.
			m.set.src.learn(key, fields)
			return nil
		} else {
			// An addition whose key is the child's after all. The child's record
			// is the one that stands, and this is the only moment that can be
			// found out -- so it is written down, and every scope after this one
			// has ours out and the child's in.
			m.set.src.clash(key)
			m.drop(am)
			if m.gone[Key(key)] {
				// Ours has already gone out in this scope. Sending the child's now
				// would hand one identity to the asker twice, which is worse than
				// either record winning, so this scope keeps ours and the next one
				// -- and every one after it -- has the child's.
				return nil
			}
		}
	}
	m.flushBefore(recordTuple(key, fields, m.set.spec.Sort))
	if m.full() {
		return nil
	}
	return m.emit(key, fields, has, whole)
}

// drop marks one of ours as not going out after all.
//
// Marked rather than removed: the order it sits in is arranged once for the
// sequence and shared with every other reader of it, so a scope that finds a
// clash notes it here and leaves the order alone. The clash itself is written
// down on the source, which is what keeps the next scope from finding it again.
func (m *merge) drop(am *amendment) {
	if m.dropped == nil {
		m.dropped = map[string]bool{}
	}
	m.dropped[Key(am.key)] = true
}

// Done is the end of one round of the child's answer.
//
// If the scope came up short of what was asked for -- a deletion landed in
// it that this source did not know about -- the child is asked again from
// where it got to. What the round taught about that deletion means the next
// scope over the same ground does not come up short again.
func (m *merge) Done(c Complete) {
	if m.done {
		return
	}
	if m.passed && c.First.Exact {
		m.childFirst = c.First
	}
	if c.Stop == StopJoined {
		// The asker holds the record the walk stopped at and everything past
		// it -- ours included, since ours crossed the same way. So nothing
		// left of ours goes out, and nothing is claimed past that point.
		m.joined = true
	}
	short := !m.full() && !m.joined
	more := c.Stop == StopFilled || c.Stop == StopJoined
	if short && more && c.Error == "" && c.Watermark != nil && m.round < rounds {
		if err := m.ask(c.Watermark, m.want.Count-m.sent+1); err == nil {
			return
		}
	}

	// Whatever is left of ours goes out, up to the count: it is the end of the
	// scope, and the records this source holds do not depend on the child
	// having sent anything.
	if !m.joined {
		m.flushBefore(nil)
	}
	m.done = true

	out := Complete{Error: c.Error}
	switch {
	case c.Error != "":
	case m.joined:
		out.Stop = StopJoined
	case c.Stop == StopExhausted && m.waiting() == 0:
		// Everything of ours from the boundary on has gone out, so where the
		// child had nothing more, neither has anyone. This outranks a scope
		// that also happened to fill: there being nothing past the end is the
		// stronger fact, and the one that saves the next question.
		out.Stop = StopExhausted
	case m.full():
		out.Stop = StopFilled
	default:
		out.Stop = c.Stop
	}
	if out.Stop != StopExhausted && out.Error == "" {
		// Ours, not the child's. The next scope quotes this back as `after`,
		// and it has to be a record this sequence can place -- which the
		// child's last one need not be, since a record we amend away never
		// reaches anybody and is never placed.
		out.Watermark = m.last
		if out.Watermark == nil {
			out.Watermark = m.want.After
		}
	}
	if m.sent > 0 {
		out.First = m.begin
		if m.passed {
			// The child was given the position and is the one that knows where it
			// landed. Nothing here moved a record, so its answer is ours.
			out.First = m.childFirst
		}
	}
	m.out.Done(out)
}

// flushBefore sends the records of this source's own that belong before a
// position, and everything left when there is none.
func (m *merge) flushBefore(at []*Value) {
	for !m.full() {
		am, stands, ok := m.peek()
		if !ok {
			return
		}
		if at != nil && CompareLevels(stands, at, m.levels) > 0 {
			return
		}
		m.i += m.step
		if m.dropped[Key(am.key)] {
			// Its key turned out to be the child's after all, and the child's
			// record has already gone out in its place.
			continue
		}
		if am.added {
			// Noted because the child may yet send a record under this key. If
			// it does, ours has already crossed and the child's is held back
			// rather than sending one identity twice.
			if m.gone == nil {
				m.gone = map[string]bool{}
			}
			m.gone[Key(am.key)] = true
		}
		// A replacement is the record entire -- that is what Replace states --
		// and so is an addition. Both go out as one.
		if m.emit(am.key, am.fields, Tally(am.fields), true) != nil {
			return
		}
	}
}

func (m *merge) emit(key *Value, fields Record, has Totals, whole bool) error {
	m.sent++
	m.last = key
	// Noted as it goes, because the next scope will name it as `after`: where
	// it stood, so this source can place its own records against it, and where
	// the child stood, so the child can be resumed without being shown an
	// identity that is not its.
	m.set.placed.put(key, recordTuple(key, fields, m.set.spec.Sort))
	m.set.resume.put(key, []*Value{m.childAt})
	if at, ok := rankAt(m.begin, m.want.Reversed, m.sent-1); ok {
		m.set.placed.putRank(key, at)
	}
	if whole {
		return m.out.Record(key, fields)
	}
	return m.out.Subset(key, fields, has)
}

// amendTuple and recordTuple place a record: the value at each sort level, and
// then the key, which is the level that makes a position mean exactly one
// record.
func amendTuple(am *amendment, fields Record, levels []SortLevel) []*Value {
	return recordTuple(am.key, fields, levels)
}

func recordTuple(key *Value, fields Record, levels []SortLevel) []*Value {
	out := make([]*Value, 0, len(levels)+1)
	for _, l := range levels {
		out = append(out, fields.Get(l.Field))
	}
	return append(out, key)
}
