package serval

// Amending another source's records.

import (
	"strings"
	"testing"
)

// four records, by size: go.mod 96, build.sh 310, README.md 2048, parser.go 14022
func amendable(t *testing.T) *AmendedSource {
	t.Helper()
	return NewAmendedSource(mustPSL(t, twoWays))
}

func key(i int64) *Value { return NewInt(i) }

func fields(name string, size int64) Record {
	return Record{Named(".name", name), Named(".size", size)}
}

// A replacement goes out instead of the child's record, so the values are
// right wherever they are read.
func TestAReplacementStandsInForTheChildsRecord(t *testing.T) {
	a := amendable(t)
	a.Replace(key(2), fields("go.mod", 96))

	out, _ := read(t, a, bySize(), &Scope{Count: 4})
	if out.joined() != "2,1,0,3" {
		t.Fatalf("the sequence is %s", out.joined())
	}
	// What Replace stated, and only that: an amendment is the record entire,
	// so the bag is exactly what was handed in.
	if got := out.fields[0].String(); got != `{ .name "go.mod"; .size 96 }` {
		t.Errorf("the replacement carries %s", got)
	}
}

// And its own values place it, not the ones the child holds.
func TestAReplacementIsPlacedByItsOwnValues(t *testing.T) {
	a := amendable(t)
	a.Replace(key(2), fields("go.mod", 99999)) // was 96, the smallest

	out, _ := read(t, a, bySize(), &Scope{Count: 4})
	if out.joined() != "1,0,3,2" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

// A deletion takes the record out, and the scope is still as long as it was
// asked for: the child was asked for enough to cover what would be removed.
func TestADeletionIsCoveredBeforeTheChildIsAsked(t *testing.T) {
	a := amendable(t)
	a.Delete(key(1), fields("build.sh", 310))

	out, done := read(t, a, bySize(), &Scope{Count: 3})
	if out.joined() != "2,0,3" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if done.Stop != StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// A deletion this source knows nothing about is discovered rather than
// predicted: the scope comes up short, the child is asked again, and what
// the round taught means the next one over the same ground does not.
func TestADeletionWithNoPlacementIsLearned(t *testing.T) {
	a := amendable(t)
	a.Delete(key(1), nil) // nothing known about where it sat

	out, _ := read(t, a, bySize(), &Scope{Count: 3})
	if out.joined() != "2,0,3" {
		t.Fatalf("the sequence is %s", out.joined())
	}

	// It knows now.
	am := a.lookup(key(1))
	if am == nil || am.seen == nil {
		t.Fatalf("nothing was learned: %#v", am)
	}
	if got := am.seen.String(); got != `{ key 1; .name "build.sh"; .size 310 }` {
		t.Errorf("what it learned is %s", got)
	}
}

// A replacement the filter no longer admits is a record gone, exactly as a
// deletion is, and it is covered the same way.
func TestAReplacementFilteredOutIsARecordGone(t *testing.T) {
	a := amendable(t)
	a.Replace(key(1), fields("build.sh", 99999)) // the filter below wants under 3000

	out, _ := read(t, a, &DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}, Filter: and(lt(".size", 3000))}, &Scope{Count: 2})
	if out.joined() != "2,0" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

// A record this source holds that the child never sends goes out anyway. Here
// the child cannot send it, because by the child's values it is filtered out.
func TestARecordTheChildNeverSendsStillGoesOut(t *testing.T) {
	a := amendable(t)
	a.Replace(key(3), fields("parser.go", 100)) // was 14022, the filter excludes it

	out, _ := read(t, a, &DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}, Filter: and(lt(".size", 3000))}, &Scope{Count: 4})
	if out.joined() != "2,3,1,0" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if got := out.fields[1].String(); got != `{ .name "parser.go"; .size 100 }` {
		t.Errorf("the record this source supplied carries %s", got)
	}
}

// Amendments are not a query. They change whenever, and a scope is answered
// against what is held when it is asked.
func TestAmendmentsChangeBetweenScopes(t *testing.T) {
	a := amendable(t)
	set, err := a.Open(&DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	first := &collector{}
	if err := set.Read(&Scope{Count: 2}, first); err != nil {
		t.Fatal(err)
	}
	if first.joined() != "2,1" {
		t.Fatalf("the first scope is %s", first.joined())
	}

	a.Delete(key(1), fields("build.sh", 310))

	second := &collector{}
	if err := set.Read(&Scope{Count: 2}, second); err != nil {
		t.Fatal(err)
	}
	if second.joined() != "2,0" {
		t.Errorf("the second scope is %s", second.joined())
	}
}

// Forgetting an amendment leaves the child's own record to stand.
func TestForgettingAnAmendmentLetsTheChildStand(t *testing.T) {
	a := amendable(t)
	a.Replace(key(2), fields("go.mod", 99999))
	a.Forget(key(2))

	out, _ := read(t, a, bySize(), &Scope{Count: 1})
	if out.joined() != "2" {
		t.Errorf("the first record is %s", out.joined())
	}
	if got := out.fields[0].String(); got != `{ key 2; .name "go.mod"; .size 96 }` {
		t.Errorf("the child's record carries %s", got)
	}
}

// It wraps any kind, including an application's records -- and including
// another amended source.
func TestItWrapsAnyKind(t *testing.T) {
	for _, kind := range []struct {
		what  string
		child Source
	}{
		{"records that are here", mustPSL(t, twoWays)},
		{"records an application has", listing()},
		{"another amended source", NewAmendedSource(mustPSL(t, twoWays))},
	} {
		t.Run(kind.what, func(t *testing.T) {
			a := NewAmendedSource(kind.child)
			a.Replace(key(2), fields("go.mod", 99999))
			a.Delete(key(1), fields("build.sh", 310))

			out, _ := read(t, a, bySize(), &Scope{Count: 3})
			if out.joined() != "0,3,2" {
				t.Errorf("the sequence is %s", out.joined())
			}
		})
	}
}

// What it claims about order is what is true of what it sent, which is the
// child's claim: ours went out in the sequence's order either way. And the
// claim is passed on before the records, which is what lets whoever is reading
// act on them as they arrive.
func TestAnOrderedChildMakesAnOrderedAnswer(t *testing.T) {
	a := amendable(t)
	a.Replace(key(2), fields("go.mod", 96))

	out, _ := read(t, a, bySize(), &Scope{Count: 4})
	if !out.ordered {
		t.Error("an ordered child did not make an ordered answer")
	}
	if out.joined() != "2,1,0,3" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

func TestItClaimsOrderOnlyWhenTheChildDid(t *testing.T) {
	a := NewAmendedSource(&jumbled{inner: mustPSL(t, twoWays)})
	a.Replace(key(2), fields("go.mod", 96))

	out, _ := read(t, a, bySize(), &Scope{Count: 4})
	if out.ordered {
		t.Error("a jumble was claimed to be in order")
	}
	if len(out.keys) != 4 {
		t.Errorf("the records that came back are %v", out.keys)
	}
}

// jumbled is a source that answers correctly and says nothing about order,
// which every application is free to do.
type jumbled struct{ inner Source }

func (j *jumbled) Open(descriptor *DataSetDescriptor) (DataSet, error) {
	set, err := j.inner.Open(descriptor)
	if err != nil {
		return nil, err
	}
	return &jumbledSet{inner: set}, nil
}

type jumbledSet struct{ inner DataSet }

func (j *jumbledSet) Close() { j.inner.Close() }
func (j *jumbledSet) Read(f *Scope, out Sink) error {
	return j.inner.Read(f, &unordered{out: out})
}

type unordered struct{ out Sink }

func (u *unordered) Ordered()                        {} // said nothing, which is what a jumble says
func (u *unordered) Record(k *Value, f Record) error { return u.out.Record(k, f) }
func (u *unordered) Subset(k *Value, f Record, has Totals) error {
	return u.out.Subset(k, f, has)
}
func (u *unordered) Done(c Complete) { u.out.Done(c) }

type errBroken struct{}

func (errBroken) Error() string { return "the records are broken" }

// A brokenSource is a child that cannot answer at all: what a source whose
// records have become unreachable does.
type brokenSource struct{}

func (brokenSource) Open(*DataSetDescriptor) (DataSet, error) { return brokenSet{}, nil }

type brokenSet struct{}

func (brokenSet) Close() {}

func (brokenSet) Read(_ *Scope, out Sink) error {
	err := errBroken{}
	out.Done(Complete{Error: err.Error()})
	return err
}

// A child that refuses ends the scope here too, rather than leaving whoever
// asked waiting.
func TestAChildThatRefusesEndsTheScope(t *testing.T) {
	a := NewAmendedSource(brokenSource{})
	set, err := a.Open(&DataSetDescriptor{})
	if err != nil {
		t.Fatal(err)
	}
	out := &collector{}
	if err := set.Read(&Scope{Count: 2}, out); err == nil {
		t.Fatal("a broken child was not reported")
	}
	if !out.ended || !strings.Contains(out.done.Error, "broken") {
		t.Errorf("the scope ended as %#v", out.done)
	}
}

// A deletion whose placement is stale is discovered, not predicted.
//
// The scope is asked for from a boundary, and this source believes the
// deleted record sits before it -- so it asks the child for no extra. The
// child sends the record anyway, it is dropped, and the scope is one short.
// So the child is asked again from where it got to, and the scope is filled.
func TestAStaleDeletionCostsASecondQuestion(t *testing.T) {
	a := amendable(t)
	// It really sits at 2048, between build.sh and parser.go. This source
	// believes it sits at 1, before the whole scope.
	a.Delete(key(0), fields("README.md", 1))

	seq := opened(t, a, bySize())
	if head, _ := seq.scope(&Scope{Count: 2}); head.joined() != "2,1" {
		t.Fatalf("the first scope is %s", head.joined())
	}
	out, _ := seq.scope(&Scope{After: NewInt(1), Count: 2})
	if out.joined() != "3" {
		t.Errorf("the scope is %s", out.joined())
	}

	// And what the round taught means the next one does not come up short.
	am := a.lookup(key(0))
	if am == nil || am.seen == nil {
		t.Fatalf("nothing was learned: %#v", am)
	}
	if got := am.seen.String(); got != `{ key 0; .name "README.md"; .size 2048 }` {
		t.Errorf("what it learned is %s", got)
	}
}

// The child having nothing more is the end of the sequence, this source's own
// records having all gone out by then.
func TestTheEndOfTheSequenceIsTheChildsEnd(t *testing.T) {
	a := amendable(t)
	a.Replace(key(9), fields("added-by-nobody", 99999))

	out, done := read(t, a, bySize(), &Scope{Count: 99})
	if done.Stop != StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
	if out.keys[len(out.keys)-1] != "9" {
		t.Errorf("the last record is %v", out.keys)
	}
}

// A child that always answers short does not get asked forever.
//
// Each round teaches something, so a second is nearly always enough. A child
// that never catches up is a child that is wrong, and the cap is what keeps
// being wrong from becoming being stuck.
func TestAChildThatNeverCatchesUpIsNotAskedForever(t *testing.T) {
	child := &dribble{}
	a := NewAmendedSource(child)

	out, done := read(t, a, unsorted(), &Scope{Count: 100})
	if child.rounds < 2 {
		t.Errorf("it gave up after %d round(s)", child.rounds)
	}
	if child.rounds > 8 {
		t.Errorf("it asked %d times", child.rounds)
	}
	if done.Stop == StopExhausted {
		t.Error("a scope that never filled claimed to be exhausted")
	}
	if len(out.keys) != child.rounds {
		t.Errorf("%d records from %d rounds", len(out.keys), child.rounds)
	}
}

// dribble answers one record at a time and never says it has run out, which is
// a source that will never fill a scope however often it is asked.
type dribble struct{ rounds int }

func (d *dribble) Open(*DataSetDescriptor) (DataSet, error) { return d, nil }
func (d *dribble) Close()                                   {}

func (d *dribble) Read(f *Scope, out Sink) error {
	d.rounds++
	out.Ordered()
	k := NewInt(int64(d.rounds))
	_ = out.Record(k, Record{Named(".n", int64(d.rounds))})
	out.Done(Complete{Stop: StopFilled, Watermark: k})
	return nil
}

// Reversed reaches the amendments too: this source's own records are placed by
// the same levels the child's are, so the merge holds whichever way the
// sequence is read.
func TestAnAmendedSourceReverses(t *testing.T) {
	forward := amendable(t)
	forward.Replace(key(2), fields("go.mod", 96))
	up, _ := read(t, forward, bySize(), &Scope{Count: 4})
	if up.joined() != "2,1,0,3" {
		t.Fatalf("forward, the sequence is %s", up.joined())
	}

	mirror := amendable(t)
	mirror.Replace(key(2), fields("go.mod", 96))
	down, _ := read(t, mirror, bySize(), &Scope{Count: 4, Reversed: true})
	if down.joined() != "3,0,1,2" {
		t.Errorf("reversed, the sequence is %s", down.joined())
	}
	if !down.ordered {
		t.Error("a reversed sequence did not say it was in order")
	}
}

// --- records of this source's own ----------------------------------------

// An addition is a record this source holds, not a statement about one of the
// child's. It goes out in its sorted place among them.
func TestAnAdditionGoesOutInItsPlace(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("added.go", 200))

	out, _ := read(t, a, bySize(), &Scope{Count: 9})
	if out.joined() != "2,90,1,0,3" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

// The count is the count, whoever the records came from.
//
// Ours are not extra on top of the scope: a scope of three is three records,
// and what is left over is the start of the next one rather than surplus
// stapled to this one. Without that, a source holding a thousand additions
// would answer every scope with the thousand that sort ahead of it.
func TestAdditionsCountTowardsTheScope(t *testing.T) {
	a := amendable(t)
	for i := 90; i < 95; i++ {
		a.Add(key(int64(i)), fields("added", int64(i)))
	}

	out, done := read(t, a, bySize(), &Scope{Count: 3})
	if len(out.keys) != 3 {
		t.Errorf("a scope of three came back %d long: %s", len(out.keys), out.joined())
	}
	if done.Stop != StopFilled {
		t.Errorf("a scope that filled said %q", done.Stop)
	}
}

// A scope can end on a record of this source's own, and the next one carries
// on from it.
//
// The child has never heard of that identity, so it is not the one handed
// down: what goes down is the last identity the child itself gave before ours
// crossed, which is the same place in the merged sequence.
func TestAScopeResumesFromARecordOfOurOwn(t *testing.T) {
	a := amendable(t)
	for i := 90; i < 95; i++ {
		a.Add(key(int64(i)), fields("added", int64(i)))
	}
	seq := opened(t, a, bySize())

	first, done := seq.scope(&Scope{Count: 3})
	if first.joined() != "90,91,92" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	if done.Watermark == nil {
		t.Fatal("a scope that filled claimed nothing")
	}

	next, done := seq.scope(&Scope{After: done.Watermark, Count: 9})
	if next.joined() != "93,94,2,1,0,3" {
		t.Errorf("the rest is %s", next.joined())
	}
	if done.Stop != StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// Where an addition's key turns out to be the child's after all, the child's
// record is the one that stands -- the opposite way round from a replacement.
//
// Nothing is asked to find that out. It surfaces when the child's copy
// arrives, and this source writes it down: the scope it surfaced on keeps
// ours, because ours had already crossed and one identity twice is worse than
// either winning, and every scope after it has the child's.
func TestAClashingAdditionLosesToTheChild(t *testing.T) {
	a := amendable(t)
	// Key 1 is the child's build.sh at size 310. Ours sorts ahead of it.
	a.Add(key(1), fields("mine.go", 5))
	seq := opened(t, a, bySize())

	first, _ := seq.scope(&Scope{Count: 9})
	if got := first.fields[0].String(); got != `{ .name "mine.go"; .size 5 }` {
		t.Errorf("the scope it surfaced on carries %s first", got)
	}
	if n := strings.Count(first.joined(), "1"); n != 1 {
		t.Errorf("one identity crossed twice: %s", first.joined())
	}

	// And from here on the child's record is the one that stands.
	next, _ := seq.scope(&Scope{Count: 9})
	if next.joined() != "2,1,0,3" {
		t.Errorf("the scope after the clash is %s", next.joined())
	}
	if got := next.fields[1].String(); got != `{ key 1; .name "build.sh"; .size 310 }` {
		t.Errorf("the child's record came back as %s", got)
	}
}

// An addition does not make the child be asked for more, the way a deletion
// does: it fills a place in the scope that the child then need not.
func TestAnAdditionAsksTheChildForLess(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	a.Add(key(90), fields("added", 5))

	read(t, a, bySize(), &Scope{Count: 3})
	if s.asked.Count != 2 {
		t.Errorf("the child was asked for %d, want 2 of the 3", s.asked.Count)
	}
}

// The other way round: the child's copy arrives first, so the child's goes out
// and ours is taken out of what is still to come.
//
// Without that, ours would follow its own copy a moment later and the same
// identity would cross twice in one scope -- which is the thing the rule is
// there to stop, whichever of the two happens to sort first.
func TestAClashingAdditionIsDroppedWhenTheChildsCameFirst(t *testing.T) {
	a := amendable(t)
	// Key 1 is the child's build.sh at size 310. Ours sorts after it.
	a.Add(key(1), fields("mine.go", 9999))

	out, _ := read(t, a, bySize(), &Scope{Count: 9})
	if out.joined() != "2,1,0,3" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if got := out.fields[1].String(); got != `{ key 1; .name "build.sh"; .size 310 }` {
		t.Errorf("the child's record came back as %s", got)
	}
}

// A scope that filled with records of ours still to come has not reached the
// end, however exhausted the child is.
//
// The child having nothing more says nothing about this source: what is left
// over is ours, and it is the start of the next scope rather than nothing.
func TestOursLeftOverIsNotTheEndOfTheSequence(t *testing.T) {
	a := amendable(t)
	for i := 90; i < 95; i++ {
		a.Add(key(int64(i)), fields("added", int64(90000+i)))
	}
	seq := opened(t, a, bySize())

	out, done := seq.scope(&Scope{Count: 6})
	if len(out.keys) != 6 {
		t.Fatalf("a scope of six came back %d long: %s", len(out.keys), out.joined())
	}
	if done.Stop != StopFilled {
		t.Errorf("a scope with three of ours still to come said %q", done.Stop)
	}

	next, done := seq.scope(&Scope{After: done.Watermark, Count: 9})
	if next.joined() != "92,93,94" {
		t.Errorf("the rest is %s", next.joined())
	}
	if done.Stop != StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// A walk that reached the record the asker already held stops there, and this
// source's own records do not run past it.
//
// `until` says the asker holds that record and everything beyond it -- ours
// included, since ours crossed the same way when it got them. So nothing left
// of ours goes out, nothing is asked of the child again, and the claim stops
// where the walk did rather than at some record of ours past it.
func TestOursDoNotRunPastAJoinedWalk(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("added", 99999)) // past everything

	out, done := read(t, a, bySize(), &Scope{Until: NewInt(0), Count: 9})
	if out.joined() != "2,1" {
		t.Errorf("the walk to the record the asker held is %s", out.joined())
	}
	if done.Stop != StopJoined {
		t.Errorf("it stopped at that record and said %q", done.Stop)
	}
	if got := valueText(done.Watermark); got != "1" {
		t.Errorf("the claim reaches %s, which is past where the walk stopped", got)
	}
}

// A walk that joined is over, and nothing is asked again on the strength of it
// being shorter than the count.
//
// The count is not short -- the records past `until` are ones the asker
// already has -- so going back to the child for more would ask it to walk
// ground the asker told it not to, four times over before giving up.
func TestAJoinedWalkAsksTheChildNothingMore(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	a.Add(key(90), fields("added", 99999))

	read(t, a, bySize(), &Scope{Until: NewInt(0), Count: 9})
	if s.reads != 1 {
		t.Errorf("the child was asked %d times for a walk that had joined", s.reads)
	}
}

// Joined outranks filled where a walk did both.
//
// Reaching the record the asker already held says its two runs are now one,
// which is the fact worth having; that the count also happened to come out
// even says nothing.
func TestAWalkThatJoinedAndFilledSaysItJoined(t *testing.T) {
	a := amendable(t)
	out, done := read(t, a, bySize(), &Scope{Until: NewInt(0), Count: 2})
	if out.joined() != "2,1" {
		t.Fatalf("the walk is %s", out.joined())
	}
	if done.Stop != StopJoined {
		t.Errorf("a walk that reached the asker's own record said %q", done.Stop)
	}
}

// The child is never asked for a negative number of records.
//
// More additions than the scope is long means this source fills it alone and
// wants nothing from the child -- which is nought, not minus two. A source
// whose records are here shrugs that off; one across the wire would have the
// whole query refused, `count` being a number of records and there being no
// such thing as fewer than none of them.
func TestTheChildIsNeverAskedForFewerThanNone(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	for i := 90; i < 95; i++ {
		a.Add(key(int64(i)), fields("added", int64(i)))
	}

	read(t, a, bySize(), &Scope{Count: 3})
	if s.asked.Count < 0 {
		t.Errorf("the child was asked for %d records", s.asked.Count)
	}
}

// An addition the filter does not admit takes nothing out of the child's
// answer, so it buys the child no extra to send.
//
// Slack is for what this source REMOVES -- a deletion, or a replacement whose
// new values no longer match and so loses the child's record too. An addition
// that does not match was never in the sequence to begin with and removes
// nothing.
func TestAnAdditionTheFilterDropsIsNotSlack(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	a.Add(key(90), fields("added", 99999)) // outside the filter below

	read(t, a, &DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}, Filter: and(lt(".size", 1000))}, &Scope{Count: 2})
	if s.asked.Count != 2 {
		t.Errorf("the child was asked for %d, want the 2 the scope wanted", s.asked.Count)
	}
}

// --- the order the amendments are arranged in ----------------------------

// The arrangement is made once for a sequence and every scope of it is a search
// into the result, so two scopes of one sequence arrange nothing twice.
func TestTheAmendmentsAreArrangedOncePerSequence(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("added", 5))
	seq := opened(t, a, bySize())

	seq.scope(&Scope{Count: 2})
	built := a.order(&DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}})
	seq.scope(&Scope{Count: 2})
	again := a.order(&DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}})

	if built != again {
		t.Error("a second scope of one sequence arranged the amendments again")
	}
}

// Ordering can be a hint; membership cannot.
//
// An amendment made between two scopes is not in an arrangement built before
// it, and nothing about walking that arrangement would ever find it -- so what
// is held carries a generation, and a source amended since arranges it again.
func TestAnAmendmentMadeBetweenScopesIsSeen(t *testing.T) {
	a := amendable(t)
	seq := opened(t, a, bySize())

	if first, _ := seq.scope(&Scope{Count: 9}); first.joined() != "2,1,0,3" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	a.Add(key(90), fields("added", 5))

	next, _ := seq.scope(&Scope{Count: 9})
	if next.joined() != "90,2,1,0,3" {
		t.Errorf("the scope after the addition is %s", next.joined())
	}
}

// An addition taken back between two scopes stops being in it.
//
// Nothing but the arrangement knows an addition exists -- no record of the
// child's will arrive to prompt a second look at it -- so it goes out for as
// long as the arrangement says it is there. Nothing else in this test amends
// anything, because an amendment of any other kind would rearrange what is
// held and hide that.
func TestAnAdditionForgottenBetweenScopesIsGone(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("added", 5))
	seq := opened(t, a, bySize())

	if first, _ := seq.scope(&Scope{Count: 9}); first.joined() != "90,2,1,0,3" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	a.Forget(key(90))

	next, _ := seq.scope(&Scope{Count: 9})
	if next.joined() != "2,1,0,3" {
		t.Errorf("the scope after forgetting it is %s", next.joined())
	}
}

// A forgotten deletion comes right whether or not anything is rearranged: a
// record of the child's is looked up in what is held as it arrives, so the
// child's own copy stands the moment nothing is held against it.
func TestADeletionForgottenBetweenScopesIsGone(t *testing.T) {
	a := amendable(t)
	a.Delete(key(1), fields("build.sh", 310))
	seq := opened(t, a, bySize())

	if first, _ := seq.scope(&Scope{Count: 9}); first.joined() != "2,0,3" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	a.Forget(key(1))

	next, _ := seq.scope(&Scope{Count: 9})
	if next.joined() != "2,1,0,3" {
		t.Errorf("the scope after forgetting it is %s", next.joined())
	}
}

// A deletion whose placement was learned stops being counted against every
// scope and takes its place in the order, which is a change to what is held
// like any other.
func TestALearnedPlacementRearrangesWhatIsHeld(t *testing.T) {
	a := amendable(t)
	a.Delete(key(0), nil) // README.md, 2048, placement unknown
	seq := opened(t, a, bySize())

	before := a.order(&DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}})
	if before.unplaced != 1 {
		t.Fatalf("a deletion with no placement was counted %d times", before.unplaced)
	}
	seq.scope(&Scope{Count: 9}) // the child sends it, and the round teaches where it sat

	after := a.order(&DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}})
	if after.unplaced != 0 {
		t.Errorf("the placement was learned and it is still counted for every scope")
	}
	if len(after.gone) != 1 {
		t.Errorf("it did not take its place in the order: %d removals", len(after.gone))
	}
}

// Two sequences over one source are arranged separately: a different sort puts
// the same amendments in a different order.
func TestEachSequenceArrangesTheAmendmentsItsOwnWay(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("aaa.go", 99999))

	up, _ := read(t, a, bySize(), &Scope{Count: 9})
	if up.joined() != "2,1,0,3,90" {
		t.Errorf("by size the sequence is %s", up.joined())
	}
	// Exact collation, so README.md leads: an uppercase R sorts below a lowercase a.
	byName, _ := read(t, a, byName(), &Scope{Count: 9})
	if byName.joined() != "0,90,1,2,3" {
		t.Errorf("by name the sequence is %s", byName.joined())
	}
}

// A reversed scope resumes after one of ours without sending it again.
//
// The order is arranged the way the sequence runs and read backwards, so the
// boundary record sits somewhere in it and the walk has to stop just short of
// it rather than just past it -- the mirror of what a forward walk does, and
// the one place the two directions cannot share a search.
func TestAReversedScopeResumesAfterOneOfOurs(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("added", 99998))
	a.Add(key(91), fields("added", 99999))
	seq := opened(t, a, bySize())

	first, done := seq.scope(&Scope{Count: 2, Reversed: true})
	if first.joined() != "91,90" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	if got := valueText(done.Watermark); got != "90" {
		t.Fatalf("the claim reaches %s", got)
	}

	next, done := seq.scope(&Scope{After: NewInt(90), Count: 9, Reversed: true})
	if next.joined() != "3,0,1,2" {
		t.Errorf("the rest is %s", next.joined())
	}
	if done.Stop != StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// A deletion nobody has seen the record of is covered for every scope.
//
// Not knowing where it sat is not knowing which scope it falls in, so the child
// is asked for one more than the scope wants. Getting that wrong is not visibly
// wrong -- the scope comes up short and the round trip that fixes it hides the
// cost -- so it is the ask that is checked here, not the answer.
func TestAnUnplacedDeletionIsCoveredInTheAsk(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	a.Delete(key(0), nil)

	read(t, a, bySize(), &Scope{Count: 3})
	if s.asked.Count != 4 {
		t.Errorf("the child was asked for %d, want the 3 plus the one that may go", s.asked.Count)
	}
}

// Only the removals past the boundary are covered, and finding them is a search
// -- which is a search of a run that has to be in order for it to land.
func TestOnlyTheRemovalsPastTheBoundaryAreCovered(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	// go.mod at 96 sits before the boundary; README.md at 2048 sits after it.
	a.Delete(key(2), fields("go.mod", 96))
	a.Delete(key(0), fields("README.md", 2048))
	seq := opened(t, a, bySize())

	// Walk as far as build.sh (310), then carry on past it.
	if first, _ := seq.scope(&Scope{Count: 1}); first.joined() != "1" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	seq.scope(&Scope{After: NewInt(1), Count: 2})
	if s.asked.Count != 3 {
		t.Errorf("the child was asked for %d, want the 2 plus the one removal past "+
			"the boundary", s.asked.Count)
	}
}

// A replacement made between two scopes goes out in the second.
//
// This is the one that would lose a record rather than merely misjudge one: the
// child's copy is dropped the moment anything is held against its key, so a
// replacement the arrangement has not caught up with takes the child's record
// out and puts nothing back.
func TestAReplacementMadeBetweenScopesIsSeen(t *testing.T) {
	a := amendable(t)
	seq := opened(t, a, bySize())

	if first, _ := seq.scope(&Scope{Count: 9}); first.joined() != "2,1,0,3" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	a.Replace(key(2), fields("go.mod", 99999)) // was the smallest; now the largest

	next, _ := seq.scope(&Scope{Count: 9})
	if next.joined() != "1,0,3,2" {
		t.Errorf("the scope after the replacement is %s", next.joined())
	}
}

// A deletion made between two scopes is covered in the ask for the second.
//
// It would come right either way -- a record of the child's is looked up in
// what is held as it arrives, so the child's copy is dropped whether or not
// anything was rearranged -- but the scope would come up short and cost a round
// trip to fill. So it is the ask that is checked, not the answer.
func TestADeletionMadeBetweenScopesIsCoveredInTheAsk(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	seq := opened(t, a, bySize())

	if first, _ := seq.scope(&Scope{Count: 1}); first.joined() != "2" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	a.Delete(key(0), fields("README.md", 2048)) // past the boundary

	seq.scope(&Scope{After: NewInt(2), Count: 2})
	if s.asked.Count != 3 {
		t.Errorf("the child was asked for %d, want the 2 plus the one that goes",
			s.asked.Count)
	}
}

// Both runs of the arrangement are in the sequence's order.
//
// A scope finds its place in each by searching, and a search of a run that is
// not in order lands somewhere arbitrary. What goes out would still come out
// right -- that run is walked, not searched past -- but the removals are only
// ever counted by searching, so an unsorted one miscounts the shortfall.
//
// Asserted on the arrangement rather than through an answer, because what is
// held arrives from a map: an unsorted run comes out sorted often enough by
// chance that an answer would pass half the time.
func TestBothRunsOfTheArrangementAreInOrder(t *testing.T) {
	a := amendable(t)
	for i, size := range []int64{700, 100, 900, 300, 500} {
		a.Add(key(int64(90+i)), fields("added", size))
	}
	for i, size := range []int64{800, 200, 1000, 400, 600} {
		a.Delete(key(int64(80+i)), fields("gone", size))
	}

	o := a.order(&DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}})
	levels := ordering1(&DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}})
	for _, run := range []struct {
		what string
		at   [][]*Value
	}{{"what goes out", o.outAt}, {"what is taken out", o.goneAt}} {
		if len(run.at) != 5 {
			t.Fatalf("%s holds %d, want 5", run.what, len(run.at))
		}
		for i := 1; i < len(run.at); i++ {
			if CompareLevels(run.at[i-1], run.at[i], levels) >= 0 {
				t.Errorf("%s is out of order at %d", run.what, i)
			}
		}
	}
}

// --- how many there are ---------------------------------------------------

// small is the sequence of records under a kilobyte, which twoWays has two of:
// go.mod at 96 and build.sh at 310.
func small() *DataSetDescriptor {
	return &DataSetDescriptor{Sort: []SortLevel{{Field: ".size"}}, Filter: lt(".size", 1000)}
}

// An amendment is worth -1, 0 or +1, and which of those it is turns on one
// question asked twice: does the filter admit the child's version, and does it
// admit ours? The child's is here only once something of it has crossed, so the
// same amendment is a floor before that and a figure after it.
func TestAReplacementIsCountedOnceTheChildsVersionIsShadowed(t *testing.T) {
	a := amendable(t)
	a.Replace(key(0), fields("tiny", 50)) // README.md was 2048, so out; now in

	// Nothing of the child's has crossed, so whether that record was ever in
	// this sequence is not known here -- and it is in now, so the figure can
	// only be the same or one more.
	if got := counted(t, a, small()); got != "at least 2" {
		t.Errorf("before the child's version crossed it counted %s", got)
	}

	// Reading the sequence the record IS in shadows it on the way past.
	read(t, a, bySize(), &Scope{Count: 9})

	if got := counted(t, a, small()); got != "3" {
		t.Errorf("with both versions here it counted %s", got)
	}
}

// A deletion of a record the filter never admitted costs nothing at all, which
// is the case a floor alone gets wrong in the expensive direction: it says
// there may be one fewer when there certainly is not.
func TestADeletionOutsideTheFilterCostsNothing(t *testing.T) {
	a := amendable(t)
	a.Delete(key(3), nil) // parser.go at 14022, which `small` never admitted

	if got := counted(t, a, small()); got != "at least 1" {
		t.Errorf("with nothing shadowed it counted %s", got)
	}

	read(t, a, bySize(), &Scope{Count: 9})

	if got := counted(t, a, small()); got != "2" {
		t.Errorf("knowing what it deleted, it counted %s", got)
	}
}

// A deletion of one the filter DID admit is one fewer, exactly.
func TestADeletionInsideTheFilterIsOneFewer(t *testing.T) {
	a := amendable(t)
	a.Delete(key(2), fields("go.mod", 96)) // stated outright, so nothing to read

	if got := counted(t, a, small()); got != "1" {
		t.Errorf("it counted %s", got)
	}
}

// An addition shadows nothing -- it names no record of the child's -- so it is
// counted or not by its own values alone, and either way exactly.
func TestAnAdditionIsCountedByItsOwnValues(t *testing.T) {
	a := amendable(t)
	a.Add(key(9), fields("tiny", 50))
	a.Add(key(8), fields("huge", 99999))

	if got := counted(t, a, small()); got != "3" {
		t.Errorf("with one addition in and one out it counted %s", got)
	}
}

// A shadow is knowledge like any other, so it goes stale like any other -- and
// a figure that was exact because of one falls back to a floor when it goes.
func TestAStaleShadowCostsTheExactness(t *testing.T) {
	a := amendable(t)
	a.Replace(key(0), fields("tiny", 50))
	read(t, a, bySize(), &Scope{Count: 9})
	if got := counted(t, a, small()); got != "3" {
		t.Fatalf("it counted %s", got)
	}

	a.Stale(key(0))

	if got := counted(t, a, small()); got != "at least 2" {
		t.Errorf("after the shadow went it counted %s", got)
	}
}

// A source amending one that will not count has nothing to move, and says so
// rather than counting only its own amendments.
func TestAmendingASourceThatWillNotCountSaysNothing(t *testing.T) {
	a := NewAmendedSource(mute(mustPSL(t, twoWays)))
	a.Add(key(9), fields("tiny", 50))

	if got := counted(t, a, small()); got != "at least 1" {
		t.Errorf("it counted %s", got)
	}
}

// An addition is taken at its word until a clash surfaces, in the figure
// exactly as in the records.
//
// Add says the key is the author's to keep clear of the child's, and nothing
// asks to find out otherwise -- it surfaces when the child's copy arrives. So
// an author who collides is over by one until that moment, and right
// afterwards: from then on the child's record is the one that stands, and the
// child is the one counting it.
func TestAClashedAdditionIsCountedByTheChildAndNotTwice(t *testing.T) {
	a := amendable(t)
	a.Add(key(2), fields("tiny", 50)) // key 2 is the child's go.mod, at 96

	if got := counted(t, a, small()); got != "3" {
		t.Errorf("before the clash surfaced it counted %s", got)
	}

	// The child's copy crossing is the only moment the clash can be found out.
	read(t, a, bySize(), &Scope{Count: 9})

	if got := counted(t, a, small()); got != "2" {
		t.Errorf("once the child's record stood it counted %s", got)
	}
}

// --- altering some of a record ---------------------------------------------

// An alteration says SOME of a record and the rest is the child's, which is what a
// cell edit is: a reader changed one member and said nothing about the others.
func TestAnAlterationChangesSomeOfTheChildsRecord(t *testing.T) {
	a := amendable(t)
	a.Alter(key(2), Record{Named(".name", "go.mod.orig")})

	out, _ := read(t, a, bySize(), &Scope{Count: 4})
	if out.joined() != "2,1,0,3" {
		t.Fatalf("the sequence is %s", out.joined())
	}
	// The member that was altered, and the one that was not -- which is the whole
	// difference from Replace, where the bag is only what was handed in.
	if got := out.fields[0].Get(".name"); !Equal(got, NewText("go.mod.orig")) {
		t.Errorf("the altered member reads %v", got)
	}
	if got := out.fields[0].Get(".size"); !Equal(got, NewInt(96)) {
		t.Errorf("the member nobody altered reads %v, want the child's own", got)
	}
}

// **An alteration cannot move the record.** The child applies the filter and the
// sort and places it; this touches what it HOLDS on the way past. So a reader who
// edits a cell does not have the row leap away from under the cursor, and the new
// position waits for the next question -- which is the point of it, not a shortcut.
func TestAnAlterationLeavesTheRecordWhereTheChildPutIt(t *testing.T) {
	a := amendable(t)
	// 96 was the smallest, so by size it came first. A replacement of 99999 moves it
	// to the end (see TestAReplacementIsPlacedByItsOwnValues); an alteration does not.
	a.Alter(key(2), Record{Named(".size", 99999)})

	out, _ := read(t, a, bySize(), &Scope{Count: 4})
	if out.joined() != "2,1,0,3" {
		t.Errorf("the sequence is %s, want the child's own order", out.joined())
	}
	if got := out.fields[0].Get(".size"); !Equal(got, NewInt(99999)) {
		t.Errorf("the altered member reads %v", got)
	}
}

// And it cannot change how many there are: the child counted it and still does.
func TestAnAlterationDoesNotChangeTheCount(t *testing.T) {
	a := amendable(t)
	count := func() RecordCount {
		set, err := a.Open(bySize())
		if err != nil {
			t.Fatal(err)
		}
		defer set.Close()
		return CountOf(set)
	}
	before := count()
	a.Alter(key(2), Record{Named(".size", 99999)})
	if after := count(); after != before {
		t.Errorf("the count went from %v to %v", before, after)
	}
}

// **Altering accumulates.** Two edits to two members of one row are two calls, and
// the second must not lose the first -- which is the case a view hits immediately,
// a reader tabbing from one column to the next.
func TestAlteringTwiceKeepsBothMembers(t *testing.T) {
	a := amendable(t)
	a.Alter(key(2), Record{Named(".name", "first")})
	a.Alter(key(2), Record{Named(".size", 777)})

	out, _ := read(t, a, bySize(), &Scope{Count: 4})
	if got := out.fields[0].Get(".name"); !Equal(got, NewText("first")) {
		t.Errorf("the first edit reads %v; the second lost it", got)
	}
	if got := out.fields[0].Get(".size"); !Equal(got, NewInt(777)) {
		t.Errorf("the second edit reads %v", got)
	}
}

// Altering a record that was REPLACED writes into the replacement, both being
// statements about the same key and a replacement being a record entire.
func TestAlteringAReplacementWritesIntoIt(t *testing.T) {
	a := amendable(t)
	a.Replace(key(2), fields("mine", 5))
	a.Alter(key(2), Record{Named(".name", "mine, corrected")})

	out, _ := read(t, a, bySize(), &Scope{Count: 4})
	var got Record
	for i, k := range out.keys {
		if k == valueText(key(2)) {
			got = out.fields[i]
		}
	}
	if got == nil {
		t.Fatalf("the record is not in %s", out.joined())
	}
	if v := got.Get(".name"); !Equal(v, NewText("mine, corrected")) {
		t.Errorf("the name reads %v", v)
	}
	if v := got.Get(".size"); !Equal(v, NewInt(5)) {
		t.Errorf("the size reads %v, want the replacement's own", v)
	}
}

// Altering a record that was DELETED says nothing. Gone is gone, and bringing it
// back under some of its members would invent the rest.
func TestAlteringADeletedRecordSaysNothing(t *testing.T) {
	a := amendable(t)
	a.Delete(key(1), fields("build.sh", 310))
	a.Alter(key(1), Record{Named(".name", "back again")})

	out, _ := read(t, a, bySize(), &Scope{Count: 4})
	if strings.Contains(out.joined(), "1") {
		t.Errorf("the deleted record is back: %s", out.joined())
	}
	held := a.Amendments()
	if len(held) != 1 || held[0].How != Removed {
		t.Fatalf("what is held is %v, want the deletion alone", held)
	}
	// **And it carries nothing**, which is what somebody saving these out reads.
	// "Removed, and here are some fields" is not a statement about anything: the
	// record is gone, and members left on it would be written to a file as though
	// they were in force.
	if held[0].Fields != nil {
		t.Errorf("the deletion carries %s", held[0].Fields)
	}
}

// An alteration against a key the child does not send means nothing: there is no
// record to alter, and inventing one would be an addition by accident.
func TestAnAlterationOfNothingAddsNothing(t *testing.T) {
	a := amendable(t)
	a.Alter(key(99), Record{Named(".name", "not a record of the child's")})

	out, _ := read(t, a, bySize(), &Scope{Count: 10})
	if strings.Contains(out.joined(), "99") {
		t.Errorf("an alteration added a record: %s", out.joined())
	}
}

// --- reading back what is held --------------------------------------------

// **What is held can be asked for**, because an amendment is meant to outlive the
// session that made it: somebody has to write these to a file or a database, and
// diffing the records against a child that has moved on is not a way to find them.
func TestWhatIsHeldCanBeReadBack(t *testing.T) {
	a := amendable(t)
	if a.Amended() {
		t.Error("a fresh source says it holds something")
	}

	a.Replace(key(0), fields("replaced", 1))
	a.Alter(key(1), Record{Named(".name", "altered")})
	a.Add(key(50), fields("added", 2))
	a.Delete(key(3), fields("gone", 3))

	if !a.Amended() {
		t.Error("it says it holds nothing")
	}
	held := a.Amendments()
	if len(held) != 4 {
		t.Fatalf("it holds %d amendments, want four", len(held))
	}

	// The four facts about a record, in the same words a source ANNOUNCES one with.
	byKey := map[string]Amendment{}
	for _, am := range held {
		byKey[Key(am.Key)] = am
	}
	for _, want := range []struct {
		key  int64
		how  Change
		name string
	}{
		{0, Replaced, "replaced"},
		{1, Altered, "altered"},
		{50, Added, "added"},
		{3, Removed, ""},
	} {
		am, ok := byKey[Key(key(want.key))]
		if !ok {
			t.Errorf("nothing is held for %d", want.key)
			continue
		}
		if am.How != want.how {
			t.Errorf("%d is held as %v, want %v", want.key, am.How, want.how)
		}
		if want.name == "" {
			// A deletion states nothing of the record: it is gone.
			if am.Fields != nil {
				t.Errorf("the deletion carries %s", am.Fields)
			}
			continue
		}
		if got := am.Fields.Get(".name"); !Equal(got, NewText(want.name)) {
			t.Errorf("%d carries %v", want.key, got)
		}
	}

	// A stable order, which is what somebody writing a file wants. Not the order
	// they were made in: one statement per key, so that order is already collapsed.
	for i := 1; i < len(held); i++ {
		if Key(held[i-1].Key) > Key(held[i].Key) {
			t.Errorf("amendment %d sorts after %d", i-1, i)
		}
	}

	// And forgetting one takes it out of what is held.
	a.Forget(key(1))
	if got := len(a.Amendments()); got != 3 {
		t.Errorf("after forgetting one, %d are held", got)
	}
}

// --- what a scope is complete up to ---------------------------------------

// oversending ignores the COUNT and sends everything from the boundary on, then says
// exhausted. Ignoring the count is the least an application can do and is documented
// as correct -- below a size the author picks it is also the fastest, and
// `hosting-a-query.md` shows it first.
//
// It honours `After`, because that is a different promise and the two are worth
// keeping apart: dropping the count only makes an answer bigger, while dropping the
// boundary answers a question nobody asked.
type oversending struct{ n int }

func (o *oversending) Open(*DataSetDescriptor) (DataSet, error) { return &oversendingSet{n: o.n}, nil }

type oversendingSet struct{ n int }

func (s *oversendingSet) Read(sc *Scope, out Sink) error {
	from := 0
	if sc != nil && sc.After != nil {
		from = int(sc.After.Int) + 1
	}
	out.Ordered()
	for i := from; i < s.n; i++ {
		if err := out.Record(NewInt(int64(i)), Record{Named("name", "r")}); err != nil {
			return err
		}
	}
	out.Done(Complete{Stop: StopExhausted})
	return nil
}
func (s *oversendingSet) Close() {}

// **A trimmed scope is FILLED and not exhausted**, whatever the child said.
//
// The child is right about its own answer and wrong about this one: it sent four
// records and said there was nothing past them, this passed three on, and the fourth
// is past the end of what went out. Saying "exhausted" would lose it -- a reader
// would hold three records and believe there were three, and never ask again.
//
// Worth a test of its own because it is the one case where the child's completion
// must not be believed, and because the child doing this is correct rather than
// sloppy: honouring the count is optional and every other hint an application drops
// only makes an answer bigger.
func TestATrimmedScopeIsFilledAndNotExhausted(t *testing.T) {
	a := NewAmendedSource(&oversending{n: 4})
	set, err := a.Open(&DataSetDescriptor{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var got collector
	if err := set.Read(&Scope{Count: 3}, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.keys) != 3 {
		t.Fatalf("it passed on %d records (%s), want the three that were asked for",
			len(got.keys), got.joined())
	}
	if got.done.Stop != StopFilled {
		t.Errorf("the scope stopped %v, want filled -- the fourth record is past it",
			got.done.Stop)
	}
	// And complete UP TO the last one that went out, so the next scope has somewhere
	// to resume from. An exhausted scope carries no watermark, which is the other half
	// of why the wrong answer here is expensive.
	if got.done.Watermark == nil {
		t.Error("it is complete up to nothing, so there is no way to ask for the rest")
	} else if !Equal(got.done.Watermark, NewInt(2)) {
		t.Errorf("it is complete up to %v, want the last record sent", got.done.Watermark)
	}

	// The rest is then askable, which is the point of getting the completion right.
	var rest collector
	if err := set.Read(&Scope{After: got.done.Watermark, Count: 3}, &rest); err != nil {
		t.Fatal(err)
	}
	if rest.joined() != "3" {
		t.Errorf("the next scope reads %q, want the record that was trimmed", rest.joined())
	}
	if rest.done.Stop != StopExhausted {
		t.Errorf("and it stopped %v, want exhausted this time", rest.done.Stop)
	}
}

// A child that sends exactly what was asked for and says it is exhausted is telling
// the truth, and it is passed on -- nothing was trimmed, so there really is nothing
// past the end and the next question is saved.
func TestAnUntrimmedScopeKeepsTheChildsExhausted(t *testing.T) {
	a := NewAmendedSource(&oversending{n: 3})
	set, err := a.Open(&DataSetDescriptor{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var got collector
	if err := set.Read(&Scope{Count: 3}, &got); err != nil {
		t.Fatal(err)
	}
	if got.done.Stop != StopExhausted {
		t.Errorf("the scope stopped %v, want the child's exhausted", got.done.Stop)
	}
}
