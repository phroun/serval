package serval

// A wrapping source cannot look a position up -- it does not hold the records.
// What it CAN do is count as it hands them on, and remember. So a reader gets
// an exact position for anything near where it already is, which is exactly the
// case a scroll thumb cares about: the top visible row is always a row that was
// just handed over.

import "testing"

func composedPair(t *testing.T, each int) (*ComposedSource, DataSet) {
	t.Helper()
	c, err := NewComposedSource(
		Include{Name: "left", Source: numbered(each)},
		Include{Name: "right", Source: numbered(each)},
	)
	if err != nil {
		t.Fatal(err)
	}
	set, err := c.Open(&Spec{})
	if err != nil {
		t.Fatal(err)
	}
	return c, set
}

// Scrolling forward, every answer knows where it began. The first is at the top
// and says so; each one after it resumes from a record the last one ranked.
func TestScrollingForwardCarriesThePositionAlong(t *testing.T) {
	_, set := composedPair(t, 100)
	defer set.Close()

	at := 0
	var from *Value
	for page := 0; page < 6; page++ {
		got := placeRead(t, set, &Scope{After: from, Count: 10})
		if len(got.keys) == 0 {
			t.Fatalf("page %d carried nothing: %q", page, got.done.Error)
		}
		if !got.done.First.Exact || got.done.First.N != at {
			t.Fatalf("page %d began at %v, want exactly %d", page, got.done.First, at)
		}
		at += len(got.keys)
		from = got.done.Watermark
	}
	if at != 60 {
		t.Errorf("six pages of ten reached %d", at)
	}
}

// Which is what a thumb needs: how long the sequence is, and where the top row
// of it sits, on a source that holds none of the records itself.
func TestAWrapperCanDrawAThumb(t *testing.T) {
	_, set := composedPair(t, 250)
	defer set.Close()

	var from *Value
	for page := 0; page < 10; page++ {
		got := placeRead(t, set, &Scope{After: from, Count: 20})
		from = got.done.Watermark
	}
	got := placeRead(t, set, &Scope{After: from, Count: 20})

	total := CountOf(set)
	if !total.Exact || total.N != 500 {
		t.Fatalf("Total is %v", total)
	}
	if !got.done.First.Exact || got.done.First.N != 200 {
		t.Fatalf("First is %v, want exactly 200", got.done.First)
	}
}

// Walking backwards the positions FALL, the walk being the only difference.
func TestWalkingBackwardsTheRanksDescend(t *testing.T) {
	_, set := composedPair(t, 50)
	defer set.Close()

	// Forward first, so there is a ranked record to turn round at.
	fwd := placeRead(t, set, &Scope{Count: 30})
	if !fwd.done.First.Exact || fwd.done.First.N != 0 {
		t.Fatalf("the forward walk began at %v", fwd.done.First)
	}
	back := placeRead(t, set, &Scope{After: fwd.done.Watermark, Count: 5, Reversed: true})
	if len(back.keys) == 0 {
		t.Fatalf("the backward walk carried nothing: %q", back.done.Error)
	}
	// The forward walk ended at position 29, so one back from it is 28.
	if !back.done.First.Exact || back.done.First.N != 28 {
		t.Errorf("walking back began at %v, want exactly 28", back.done.First)
	}
}

// The rule that makes ranks safe to keep at all.
//
// A tuple is a record's own values and stops being true only when that record
// moves. A rank counts everybody in front of it, so a record added or removed
// ANYWHERE earlier makes it wrong about a record that has not moved. There is
// no such thing as forgetting the ranks that were affected, so being told
// anything changed drops them ALL -- including of records the notice never
// named.
func TestBeingToldAnythingChangedDropsEveryRank(t *testing.T) {
	c, set := composedPair(t, 100)
	defer set.Close()

	first := placeRead(t, set, &Scope{Count: 10})
	second := placeRead(t, set, &Scope{After: first.done.Watermark, Count: 10})
	if !second.done.First.Exact || second.done.First.N != 10 {
		t.Fatalf("before any notice, the second page began at %v", second.done.First)
	}
	far := second.done.Watermark

	// A notice about a DIFFERENT record, at the very start of the sequence.
	c.Stale(composedKey("left", NewInt(0)))

	after := placeRead(t, set, &Scope{After: far, Count: 10})
	if len(after.keys) == 0 {
		t.Fatalf("it could not carry on at all: %q", after.done.Error)
	}
	if after.done.First.Exact {
		t.Errorf("a rank survived a notice about a record before it: %v",
			after.done.First)
	}
	// And the reader gets its bearings back by reading from somewhere it knows.
	again := placeRead(t, set, &Scope{Count: 10})
	if !again.done.First.Exact || again.done.First.N != 0 {
		t.Errorf("it could not find the top again: %v", again.done.First)
	}
}

// An amended source counts the same way, and every amendment drops the ranks
// for the same reason a notice does: adding or deleting a record moves
// everything after it.
func TestAnAmendedSourceRanksAndForgets(t *testing.T) {
	a := NewAmendedSource(numbered(100))
	set, err := a.Open(&Spec{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	first := placeRead(t, set, &Scope{Count: 10})
	second := placeRead(t, set, &Scope{After: first.done.Watermark, Count: 10})
	if !second.done.First.Exact || second.done.First.N != 10 {
		t.Fatalf("the second page began at %v, want exactly 10", second.done.First)
	}
	far := second.done.Watermark

	// One record added, far away and under a key of its own.
	a.Add(NewText("zzz"), Record{Named("n", "mine")})

	after := placeRead(t, set, &Scope{After: far, Count: 5})
	if after.done.First.Exact {
		t.Errorf("a rank survived a record being added: %v", after.done.First)
	}
}

// Ranks written during a BACKWARD walk descend, so a forward scope resuming
// from one of them picks up where it should. Nothing but the walk differs, and
// the arithmetic has to turn over with it.
func TestRanksWrittenWalkingBackwardsDescend(t *testing.T) {
	_, set := composedPair(t, 50)
	defer set.Close()

	fwd := placeRead(t, set, &Scope{Count: 30}) // positions 0..29
	back := placeRead(t, set, &Scope{After: fwd.done.Watermark, Count: 5, Reversed: true})
	if len(back.keys) != 5 {
		t.Fatalf("the backward walk carried %d records: %q", len(back.keys), back.done.Error)
	}
	// It walked 28, 27, 26, 25, 24 and ranked each one as it went, so carrying
	// on forward from the last of them begins at 25.
	//
	// From the record itself rather than from the watermark: a composition's
	// watermark is the lowest of its includes' tails, which need not be a record
	// this scope handed on at all -- and one never handed on was never ranked.
	on := placeRead(t, set, &Scope{After: back.ids[4], Count: 3})
	if !on.done.First.Exact || on.done.First.N != 25 {
		t.Errorf("carrying on from a backward walk began at %v, want exactly 25",
			on.done.First)
	}
}

// A rank is only ever written from a start that was KNOWN. One reckoned from a
// start that was itself a guess would be a guess wearing an exact answer's
// clothes -- and would then be handed to the next scope as though it were a
// fact.
func TestNoRankIsWrittenFromAStartThatWasNotKnown(t *testing.T) {
	c, set := composedPair(t, 100)
	defer set.Close()

	first := placeRead(t, set, &Scope{Count: 10})
	lost := first.done.Watermark
	c.Stale(composedKey("left", NewInt(0))) // every rank goes

	// This answer cannot know where it begins, so nothing in it may be ranked.
	blind := placeRead(t, set, &Scope{After: lost, Count: 10})
	if blind.done.First.Exact {
		t.Fatalf("it claimed to know where it began: %v", blind.done.First)
	}
	// And so the scope after it cannot know either. If the blind one had ranked
	// its records from nowhere, this would report a confident, invented figure.
	on := placeRead(t, set, &Scope{After: blind.done.Watermark, Count: 10})
	if on.done.First.Exact {
		t.Errorf("a rank was invented from an unknown start: %v", on.done.First)
	}
}

// Every amendment moves the records after it, so every one of them drops the
// ranks -- not only the one that happens to have a test of its own.
func TestEveryAmendmentDropsTheRanks(t *testing.T) {
	for _, c := range []struct {
		what string
		do   func(*AmendedSource)
	}{
		{"Replace", func(a *AmendedSource) { a.Replace(NewInt(4), Record{Named("n", "x")}) }},
		{"Add", func(a *AmendedSource) { a.Add(NewText("zzz"), Record{Named("n", "x")}) }},
		{"Delete", func(a *AmendedSource) { a.Delete(NewInt(4), nil) }},
		{"Forget", func(a *AmendedSource) { a.Forget(NewInt(4)) }},
		{"Stale", func(a *AmendedSource) { a.Stale(NewInt(4)) }},
	} {
		a := NewAmendedSource(numbered(100))
		a.Replace(NewInt(7), Record{Named("n", "first")}) // so Stale has something to clear
		set, err := a.Open(&Spec{})
		if err != nil {
			t.Fatal(err)
		}

		first := placeRead(t, set, &Scope{Count: 10})
		second := placeRead(t, set, &Scope{After: first.done.Watermark, Count: 10})
		if !second.done.First.Exact {
			set.Close()
			t.Fatalf("%s: it did not know where it was to begin with", c.what)
		}
		far := second.done.Watermark

		c.do(a)

		after := placeRead(t, set, &Scope{After: far, Count: 5})
		if after.done.First.Exact {
			t.Errorf("%s: a rank survived it (%v)", c.what, after.done.First)
		}
		set.Close()
	}
}

// A backward walk from the sequence's FIRST record has nowhere to go: nothing
// stands before position zero. It carries nothing and claims nothing, rather
// than reporting a position one short of the start.
func TestWalkingBackFromTheFirstRecordGoesNowhere(t *testing.T) {
	_, set := composedPair(t, 20)
	defer set.Close()

	one := placeRead(t, set, &Scope{Count: 1}) // the record at position 0
	if !one.done.First.Exact || one.done.First.N != 0 {
		t.Fatalf("the first record is at %v", one.done.First)
	}
	back := placeRead(t, set, &Scope{After: one.ids[0], Count: 5, Reversed: true})
	if len(back.keys) != 0 {
		t.Errorf("it walked back off the start and carried %d records", len(back.keys))
	}
	if back.done.First.Exact {
		t.Errorf("it claimed to begin at %v, and there is nothing there",
			back.done.First)
	}
}

// A position can be handed down to a child where this source holds nothing of
// its own: the sequence IS the child's, so `from` means the same thing to both
// and where it began is the child's to say.
//
// That is the case a list reading its own items is in, and it is the reason this
// is worth having.
func TestAPositionGoesDownToAnUnamendedChild(t *testing.T) {
	a := NewAmendedSource(numbered(500))
	set, err := a.Open(&Spec{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	got := placeRead(t, set, &Scope{From: 300, Count: 5})
	if len(got.keys) == 0 {
		t.Fatalf("it carried nothing: %q", got.done.Error)
	}
	if got.keys[0] != 300 {
		t.Errorf("it began with record %d, want 300", got.keys[0])
	}
	if !got.done.First.Exact || got.done.First.N != 300 {
		t.Errorf("it reported First %v, want exactly 300", got.done.First)
	}
}

// Anything held stops it, a replacement included -- and the reason is not that a
// replacement moves a record. It is that this source positions its own records
// against the record a scope resumed past, and a scope naming a PLACE names no
// record to position against: the replacement would go out ahead of the child's
// first record, and the answer would begin somewhere it said it did not.
func TestAnythingHeldStopsAPositionGoingDown(t *testing.T) {
	for _, c := range []struct {
		what string
		do   func(*AmendedSource)
	}{
		{"a replacement", func(a *AmendedSource) { a.Replace(NewInt(7), Record{Named("n", "x")}) }},
		{"an addition", func(a *AmendedSource) { a.Add(NewText("zzz"), Record{Named("n", "x")}) }},
		{"a deletion", func(a *AmendedSource) { a.Delete(NewInt(9), nil) }},
	} {
		a := NewAmendedSource(numbered(500))
		c.do(a)
		set, err := a.Open(&Spec{})
		if err != nil {
			t.Fatal(err)
		}

		got := placeRead(t, set, &Scope{From: 300, Count: 5})
		if len(got.keys) == 0 {
			set.Close()
			t.Fatalf("%s: it carried nothing: %q", c.what, got.done.Error)
		}
		if !got.done.First.Exact || got.done.First.N != 0 {
			t.Errorf("%s: it reported First %v, want exactly 0", c.what, got.done.First)
		}
		set.Close()
	}
}
