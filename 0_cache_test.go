package serval

import (
	"testing"
)

// over is the common case: one source with one sequence read out of it, where
// the two keys are the same name. Where a case is about SHARING, it names the
// source and the sequence separately, because that is the whole point of them
// being two keys.
func over(name string) dataSet { return dataSet{source: name, set: name} }

// wad is a run of whole records as a source sending everything would hand them
// back.
func wad(from, to int64) []*cachedRecord {
	out := []*cachedRecord{}
	for i := from; i <= to; i++ {
		out = append(out, whole(i))
	}
	return out
}

// slims is the same stretch as SUBSETS: only the fields named, which is what a
// query asking `fields={ ... }` leaves behind.
func slims(from, to int64, names ...string) []*cachedRecord {
	out := []*cachedRecord{}
	for i := from; i <= to; i++ {
		f := Record{}
		for _, n := range names {
			f = append(f, Named(n, i))
		}
		out = append(out, newRecord("", NewInt(i), f, false, 0))
	}
	return out
}

// filed holds an answer the way a source's reply would arrive: everything from
// `after` up to the last record, complete because the count was reached.
func filed(c *cache, ds dataSet, after *Value, recs []*cachedRecord) {
	sc := &Scope{After: after, Count: len(recs)}
	c.hold(ds, sc, recs, Complete{
		Stop: StopFilled, Watermark: recs[len(recs)-1].id,
	})
}

// ended holds an answer that ran out of records, which claims the end of the
// sequence rather than a watermark.
func ended(c *cache, ds dataSet, after *Value, recs []*cachedRecord) {
	sc := &Scope{After: after, Count: len(recs) + 1}
	c.hold(ds, sc, recs, Complete{Stop: StopExhausted})
}

func asked(c *cache, ds dataSet, wanted Record, sc *Scope) (string, Stop, bool) {
	es, done, ok := c.serve(ds, wanted, sc)
	return ids(es), done.Stop, ok
}

// inCache reports whether one record of a data set still stands in the cache at
// all.
func inCache(c *cache, ds dataSet, id int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at[keyed(ds.set, NewInt(id))] != nil
}

// known is what the cache knows one record of a source holds, and nil for one
// it knows nothing about.
func known(c *cache, src string, id int64) *cachedRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recs[keyed(src, NewInt(id))]
}

// A scope answered once is answered again from what was filed, rather than
// asked a second time.
func TestAnAnsweredScopeIsAnsweredAgainFromTheCache(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), nil, wad(1, 5))

	got, stop, ok := asked(c, over("files"), nil, &Scope{Count: 3})
	if !ok || got != "1,2,3" || stop != StopFilled {
		t.Errorf("from the beginning it gave %s / %s, held %v", got, stop, ok)
	}
	// And from the middle of it, which is where a reader scrolling would ask.
	got, stop, ok = asked(c, over("files"), nil, &Scope{After: NewInt(2), Count: 2})
	if !ok || got != "3,4" || stop != StopFilled {
		t.Errorf("after 2 it gave %s / %s, held %v", got, stop, ok)
	}
}

// A scope reaching past what the run guarantees is a miss, and not a short
// answer: where the run stops is where the knowledge stops, and there may be
// more records there.
func TestAScopePastTheGuaranteeIsAMiss(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), nil, wad(1, 5))

	if got, _, ok := asked(c, over("files"), nil, &Scope{Count: 9}); ok {
		t.Errorf("it answered %s for nine records out of a run of five", got)
	}
	if _, _, ok := asked(c, over("files"), nil, &Scope{After: NewInt(5), Count: 1}); ok {
		t.Error("it answered past the end of what it holds")
	}
	// A record of another sequence entirely is not held at all.
	if _, _, ok := asked(c, over("colours"), nil, &Scope{Count: 1}); ok {
		t.Error("it answered for a sequence it has never seen")
	}
}

// A run that reaches the end of the sequence answers a scope running off it,
// because there is nothing there to be missing.
func TestARunToTheEndAnswersPastItsLastRecord(t *testing.T) {
	c := newCache(1 << 20)
	ended(c, over("files"), nil, wad(1, 3))

	got, stop, ok := asked(c, over("files"), nil, &Scope{Count: 9})
	if !ok || got != "1,2,3" || stop != StopExhausted {
		t.Errorf("it gave %s / %s, held %v", got, stop, ok)
	}
	for _, sc := range []*Scope{
		{After: NewInt(3), Count: 2},
		{After: NewInt(3)}, // and with no count, which is not a count of none
	} {
		got, stop, ok = asked(c, over("files"), nil, sc)
		if !ok || got != "" || stop != StopExhausted {
			t.Errorf("past the last record it gave %s / %s, held %v", got, stop, ok)
		}
	}
}

// A scope naming no count asks for everything there is, so only a run reaching
// the end of the sequence can answer it.
func TestAScopeWithNoCountAsksToTheEnd(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), nil, wad(1, 4))
	if got, stop, ok := asked(c, over("files"), nil, &Scope{}); ok {
		t.Errorf("a run stopping at 4 answered for everything: %s / %s", got, stop)
	}

	ended(c, over("colours"), nil, wad(1, 4))
	got, stop, ok := asked(c, over("colours"), nil, &Scope{})
	if !ok || got != "1,2,3,4" || stop != StopExhausted {
		t.Errorf("a run to the end gave %s / %s, held %v", got, stop, ok)
	}
}

// A walk that reaches the record the asker already holds has joined two runs it
// held separately, and says so.
func TestAWalkThatReachesUntilIsJoined(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), nil, wad(1, 6))

	got, stop, ok := asked(c, over("files"), nil,
		&Scope{After: NewInt(1), Until: NewInt(4), Count: 9})
	if !ok || got != "2,3" || stop != StopJoined {
		t.Errorf("it gave %s / %s, held %v", got, stop, ok)
	}
}

// Read backwards a run answers the same way, the ends swapping over.
func TestAScopeReadBackwards(t *testing.T) {
	c := newCache(1 << 20)
	// Records arrive furthest-first, and the answer is guaranteed from the
	// watermark up to the record asked past.
	c.hold(over("files"), &Scope{After: NewInt(9), Count: 4, Reversed: true},
		[]*cachedRecord{whole(8), whole(7), whole(6), whole(5)},
		Complete{Stop: StopFilled, Watermark: NewInt(5)})

	got, stop, ok := asked(c, over("files"), nil,
		&Scope{After: NewInt(9), Count: 2, Reversed: true})
	if !ok || got != "8,7" || stop != StopFilled {
		t.Errorf("backwards from 9 it gave %s / %s, held %v", got, stop, ok)
	}
	// And forwards from inside the same run, which is the same records the
	// other way about.
	got, _, ok = asked(c, over("files"), nil, &Scope{After: NewInt(6), Count: 2})
	if !ok || got != "7,8" {
		t.Errorf("forwards from 6 it gave %s, held %v", got, ok)
	}
}

// Scrolling leaves one run rather than a run per screenful.
func TestScrollingJoinsOntoWhatIsAlreadyHeld(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), nil, wad(1, 3))
	filed(c, over("files"), NewInt(3), wad(4, 6))

	if n := len(c.sets["files"]); n != 1 {
		t.Errorf("two scopes back to back left %d runs", n)
	}
	got, _, ok := asked(c, over("files"), nil, &Scope{Count: 6})
	if !ok || got != "1,2,3,4,5,6" {
		t.Errorf("the joined run reads %s, held %v", got, ok)
	}
	// And a scope crossing where the join was is one walk, not two.
	if got, _, ok := asked(c, over("files"), nil, &Scope{After: NewInt(2), Count: 3}); !ok || got != "3,4,5" {
		t.Errorf("across the join it gave %s, held %v", got, ok)
	}
}

// A run filed before the one it joins onto is joined just the same.
func TestARunFiledOutOfOrderStillJoins(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), NewInt(3), wad(4, 6))
	filed(c, over("files"), nil, wad(1, 3))

	if n := len(c.sets["files"]); n != 1 {
		t.Errorf("it left %d runs", n)
	}
	if got, _, ok := asked(c, over("files"), nil, &Scope{Count: 6}); !ok || got != "1,2,3,4,5,6" {
		t.Errorf("it reads %s, held %v", got, ok)
	}
}

// --- the two tables ------------------------------------------------------

// What a record holds decides what a walk over it can answer, and holding more
// than was asked for is an answer.
func TestAWalkAnswersOnlyForRecordsThatAreKnownWellEnough(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), nil, slims(1, 4, "name", "size"))

	if _, _, ok := asked(c, over("files"), Record{Named("name", 0)}, &Scope{Count: 2}); !ok {
		t.Error("records holding name and size would not answer for name")
	}
	if _, _, ok := asked(c, over("files"), Record{Named("mode", 0)}, &Scope{Count: 2}); ok {
		t.Error("records holding name and size answered for mode")
	}
	// The whole record is not covered by part of one.
	if _, _, ok := asked(c, over("files"), nil, &Scope{Count: 2}); ok {
		t.Error("records holding two fields answered for the whole record")
	}
}

// One record known only in part is enough to miss, however well known the rest
// of the walk is. A stretch that knows `size` for all but one of its records is
// not an answer to a question about size.
func TestOneRecordKnownTooThinlyMissesTheWholeWalk(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), nil, append(
		slims(1, 3, "name", "size"), slims(4, 4, "name")...))

	size := Record{Named("size", 0)}
	if _, _, ok := asked(c, over("files"), size, &Scope{Count: 3}); !ok {
		t.Error("the three records that do know size would not answer")
	}
	if _, _, ok := asked(c, over("files"), size, &Scope{Count: 4}); ok {
		t.Error("a walk through a record that does not know size answered for it")
	}
}

// A second answer over a stretch already placed is not placed again -- there is
// nothing yet to decide which order is right -- but what it says the records
// HOLD is taken, because knowledge grows.
//
// Which is what a top-up is: ask for one field, then for two, and the second
// answer says nothing new about where anything stands and everything new about
// what it holds.
func TestASecondAnswerTopsUpWhatIsKnownWithoutPlacingAnything(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), nil, slims(1, 4, "name"))

	both := Record{Named("name", 0), Named("size", 0)}
	if _, _, ok := asked(c, over("files"), both, &Scope{Count: 4}); ok {
		t.Fatal("it answered for size before anything had said what size was")
	}

	// Read twice, so the places have proved themselves. What they learn next is
	// protected along with them rather than counted as being on probation.
	name := Record{Named("name", 0)}
	for i := 0; i < 2; i++ {
		if _, _, ok := asked(c, over("files"), name, &Scope{Count: 4}); !ok {
			t.Fatal("it would not answer for the name it holds")
		}
	}

	filed(c, over("files"), nil, slims(1, 4, "name", "size"))
	if n := len(c.sets["files"]); n != 1 {
		t.Errorf("the second answer left %d runs rather than one", n)
	}
	if got, _, ok := asked(c, over("files"), both, &Scope{Count: 4}); !ok || got != "1,2,3,4" {
		t.Errorf("after the top-up it gave %s, held %v", got, ok)
	}
	// It learned SIZE and not name over again: a field already known keeps the
	// value it has, and is not filed a second time beside itself.
	if r := known(c, "files", 1); r == nil || len(r.fields) != 2 {
		t.Errorf("after a top-up naming name and size the record holds %s", r.fields)
	}
	sound(t, c)
}

// A second answer that says a record arrived WHOLE settles every question about
// it, including ones neither answer named.
func TestAnAnswerThatArrivesWholeSettlesEverything(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), nil, slims(1, 3, "name"))
	if _, _, ok := asked(c, over("files"), nil, &Scope{Count: 3}); ok {
		t.Fatal("a subset answered for the whole record")
	}

	filed(c, over("files"), nil, wad(1, 3))
	if r := known(c, "files", 1); r == nil || !r.whole {
		t.Error("an answer that arrived whole left the record a subset")
	}
	if _, _, ok := asked(c, over("files"), nil, &Scope{Count: 3}); !ok {
		t.Error("a record known whole would not answer for the whole record")
	}
	sound(t, c)
}

// A record that ever arrived whole is whole from then on, and a later answer
// teaches it nothing -- not even a field it appears not to carry, because a
// whole record carries everything the record has and that is what whole means.
func TestAWholeRecordLearnsNothingMore(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), nil, wad(1, 3))
	was, held := c.Cost(), len(known(c, "files", 1).fields)

	filed(c, over("files"), nil, slims(1, 3, "thumbnail"))
	r := known(c, "files", 1)
	if r == nil || !r.whole {
		t.Error("a whole record stopped being whole")
	}
	if len(r.fields) != held {
		t.Errorf("a whole record of %d fields took another and holds %s", held, r.fields)
	}
	if c.Cost() != was {
		t.Errorf("learning nothing took the cache from %d to %d", was, c.Cost())
	}
	sound(t, c)
}

// Two data sets over ONE source read the same records. Sorting the same files
// by name and then by size is two orders over one body of values, and the
// values are held once.
func TestTwoDataSetsOverOneSourceShareItsRecords(t *testing.T) {
	c := newCache(1 << 20)
	byName := dataSet{source: "files", set: "files/by-name"}
	bySize := dataSet{source: "files", set: "files/by-size"}

	filed(c, byName, nil, wad(1, 4))
	before := c.Cost()

	// The same records in another order, and read the other way about for good
	// measure. The places are new; what they hold is already known, and is the
	// same copy.
	back := &Scope{After: NewInt(9), Count: 4, Reversed: true}
	c.hold(bySize, back,
		[]*cachedRecord{whole(4), whole(2), whole(3), whole(1)},
		Complete{Stop: StopFilled, Watermark: NewInt(1)})

	if n := len(c.sets["files/by-name"]); n != 1 {
		t.Errorf("the first sequence has %d runs", n)
	}
	if got, _, ok := asked(c, bySize, nil, back); !ok || got != "4,2,3,1" {
		t.Errorf("the second order reads %s, held %v", got, ok)
	}
	for i := int64(1); i <= 4; i++ {
		r := known(c, "files", i)
		if r == nil {
			t.Fatalf("record %d is known to neither", i)
		}
		if len(r.refs) != 2 {
			t.Errorf("record %d is pointed at from %d places, not two", i, len(r.refs))
		}
	}
	// Four more places, and no more fields. A second order over records already
	// known costs what the places cost and nothing more, which is a fraction of
	// what the first one did -- where holding the fields twice would have cost
	// the same again.
	added, places := c.Cost()-before, 4*(entryOverhead+costOfValue(NewInt(1)))
	if added != places {
		t.Errorf("a second order over the same records cost %d on top of %d, "+
			"where four places come to %d", added, before, places)
	}
	sound(t, c)
}

// One data set learning something teaches the other, the record being one
// record.
func TestWhatOneDataSetLearnsTheOtherKnows(t *testing.T) {
	c := newCache(1 << 20)
	byName := dataSet{source: "files", set: "files/by-name"}
	bySize := dataSet{source: "files", set: "files/by-size"}

	filed(c, byName, nil, slims(1, 3, "name"))
	filed(c, bySize, nil, []*cachedRecord{
		newRecord("", NewInt(3), Record{Named("name", 3), Named("size", 30)}, false, 0),
		newRecord("", NewInt(2), Record{Named("name", 2), Named("size", 20)}, false, 0),
		newRecord("", NewInt(1), Record{Named("name", 1), Named("size", 10)}, false, 0),
	})

	both := Record{Named("name", 0), Named("size", 0)}
	if got, _, ok := asked(c, byName, both, &Scope{Count: 3}); !ok || got != "1,2,3" {
		t.Errorf("the first sequence gave %s for size, held %v", got, ok)
	}
	sound(t, c)
}

// What a record costs is charged once, whatever points at it -- and the charge
// outlives the place that happened to arrive first.
//
// The last place to let go of a record lets go of what it held: knowledge
// nothing puts anywhere is not worth the room, and the source will say it again
// if it is ever asked.
func TestARecordIsChargedOnceAndKeptWhileAnythingPlacesIt(t *testing.T) {
	byName := dataSet{source: "files", set: "files/by-name"}
	bySize := dataSet{source: "files", set: "files/by-size"}
	place := entryOverhead + costOfValue(NewInt(1))

	// Filed by name first, so it is the place in THAT order that carries what
	// each record costs.
	build := func() *cache {
		c := newCache(1 << 20)
		filed(c, byName, nil, wad(1, 3))
		filed(c, bySize, nil, []*cachedRecord{whole(3), whole(2), whole(1)})
		return c
	}

	// Dropping the place that carries nothing frees the place and no more, and
	// what the record costs stays where it was.
	c := build()
	was := c.Cost()
	c.mu.Lock()
	c.drop(c.sets["files/by-size"][0], false) // record 1 is at that run's end
	c.mu.Unlock()
	if c.Cost() != was-place {
		t.Errorf("dropping a place that carried nothing freed %d, not %d",
			was-c.Cost(), place)
	}
	if known(c, "files", 1) == nil {
		t.Error("the other order still places record 1, and it was forgotten")
	}
	sound(t, c)

	// And dropping the one that does hands the charge to what is left, rather
	// than taking it off the books while the record is still held.
	c = build()
	was = c.Cost()
	c.mu.Lock()
	c.drop(c.sets["files/by-name"][0], true) // record 1 is at that run's start
	c.mu.Unlock()
	if c.Cost() != was-place {
		t.Errorf("dropping the place that carried the charge freed %d, not %d",
			was-c.Cost(), place)
	}
	if known(c, "files", 1) == nil {
		t.Error("the other order still places record 1, and it was forgotten")
	}
	sound(t, c)

	// Now nothing places it.
	c.mu.Lock()
	c.drop(c.sets["files/by-size"][0], false)
	c.mu.Unlock()
	if known(c, "files", 1) != nil {
		t.Error("nothing places record 1 any more, and it is still held")
	}
	sound(t, c)
}

// A name and the identity after it do not run together into one key.
//
// The two are written one after the other, so without something between them a
// source called `a` holding the record named `i123;` and a source called `ay5:`
// holding the record `123` key alike -- `y5:` being how a five-character name
// is spelled, and `i123;` being how the number is. They are different records
// of different sources and they stay apart.
func TestANameAndTheIdentityAfterItDoNotRunTogether(t *testing.T) {
	c := newCache(1 << 20)
	one := dataSet{source: "a", set: "a"}
	two := dataSet{source: "ay5:", set: "ay5:"}
	odd, plain := NewSymbol("i123;"), NewInt(123)

	place := func(ds dataSet, id *Value, who string) {
		c.hold(ds, &Scope{Count: 1},
			[]*cachedRecord{newRecord("", id, Record{Named("name", who)}, false, 0)},
			Complete{Stop: StopFilled, Watermark: id})
	}
	place(one, odd, "left")
	place(two, plain, "right")

	if len(c.recs) != 2 || len(c.runs) != 2 {
		t.Fatalf("two records of two sources came to %d of them in %d runs",
			len(c.recs), len(c.runs))
	}
	for _, w := range []struct {
		ds   dataSet
		id   *Value
		want string
	}{{one, odd, "left"}, {two, plain, "right"}} {
		r := c.recs[keyed(w.ds.source, w.id)]
		if r == nil || r.fields.Get("name").Str != w.want {
			t.Errorf("%q holds %s for %s", w.ds.source, r.fields, valueText(w.id))
		}
	}
	sound(t, c)
}

// Making room for an answer can hand that answer a charge it did not arrive
// with: it shares its records with the run being evicted, and the places
// carrying what those records cost are the ones going. The answer is not in the
// table while that happens, so it is measured again before it is counted -- and
// the books still add up.
func TestAnAnswerChargedWhileItIsBeingFiledStillAddsUp(t *testing.T) {
	one := ent(1).cost
	byName := dataSet{source: "files", set: "files/by-name"}
	bySize := dataSet{source: "files", set: "files/by-size"}

	// Nine records' room, so that a run of four is well under the share one run
	// is cut down to and nothing is trimmed on the way in.
	build := func() *cache {
		c := newCache(one * 9)
		// Four records that have proved themselves, of another source, so that
		// they are not what gives way.
		filed(c, over("other"), nil, wad(1, 4))
		for i := 0; i < 2; i++ {
			asked(c, over("other"), nil, &Scope{Count: 4})
		}
		// And four nothing has asked for twice, which are where the charges sit.
		filed(c, byName, nil, wad(1, 4))
		return c
	}

	// The second order over the same records has to evict the first to fit, and
	// every place it evicts hands its charge to a place in the answer being
	// filed -- which is not in the table to be charged through yet.
	c := build()
	filed(c, bySize, nil, []*cachedRecord{whole(4), whole(3), whole(2), whole(1)})
	sound(t, c)
	if !inCache(c, over("other"), 1) {
		t.Error("what had proved itself gave way to an order over records already held")
	}

	// And where the two overlap only in part, so that some of what is evicted
	// hands its charge on and some of it goes for good.
	c = build()
	filed(c, bySize, nil, []*cachedRecord{whole(3), whole(2), whole(9), whole(8)})
	sound(t, c)
}

// An answer naming one record twice is two places for one record, and there is
// no order in which both are right. None of it is placed.
func TestAnAnswerNamingOneRecordTwiceIsRefused(t *testing.T) {
	c := newCache(1 << 20)
	c.hold(over("files"), &Scope{Count: 3},
		[]*cachedRecord{whole(1), whole(2), whole(1)},
		Complete{Stop: StopFilled, Watermark: NewInt(1)})

	if c.Cost() != 0 || len(c.runs) != 0 || len(c.recs) != 0 {
		t.Errorf("it held %d in %d runs over %d records",
			c.Cost(), len(c.runs), len(c.recs))
	}
	sound(t, c)
}

// An answer overlapping records already placed does not place them twice.
func TestAnOverlappingAnswerIsNotPlacedTwice(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, over("files"), nil, wad(1, 4))
	was := c.Cost()

	filed(c, over("files"), NewInt(2), wad(3, 6))
	if c.Cost() != was {
		t.Errorf("an overlapping answer took the cache from %d to %d", was, c.Cost())
	}
	if inCache(c, over("files"), 5) {
		t.Error("it placed part of the overlapping answer")
	}
	if known(c, "files", 5) != nil {
		t.Error("it kept knowledge of a record it placed nowhere")
	}
	sound(t, c)
}

// --- the books -----------------------------------------------------------

// sound checks everything the cache says about itself against what it is
// actually holding. A cache whose books are wrong evicts the wrong things, or
// stops evicting at all, and neither shows up as a wrong answer until much
// later -- so it is checked after anything that moves records about.
func sound(t *testing.T, c *cache) {
	t.Helper()
	cost, warm, n := 0, 0, 0
	seen := map[*cachedRecord]int{}
	for _, s := range c.runs {
		if s.n == 0 {
			t.Error("an emptied run is still held")
		}
		cold, held, mine := 0, 0, 0
		for e := s.head; e != nil; e = e.next {
			cost, n, held, mine = cost+e.cost, n+1, held+1, mine+e.cost
			if e.warm {
				warm += e.cost
			} else {
				cold += e.cost
			}
			if e.scope != s.id {
				t.Errorf("record %s sits in run %d saying it is in %d",
					valueText(e.id), s.id, e.scope)
			}
			if e.rec == nil {
				t.Errorf("record %s stands in run %d holding nothing at all",
					valueText(e.id), s.id)
				continue
			}
			seen[e.rec]++
			if c.recs[keyed(e.rec.src, e.id)] != e.rec {
				t.Errorf("record %s stands in run %d pointing at knowledge the "+
					"cache no longer keeps", valueText(e.id), s.id)
			}
		}
		if held != s.n || cold != s.cold || mine != s.cost {
			t.Errorf("a run of %d at %d says %d at %d, and %d of it is on "+
				"probation against %d", held, mine, s.n, s.cost, cold, s.cold)
		}
	}
	if cost != c.cost {
		t.Errorf("the runs hold %d and the total says %d", cost, c.cost)
	}
	if warm != c.warm {
		t.Errorf("%d is protected and the total says %d", warm, c.warm)
	}
	if len(c.at) != n {
		t.Errorf("the lookup names %d places and the runs hold %d", len(c.at), n)
	}
	// Every record is pointed at by exactly the places that say they point at
	// it, and none is kept that nothing points at.
	if len(c.recs) != len(seen) {
		t.Errorf("%d records are known and %d are pointed at", len(c.recs), len(seen))
	}
	for r, places := range seen {
		if len(r.refs) != places {
			t.Errorf("record %s is pointed at from %d places and lists %d",
				valueText(r.id), places, len(r.refs))
		}
		for _, e := range r.refs {
			if e.rec != r {
				t.Errorf("record %s lists a place that points elsewhere",
					valueText(r.id))
			}
		}
		// Exactly one place carries what the record costs, and it is the first.
		own := entryOverhead + costOfValue(r.id)
		for i, e := range r.refs {
			want := own
			if i == 0 {
				want += r.cost
			}
			if e.cost != want {
				t.Errorf("place %d of record %s costs %d, and should cost %d",
					i, valueText(r.id), e.cost, want)
			}
		}
	}
	for set, ss := range c.sets {
		for _, s := range ss {
			if c.runs[s.id] != s {
				t.Errorf("%q lists a run the cache has forgotten", set)
			}
		}
	}
	if c.cost > c.limit && c.cost > 0 {
		t.Errorf("it holds %d against a limit of %d", c.cost, c.limit)
	}
}

// What the cache says it is holding is what it is holding, through eviction.
func TestTheTotalIsWhatTheRunsHold(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 10)
	for i := int64(0); i < 8; i++ {
		filed(c, over("files"), NewInt(i*3), wad(i*3+1, i*3+3))
		asked(c, over("files"), nil, &Scope{After: NewInt(i * 3), Count: 3})
		sound(t, c)
	}
}

// The point of the two segments: a flood of records nothing has asked for twice
// does not push out the records that have proved themselves.
func TestAFloodDoesNotEvictWhatHasProvedItself(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 40)

	// Five records, read twice, so they are warm.
	filed(c, over("proven"), nil, wad(1, 5))
	for i := 0; i < 2; i++ {
		if _, _, ok := asked(c, over("proven"), nil, &Scope{Count: 5}); !ok {
			t.Fatal("the proven run would not answer")
		}
	}

	// Then a walk of two hundred records through the same cache, each read once
	// as it goes past, which is what a scan does.
	for i := int64(0); i < 40; i++ {
		filed(c, over("flood"), NewInt(1000+i*5), wad(1000+i*5+1, 1000+i*5+5))
		asked(c, over("flood"), nil, &Scope{After: NewInt(1000 + i*5), Count: 5})
	}

	for i := int64(1); i <= 5; i++ {
		if !inCache(c, over("proven"), i) {
			t.Errorf("record %d was flushed out by the flood", i)
		}
	}
	if got, _, ok := asked(c, over("proven"), nil, &Scope{Count: 5}); !ok || got != "1,2,3,4,5" {
		t.Errorf("the proven run now gives %s, held %v", got, ok)
	}
	sound(t, c)
}

// One answer larger than the cache does not evict everything else on its way in
// only to be cut down afterwards. It is cut down first.
func TestOneEnormousAnswerIsCutDownBeforeAnythingGivesWayForIt(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 20)
	filed(c, over("proven"), nil, wad(1, 4))
	for i := 0; i < 2; i++ {
		asked(c, over("proven"), nil, &Scope{Count: 4})
	}

	filed(c, over("flood"), nil, wad(100, 500))

	for i := int64(1); i <= 4; i++ {
		if !inCache(c, over("proven"), i) {
			t.Errorf("record %d gave way to one enormous answer", i)
		}
	}
	// What was kept of the flood is its END -- the part a reader scrolling down
	// is next to -- and no more than its share.
	if !inCache(c, over("flood"), 500) || inCache(c, over("flood"), 100) {
		t.Error("it kept the wrong end of the flood")
	}
	if c.Cost() > c.Limit() {
		t.Errorf("it holds %d against a limit of %d", c.Cost(), c.Limit())
	}
	// And what it shed on the way in it does not still know: a record cut off
	// the front of a flood is placed nowhere.
	if known(c, "flood", 100) != nil {
		t.Error("it kept what it knew about a record it never placed")
	}
	sound(t, c)
}

// proven fills the cache with records that have all been read twice, spread
// over enough sequences that no one run is large enough to be taken for a
// flood.
func proven(c *cache, sets int, each int64) {
	for i := 0; i < sets; i++ {
		ds := over(string(rune('a' + i)))
		filed(c, ds, nil, wad(1, each))
		asked(c, ds, nil, &Scope{Count: int(each)})
		asked(c, ds, nil, &Scope{Count: int(each)})
	}
}

// A run that outgrows its share as it is scrolled loses the end the reader has
// left behind -- and that is decided by which way the reader is going, not by
// which end was read longest ago. Records that have just arrived have been read
// never, which would sort them coldest of all, and they are the very ones the
// reader is sitting on.
func TestAGrowingRunLosesTheEndTheReaderHasLeft(t *testing.T) {
	one := ent(1).cost

	down := newCache(one * 20) // so one run may hold ten
	filed(down, over("files"), nil, wad(1, 6))
	asked(down, over("files"), nil, &Scope{Count: 6})
	filed(down, over("files"), NewInt(6), wad(7, 12))
	if inCache(down, over("files"), 1) || !inCache(down, over("files"), 12) {
		t.Error("scrolling down lost the end it was scrolling towards")
	}
	sound(t, down)

	up := newCache(one * 20)
	up.hold(over("files"), &Scope{After: NewInt(100), Count: 6, Reversed: true},
		[]*cachedRecord{whole(99), whole(98), whole(97), whole(96), whole(95), whole(94)},
		Complete{Stop: StopFilled, Watermark: NewInt(94)})
	asked(up, over("files"), nil, &Scope{After: NewInt(100), Count: 6, Reversed: true})
	up.hold(over("files"), &Scope{After: NewInt(94), Count: 6, Reversed: true},
		[]*cachedRecord{whole(93), whole(92), whole(91), whole(90), whole(89), whole(88)},
		Complete{Stop: StopFilled, Watermark: NewInt(88)})
	if inCache(up, over("files"), 99) || !inCache(up, over("files"), 88) {
		t.Error("scrolling up lost the end it was scrolling towards")
	}
	sound(t, up)
}

// A run outgrowing its share stops at records that have proved themselves
// rather than eating through them. The share is a cap on what is UNPROVEN, so
// taking a proven record for it would not even bring the run under the cap.
func TestAGrowingRunDoesNotEatThroughWhatItHasProved(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 60) // so one run may hold thirty on probation

	filed(c, over("files"), nil, wad(1, 10))
	for i := 0; i < 2; i++ {
		asked(c, over("files"), nil, &Scope{Count: 10})
	}
	filed(c, over("files"), NewInt(10), wad(11, 25))
	filed(c, over("files"), NewInt(25), wad(26, 45))

	for i := int64(1); i <= 10; i++ {
		if !inCache(c, over("files"), i) {
			t.Errorf("record %d was eaten by the run it had proved itself in", i)
		}
	}
	sound(t, c)
}

// A refusal is an answer, and it is not a run: nothing is guaranteed between
// anything.
func TestARefusalIsNotFiled(t *testing.T) {
	c := newCache(1 << 20)
	c.hold(over("files"), &Scope{Count: 3}, wad(1, 3),
		Complete{Error: "the records are gone"})
	if c.Cost() != 0 || len(c.runs) != 0 || len(c.recs) != 0 {
		t.Errorf("a refusal left %d bytes in %d runs over %d records",
			c.Cost(), len(c.runs), len(c.recs))
	}
}

// A cache too small to hold one record holds none, and does not keep a run of
// nothing to say so -- nor anything it learned on the way in.
func TestACacheTooSmallForOneRecordHoldsNothing(t *testing.T) {
	c := newCache(1)
	filed(c, over("files"), nil, wad(1, 5))
	if c.Cost() != 0 || len(c.runs) != 0 || len(c.sets) != 0 || len(c.recs) != 0 {
		t.Errorf("it holds %d in %d runs over %d sequences and %d records",
			c.Cost(), len(c.runs), len(c.sets), len(c.recs))
	}
	sound(t, c)
}

// The protected segment cannot fill the cache: something has to be left on
// probation, or nothing new could ever prove itself.
func TestTheProtectedSegmentIsHeldUnderItsShare(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 20)
	proven(c, 4, 5)

	if c.warm == 0 {
		t.Fatal("nothing was protected at all")
	}
	if c.warm > c.limit*warmShare/warmOf {
		t.Errorf("%d of a %d cache is protected, past the share of %d",
			c.warm, c.limit, c.limit*warmShare/warmOf)
	}
}

// A cache with nothing left on probation still makes room, by cooling what has
// proved itself and taking it on the next pass. Without that it would wedge:
// full of protected records, unable to evict any of them, and unable to hold
// anything new.
func TestACacheOfNothingButProvenRecordsStillMakesRoom(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 20)
	proven(c, 4, 5)

	c.SetLimit(one * 4)
	if c.Cost() > one*4 {
		t.Errorf("it holds %d against a limit of %d", c.Cost(), one*4)
	}
	if c.Cost() == 0 {
		t.Error("it emptied itself rather than trimming")
	}
	sound(t, c)
}

// Eviction that takes a whole run leaves nothing behind it: no run of no
// records, no place in the lookup that no run holds, no record nothing places,
// no cost still counted.
func TestEvictionLeavesNothingDangling(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 6)
	for i := 0; i < 6; i++ {
		ds := over(string(rune('a' + i)))
		filed(c, ds, nil, wad(1, 4))
		asked(c, ds, nil, &Scope{Count: 4})
		sound(t, c)
	}
	if len(c.runs) == 6 {
		t.Fatal("nothing was evicted, so nothing was proved")
	}
}

// Shrinking the cache evicts down to the new size at once.
func TestSettingTheLimitEvictsDownToIt(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 100)
	filed(c, over("files"), nil, wad(1, 30))

	c.SetLimit(one * 5)
	if c.Cost() > one*5 {
		t.Errorf("after shrinking to %d it holds %d", one*5, c.Cost())
	}
	if c.Cost() == 0 {
		t.Error("it emptied itself rather than trimming")
	}
	sound(t, c)
}
