package serval

// How many records a sequence has: what says it, what moves it, and what it
// costs to ask twice.

import "testing"

// counted opens a sequence, asks how many records it has, and lets it go --
// which is what a reader sizing a bar does.
func counted(t *testing.T, src Source, spec *Spec) string {
	t.Helper()
	v, err := src.Open(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	return CountOf(v).String()
}

// under is the same sequence with a filter on it, for the cases about
// membership rather than order.
func under(f *Filter) *Spec {
	return &Spec{Sort: []SortLevel{{Field: ".name"}}, Filter: f}
}

// --- the figure itself ----------------------------------------------------

// Three things can be said, and the third is saying nothing.
func TestACountIsAFigureOrAFloorOrNothing(t *testing.T) {
	for _, c := range []struct {
		RecordCount
		want string
	}{
		{Exactly(5), "5"},
		{Exactly(0), "0"},      // a sequence known to be empty is not the same as
		{Unknown(), "unknown"}, // a sequence nobody has counted
		{AtLeast(5), "at least 5"},
		{AtLeast(0), "unknown"}, // every sequence has at least none of them
	} {
		if got := c.String(); got != c.want {
			t.Errorf("%+v reads as %s", c.RecordCount, got)
		}
	}
	if Exactly(0).Nothing() {
		t.Error("a sequence counted and found empty says nothing")
	}
}

// A record certainly arriving or certainly leaving keeps whatever the figure
// was: a floor one higher is still a floor, and an exact count that gains one
// is exact. Which is why counting needs no deltas of its own -- the notice that
// said Added has already said +1.
func TestArrivingAndLeavingKeepTheFigure(t *testing.T) {
	for _, c := range []struct {
		got  RecordCount
		want string
	}{
		{Exactly(5).Add(1), "6"},
		{Exactly(5).Take(1), "4"},
		{AtLeast(5).Add(1), "at least 6"},
		{AtLeast(5).Take(1), "at least 4"},
		{Exactly(0).Take(1), "0"}, // never below none of them
	} {
		if got := c.got.String(); got != c.want {
			t.Errorf("it came to %s, and should be %s", got, c.want)
		}
	}
}

// A record that MAY have left costs the exactness and keeps a floor, which is
// the whole reason a floor is carried: without one, a single doubtful record
// would cost the entire count and the next honest thing to say would be
// nothing at all.
func TestADoubtfulRecordCostsTheExactnessAndNotTheFigure(t *testing.T) {
	if got := Exactly(5).Doubt(1).String(); got != "at least 4" {
		t.Errorf("one record in doubt left %s", got)
	}
	if got := Exactly(5).Doubt(0).String(); got != "at least 5" {
		t.Errorf("a record that may have JOINED left %s", got)
	}
	if got := Exactly(5).Doubt(9).String(); got != "unknown" {
		t.Errorf("more doubt than there were records left %s", got)
	}
}

// Two sequences added are exact only where both halves are, a floor plus a
// figure being still only a floor.
func TestTwoCountsAddedAreExactOnlyWhereBothAre(t *testing.T) {
	for _, c := range []struct {
		got  RecordCount
		want string
	}{
		{Exactly(2).And(Exactly(3)), "5"},
		{Exactly(2).And(AtLeast(3)), "at least 5"},
		{AtLeast(2).And(Exactly(3)), "at least 5"},
		{Exactly(2).And(Unknown()), "at least 2"},
	} {
		if got := c.got.String(); got != c.want {
			t.Errorf("added they came to %s, and should be %s", got, c.want)
		}
	}
}

// --- what a source says ---------------------------------------------------

// A source holding its own records counts them for nothing: they were ordered
// when the sequence was opened, so the figure is the length of something that
// already exists.
func TestASourceHoldingItsRecordsCountsThemForNothing(t *testing.T) {
	src := mustPSL(t, many(20))

	if got := counted(t, src, byName()); got != "20" {
		t.Errorf("twenty records counted as %s", got)
	}
	// The FILTER decides it, the sort cannot: sorting the same records makes
	// no more and no fewer of them.
	if got := counted(t, src, under(lt(".size", 8))); got != "8" {
		t.Errorf("a filter admitting eight counted as %s", got)
	}
	if got := counted(t, src, bySize()); got != "20" {
		t.Errorf("the same records in another order counted as %s", got)
	}
	// And a sequence nothing is in is counted, not unknown.
	if got := counted(t, src, under(lt(".size", -1))); got != "0" {
		t.Errorf("a filter admitting nothing counted as %s", got)
	}
}

// A data set that does not count says so, rather than saying zero.
func TestASourceThatDoesNotCountSaysNothing(t *testing.T) {
	v, err := (&listSource{}).Open(byName())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if got := CountOf(v).String(); got != "unknown" {
		t.Errorf("a source that cannot count said %s", got)
	}
}

// --- through the wrapper --------------------------------------------------

// The count is a fact about MEMBERSHIP, so a second sort over the same filter
// has it without asking. Which is the re-sort case: click a column header and
// the order is new while the count is the one already known.
func TestARESortKeepsTheCount(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, many(20))

	if got := counted(t, src, byName()); got != "20" {
		t.Fatalf("it counted %s", got)
	}
	if n.counted != 1 {
		t.Fatalf("the first count asked the source %d times", n.counted)
	}
	if got := counted(t, src, bySize()); got != "20" {
		t.Errorf("sorted the other way it counted %s", got)
	}
	if n.counted != 1 {
		t.Errorf("a re-sort asked the source to count %d times over", n.counted-1)
	}
	// A different FILTER is a different membership, and is asked.
	if got := counted(t, src, under(lt(".size", 8))); got != "8" {
		t.Errorf("under a filter it counted %s", got)
	}
	if n.counted != 2 {
		t.Errorf("a new filter asked the source %d times", n.counted-1)
	}
}

// A sequence read to its end has been counted by the reading, so the source is
// never asked -- which is the minimal implementation's answer arriving free:
// send everything, say exhausted, and the figure falls out of what was filed.
func TestReadingToTheEndCountsItWithoutAsking(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, many(20))

	draw(t, src, byName(), &Scope{Count: 99})
	if got := counted(t, src, byName()); got != "20" {
		t.Errorf("after reading the lot it counted %s", got)
	}
	if n.counted != 0 {
		t.Errorf("it asked the source to count %d times over", n.counted)
	}
}

// Part of a sequence read is a FLOOR, which is most of what a bar wants and is
// what an incremental reader has anyway.
func TestPartOfASequenceReadIsAFloor(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	// A source that will not count, so that what is held is all there is.
	src := NewCachedSource(mute(mustPSL(t, many(20))))

	if got := counted(t, src, byName()); got != "unknown" {
		t.Fatalf("before reading anything it counted %s", got)
	}
	draw(t, src, byName(), &Scope{Count: 6})
	if got := counted(t, src, byName()); got != "at least 6" {
		t.Errorf("holding six it counted %s", got)
	}
	// Two stretches apart are still records of one sequence, so they add.
	draw(t, src, byName(), &Scope{After: NewInt(14), Count: 3})
	if got := counted(t, src, byName()); got != "at least 9" {
		t.Errorf("holding nine over two stretches it counted %s", got)
	}
}

// --- what a notice costs it -----------------------------------------------

// told about a sequence through the wrapper, which is how a source says it.
func staled(t *testing.T, src *CachedSource, spec *Spec, n Notice) {
	t.Helper()
	src.Stale(spec, n)
}

// stated is the figure the cache is HOLDING for a membership, which is what a
// notice moves. What RecordCount answers can be better than this -- the source
// is the authority and is free to say so again -- so a case about what a notice
// costs asks here, and the case below is about the asking.
func stated(c *cache, src *CachedSource, spec *Spec) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[src.key+"\x00"+FilterKey(spec.Filter)].String()
}

func TestANoticeMovesTheCountByWhatItSays(t *testing.T) {
	// Each case counts the sequence, sends one notice, and counts it again.
	for _, c := range []struct {
		what string
		n    Notice
		want string
	}{
		{"a record appeared", Notice{Extent: atRecord(3), Change: Added}, "21"},
		{"a record left", Notice{Extent: atRecord(3), Change: Removed}, "19"},
		{"a stretch left", Notice{Extent: fromTo(3, 5), Change: Removed}, "17"},
		{"a record may have moved",
			Notice{Extent: atRecord(3), Change: Replaced}, "at least 19"},
		{"a field the filter tests changed", Notice{Extent: atRecord(3),
			Change: Altered, Fields: []string{".size"}}, "at least 19"},
		{"a field only the sort uses changed", Notice{Extent: atRecord(3),
			Change: Altered, Fields: []string{".name"}}, "20"},
		{"a field nothing is decided by changed", Notice{Extent: atRecord(3),
			Change: Altered, Fields: []string{".modified"}}, "20"},
		{"a stretch nobody can walk", Notice{Extent: fromTo(3, 99),
			Change: Removed}, "unknown"},
	} {
		held := ownCache(t, 1<<20, 1<<20)
		src, _ := cached(t, many(20))
		// Sorted by name and filtered on size, so that the two fields ask
		// different questions -- which is the point of half these cases.
		spec := &Spec{
			Sort:   []SortLevel{{Field: ".name"}},
			Filter: lt(".size", 100),
		}
		if got := counted(t, src, spec); got != "20" {
			t.Fatalf("%s: it counted %s to begin with", c.what, got)
		}
		draw(t, src, spec, &Scope{Count: 8}) // so a stretch has somewhere to be

		staled(t, src, spec, c.n)

		if got := stated(held, src, spec); got != c.want {
			t.Errorf("%s: it holds %s, and should hold %s", c.what, got, c.want)
		}
	}
}

// A figure in doubt is asked about again, and a source that says otherwise is
// believed -- it is the authority, and a notice that put the figure in doubt is
// exactly the moment to go back to it. An EXACT figure is not re-asked, there
// being nothing better to be had.
func TestAFigureInDoubtIsAskedAboutAgain(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, many(20))
	spec := under(nil)

	if got := counted(t, src, spec); got != "20" {
		t.Fatalf("it counted %s", got)
	}
	was := n.counted

	src.Stale(spec, Notice{Extent: atRecord(3), Change: Removed})
	if got := counted(t, src, spec); got != "19" || n.counted != was {
		t.Errorf("after a deletion it counted %s, asking %d times over",
			got, n.counted-was)
	}

	src.Stale(spec, Notice{Extent: atRecord(4), Change: Replaced})
	if got := counted(t, src, spec); got != "20" || n.counted != was+1 {
		t.Errorf("after a doubtful notice it counted %s, asking %d times over",
			got, n.counted-was)
	}
}

// A notice naming no sequence names no filter, so it says nothing about who is
// a member of what and moves no count. A source that changes what is IN a
// sequence says so against that sequence.
func TestANoticeWithNoSequenceMovesNoCount(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, many(20))
	if got := counted(t, src, byName()); got != "20" {
		t.Fatalf("it counted %s", got)
	}

	src.Stale(nil, Notice{Extent: atRecord(3), Change: Removed})

	if got := counted(t, src, byName()); got != "20" {
		t.Errorf("a notice with no sequence moved the count to %s", got)
	}
}

// The count follows membership and not order, so a notice against one sort is
// answered for every sort over that filter.
func TestANoticeAgainstOneSortMovesTheCountForBoth(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, many(20))
	byName, bySize := under(nil), &Spec{Sort: []SortLevel{{Field: ".size"}}}

	if got := counted(t, src, byName); got != "20" {
		t.Fatalf("it counted %s", got)
	}
	staled(t, src, byName, Notice{Extent: atRecord(3), Change: Removed})

	if got := counted(t, src, bySize); got != "19" {
		t.Errorf("the other order counted %s", got)
	}
}

// --- a source that counts however it likes --------------------------------

// saying wraps a source and counts it however the test says, including not at
// all -- which is every source that cannot answer the question cheaply.
type saying struct {
	child Source
	count RecordCount
}

func mute(child Source) *saying { return &saying{child: child} }

func (m *saying) Open(spec *Spec) (DataSet, error) {
	v, err := m.child.Open(spec)
	if err != nil {
		return nil, err
	}
	return &sayingSet{src: m, child: v}, nil
}

type sayingSet struct {
	src   *saying
	child DataSet
}

func (s *sayingSet) Close()                   { s.child.Close() }
func (s *sayingSet) RecordCount() RecordCount { return s.src.count }

// Read strips the figure off the ANSWER as well as off the question.
//
// A source that will not count is one that will not count either way round:
// Total rides a completion precisely so that a source able to count says so on
// an answer it was sending anyway, so muting RecordCount alone would leave the
// figure crossing by the other road and this would be testing nothing.
func (s *sayingSet) Read(sc *Scope, out Sink) error {
	return s.child.Read(sc, &sayingSink{Sink: out, count: s.src.count})
}

type sayingSink struct {
	Sink
	count RecordCount
}

func (k *sayingSink) Done(c Complete) {
	c.Total = k.count
	k.Sink.Done(c)
}

// An exact figure always stands over a floor, whichever way round the numbers
// fall. A floor is a claim that there are AT LEAST this many, so a higher one
// says more than a lower one -- but it never says more than a source that has
// counted, however high it is.
func TestAnExactFigureStandsOverAHigherFloor(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src := &saying{child: mustPSL(t, many(20)), count: AtLeast(25)}
	held := NewCachedSource(src)

	if got := counted(t, held, byName()); got != "at least 25" {
		t.Fatalf("a stated floor came back as %s", got)
	}
	// Records went, and the source has counted what is left. The smaller figure
	// is the better one, because it is the one somebody counted.
	src.count = Exactly(20)
	if got := counted(t, held, byName()); got != "20" {
		t.Errorf("an exact figure below the floor came back as %s", got)
	}
}

// A source may volunteer the figure on an answer it was sending anyway, rather
// than waiting to be asked -- and a wrapper files it, so nobody asks again.
func TestASourceMayVolunteerTheFigureWithAnAnswer(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	// It will not answer the question, but it says the figure as it goes.
	src := NewCachedSource(&telling{child: mustPSL(t, many(20)), total: Exactly(20)})

	draw(t, src, byName(), &Scope{Count: 3})

	if got := counted(t, src, byName()); got != "20" {
		t.Errorf("after an answer that stated it, it counted %s", got)
	}
	// And it is the membership's, so the other order has it too.
	if got := counted(t, src, bySize()); got != "20" {
		t.Errorf("the other order counted %s", got)
	}
}

// telling is a source that will not be asked how many records there are, and
// says so on the way past instead.
type telling struct {
	child Source
	total RecordCount
}

func (m *telling) Open(spec *Spec) (DataSet, error) {
	v, err := m.child.Open(spec)
	if err != nil {
		return nil, err
	}
	return &tellingSet{src: m, child: v}, nil
}

type tellingSet struct {
	src   *telling
	child DataSet
}

func (s *tellingSet) Close() { s.child.Close() }

func (s *tellingSet) Read(sc *Scope, out Sink) error {
	return s.child.Read(sc, &tellingSink{src: s.src, out: out})
}

type tellingSink struct {
	src *telling
	out Sink
}

func (k *tellingSink) Ordered()                         { k.out.Ordered() }
func (k *tellingSink) Record(id *Value, f Record) error { return k.out.Record(id, f) }
func (k *tellingSink) Subset(id *Value, f Record, h Totals) error {
	return k.out.Subset(id, f, h)
}

func (k *tellingSink) Done(c Complete) {
	c.Total = k.src.total
	k.out.Done(c)
}
