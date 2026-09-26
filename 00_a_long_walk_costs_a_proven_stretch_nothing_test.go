package serval

// What a reader walking a long way costs everything else.
//
// A reader that wants row nine hundred of a source which cannot jump to a position
// has to walk there, a stretch at a time, past eight hundred and eighty records it
// will never look at. Those records cross the wire, are parsed, and are filed -- and
// the question this answers is whether they cost anything they should not.
//
// **They should not, and the cache is built so that they do not.** Every place
// arrives on probation; eviction takes from probation entirely rather than
// preferentially; and one run's probationary part is capped, so a run pouring in eats
// its own far end rather than the rest of the cache. `0_cache_test.go` pins each of
// those on its own, with runs made by hand.
//
// What is here is the COMPOSITION, over a real wrapper and a real walk: that the
// questions a walking reader actually asks produce the growing run those rules
// govern, rather than a heap of disjoint ones each filed whole. It is the difference
// between a defence that exists and a defence that applies.

import (
	"fmt"
	"strings"
	"testing"
)

// manyRecords is a PSL body of n records, keyed by position.
func manyRecords(n int) string {
	var b strings.Builder
	b.WriteString("(\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  (key: %d, name: \"row%05d\"),\n", i, i)
	}
	b.WriteString(")")
	return b.String()
}

// walked reads a sequence the way a view reaching a far position does: a stretch at a
// time, carrying on past the last record it was handed. Returns how far it got.
func walked(t *testing.T, set DataSet, to, step int) int {
	t.Helper()
	var last *Value
	at := 0
	for at < to {
		sc := &Scope{Count: step}
		if last != nil {
			sc.After = last
		}
		var got collector
		if err := set.Read(sc, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.keys) == 0 {
			break
		}
		at += len(got.keys)
		last = NewInt(int64(at - 1))
	}
	return at
}

// **A long walk does not cost a reader a stretch that has proved itself.**
//
// The walked-past records are read once each and never looked at again, which is what
// probation is for; the proven stretch is in the protected segment, and a warm place is
// only ever touched once there is no probationary place left anywhere in the cache.
//
// **Proving it takes THREE reads, not two.** The doctrine above says a place handed
// out a second time is warm, and the promotion is on the second SERVE -- the first
// hand-out is the miss, which files the run and forwards the source's own answer
// without the cache serving anything. So: read once and the places exist with nothing
// recorded about them; twice and the first serve records a hand-out; three times and
// the second serve promotes. Measured, not assumed, and the figure is here because it
// is the difference between a defence that applies and one that does not.
func TestALongWalkDoesNotCostAProvenStretch(t *testing.T) {
	ownCache(t, 30, 4096)

	src, counted := cached(t, manyRecords(400))
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	for i := 0; i < 3; i++ {
		var got collector
		if err := set.Read(&Scope{Count: 5}, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.keys) != 5 {
			t.Fatalf("read %d of the proven stretch holds %d rows", i, len(got.keys))
		}
	}
	proven := counted.reads
	if proven != 1 {
		t.Fatalf("three reads of one stretch asked the source %d times", proven)
	}

	// Another reader walks a long way through the same sequence.
	if got := walked(t, set, 380, 20); got < 380 {
		t.Fatalf("the walk reached %d of 380", got)
	}
	if counted.reads <= proven {
		t.Fatalf("the walk asked the source nothing; it cannot have walked")
	}

	// And the proven stretch is still answered out of the cache.
	was := counted.reads
	var again collector
	if err := set.Read(&Scope{Count: 5}, &again); err != nil {
		t.Fatal(err)
	}
	if counted.reads != was {
		t.Errorf("the proven stretch was asked for again: %d reads became %d --"+
			" the walk pushed out what had proved itself", was, counted.reads)
	}
	if got := strings.Join(again.keys, ","); got != "0,1,2,3,4" {
		t.Errorf("the proven stretch now reads %q", got)
	}
	sound(t, hot)
}

// And a stretch read only TWICE does not survive it, which is the same fact said from
// the other side: two reads leave the places on probation, and probation is what a
// flood takes.
//
// It matters because of who reads twice. A view that has settled reads a stretch ONCE
// -- it holds the identities, so it stops asking -- so nothing a view holds can reach
// the protected segment at all. What saves it is that it does not need to: the spine
// keeps the identities and the items keep their text, so a settled view draws without
// the cache. What the cache buys is coming BACK to a stretch, and that is what a flood
// costs today.
func TestAStretchReadTwiceIsStillOnProbation(t *testing.T) {
	ownCache(t, 30, 4096)

	src, counted := cached(t, manyRecords(400))
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	for i := 0; i < 2; i++ {
		var got collector
		if err := set.Read(&Scope{Count: 5}, &got); err != nil {
			t.Fatal(err)
		}
	}
	if got := walked(t, set, 380, 20); got < 380 {
		t.Fatalf("the walk reached %d of 380", got)
	}

	was := counted.reads
	var again collector
	if err := set.Read(&Scope{Count: 5}, &again); err != nil {
		t.Fatal(err)
	}
	if counted.reads == was {
		t.Error("a stretch read twice survived the walk; if promotion now happens on" +
			" the first serve, the test above wants two reads rather than three")
	}
	sound(t, hot)
}

// **And the walk's own runs JOIN**, which is what puts them under the rule that
// bounds them.
//
// A walk asks to carry on past the last record it was handed, so each answer begins
// where the last one ended and the two are one run. That is what makes it the growing
// run `TestAGrowingRunLosesTheEndTheReaderHasLeft` governs -- it loses the end the
// reader has left and keeps the end it is heading towards. Disjoint runs would each be
// filed whole and none of them trimmed, and the cap would never bite.
func TestAWalksRunsJoinRatherThanPilingUp(t *testing.T) {
	ownCache(t, 1<<20, 1<<20) // roomy, so nothing is trimmed and the shape is the point

	src, _ := cached(t, manyRecords(200))
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	if got := walked(t, set, 100, 10); got < 100 {
		t.Fatalf("the walk reached %d of 100", got)
	}

	hot.mu.Lock()
	runs := 0
	for _, byset := range hot.sets {
		runs += len(byset)
	}
	hot.mu.Unlock()
	if runs != 1 {
		t.Errorf("ten questions left %d runs, want one -- a walk carries on from what"+
			" it was handed, so each answer joins the last", runs)
	}
	sound(t, hot)
}

// A walk through a cache too small to hold it stays bounded: the run eats its own far
// end rather than the rest of the cache, so what is held never passes the share.
func TestALongWalkStaysWithinItsShare(t *testing.T) {
	c := ownCache(t, 30, 4096)

	src, _ := cached(t, manyRecords(400))
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	if got := walked(t, set, 380, 20); got < 380 {
		t.Fatalf("the walk reached %d of 380", got)
	}

	c.mu.Lock()
	cost, limit := c.cost, c.limit
	c.mu.Unlock()
	if cost > limit {
		t.Errorf("after walking 380 records the cache holds %d against a limit of %d",
			cost, limit)
	}
	sound(t, c)
}
