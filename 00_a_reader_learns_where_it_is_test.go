package serval

// Where an answer sits in its sequence, and asking for a place rather than a
// record.
//
// A reader with a scroll thumb knows two things and needs a third: how long the
// sequence is (Total), how many rows it shows, and WHERE IT IS. The third is
// Complete.First, and it is the calibration the whole scheme runs on -- ask,
// read where you landed, ask again from what you learned.

import (
	"fmt"
	"testing"
)

// rows numbers n records, so a position and a key are the same figure and a
// test can say which record it expected by saying where.
func numbered(n int) *ListSource {
	out := make([]Row, n)
	for i := range out {
		out[i] = NewRow(NewInt(int64(i)), Record{Named("n", fmt.Sprint(i))})
	}
	return NewListSource(out)
}

// took is the keys an answer carried, and what ended it.
type took struct {
	keys  []int64
	ids   []*Value
	done  Complete
	order bool
}

func (t *took) Ordered() { t.order = true }
func (t *took) Record(id *Value, f Record) error {
	t.keys = append(t.keys, id.Int)
	t.ids = append(t.ids, id)
	return nil
}
func (t *took) Subset(id *Value, f Record, _ Totals) error {
	t.keys = append(t.keys, id.Int)
	t.ids = append(t.ids, id)
	return nil
}
func (t *took) Done(c Complete) { t.done = c }

func placeRead(t *testing.T, set DataSet, s *Scope) *took {
	t.Helper()
	got := &took{}
	if err := set.Read(s, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	return got
}

func placeSet(t *testing.T, src Source) DataSet {
	t.Helper()
	set, err := src.Open(&DataSetDescriptor{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return set
}

// An answer says where it began, whether the reader named a record or nothing
// at all. The reader that asked After a record it holds learns where that
// record stands without asking a question of its own.
func TestAnAnswerSaysWhereItBegan(t *testing.T) {
	set := placeSet(t, numbered(100))
	defer set.Close()

	got := placeRead(t, set, &Scope{Count: 5})
	if !got.done.First.Exact || got.done.First.N != 0 {
		t.Errorf("a scope from the start began at %v, want exactly 0", got.done.First)
	}

	// After record 41, so the answer begins at 42 -- which the reader now knows
	// without having counted anything.
	got = placeRead(t, set, &Scope{After: NewInt(41), Count: 5})
	if !got.done.First.Exact || got.done.First.N != 42 {
		t.Errorf("after 41 began at %v, want exactly 42", got.done.First)
	}
	if got.keys[0] != 42 {
		t.Errorf("and it carried %d first", got.keys[0])
	}
}

// A scope that carried no records began nowhere, and says so rather than
// naming the place it was aiming at. Unknown is an answer.
func TestAnAnswerWithNoRecordsSaysNothingAboutWhereItBegan(t *testing.T) {
	set := placeSet(t, numbered(100))
	defer set.Close()

	// A count of nothing, from a place that certainly exists.
	got := placeRead(t, set, &Scope{From: 50, Count: 0})
	if len(got.keys) != 0 {
		t.Fatalf("it sent %d records for a count of none", len(got.keys))
	}
	if got.done.First.Exact || got.done.First.N != 0 {
		t.Errorf("an empty answer claimed to begin at %v", got.done.First)
	}

	// And walking off the end sends nothing, from nowhere.
	got = placeRead(t, set, &Scope{After: NewInt(99), Count: 5})
	if len(got.keys) != 0 {
		t.Fatalf("past the last record it sent %v", got.keys)
	}
	if got.done.First.Exact {
		t.Errorf("an answer past the end claimed to begin at %v", got.done.First)
	}
}

// A source holding its records lands on the position asked for exactly. This
// is the thumb dragged to the middle: no identity is known there, and one
// question puts the reader within a row of where it pointed.
func TestAPositionIsHonouredExactlyWhereTheRecordsAreHere(t *testing.T) {
	set := placeSet(t, numbered(1000))
	defer set.Close()

	for _, at := range []int{1, 499, 500, 998, 999} {
		got := placeRead(t, set, &Scope{From: at, Count: 3})
		if len(got.keys) == 0 {
			t.Fatalf("from %d carried nothing", at)
		}
		if got.keys[0] != int64(at) {
			t.Errorf("from %d began with record %d", at, got.keys[0])
		}
		if !got.done.First.Exact || got.done.First.N != at {
			t.Errorf("from %d reported First %v", at, got.done.First)
		}
	}
}

// Total and First together are a scroll thumb: how long, and where.
func TestTotalAndFirstAreAThumb(t *testing.T) {
	set := placeSet(t, numbered(1000))
	defer set.Close()

	got := placeRead(t, set, &Scope{From: 600, Count: 20})
	total := CountOf(set)
	if !total.Exact || total.N != 1000 {
		t.Fatalf("Total is %v", total)
	}
	if !got.done.First.Exact || got.done.First.N != 600 {
		t.Fatalf("First is %v", got.done.First)
	}
	// Which is all a thumb needs: 20 rows of 1000, starting 60% down.
	if frac := float64(got.done.First.N) / float64(total.N); frac != 0.6 {
		t.Errorf("the thumb sits at %v, want 0.6", frac)
	}
}

// A position past either end is clamped rather than refused. A thumb is
// dragged, and a reader that overshoots by a row wants the last row rather
// than an error.
func TestAPositionPastTheEndsIsClamped(t *testing.T) {
	set := placeSet(t, numbered(10))
	defer set.Close()

	got := placeRead(t, set, &Scope{From: 999, Count: 3})
	if len(got.keys) != 1 || got.keys[0] != 9 {
		t.Errorf("from past the end carried %v, want just the last record", got.keys)
	}
	if !got.done.First.Exact || got.done.First.N != 9 {
		t.Errorf("it reported First %v, want exactly 9", got.done.First)
	}

	got = placeRead(t, set, &Scope{From: -5, Count: 2})
	if len(got.keys) != 2 || got.keys[0] != 0 {
		t.Errorf("from before the start carried %v", got.keys)
	}
	if !got.done.First.Exact || got.done.First.N != 0 {
		t.Errorf("it reported First %v, want exactly 0", got.done.First)
	}
}

// A position is a place in the SEQUENCE, which is the order Total and First are
// counted in -- not a place in the walk. So a reversed scope from a position
// starts at that record and walks BACK from it, rather than that far in from
// the end.
func TestAPositionIsInTheSequenceAndNotInTheWalk(t *testing.T) {
	set := placeSet(t, numbered(100))
	defer set.Close()

	got := placeRead(t, set, &Scope{From: 50, Count: 3, Reversed: true})
	want := []int64{50, 49, 48}
	if fmt.Sprint(got.keys) != fmt.Sprint(want) {
		t.Errorf("reversed from 50 carried %v, want %v", got.keys, want)
	}
	if !got.done.First.Exact || got.done.First.N != 50 {
		t.Errorf("it reported First %v, want exactly 50", got.done.First)
	}
}

// A record is not a position, so a scope naming both is refused rather than
// quietly answered from one of them. A reader holding the record it wants to
// carry on past knows something better than a place, and one that meant both
// has a bug this is the only chance to show it.
func TestAScopeNamingBothARecordAndAPlaceIsRefused(t *testing.T) {
	set := placeSet(t, numbered(100))
	defer set.Close()

	got := placeRead(t, set, &Scope{After: NewInt(10), From: 50, Count: 3})
	if got.done.Error == "" {
		t.Fatalf("it answered with %v", got.keys)
	}
	if len(got.keys) != 0 {
		t.Errorf("it refused and sent %v anyway", got.keys)
	}

	// After alone still works, and From's zero value asks for nothing.
	got = placeRead(t, set, &Scope{After: NewInt(10), Count: 1})
	if got.done.Error != "" || len(got.keys) != 1 || got.keys[0] != 11 {
		t.Errorf("after alone: %v, %q", got.keys, got.done.Error)
	}
}

// The loop the whole design rests on: ask near a place, read where you landed,
// ask again from what you learned. Here with a source that lands exactly, so
// one round is enough -- what is being checked is that the reader needs to know
// nothing about the data to run it.
func TestAReaderConvergesOnAPlaceItAsksFor(t *testing.T) {
	set := placeSet(t, numbered(10000))
	defer set.Close()

	want := 7321
	at, rounds := 0, 0
	for {
		rounds++
		if rounds > 10 {
			t.Fatalf("it did not converge, and stalled at %d", at)
		}
		got := placeRead(t, set, &Scope{From: at, Count: 10})
		if !got.done.First.Exact {
			t.Fatal("this source should know exactly where it is")
		}
		landed := got.done.First.N
		if landed == want {
			break
		}
		// The correction, which is all the reader ever does with First.
		at += want - landed
	}
	if rounds != 2 {
		t.Errorf("it took %d rounds to reach a place this source lands on exactly", rounds)
	}
}

// A source that cannot place a position must not be TAKEN to have placed one.
//
// A composition does not hold its records and cannot look a position up, so it
// answers a From from the beginning -- and says Exactly(0), which is the truth
// and is not what was asked for. That difference is the whole safety property:
// a reader told 0 when it asked for 600 knows it did not get there, where a
// reader told nothing about a source that had quietly started at the top would
// paint the first rows of the sequence as though they were the six hundredth.
func TestASourceThatCannotSeekSaysWhereItActuallyStarted(t *testing.T) {
	left, err := NewComposedSource(
		Include{Name: "left", Source: numbered(50)},
		Include{Name: "right", Source: numbered(50)},
	)
	if err != nil {
		t.Fatal(err)
	}
	// SORTED, so the includes interleave by value and no arithmetic turns a
	// position in the whole into a position in each part.
	set, err := left.Open(&DataSetDescriptor{Sort: []SortLevel{{Field: "n"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	got := &took{}
	if err := set.Read(&Scope{From: 60, Count: 5}, got); err != nil {
		t.Fatal(err)
	}
	if got.done.First.Exact != true || got.done.First.N != 0 {
		t.Fatalf("it reported First %v, want exactly 0 -- the place it really began",
			got.done.First)
	}
}

// And that is what makes the reader's loop STOP rather than spin: ask a
// different place, get the same answer back, and the source has told you it
// cannot seek. Nothing was negotiated to find that out.
func TestTheLoopStopsAgainstASourceThatCannotSeek(t *testing.T) {
	c, err := NewComposedSource(
		Include{Name: "left", Source: numbered(500)},
		Include{Name: "right", Source: numbered(500)},
	)
	if err != nil {
		t.Fatal(err)
	}
	set, err := c.Open(&DataSetDescriptor{Sort: []SortLevel{{Field: "n"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	at, rounds, last := 700, 0, RecordCount{}
	for {
		rounds++
		if rounds > 4 {
			t.Fatal("the reader kept asking a source that never moves")
		}
		got := &took{}
		if err := set.Read(&Scope{From: at, Count: 10}, got); err != nil {
			t.Fatal(err)
		}
		if !got.done.First.Exact {
			break // it said nothing, which is also an end to asking
		}
		if rounds > 1 && got.done.First == last {
			break // it did not move, so it does not seek
		}
		last = got.done.First
		at += 700 - got.done.First.N
	}
	if rounds != 2 {
		t.Errorf("it took %d rounds to learn the source does not seek", rounds)
	}
}

// An amended source is the same: it wraps one child and cannot place a position
// either, so it says where it really began.
func TestAnAmendedSourceSaysWhereItActuallyStarted(t *testing.T) {
	a := NewAmendedSource(numbered(100))
	// An ADDITION, which moves every position after it, so a position cannot be
	// handed down and this source starts where it would have started anyway.
	a.Add(NewText("zzz"), Record{Named("n", "mine")})
	set, err := a.Open(&DataSetDescriptor{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	got := &took{}
	if err := set.Read(&Scope{From: 40, Count: 5}, got); err != nil {
		t.Fatal(err)
	}
	if !got.done.First.Exact || got.done.First.N != 0 {
		t.Errorf("it reported First %v, want exactly 0", got.done.First)
	}
	// And it refuses a scope naming both, the same as everyone else.
	got = &took{}
	if err := set.Read(&Scope{After: NewInt(5), From: 40, Count: 5}, got); err != nil {
		t.Fatal(err)
	}
	if got.done.Error == "" {
		t.Error("it took both a record and a place")
	}
}

// The two answers a wrapping source must NOT give, both of which would be read
// as "you are at the top of the sequence".
func TestAWrapperDoesNotClaimAPlaceItDoesNotKnow(t *testing.T) {
	c, err := NewComposedSource(
		Include{Name: "left", Source: numbered(50)},
		Include{Name: "right", Source: numbered(50)},
	)
	if err != nil {
		t.Fatal(err)
	}
	set, err := c.Open(&DataSetDescriptor{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	// An answer that carried nothing began nowhere. Position zero is a place,
	// and a scope that sent no records did not start at it.
	empty := &took{}
	if err := set.Read(&Scope{Count: 0}, empty); err != nil {
		t.Fatal(err)
	}
	if len(empty.keys) != 0 {
		t.Fatalf("a count of none carried %v", empty.keys)
	}
	if empty.done.First.Exact {
		t.Errorf("an empty answer claimed to begin at %v", empty.done.First)
	}

	// And a resume from a record this source never ranked is nowhere it can
	// name. It has a note of where its includes stood -- enough to carry on --
	// and no idea how many records came before.
	stray := &took{}
	if err := set.Read(&Scope{Count: 3}, stray); err != nil {
		t.Fatal(err)
	}
	c.Stale(stray.done.Watermark) // which drops the rank and keeps the note
	after := &took{}
	if err := set.Read(&Scope{After: stray.done.Watermark, Count: 3}, after); err != nil {
		t.Fatal(err)
	}
	if after.done.First.Exact {
		t.Errorf("a resume from an unranked record claimed to begin at %v",
			after.done.First)
	}
}
