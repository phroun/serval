package serval

import (
	"testing"
)

// The two caches are sized apart, so a test sizes apart what it is about: how
// many PLACES the order may hold, and how many RECORDS the values may.
var (
	onePlace  = entryOverhead + costOfValue(NewInt(1))
	oneRecord = whole(1).cost
)

// sized is a cache with room for so many of each.
func sized(places, records int) *cache {
	c := newCache(0)
	c.limit = places * onePlace
	c.flesh.setLimit(records * oneRecord)
	return c
}

// roomy is a cache nothing will be evicted from, for the tests that are about
// what is answered rather than about what gives way.
func roomy() *cache { return sized(1<<20, 1<<20) }

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
// query asking `fields={ ... }` leaves behind. The records are said to have one
// more named member than was sent, so that a subset stays a subset however many
// of them are asked for.
func slims(from, to int64, names ...string) []*cachedRecord {
	return slimsOf(Totals{Named: len(names) + 1}, from, to, names...)
}

// slimsOf is the same, saying exactly how many members the records have.
func slimsOf(has Totals, from, to int64, names ...string) []*cachedRecord {
	more := has
	out := []*cachedRecord{}
	for i := from; i <= to; i++ {
		f := Record{}
		for _, n := range names {
			f = append(f, Named(n, i))
		}
		out = append(out, newRecord("", NewInt(i), f, more, 0))
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
	rs, done, ok := c.serve(ds, wanted, sc)
	out := ""
	for _, r := range rs {
		if out != "" {
			out += ","
		}
		out += valueText(r.id)
	}
	return out, done.Stop, ok
}

// placed reports whether one record of a data set still stands in the order.
func placed(c *cache, ds dataSet, id int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at[keyed(ds.set, NewInt(id))] != nil
}

// known is what the cache knows one record of a source holds, and nil for one
// it knows nothing about.
func known(c *cache, src string, id int64) *cachedRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.flesh.get(src, NewInt(id))
}

// A scope answered once is answered again from what was filed, rather than
// asked a second time.
func TestAnAnsweredScopeIsAnsweredAgainFromTheCache(t *testing.T) {
	c := roomy()
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
	c := roomy()
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
	c := roomy()
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
	c := roomy()
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
	c := roomy()
	filed(c, over("files"), nil, wad(1, 6))

	got, stop, ok := asked(c, over("files"), nil,
		&Scope{After: NewInt(1), Until: NewInt(4), Count: 9})
	if !ok || got != "2,3" || stop != StopJoined {
		t.Errorf("it gave %s / %s, held %v", got, stop, ok)
	}
}

// Read backwards a run answers the same way, the ends swapping over.
func TestAScopeReadBackwards(t *testing.T) {
	c := roomy()
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
	c := roomy()
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
	c := roomy()
	filed(c, over("files"), NewInt(3), wad(4, 6))
	filed(c, over("files"), nil, wad(1, 3))

	if n := len(c.sets["files"]); n != 1 {
		t.Errorf("it left %d runs", n)
	}
	if got, _, ok := asked(c, over("files"), nil, &Scope{Count: 6}); !ok || got != "1,2,3,4,5,6" {
		t.Errorf("it reads %s, held %v", got, ok)
	}
}

// --- the order and the values ---------------------------------------------

// What a record holds decides what a walk over it can answer, and holding more
// than was asked for is an answer.
func TestAWalkAnswersOnlyForRecordsThatAreKnownWellEnough(t *testing.T) {
	c := roomy()
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
	c := roomy()
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
	c := roomy()
	filed(c, over("files"), nil, slims(1, 4, "name"))

	both := Record{Named("name", 0), Named("size", 0)}
	if _, _, ok := asked(c, over("files"), both, &Scope{Count: 4}); ok {
		t.Fatal("it answered for size before anything had said what size was")
	}

	// Read twice, so the places have proved themselves.
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

// A record growing past what the cache has room for gives way like anything
// else: a top-up is something arriving, even though it arrives inside a record
// that is already here.
func TestATopUpMakesRoomForWhatItAdds(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, slims(1, 6, "name"))
	c.flesh.setLimit(c.flesh.cost) // room for exactly what is held, and no more

	filed(c, over("files"), nil, slims(1, 6, "name", "size"))
	if c.flesh.cost > c.flesh.limit {
		t.Errorf("after a top-up the values hold %d against a limit of %d",
			c.flesh.cost, c.flesh.limit)
	}
	if len(c.flesh.at) == 6 {
		t.Error("every record grew and none gave way, so nothing here is shown")
	}
	sound(t, c)
}

// A second answer that says a record arrived WHOLE settles every question about
// it, including ones neither answer named.
func TestAnAnswerThatArrivesWholeSettlesEverything(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, slims(1, 3, "name"))
	if _, _, ok := asked(c, over("files"), nil, &Scope{Count: 3}); ok {
		t.Fatal("a subset answered for the whole record")
	}

	filed(c, over("files"), nil, wad(1, 3))
	if r := known(c, "files", 1); r == nil || !r.entire() {
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
	c := roomy()
	filed(c, over("files"), nil, wad(1, 3))
	was, held := c.Cost(), len(known(c, "files", 1).fields)

	filed(c, over("files"), nil, slims(1, 3, "thumbnail"))
	r := known(c, "files", 1)
	if r == nil || !r.entire() {
		t.Error("a record known entire stopped being so")
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
	c := roomy()
	byName := dataSet{source: "files", set: "files/by-name"}
	bySize := dataSet{source: "files", set: "files/by-size"}

	filed(c, byName, nil, wad(1, 4))
	before := c.Cost()

	// The same records in another order, and read the other way about for good
	// measure. The places are new; what they hold is already known.
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
	if n := len(c.flesh.at); n != 4 {
		t.Errorf("two orders over four records know %d of them", n)
	}
	// Four more places, and no more values.
	if added, want := c.Cost()-before, 4*onePlace; added != want {
		t.Errorf("a second order over the same records cost %d on top of %d, "+
			"where four places come to %d", added, before, want)
	}
	sound(t, c)
}

// One data set learning something teaches the other, the record being one
// record.
func TestWhatOneDataSetLearnsTheOtherKnows(t *testing.T) {
	c := roomy()
	byName := dataSet{source: "files", set: "files/by-name"}
	bySize := dataSet{source: "files", set: "files/by-size"}

	filed(c, byName, nil, slims(1, 3, "name"))
	filed(c, bySize, nil, []*cachedRecord{
		newRecord("", NewInt(3), Record{Named("name", 3), Named("size", 30)}, Totals{Named: 3}, 0),
		newRecord("", NewInt(2), Record{Named("name", 2), Named("size", 20)}, Totals{Named: 3}, 0),
		newRecord("", NewInt(1), Record{Named("name", 1), Named("size", 10)}, Totals{Named: 3}, 0),
	})

	both := Record{Named("name", 0), Named("size", 0)}
	if got, _, ok := asked(c, byName, both, &Scope{Count: 3}); !ok || got != "1,2,3" {
		t.Errorf("the first sequence gave %s for size, held %v", got, ok)
	}
	sound(t, c)
}

// --- the two caches let go of each other ----------------------------------

// The values outlive the order. Close one sort and open another and the new
// order is new, while every value it needs is still here -- which is the whole
// reason the records are not kept with the run that placed them.
func TestTheValuesOutliveTheOrder(t *testing.T) {
	c := sized(4, 64) // room for one order of four places, and values to spare
	byName := dataSet{source: "files", set: "files/by-name"}
	bySize := dataSet{source: "files", set: "files/by-size"}

	filed(c, byName, nil, wad(1, 4))
	// A second order of four, which the order has no room for beside the first.
	filed(c, bySize, nil, []*cachedRecord{whole(4), whole(3), whole(2), whole(1)})

	if placed(c, byName, 1) && placed(c, bySize, 1) {
		t.Fatal("both orders fit, so nothing was given up and nothing is shown")
	}
	for i := int64(1); i <= 4; i++ {
		if known(c, "files", i) == nil {
			t.Errorf("record %d was forgotten along with the order that placed it", i)
		}
	}
	sound(t, c)
}

// And the order outlives the values. A run whose records have been evicted
// still knows what comes after what -- which is what a top-up will be asked
// against, rather than the stretch being walked from the start.
func TestTheOrderOutlivesTheValues(t *testing.T) {
	c := sized(64, 3) // room for the order, and for three records of values
	filed(c, over("files"), nil, wad(1, 6))

	if n := len(c.flesh.at); n != 3 {
		t.Fatalf("a cache of three records holds %d", n)
	}
	// The three it kept are the ones that arrived LAST, which is where the
	// reader is: records come in walk order, and the front of a walk is what it
	// has already gone past.
	for i := int64(1); i <= 6; i++ {
		if kept := known(c, "files", i) != nil; kept != (i > 3) {
			t.Errorf("record %d of six, into room for three, kept=%v", i, kept)
		}
	}
	for i := int64(1); i <= 6; i++ {
		if !placed(c, over("files"), i) {
			t.Errorf("place %d went with the values standing in it", i)
		}
	}
	// The order is still there and still one run, and the walk misses because
	// the records are not known -- not because the sequence is not.
	if n := len(c.sets["files"]); n != 1 {
		t.Errorf("the order is in %d runs", n)
	}
	if _, _, ok := asked(c, over("files"), nil, &Scope{Count: 6}); ok {
		t.Error("it answered for records it no longer knows anything about")
	}
	// And a fresh answer over the same stretch tops those records back up
	// without disturbing the order.
	filed(c, over("files"), nil, wad(1, 6))
	if n := len(c.sets["files"]); n != 1 {
		t.Errorf("topping the values back up left %d runs", n)
	}
	sound(t, c)
}

// An answer overlapping records already placed does not place them twice --
// and still says what those records hold.
func TestAnOverlappingAnswerIsNotPlacedTwice(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, slims(1, 4, "name"))
	runs := len(c.sets["files"])

	filed(c, over("files"), NewInt(2), slims(3, 6, "name", "size"))
	if n := len(c.sets["files"]); n != runs {
		t.Errorf("an overlapping answer took the order from %d runs to %d", runs, n)
	}
	if placed(c, over("files"), 5) {
		t.Error("it placed part of the overlapping answer")
	}
	// What it said about the records it overlapped is known, including for the
	// ones it never placed: knowledge is knowledge wherever it stands.
	if r := known(c, "files", 3); r == nil || !r.fields.Has("size") {
		t.Error("it dropped what the overlapping answer said about a record it holds")
	}
	if known(c, "files", 5) == nil {
		t.Error("it dropped what the overlapping answer said about record 5")
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
	c := roomy()
	one := dataSet{source: "a", set: "a"}
	two := dataSet{source: "ay5:", set: "ay5:"}
	odd, plain := NewSymbol("i123;"), NewInt(123)

	place := func(ds dataSet, id *Value, who string) {
		c.hold(ds, &Scope{Count: 1},
			[]*cachedRecord{newRecord("", id, Record{Named("name", who)}, Totals{Named: 2}, 0)},
			Complete{Stop: StopFilled, Watermark: id})
	}
	place(one, odd, "left")
	place(two, plain, "right")

	if len(c.flesh.at) != 2 || len(c.runs) != 2 {
		t.Fatalf("two records of two sources came to %d of them in %d runs",
			len(c.flesh.at), len(c.runs))
	}
	for _, w := range []struct {
		ds   dataSet
		id   *Value
		want string
	}{{one, odd, "left"}, {two, plain, "right"}} {
		r := c.flesh.at[keyed(w.ds.source, w.id)]
		if r == nil || r.fields.Get("name").Str != w.want {
			t.Errorf("%q holds %s for %s", w.ds.source, r.fields, valueText(w.id))
		}
	}
	sound(t, c)
}

// An answer naming one record twice is two places for one record, and there is
// no order in which both are right. None of it is placed -- though what it says
// those records hold is still taken.
func TestAnAnswerNamingOneRecordTwiceIsNotPlaced(t *testing.T) {
	c := roomy()
	c.hold(over("files"), &Scope{Count: 3},
		[]*cachedRecord{whole(1), whole(2), whole(1)},
		Complete{Stop: StopFilled, Watermark: NewInt(1)})

	if len(c.runs) != 0 || len(c.at) != 0 {
		t.Errorf("it placed %d records in %d runs", len(c.at), len(c.runs))
	}
	sound(t, c)
}

// --- the books -----------------------------------------------------------

// sound checks everything both caches say about themselves against what they
// are actually holding. A cache whose books are wrong evicts the wrong things,
// or stops evicting at all, and neither shows up as a wrong answer until much
// later -- so it is checked after anything that moves records about.
func sound(t *testing.T, c *cache) {
	t.Helper()
	soundOrder(t, c)
	soundValues(t, c.flesh)
}

func soundOrder(t *testing.T, c *cache) {
	t.Helper()
	cost, warm, n := 0, 0, 0
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
			if want := entryOverhead + costOfValue(e.id); e.cost != want {
				t.Errorf("place %s costs %d, and should cost %d",
					valueText(e.id), e.cost, want)
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
	for set, ss := range c.sets {
		for _, s := range ss {
			if c.runs[s.id] != s {
				t.Errorf("%q lists a run the cache has forgotten", set)
			}
		}
	}
	if c.cost > c.limit && c.cost > 0 {
		t.Errorf("the order holds %d against a limit of %d", c.cost, c.limit)
	}
}

func soundValues(t *testing.T, rc *recordCache) {
	t.Helper()
	cost, warm, listed := 0, 0, 0
	for _, seg := range []struct {
		warm        bool
		front, back *cachedRecord
	}{{false, rc.coldFront, rc.coldBack}, {true, rc.warmFront, rc.warmBack}} {
		// Bounded, so that a segment relinked into a ring is reported rather
		// than walked for ever.
		var last *cachedRecord
		for r := seg.front; r != nil; r = r.next {
			if listed > len(rc.at) {
				t.Fatalf("a segment holds more records than the table names, " +
					"so it has been linked into a ring")
			}
			listed, cost = listed+1, cost+r.cost
			if r.warm != seg.warm {
				t.Errorf("record %s is in the %v segment saying it is %v",
					valueText(r.id), seg.warm, r.warm)
			}
			if r.warm {
				warm += r.cost
			}
			if r.prior != last {
				t.Errorf("record %s looks back at the wrong record", valueText(r.id))
			}
			if rc.at[keyed(r.src, r.id)] != r {
				t.Errorf("record %s is in a segment and not in the table",
					valueText(r.id))
			}
			if want := costOfFields(r.fields); r.cost != want {
				t.Errorf("record %s holds %s at %d, and should cost %d",
					valueText(r.id), r.fields, r.cost, want)
			}
			last = r
		}
		if last != seg.back {
			t.Error("a segment's back is not the last record in it")
		}
	}
	if listed != len(rc.at) {
		t.Errorf("the segments hold %d records and the table names %d",
			listed, len(rc.at))
	}
	if cost != rc.cost {
		t.Errorf("the records come to %d and the total says %d", cost, rc.cost)
	}
	if warm != rc.warm {
		t.Errorf("%d of them is protected and the total says %d", warm, rc.warm)
	}
	if rc.cost > rc.limit && rc.cost > 0 {
		t.Errorf("the values hold %d against a limit of %d", rc.cost, rc.limit)
	}
}

// What the caches say they are holding is what they are holding, through
// eviction.
func TestTheTotalIsWhatIsHeld(t *testing.T) {
	c := sized(10, 10)
	for i := int64(0); i < 8; i++ {
		filed(c, over("files"), NewInt(i*3), wad(i*3+1, i*3+3))
		asked(c, over("files"), nil, &Scope{After: NewInt(i * 3), Count: 3})
		sound(t, c)
	}
}

// The point of the two segments, on each side: a flood of records nothing has
// asked for twice does not push out what has proved itself.
func TestAFloodDoesNotEvictWhatHasProvedItself(t *testing.T) {
	c := sized(40, 40)

	// Five records, read twice, so they are warm on both counts.
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
		if !placed(c, over("proven"), i) {
			t.Errorf("place %d was flushed out by the flood", i)
		}
		if known(c, "proven", i) == nil {
			t.Errorf("record %d was flushed out by the flood", i)
		}
	}
	if got, _, ok := asked(c, over("proven"), nil, &Scope{Count: 5}); !ok || got != "1,2,3,4,5" {
		t.Errorf("the proven run now gives %s, held %v", got, ok)
	}
	sound(t, c)
}

// One answer larger than the order's room does not evict everything else on its
// way in only to be cut down afterwards. It is cut down first.
func TestOneEnormousAnswerIsCutDownBeforeAnythingGivesWayForIt(t *testing.T) {
	c := sized(20, 1<<20)
	filed(c, over("proven"), nil, wad(1, 4))
	for i := 0; i < 2; i++ {
		asked(c, over("proven"), nil, &Scope{Count: 4})
	}

	filed(c, over("flood"), nil, wad(100, 500))

	for i := int64(1); i <= 4; i++ {
		if !placed(c, over("proven"), i) {
			t.Errorf("place %d gave way to one enormous answer", i)
		}
	}
	// What was kept of the flood is its END -- the part a reader scrolling down
	// is next to -- and no more than its share.
	if !placed(c, over("flood"), 500) || placed(c, over("flood"), 100) {
		t.Error("it kept the wrong end of the flood")
	}
	if c.cost > c.limit {
		t.Errorf("the order holds %d against a limit of %d", c.cost, c.limit)
	}
	// The values are another cache with its own room, and it was given room:
	// what the order never placed it still knows, and will not ask for again.
	if known(c, "flood", 100) == nil {
		t.Error("it forgot a record whose place it cut, though it had the room")
	}
	sound(t, c)
}

// proven fills both caches with records that have all been read twice, spread
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
// which end was read longest ago. Places that have just arrived have been read
// never, which would sort them coldest of all, and they are the very ones the
// reader is sitting on.
func TestAGrowingRunLosesTheEndTheReaderHasLeft(t *testing.T) {
	down := sized(20, 1<<20) // so one run may hold ten places
	filed(down, over("files"), nil, wad(1, 6))
	asked(down, over("files"), nil, &Scope{Count: 6})
	filed(down, over("files"), NewInt(6), wad(7, 12))
	if placed(down, over("files"), 1) || !placed(down, over("files"), 12) {
		t.Error("scrolling down lost the end it was scrolling towards")
	}
	sound(t, down)

	up := sized(20, 1<<20)
	up.hold(over("files"), &Scope{After: NewInt(100), Count: 6, Reversed: true},
		[]*cachedRecord{whole(99), whole(98), whole(97), whole(96), whole(95), whole(94)},
		Complete{Stop: StopFilled, Watermark: NewInt(94)})
	asked(up, over("files"), nil, &Scope{After: NewInt(100), Count: 6, Reversed: true})
	up.hold(over("files"), &Scope{After: NewInt(94), Count: 6, Reversed: true},
		[]*cachedRecord{whole(93), whole(92), whole(91), whole(90), whole(89), whole(88)},
		Complete{Stop: StopFilled, Watermark: NewInt(88)})
	if placed(up, over("files"), 99) || !placed(up, over("files"), 88) {
		t.Error("scrolling up lost the end it was scrolling towards")
	}
	sound(t, up)
}

// A run outgrowing its share stops at places that have proved themselves rather
// than eating through them. The share is a cap on what is UNPROVEN, so taking a
// proven place for it would not even bring the run under the cap.
func TestAGrowingRunDoesNotEatThroughWhatItHasProved(t *testing.T) {
	c := sized(60, 1<<20) // so one run may hold thirty places on probation

	filed(c, over("files"), nil, wad(1, 10))
	for i := 0; i < 2; i++ {
		asked(c, over("files"), nil, &Scope{Count: 10})
	}
	filed(c, over("files"), NewInt(10), wad(11, 25))
	filed(c, over("files"), NewInt(25), wad(26, 45))

	for i := int64(1); i <= 10; i++ {
		if !placed(c, over("files"), i) {
			t.Errorf("place %d was eaten by the run it had proved itself in", i)
		}
	}
	sound(t, c)
}

// A refusal is an answer, and it is not a run: nothing is guaranteed between
// anything, and nothing is learned either.
func TestARefusalIsNotFiled(t *testing.T) {
	c := roomy()
	c.hold(over("files"), &Scope{Count: 3}, wad(1, 3),
		Complete{Error: "the records are gone"})
	if c.Cost() != 0 || len(c.runs) != 0 || len(c.flesh.at) != 0 {
		t.Errorf("a refusal left %d bytes in %d runs over %d records",
			c.Cost(), len(c.runs), len(c.flesh.at))
	}
}

// A cache too small for one of a thing holds none of it -- and the two are
// separate, so each answers for itself.
func TestACacheTooSmallForOneThingHoldsNoneOfIt(t *testing.T) {
	c := sized(0, 0)
	filed(c, over("files"), nil, wad(1, 5))
	if c.cost != 0 || len(c.runs) != 0 || len(c.sets) != 0 {
		t.Errorf("the order holds %d in %d runs over %d sequences",
			c.cost, len(c.runs), len(c.sets))
	}
	if c.flesh.cost != 0 || len(c.flesh.at) != 0 {
		t.Errorf("the values hold %d over %d records", c.flesh.cost, len(c.flesh.at))
	}
	sound(t, c)

	// With room for the order and none for the values, the order is still held.
	c = sized(64, 0)
	filed(c, over("files"), nil, wad(1, 5))
	if len(c.at) != 5 {
		t.Errorf("the order holds %d places of five", len(c.at))
	}
	if len(c.flesh.at) != 0 {
		t.Errorf("the values hold %d records with no room at all", len(c.flesh.at))
	}
	sound(t, c)
}

// The protected segment cannot fill either cache: something has to be left on
// probation, or nothing new could ever prove itself.
func TestTheProtectedSegmentIsHeldUnderItsShare(t *testing.T) {
	c := sized(20, 20)
	proven(c, 4, 5)

	if c.warm == 0 || c.flesh.warm == 0 {
		t.Fatalf("nothing was protected: %d of the order, %d of the values",
			c.warm, c.flesh.warm)
	}
	if c.warm > c.limit*warmShare/warmOf {
		t.Errorf("%d of a %d order is protected, past the share of %d",
			c.warm, c.limit, c.limit*warmShare/warmOf)
	}
	if c.flesh.warm > c.flesh.limit*warmShare/warmOf {
		t.Errorf("%d of a %d of values is protected, past the share of %d",
			c.flesh.warm, c.flesh.limit, c.flesh.limit*warmShare/warmOf)
	}
}

// A cache with nothing left on probation still makes room, by cooling what has
// proved itself and taking it on the next pass. Without that it would wedge:
// full of protected records, unable to evict any of them, and unable to hold
// anything new.
func TestACacheOfNothingButProvenRecordsStillMakesRoom(t *testing.T) {
	c := sized(20, 20)
	proven(c, 4, 5)

	c.SetLimit(4*onePlace + 4*oneRecord)
	if c.Cost() > c.Limit() {
		t.Errorf("it holds %d against a limit of %d", c.Cost(), c.Limit())
	}
	if c.cost == 0 || c.flesh.cost == 0 {
		t.Errorf("it emptied itself rather than trimming: %d and %d",
			c.cost, c.flesh.cost)
	}
	sound(t, c)
}

// Eviction that takes a whole run leaves nothing behind it: no run of no
// records, no place in the lookup that no run holds, no cost still counted.
func TestEvictionLeavesNothingDangling(t *testing.T) {
	c := sized(6, 6)
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

// Shrinking the cache evicts down to the new size at once, on both counts.
func TestSettingTheLimitEvictsDownToIt(t *testing.T) {
	c := sized(100, 100)
	filed(c, over("files"), nil, wad(1, 30))
	order, values := c.cost, c.flesh.cost

	c.SetLimit(5*onePlace + 5*oneRecord)
	if c.Cost() > c.Limit() {
		t.Errorf("after shrinking to %d it holds %d", c.Limit(), c.Cost())
	}
	// Both halves give way, and not just whichever one the total happened to
	// be under: they are sized apart, so they are shrunk apart.
	if c.cost >= order || c.flesh.cost >= values {
		t.Errorf("shrinking took the order from %d to %d and the values from "+
			"%d to %d", order, c.cost, values, c.flesh.cost)
	}
	if c.cost == 0 || c.flesh.cost == 0 {
		t.Errorf("it emptied itself rather than trimming: %d and %d",
			c.cost, c.flesh.cost)
	}
	sound(t, c)
}

// A new cache splits its room between the order and the values, the order
// getting the smaller share because a place is a fraction of a record.
func TestANewCacheSplitsItsRoom(t *testing.T) {
	c := newCache(1 << 20)
	if c.Limit() != 1<<20 {
		t.Errorf("a cache of %d says its limit is %d", 1<<20, c.Limit())
	}
	if c.limit >= c.flesh.limit {
		t.Errorf("the order got %d of it and the values %d", c.limit, c.flesh.limit)
	}
	if c.limit == 0 {
		t.Error("the order got none of it")
	}
}
