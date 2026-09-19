package serval

// Being told that what is held is no longer true, and what each reason costs.

import (
	"strings"
	"testing"
)

// told is one notice answered against one sequence, as a source would send it.
func told(c *cache, ds dataSet, descriptor *DataSetDescriptor, n Notice) {
	c.stale(ds, descriptor.Roles(), n)
}

// The two shapes an extent takes: one record, and a stretch between two.
func atRecord(id int64) Extent { return Extent{First: NewInt(id)} }
func fromTo(a, b int64) Extent { return Extent{First: NewInt(a), Last: NewInt(b)} }

// order is the places one sequence holds, as text.
func order(c *cache, ds dataSet) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []string{}
	for _, s := range c.sets[ds.set] {
		part := []string{}
		for e := s.head; e != nil; e = e.next {
			part = append(part, valueText(e.id))
		}
		out = append(out, valueText(s.begin)+"("+strings.Join(part, ",")+")"+valueText(s.end))
	}
	return strings.Join(out, " ")
}

// --- a record that has left the sequence ----------------------------------

// A deletion is cheaper than a move, and this is the whole reason a notice says
// which it was: everything still in the sequence between the run's ends is
// still here, so the claim is as good as it was and the run stays whole.
func TestARemovedRecordLeavesTheClaimWhole(t *testing.T) {
	c := roomy()
	filed(c, over("files"), NewInt(0), wad(1, 5))

	told(c, over("files"), nil, Notice{Extent: atRecord(3), Change: Removed})

	if len(c.runs) != 1 {
		t.Fatalf("a deletion left %d runs", len(c.runs))
	}
	if got := order(c, over("files")); got != "0(1,2,4,5)5" {
		t.Errorf("the run is %s", got)
	}
	// And it still answers across the place the record stood, which is the
	// point: nothing between the ends is missing, because that record is not
	// between them any more.
	if got, stop, ok := asked(c, over("files"), nil,
		&Scope{After: NewInt(0), Count: 4}); !ok || got != "1,2,4,5" || stop != StopFilled {
		t.Errorf("reading across it gave %s / %s, held %v", got, stop, ok)
	}
	// The values go with it, and nobody else's do.
	if known(c, "files", 3) != nil {
		t.Error("a record that left the sequence is still known")
	}
	if known(c, "files", 2) == nil || known(c, "files", 4) == nil {
		t.Error("its neighbours were forgotten with it")
	}
	sound(t, c)
}

// A run's own boundaries are the exception. They say where the guarantee starts
// and stops, and one anchored to a record that has left the sequence is a
// boundary nothing can be matched against.
func TestRemovingARunsOwnBoundaryTakesTheRun(t *testing.T) {
	for _, gone := range []int64{
		0, // the beginning, which the run does not even hold
		5, // the end, which it does
	} {
		c := roomy()
		filed(c, over("files"), NewInt(0), wad(1, 5))

		told(c, over("files"), nil, Notice{Extent: atRecord(gone), Change: Removed})

		if len(c.runs) != 0 || len(c.at) != 0 {
			t.Errorf("removing the boundary at %d left %d places in %d runs",
				gone, len(c.at), len(c.runs))
		}
		// The order went; what the records HOLD did not. Knowledge nothing
		// places is still true.
		if known(c, "files", 2) == nil {
			t.Errorf("removing the boundary at %d forgot a record's values", gone)
		}
		sound(t, c)
	}
}

// A run of one record that is not its own boundary goes when that record leaves
// the sequence: there is nothing left between its ends to guarantee.
func TestARunEmptiedByADeletionIsForgotten(t *testing.T) {
	c := roomy()
	// Guaranteed from record 2 to the end of the sequence, holding one record,
	// which is therefore neither of its boundaries.
	ended(c, over("files"), NewInt(2), wad(3, 3))

	told(c, over("files"), nil, Notice{Extent: atRecord(3), Change: Removed})

	if len(c.runs) != 0 || len(c.sets) != 0 || len(c.at) != 0 {
		t.Errorf("it left %d places in %d runs over %d sequences",
			len(c.at), len(c.runs), len(c.sets))
	}
	sound(t, c)
}

// --- a record that may have moved -----------------------------------------

// A replacement takes its run with it. It is still in the sequence and may be
// anywhere in it, and a run cut around it would leave a boundary anchored to a
// record that is no longer where it says.
func TestAReplacedRecordTakesItsRunWithIt(t *testing.T) {
	c := roomy()
	filed(c, over("files"), NewInt(0), wad(1, 5))

	told(c, over("files"), nil, Notice{Extent: atRecord(3), Change: Replaced})

	if len(c.runs) != 0 || len(c.at) != 0 {
		t.Errorf("it left %d places in %d runs", len(c.at), len(c.runs))
	}
	if known(c, "files", 3) != nil {
		t.Error("a replaced record is still known by what it used to hold")
	}
	// The rest of the run kept what it holds: only the ORDER was in doubt.
	if known(c, "files", 2) == nil {
		t.Error("a neighbour's values went with the run")
	}
	sound(t, c)
}

// --- a field that may have changed ----------------------------------------

// The same notice is a lost run in the sequence the field decides and nothing
// at all in the sequence it only decorates -- which is the two caches earning
// their keep. The VALUES are forgotten once, for the source, however many
// sequences read them.
func TestAnAlteredFieldCostsTheOrderOnlyWhereItDecidesIt(t *testing.T) {
	c := roomy()
	byName, bySize := dataSet{source: "files", set: "files\x00name"},
		dataSet{source: "files", set: "files\x00size"}
	filed(c, byName, nil, wad(1, 5))
	filed(c, bySize, nil, wad(1, 5))

	told(c, bySize, &DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}},
		Notice{Extent: atRecord(3), Change: Altered, Fields: []string{".size"}})

	if got := order(c, bySize); got != "" {
		t.Errorf("the sequence the field decides still holds %s", got)
	}
	if got := order(c, byName); got != "undefined(1,2,3,4,5)5" {
		t.Errorf("the sequence it only decorates holds %s", got)
	}
	// One copy of the values, so one forgetting: the record is known to the
	// source and not to either order.
	r := known(c, "files", 3)
	if r == nil {
		t.Fatal("the record was dropped where only one field changed")
	}
	if r.fields.Has(".size") {
		t.Errorf("it still holds %s under a name that may have changed", r.fields)
	}
	if r.fields.Get(".name") == nil {
		t.Error("everything else about it was forgotten too")
	}
	sound(t, c)
}

// A field that decides nothing costs the order nothing at all.
func TestAnAlteredFieldThatDecidesNothingCostsOnlyTheValues(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, wad(1, 5))

	told(c, over("files"), byName(),
		Notice{Extent: atRecord(3), Change: Altered, Fields: []string{".modified"}})

	if got := order(c, over("files")); got != "undefined(1,2,3,4,5)5" {
		t.Errorf("a repaint cost the order: %s", got)
	}
	sound(t, c)
}

// Altered naming NO fields is a source saying it cannot tell where, and the
// safe reading of that is everywhere: the order may have moved and nothing the
// record holds is left to trust.
func TestAlteredNamingNoFieldIsEveryField(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, wad(1, 5))

	told(c, over("files"), byName(), Notice{Extent: atRecord(3), Change: Altered})

	if len(c.runs) != 0 {
		t.Errorf("it left %d runs", len(c.runs))
	}
	if known(c, "files", 3) != nil {
		t.Error("the record is still known")
	}
	sound(t, c)
}

// Unlearning leaves everything else as true as it was, which is what makes it
// worth telling apart from a replacement.
func TestUnlearningLeavesEverythingElseTrue(t *testing.T) {
	c := roomy()
	// Three named members, two of them sent, so nothing the counts settle is in
	// play -- see the case below for that.
	filed(c, over("files"), nil, slimsOf(Totals{Named: 3}, 1, 3, ".name", ".size"))

	told(c, over("files"), byName(),
		Notice{Extent: atRecord(2), Change: Altered, Fields: []string{".size"}})

	r := known(c, "files", 2)
	if r == nil {
		t.Fatal("the record was dropped")
	}
	if r.fields.Has(".size") || r.fields.Get(".name") == nil {
		t.Errorf("it holds %s", r.fields)
	}
	if r.knows.Named != 1 {
		t.Errorf("it says it knows %d of its members", r.knows.Named)
	}
	// So a question about the field that changed is a question again, and one
	// about the field that did not is still answered.
	if _, _, ok := asked(c, over("files"), Record{{Name: ".size"}}, &Scope{Count: 3}); ok {
		t.Error("it answered about the field that may have changed")
	}
	if _, _, ok := asked(c, over("files"), Record{{Name: ".name"}}, &Scope{Count: 3}); !ok {
		t.Error("it stopped answering about the field that did not")
	}
	sound(t, c)
}

// Both caches keep their books through a forgetting, the protected segment's
// included: a record that has proved itself costs less after it is unlearned,
// and a total left saying otherwise stops the cache evicting the right things
// long before anything looks like a wrong answer.
func TestUnlearningKeepsTheBooksInStep(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, slimsOf(Totals{Named: 3}, 1, 3, ".name", ".size"))

	// Read twice, so that what is held has proved itself and is protected.
	for i := 0; i < 2; i++ {
		if _, _, ok := asked(c, over("files"), Record{{Name: ".name"}},
			&Scope{Count: 3}); !ok {
			t.Fatal("the stretch it had just filed was not held")
		}
	}
	if c.flesh.warm == 0 {
		t.Fatal("nothing was protected after two readings of it")
	}
	was := c.flesh.warm

	told(c, over("files"), byName(),
		Notice{Extent: fromTo(1, 3), Change: Altered, Fields: []string{".size"}})

	if c.flesh.warm >= was {
		t.Errorf("what is protected cost %d and now costs %d, having lost a "+
			"field from every record of it", was, c.flesh.warm)
	}
	sound(t, c)
}

// A record answers about a name it does not carry wherever its own counts
// settle it. If that is the name that may have changed, the counts are what is
// wrong -- and a total is the source's statement, handed on to whoever asks, so
// it cannot be narrowed without being invented. The record goes instead.
func TestAlteringANameTheTotalsSettleDropsTheRecord(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, wad(1, 3)) // whole records: every member known

	told(c, over("files"), byName(),
		Notice{Extent: atRecord(2), Change: Altered, Fields: []string{".tag"}})

	if r := known(c, "files", 2); r != nil {
		t.Errorf("it still holds %s and says that is all of them", r.fields)
	}
	if known(c, "files", 1) == nil {
		t.Error("a record the notice did not name went with it")
	}
	// Its place is untouched: the values went and the order did not.
	if !placed(c, over("files"), 2) {
		t.Error("the record lost its place as well")
	}
	sound(t, c)
}

// --- a record that appeared -----------------------------------------------

// The run said everything between its ends was here, and something is now
// between them that is not, so the claim is cut there -- and nothing is lost
// but the promise across the cut.
func TestAnAdditionCutsTheClaimAcrossIt(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, wad(1, 5))

	told(c, over("files"), nil, Notice{Extent: atRecord(3), Change: Added})

	if len(c.runs) != 2 || len(c.at) != 5 {
		t.Fatalf("the cut left %d places in %d runs", len(c.at), len(c.runs))
	}
	if got := order(c, over("files")); got != "undefined(1,2,3)3 3(4,5)5" {
		t.Errorf("the two runs are %s", got)
	}
	// Either side still answers; across the cut nothing does, which is exactly
	// what has stopped being true.
	if got, _, ok := asked(c, over("files"), nil, &Scope{Count: 3}); !ok || got != "1,2,3" {
		t.Errorf("the first half gave %s, held %v", got, ok)
	}
	if _, _, ok := asked(c, over("files"), nil, &Scope{Count: 5}); ok {
		t.Error("a run still claimed across the place a record appeared")
	}
	sound(t, c)
}

// Appending past a watermark costs the reader no places at all: the run gives
// up the promise that the sequence stops there and keeps everything it holds.
// It is the log case, and it is meant to be cheap.
func TestAnAppendPastTheEndCostsNoPlaces(t *testing.T) {
	c := roomy()
	ended(c, over("files"), nil, wad(1, 5))

	if _, stop, ok := asked(c, over("files"), nil, &Scope{Count: 99}); !ok ||
		stop != StopExhausted {
		t.Fatalf("before the append it read to %s, held %v", stop, ok)
	}

	told(c, over("files"), nil, Notice{Extent: atRecord(5), Change: Added})

	if len(c.at) != 5 || len(c.runs) != 1 {
		t.Errorf("an append cost %d of five places", 5-len(c.at))
	}
	if got := order(c, over("files")); got != "undefined(1,2,3,4,5)5" {
		t.Errorf("the run is %s", got)
	}
	// What it gave up is the claim that there is nothing past it.
	if _, _, ok := asked(c, over("files"), nil, &Scope{Count: 99}); ok {
		t.Error("it still says the sequence ends where it stopped reading")
	}
	if got, stop, ok := asked(c, over("files"), nil, &Scope{Count: 5}); !ok ||
		got != "1,2,3,4,5" || stop != StopFilled {
		t.Errorf("what it still holds gave %s / %s, held %v", got, stop, ok)
	}
	sound(t, c)
}

// A record that landed before everything costs one place, and it is the
// beginning being EXCLUSIVE that costs it: the only record the run could name
// without lying is the one it has to give up in order to name it.
func TestAnAdditionBeforeEverythingCostsTheFirstPlace(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, wad(1, 5))

	told(c, over("files"), nil, Notice{Change: Added}) // fell before everything

	if got := order(c, over("files")); got != "1(2,3,4,5)5" {
		t.Errorf("the run is %s", got)
	}
	if placed(c, over("files"), 1) {
		t.Error("the record the run gave up to say where it starts is still placed")
	}
	// Its values stayed: it is the run's claim that was too wide, not what
	// anybody knows about that record.
	if known(c, "files", 1) == nil {
		t.Error("the values went with the place")
	}
	sound(t, c)
}

// An addition after a record this sequence does not place could have fallen
// anywhere, so every OPEN claim is pulled back and every closed one is left
// alone -- a run closed at both ends says what stands between two records it
// holds, and this fell after one it does not.
func TestAnAdditionAtAPlaceNobodyHoldsPullsBackTheOpenClaims(t *testing.T) {
	c := roomy()
	open, shut := dataSet{source: "files", set: "open"}, dataSet{source: "files", set: "shut"}
	ended(c, open, nil, wad(1, 5))
	filed(c, shut, NewInt(0), wad(1, 5))

	for _, ds := range []dataSet{open, shut} {
		told(c, ds, nil, Notice{Extent: atRecord(99), Change: Added})
	}

	if got := order(c, open); got != "1(2,3,4,5)5" {
		t.Errorf("the open run is %s", got)
	}
	if got := order(c, shut); got != "0(1,2,3,4,5)5" {
		t.Errorf("the closed run is %s, and nothing of it was at risk", got)
	}
	sound(t, c)
}

// --- a stretch, and one that cannot be walked ------------------------------

// A stretch held end to end is answered exactly: what falls between two
// identities is the sequence's own answer, and a run is that answer written
// down.
func TestAStretchTheCacheHoldsIsAnsweredExactly(t *testing.T) {
	c := roomy()
	filed(c, over("files"), NewInt(0), wad(1, 6))

	told(c, over("files"), nil, Notice{Extent: fromTo(2, 4), Change: Removed})

	if got := order(c, over("files")); got != "0(1,5,6)6" {
		t.Errorf("the run is %s", got)
	}
	for _, id := range []int64{2, 3, 4} {
		if known(c, "files", id) != nil {
			t.Errorf("record %d in the stretch is still known", id)
		}
	}
	for _, id := range []int64{1, 5, 6} {
		if known(c, "files", id) == nil {
			t.Errorf("record %d outside the stretch was forgotten", id)
		}
	}
	sound(t, c)
}

// A stretch this cache cannot walk end to end names records it cannot name, and
// answering for the ones it can would leave the rest held and wrong. So it is
// answered bluntly: the sequence's order goes, and every record held of that
// source with it.
func TestAStretchThatCannotBeWalkedIsAnsweredBluntly(t *testing.T) {
	c := roomy()
	// Three stretches apart, so that forgetting them is a walk of a list that
	// forgetting rewrites -- and the third one is the one that goes missing
	// where that list is the cache's own rather than a copy of it.
	filed(c, over("files"), nil, wad(1, 3))
	filed(c, over("files"), NewInt(9), wad(10, 12))
	filed(c, over("files"), NewInt(19), wad(20, 22))
	filed(c, over("colours"), nil, wad(1, 3))

	// From a record it holds, to one on the far side of a gap.
	told(c, over("files"), nil, Notice{Extent: fromTo(2, 11), Change: Removed})

	if got := order(c, over("files")); got != "" {
		t.Errorf("the sequence still holds %s", got)
	}
	for _, id := range []int64{1, 2, 3, 10, 11, 12, 20, 21, 22} {
		if known(c, "files", id) != nil {
			t.Errorf("record %d of that source is still known", id)
		}
	}
	// And nothing of anybody else's, which is what the source key is for.
	if known(c, "colours", 2) == nil || order(c, over("colours")) == "" {
		t.Error("another source's records were forgotten too")
	}
	sound(t, c)
}

// A stretch starting at a record this sequence does not place cannot be walked
// either, and is the same blunt answer.
func TestAStretchFromAPlaceNobodyHoldsIsAnsweredBluntly(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, wad(1, 5))

	told(c, over("files"), nil, Notice{Extent: fromTo(99, 3), Change: Replaced})

	if got := order(c, over("files")); got != "" {
		t.Errorf("the sequence still holds %s", got)
	}
	if len(c.flesh.at) != 0 {
		t.Errorf("%d records are still known", len(c.flesh.at))
	}
	sound(t, c)
}

// A notice with no extent at all is the widest thing a source can say -- the
// whole sequence -- and costs the most.
func TestANoticeWithNoExtentIsTheWholeSequence(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, wad(1, 5))

	told(c, over("files"), nil, Notice{Change: Replaced})

	if got := order(c, over("files")); got != "" {
		t.Errorf("the sequence still holds %s", got)
	}
	if len(c.flesh.at) != 0 {
		t.Errorf("%d records are still known", len(c.flesh.at))
	}
	sound(t, c)
}

// A notice about a record this sequence does not place costs it nothing. It is
// one identity, which names itself whether or not anything holds it -- so there
// is nothing here to be wrong about, and nothing to be blunt about either.
func TestANoticeAboutARecordNobodyPlacesCostsNothing(t *testing.T) {
	for _, e := range []Extent{
		{First: NewInt(9)},                  // one record, said the short way
		{First: NewInt(9), Last: NewInt(9)}, // and the long way
	} {
		c := roomy()
		filed(c, over("files"), nil, wad(1, 5))

		told(c, over("files"), nil, Notice{Extent: e, Change: Replaced})

		if got := order(c, over("files")); got != "undefined(1,2,3,4,5)5" {
			t.Errorf("a notice about a record it does not place left %s", got)
		}
		sound(t, c)
	}
}

// Added names no record at all: its extent is where the record fell, and not
// what it is. Nothing here holds the record that appeared -- that is what makes
// it an addition -- so there is nothing of it to forget.
func TestAnAdditionNamesNoRecord(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, wad(1, 5))

	if got, exact := c.named(over("files"), Notice{Extent: atRecord(3), Change: Added}); got != nil || !exact {
		t.Errorf("an addition named %d records, exactly: %v", len(got), exact)
	}
	if known(c, "files", 3) == nil {
		t.Error("the record the addition fell after was forgotten")
	}
}

// --- the wrapper ----------------------------------------------------------

// A notice with no descriptor is about RECORDS and not about anyone's order, which is
// what a record known to the source but placed in no live sequence needs: there
// is no stretch to name it in.
func TestANoticeWithNoSequenceCostsTheValuesOnly(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, twoWays)
	draw(t, src, byName(), &Scope{Count: 4})

	src.Stale(nil, Notice{Extent: atRecord(1), Change: Replaced})

	if got := covered(t, src, byName()); got != "0..3" {
		t.Errorf("a notice about records cost the order: %s", got)
	}
	if known(c, src.key, 1) != nil {
		t.Error("the record is still known")
	}
	sound(t, c)
}

// A STRETCH with no sequence to read it against is an extent nobody can walk --
// what falls between two identities is an order's answer and there is no order
// -- so it is the blunt one: every record of that source goes, and no order
// does.
func TestAStretchWithNoSequenceForgetsEveryRecordOfTheSource(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, twoWays)
	draw(t, src, byName(), &Scope{Count: 4})

	src.Stale(nil, Notice{Extent: fromTo(0, 2), Change: Replaced})

	if got := covered(t, src, byName()); got != "0..3" {
		t.Errorf("a notice with no sequence cost the order: %s", got)
	}
	if len(c.flesh.at) != 0 {
		t.Errorf("%d records of that source are still known", len(c.flesh.at))
	}
	sound(t, c)
}

// Through the wrapper, over two sequences of one source: the order is forgotten
// per sequence and the values are forgotten once, so the sequence the field
// decorates keeps every place it had and still has to ask again for what those
// places hold.
func TestTheWrapperForgetsTheOrderPerSequenceAndTheValuesOnce(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, twoWays)
	draw(t, src, byName(), &Scope{Count: 4})
	draw(t, src, bySize(), &Scope{Count: 4})
	was := n.reads

	src.Stale(bySize(), Notice{
		Extent: atRecord(0), Change: Altered, Fields: []string{".size"},
	})

	if got := covered(t, src, bySize()); got != "" {
		t.Errorf("the sequence .size decides still covers %s", got)
	}
	if got := covered(t, src, byName()); got != "0..3" {
		t.Errorf("the sequence it only decorates covers %s", got)
	}
	// The order is still here and what stood in it is not, so reading it is a
	// question about those records rather than about where they stand.
	if out, _ := draw(t, src, byName(), &Scope{Count: 4}); out.joined() != "0,1,2,3" {
		t.Errorf("reading it again gave %s", out.joined())
	}
	if n.reads == was {
		t.Error("it answered about a field it had been told may have changed")
	}
	sound(t, c)
}

// The reason is part of what a notice says, and it reads as itself.
func TestAChangeSaysWhatItIs(t *testing.T) {
	for _, c := range []struct {
		Change
		want string
	}{{Added, "added"}, {Removed, "removed"}, {Replaced, "replaced"},
		{Altered, "altered"}, {Change(9), "?"}} {
		if got := c.String(); got != c.want {
			t.Errorf("%d reads as %s", int(c.Change), got)
		}
	}
}
