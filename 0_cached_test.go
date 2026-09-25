package serval

// A source that answers out of what is already held.
//
// What is tested here is the WRAPPER: that a scope answered once does not reach
// the source again, that what the source said is what the asker gets either
// way, and that the cache the wrapper draws on is the process's one and not one
// of its own.

import (
	"fmt"
	"strings"
	"testing"
)

// ownCache gives one test the process's cache to itself. There is only the one
// -- that is the design -- so a test that wants a small one puts a small one
// there and puts the old one back afterwards.
func ownCache(t *testing.T, places, records int) *cache {
	t.Helper()
	was := hot
	hot = sized(places, records)
	t.Cleanup(func() { hot = was })
	return hot
}

// counting is a source that says how much it was asked, so that a test can tell
// an answer that came from the cache from one that came from underneath.
type counting struct {
	child   Source
	opens   int
	reads   int
	sent    int
	counted int // how often it was asked how many records there are
}

func (c *counting) Open(descriptor *DataSetDescriptor) (DataSet, error) {
	v, err := c.child.Open(descriptor)
	if err != nil {
		return nil, err
	}
	c.opens++
	return &countingSet{src: c, child: v}, nil
}

type countingSet struct {
	src   *counting
	child DataSet
}

func (s *countingSet) Close() { s.child.Close() }

// A pass-through counts whatever its child counts, which is what any wrapper
// that does not change who is in the sequence owes the one above it.
func (s *countingSet) RecordCount() RecordCount {
	s.src.counted++
	return CountOf(s.child)
}

func (s *countingSet) Read(sc *Scope, out Sink) error {
	s.src.reads++
	return s.child.Read(sc, &countingSink{src: s.src, out: out})
}

type countingSink struct {
	src *counting
	out Sink
}

func (k *countingSink) Ordered() { k.out.Ordered() }
func (k *countingSink) Record(id *Value, f Record) error {
	k.src.sent++
	return k.out.Record(id, f)
}
func (k *countingSink) Subset(id *Value, f Record, has Totals) error {
	k.src.sent++
	return k.out.Subset(id, f, has)
}
func (k *countingSink) Done(c Complete) { k.out.Done(c) }

// cached is a counted PSL source behind a CachedSource, which is the shape
// every test here is about.
func cached(t *testing.T, text string) (*CachedSource, *counting) {
	t.Helper()
	n := &counting{child: mustPSL(t, text)}
	return NewCachedSource(n), n
}

// draw opens a sequence, reads one scope out of it and lets it go, which is
// what a reader that asks twice actually does.
func draw(t *testing.T, src Source, descriptor *DataSetDescriptor, sc *Scope) (*collector, Complete) {
	t.Helper()
	v, err := src.Open(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	return fill(t, v, sc)
}

// A scope answered once is answered again without the source below being asked.
func TestAScopeAnsweredOnceIsNotAskedAgain(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, twoWays)

	first, done := draw(t, src, byName(), &Scope{Count: 3})
	if first.joined() != "0,1,2" || done.Stop != StopFilled {
		t.Fatalf("the first reading gave %s / %s", first.joined(), done.Stop)
	}
	if n.reads != 1 {
		t.Fatalf("the first reading asked the source %d times", n.reads)
	}

	again, done := draw(t, src, byName(), &Scope{Count: 3})
	if n.reads != 1 {
		t.Errorf("the second reading asked the source %d times over", n.reads-1)
	}
	if again.joined() != first.joined() || done.Stop != StopFilled {
		t.Errorf("held, it gave %s / %s where the source gave %s",
			again.joined(), done.Stop, first.joined())
	}
	// And it is told the order before the records, the same as the source does.
	if !again.ordered {
		t.Error("a held answer never said its records were in order")
	}
	// Whole records come back whole: what the source said about how much of
	// each record it sent is part of the answer.
	for i, w := range again.whole {
		if w != first.whole[i] {
			t.Errorf("record %s came back whole=%v where the source said %v",
				again.keys[i], w, first.whole[i])
		}
	}
}

// A scope from the middle of what is held is answered out of it too, which is
// what a reader scrolling asks for.
func TestAScopeInsideWhatIsHeldIsAnsweredFromIt(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, twoWays)

	if _, done := draw(t, src, byName(), &Scope{Count: 4}); done.Stop != StopExhausted {
		t.Fatalf("reading everything stopped with %s", done.Stop)
	}
	was := n.reads

	out, done := draw(t, src, byName(), &Scope{After: NewInt(1), Count: 2})
	if n.reads != was {
		t.Errorf("a scope inside what is held asked the source again")
	}
	if out.joined() != "2,3" || done.Stop != StopFilled {
		t.Errorf("after 1 it gave %s / %s", out.joined(), done.Stop)
	}
}

// Two sequences over ONE wrapper share what their records hold. The second sort
// is a new order and not a new fetch of the values.
func TestTwoSequencesOverOneWrapperShareWhatTheyKnow(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, twoWays)

	draw(t, src, byName(), &Scope{Count: 4})
	if n := len(c.flesh.at); n != 4 {
		t.Fatalf("reading four records left %d known", n)
	}
	draw(t, src, bySize(), &Scope{Count: 4})

	if n := len(c.flesh.at); n != 4 {
		t.Errorf("a second order over the same four records left %d known", n)
	}
	if n := len(c.sets); n != 2 {
		t.Errorf("two orders over one source left %d sequences", n)
	}
}

// Two wrappers do NOT share, however alike the sources under them look. Nothing
// here can check that two bodies of records are one, and a wrong answer to that
// hands out another source's records.
func TestTwoWrappersShareNothing(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	one, _ := cached(t, twoWays)
	two, second := cached(t, twoWays)

	draw(t, one, byName(), &Scope{Count: 4})
	draw(t, two, byName(), &Scope{Count: 4})

	if second.reads != 1 {
		t.Errorf("the second wrapper asked its source %d times, so it shared", second.reads)
	}
	if n := len(c.flesh.at); n != 8 {
		t.Errorf("two wrappers over four records each left %d known, not eight", n)
	}
}

// One cache and one quota: what two wrappers hold is counted together, and they
// give way to each other under the one limit.
func TestEveryWrapperDrawsOnTheOneCache(t *testing.T) {
	c := ownCache(t, 6, 1<<20) // room for six places between them
	one, _ := cached(t, twoWays)
	two, _ := cached(t, twoWays)

	draw(t, one, byName(), &Scope{Count: 4})
	draw(t, two, byName(), &Scope{Count: 4})

	if len(c.at) > 6 {
		t.Errorf("two wrappers hold %d places against a limit of six", len(c.at))
	}
	if c.cost > c.limit {
		t.Errorf("they hold %d against a limit of %d", c.cost, c.limit)
	}
	if len(c.at) == 8 {
		t.Error("nothing gave way, so nothing here is shown")
	}
	// And the figure is the one the process is sized by.
	if CacheLimit() != c.limit+c.flesh.limit {
		t.Errorf("CacheLimit says %d and the cache holds %d and %d",
			CacheLimit(), c.limit, c.flesh.limit)
	}
	sound(t, c)
}

// A query asking for fields is answered from what is held only where those
// fields are known -- and a later answer that brings them makes it so.
func TestAQueryIsAnsweredOnlyWhereItsFieldsAreKnown(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, twoWays)

	named := &DataSetDescriptor{Sort: []SortLevel{{Field: ".name"}}, Fields: Record{{Name: ".name"}}}
	draw(t, src, named, &Scope{Count: 4})
	was := n.reads

	// The same fields again: held.
	draw(t, src, named, &Scope{Count: 4})
	if n.reads != was {
		t.Error("the same question was asked of the source twice")
	}

	// One field more: not held, so it is asked -- and then it is.
	both := &DataSetDescriptor{Sort: []SortLevel{{Field: ".name"}},
		Fields: Record{{Name: ".name"}, {Name: ".size"}}}
	draw(t, src, both, &Scope{Count: 4})
	if n.reads != was+1 {
		t.Errorf("asking for a field nothing knew took %d readings", n.reads-was)
	}
	draw(t, src, both, &Scope{Count: 4})
	if n.reads != was+1 {
		t.Error("the topped-up question was asked of the source again")
	}
	// And the first question is still answered, the top-up having added to
	// what was known rather than replaced it.
	draw(t, src, named, &Scope{Count: 4})
	if n.reads != was+1 {
		t.Error("topping a record up lost what was known about it before")
	}
}

// A field the record has not got comes back as an ABSENCE, and the next query
// naming it is answered without the source being asked again.
//
// `plain` is a record with no `.size`: it is a string rather than a list, so it
// carries a key and a value and nothing else.
func TestAFieldARecordHasNotGotIsAnsweredWithoutAskingTwice(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, doc)

	sized := &DataSetDescriptor{Sort: []SortLevel{{Field: ".name"}},
		Fields: Record{{Name: ".size"}}}
	out, _ := draw(t, src, sized, &Scope{Count: 9})

	plain := -1
	for i, k := range out.keys {
		if k == `"plain"` {
			plain = i
		}
	}
	if plain < 0 {
		t.Fatalf("the record with no size never came out: %v", out.keys)
	}
	// Present with nothing under it: a guarantee, rather than a silence.
	if !out.fields[plain].Has(".size") {
		t.Errorf("the record with no size left it out: %s", out.fields[plain])
	}
	if out.fields[plain].Get(".size") != nil {
		t.Errorf("the record with no size sent one: %s", out.fields[plain])
	}

	was := n.reads
	draw(t, src, sized, &Scope{Count: 9})
	if n.reads != was {
		t.Error("a field known to be absent was asked about again")
	}
}

// Knowing how many members a record has settles the ones nobody asked about.
// Two queries that between them cover a record leave the next one -- for a
// field neither named -- answered out of what is held.
func TestTheTotalsSettleAFieldNobodyAskedAbout(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, twoWays)

	// `twoWays` records carry three members: key, .name and .size. Ask for two
	// of them, then the third.
	draw(t, src, &DataSetDescriptor{Sort: []SortLevel{{Field: ".name"}},
		Fields: Record{{Name: "key"}, {Name: ".name"}}}, &Scope{Count: 4})
	draw(t, src, &DataSetDescriptor{Sort: []SortLevel{{Field: ".name"}},
		Fields: Record{{Name: ".size"}}}, &Scope{Count: 4})
	was := n.reads

	// Three of three are known, so a fourth name has nothing left to be, and
	// the whole record is answered without anyone being asked.
	draw(t, src, &DataSetDescriptor{Sort: []SortLevel{{Field: ".name"}},
		Fields: Record{{Name: ".mode"}}}, &Scope{Count: 4})
	if n.reads != was {
		t.Error("a name the totals had settled was asked about")
	}
	draw(t, src, byName(), &Scope{Count: 4})
	if n.reads != was {
		t.Error("a record its answers had covered was fetched entire")
	}
}

// A query that says what it does NOT want is answered out of a record known
// entire, and narrowed on the way out.
func TestAnExcludingQueryIsAnsweredAndNarrowed(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, twoWays)

	draw(t, src, byName(), &Scope{Count: 4}) // whole records, so entire
	was := n.reads

	out, _ := draw(t, src, &DataSetDescriptor{Sort: []SortLevel{{Field: ".name"}},
		Exclude: Record{{Name: ".size"}}}, &Scope{Count: 4})
	if n.reads != was {
		t.Error("an excluding query over records known entire asked the source")
	}
	for i, f := range out.fields {
		if f.Has(".size") {
			t.Errorf("record %s came back carrying what was excluded: %s",
				out.keys[i], f)
		}
		if !f.Has(".name") {
			t.Errorf("record %s lost what was not excluded: %s", out.keys[i], f)
		}
		// Narrowed, so it is a SUBSET however entire the record behind it is --
		// and it says how many members that record has, so that whoever takes
		// it can tell what is missing from what is not there.
		if out.whole[i] {
			t.Errorf("record %s went out whole with a member taken out of it",
				out.keys[i])
		}
		if out.has[i] != (Totals{Named: 3}) {
			t.Errorf("record %s says it has %+v, and it has three named members",
				out.keys[i], out.has[i])
		}
	}
	// And the record is still held entire: narrowing happens on the way out
	// rather than by throwing anything away, so the next query wanting the lot
	// still has it.
	if got, ok := hot.serve(setKeyOf(src, byName()), nil, &Scope{Count: 4}); !ok ||
		!got.whole {
		t.Error("an excluding query left the records narrowed in the cache")
	}
}

// Letting a sequence go does not let go of what it taught: the records belong
// to the source and the order to the sequence, and neither has gone anywhere.
func TestClosingASequenceKeepsWhatItLearned(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, twoWays)

	v, err := src.Open(byName())
	if err != nil {
		t.Fatal(err)
	}
	fill(t, v, &Scope{Count: 4})
	v.Close()
	was := n.reads

	draw(t, src, byName(), &Scope{Count: 4})
	if n.reads != was {
		t.Error("what a closed sequence had learned was asked for again")
	}
}

// A sink told that an answer has ended may ask for the next one at once, and
// should find this one already here: the run is filed BEFORE Done says so.
func TestAnAnswerIsFiledBeforeTheSinkIsToldItEnded(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, twoWays)

	v, err := src.Open(byName())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	// The sink reads the same stretch again from inside Done, which is what a
	// reader that wants the next screenful the moment this one lands does.
	again := &asking{t: t, src: src}
	if err := v.Read(&Scope{Count: 4}, again); err != nil {
		t.Fatal(err)
	}
	if !again.asked {
		t.Fatal("the sink never asked again, so nothing here is shown")
	}
	if n.reads != 1 {
		t.Errorf("asking again from inside Done took %d readings of the source",
			n.reads)
	}
	if again.got != "0,1,2,3" {
		t.Errorf("asking again from inside Done gave %s", again.got)
	}
}

// asking is a sink that reads the same stretch over again the moment it is told
// this one ended.
type asking struct {
	collector
	t     *testing.T
	src   Source
	asked bool
	got   string
}

func (a *asking) Done(c Complete) {
	a.collector.Done(c)
	if a.asked {
		return
	}
	a.asked = true
	out, _ := draw(a.t, a.src, byName(), &Scope{Count: 4})
	a.got = out.joined()
}

// many is a PSL list of n records, for the answers a screenful does not bound.
func many(n int) string {
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		parts = append(parts, fmt.Sprintf("(name: %q, size: %d)", fmt.Sprintf("f%03d", i), i))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// An answer larger than the cache would ever keep is cut down AS IT ARRIVES,
// rather than assembled whole in memory and cut down afterwards. The asker gets
// every record of it either way.
func TestAnEnormousAnswerIsNotAssembledBeforeItIsCutDown(t *testing.T) {
	c := ownCache(t, 8, 1<<20) // so one run may hold four places
	src, _ := cached(t, many(20))

	out, done := draw(t, src, byName(), &Scope{Count: 99})
	if len(out.keys) != 20 || done.Stop != StopExhausted {
		t.Fatalf("the source sent %d records / %s, and the asker should get all "+
			"of them", len(out.keys), done.Stop)
	}
	if n := len(c.at); n > c.mostPlaces() {
		t.Errorf("it kept %d places of a run bounded to %d", n, c.mostPlaces())
	}
	if len(c.at) == 0 {
		t.Fatal("it kept none of it, so nothing here is shown")
	}

	// What it kept is the BACK of the walk, where the reader is.
	last := out.keys[len(out.keys)-1]
	if !placedText(c, src, byName(), last) {
		t.Errorf("it dropped record %s, which is the one the reader is next to", last)
	}
	if placedText(c, src, byName(), out.keys[0]) {
		t.Errorf("it kept record %s, which the reader has long gone past", out.keys[0])
	}

	// And the run says where it really starts: asking from the beginning again
	// is a miss, because the front of that answer is not here.
	if _, ok := hot.serve(setKeyOf(src, byName()), nil, &Scope{Count: 99}); ok {
		t.Error("a run that kept its back end answered for its front")
	}
	sound(t, c)
}

// An answer larger than the cache would ever keep is cut down AS IT ARRIVES.
// What is held in hand while a long answer streams past is bounded, rather than
// the whole of it being assembled and then found to have been too much.
func TestALongAnswerIsNotAssembledInHand(t *testing.T) {
	f := &filing{scope: &Scope{}, out: &collector{}, most: 4}
	for i := int64(0); i < 100; i++ {
		if err := f.Record(NewInt(i), Record{Named(".name", i)}); err != nil {
			t.Fatal(err)
		}
		if len(f.kept) > f.most {
			t.Fatalf("after %d records it holds %d in hand, bounded to %d",
				i+1, len(f.kept), f.most)
		}
	}
	// And what it holds is the back of the walk, starting past what it dropped.
	if valueText(f.from) != "95" {
		t.Errorf("it kept four of a hundred and says the run starts past %s",
			valueText(f.from))
	}
	if len(f.kept) != 4 || valueText(f.kept[0].id) != "96" {
		t.Errorf("it kept %d records beginning at %s",
			len(f.kept), valueText(f.kept[0].id))
	}
}

// A record with no identity cannot be placed, and an answer holding one is not
// filed at all -- leaving it out would say there was nothing between the
// records either side of it, which is the one thing a run claims.
func TestAnAnswerWithAnUnidentifiedRecordIsNotFiled(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	src := NewCachedSource(&nameless{})

	out, done := draw(t, src, &DataSetDescriptor{}, &Scope{Count: 9})
	if len(out.keys) != 3 || done.Stop != StopExhausted {
		t.Fatalf("the asker got %d records / %s", len(out.keys), done.Stop)
	}
	if len(c.runs) != 0 || len(c.flesh.at) != 0 {
		t.Errorf("it filed %d runs over %d records out of an answer it cannot "+
			"place", len(c.runs), len(c.flesh.at))
	}
}

// nameless is a source that sends a record with no identity in the middle of
// an otherwise ordinary answer.
type nameless struct{}

func (nameless) Open(*DataSetDescriptor) (DataSet, error) { return namelessSet{}, nil }

type namelessSet struct{}

func (namelessSet) Close() {}

func (namelessSet) Read(sc *Scope, out Sink) error {
	out.Ordered()
	for _, id := range []*Value{NewInt(1), nil, NewInt(3)} {
		if err := out.Subset(id, Record{Named(".name", "x")}, Totals{Named: 2}); err != nil {
			return err
		}
	}
	out.Done(Complete{Stop: StopExhausted})
	return nil
}

// setKeyOf and placedText reach into the wrapper the way a test may and nothing
// else should: by asking what it would have keyed things under.
func setKeyOf(src *CachedSource, descriptor *DataSetDescriptor) dataSet {
	return dataSet{source: src.key, set: src.key + "\x00" + dataSetKey(descriptor)}
}

func placedText(c *cache, src *CachedSource, descriptor *DataSetDescriptor, key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.at {
		if strings.HasPrefix(k, setKeyOf(src, descriptor).set) && valueText(e.id) == key {
			return true
		}
	}
	return false
}

// A refusal to open is the source's, and it happens whether or not anything
// would have been answered from the cache. An ordering that is quietly a little
// different corrupts every answer after it -- so a sequence that cannot be
// produced is refused at the one point that can refuse it.
func TestOpeningIsRefusedByTheSourceAndNotByTheCache(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, twoWays)

	bad := &DataSetDescriptor{Sort: []SortLevel{{Field: ".name", Level: Level{Collation: "klingon"}}}}
	if v, err := src.Open(bad); err == nil {
		v.Close()
		t.Fatal("a collation nothing carries was opened")
	}
	if n.opens != 0 {
		t.Error("the child said yes to it")
	}
}

// A scope refused by the source is an answer and not a run: nothing is
// guaranteed between anything.
func TestARefusedScopeIsNotHeld(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, twoWays)

	out, done := draw(t, src, byName(), &Scope{After: NewSymbol("nowhere"), Count: 2})
	if done.Error == "" {
		t.Fatalf("a scope past a record of no sequence was answered: %s", out.joined())
	}
	if len(c.runs) != 0 || c.flesh.cost != 0 {
		t.Errorf("a refusal left %d runs and %d of values", len(c.runs), c.flesh.cost)
	}
}

// The wrapper keeps its own copy of the run, because a source that reuses the
// slice it hands each record in would otherwise rewrite what was filed.
func TestWhatIsFiledIsNotTheSourcesOwnSlice(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	shared := Record{Named(".name", "first")}
	src := NewCachedSource(&reusing{bag: shared})

	draw(t, src, &DataSetDescriptor{}, &Scope{Count: 2})
	shared[0] = Named("name", "rewritten")

	for _, want := range []string{"first", "second"} {
		found := false
		for _, r := range c.flesh.at {
			if v := r.fields.Get(".name"); v != nil && v.Str == want {
				found = true
			}
		}
		if !found {
			t.Errorf("no record holds %q: the source's own slice was filed", want)
		}
	}
}

// reusing is a source that hands every record the same slice, filled in again,
// which is what a source reading a file into one buffer does.
type reusing struct{ bag Record }

func (r *reusing) Open(*DataSetDescriptor) (DataSet, error) { return &reusingSet{src: r}, nil }

type reusingSet struct{ src *reusing }

func (s *reusingSet) Close() {}

func (s *reusingSet) Read(sc *Scope, out Sink) error {
	out.Ordered()
	for i, name := range []string{"first", "second"} {
		s.src.bag[0] = Named(".name", name)
		if err := out.Subset(NewInt(int64(i)), s.src.bag, Totals{Named: 2}); err != nil {
			return err
		}
	}
	out.Done(Complete{Stop: StopExhausted})
	return nil
}

// --- the order known, the values not --------------------------------------

// forget takes what is known about some records out of the flesh cache and
// leaves their places exactly where they are, which is the state the two halves
// below are both about: the order outlives the values.
func forget(c *cache, src *CachedSource, ids ...int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		if r := c.flesh.get(src.key, NewInt(id)); r != nil {
			c.flesh.drop(r)
		}
	}
}

// A place whose record has been let go of is answered by asking about THAT
// RECORD, not by reading the stretch again. Where each of them stands is not in
// question -- only what they hold.
func TestAValueOnlyMissAsksAboutTheRecordsAndNotTheStretch(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, many(20))

	draw(t, src, byName(), &Scope{Count: 6})
	forget(c, src, 2, 4)
	reads, sent, opens := n.reads, n.sent, n.opens

	out, done := draw(t, src, byName(), &Scope{Count: 6})

	if out.joined() != "0,1,2,3,4,5" || done.Stop != StopFilled {
		t.Fatalf("it gave %s / %s", out.joined(), done.Stop)
	}
	if n.reads != reads+1 {
		t.Errorf("it asked the source %d times", n.reads-reads)
	}
	// Two records went, so two came back -- not the six the scope covers.
	if n.sent-sent != 2 {
		t.Errorf("it fetched %d records to replace two", n.sent-sent)
	}
	// Two: this reading's own sequence, and the narrow question beside it.
	if n.opens != opens+2 {
		t.Errorf("it opened %d sequences", n.opens-opens)
	}
	// And it placed nothing: those records already stand somewhere, and the
	// order an identity filter produced them in is nobody's.
	if len(c.sets) != 1 || len(c.runs) != 1 {
		t.Errorf("the top-up left %d runs over %d sequences", len(c.runs), len(c.sets))
	}
	sound(t, c)
}

// picky refuses a sequence whose filter tests identity, which is what a source
// that cannot answer "these particular records" looks like.
type picky struct{ child Source }

func (p *picky) Open(descriptor *DataSetDescriptor) (DataSet, error) {
	if descriptor.Filter != nil && descriptor.Filter.Op == OpID {
		return nil, fmt.Errorf("i do not answer questions about identities")
	}
	return p.child.Open(descriptor)
}

// The narrow question is an optimisation, so a source that will not answer one
// costs the reader nothing but the ordinary read.
func TestASourceThatWillNotAnswerNarrowlyIsReadTheOrdinaryWay(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	n := &counting{child: &picky{child: mustPSL(t, many(20))}}
	src := NewCachedSource(n)

	draw(t, src, byName(), &Scope{Count: 6})
	forget(c, src, 2)

	out, done := draw(t, src, byName(), &Scope{Count: 6})

	if out.joined() != "0,1,2,3,4,5" || done.Stop != StopFilled {
		t.Errorf("it gave %s / %s", out.joined(), done.Stop)
	}
	sound(t, c)
}

// --- places ---------------------------------------------------------------

// A placer is a collector with somewhere to put places.
type placer struct {
	*collector
	put     []string
	carried []Record
	settled *Complete
}

func (p *placer) Place(id *Value, fields Record) error {
	if p.settled != nil {
		panic("a place arrived after the order was settled")
	}
	p.put = append(p.put, valueText(id))
	p.carried = append(p.carried, fields)
	return nil
}

func (p *placer) Placed(c Complete) {
	if p.ended {
		panic("the order was settled after the scope was done")
	}
	p.settled = &c
}

func (p *placer) placed() string { return strings.Join(p.put, ",") }

func drawPlacing(t *testing.T, src Source, descriptor *DataSetDescriptor, sc *Scope) *placer {
	t.Helper()
	v, err := src.Open(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	out := &placer{collector: &collector{}}
	if err := v.Read(sc, out); err != nil {
		t.Fatal(err)
	}
	return out
}

// A sink with somewhere to put places gets the ORDER at once, and the records
// it can be given, and nothing is fetched for the rest: what is worth asking
// for is then the reader's to decide.
func TestASinkThatTakesPlacesIsGivenTheOrderAndAsksForNothing(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	src, n := cached(t, many(20))

	draw(t, src, byName(), &Scope{Count: 6})
	forget(c, src, 2, 4)
	reads := n.reads

	out := drawPlacing(t, src, byName(), &Scope{Count: 6})

	if got := out.placed(); got != "2,4" {
		t.Errorf("it placed %s", got)
	}
	if got := out.joined(); got != "0,1,3,5" {
		t.Errorf("it gave records for %s", got)
	}
	if n.reads != reads {
		t.Errorf("it asked the source %d times over", n.reads-reads)
	}
	// The order is settled before the scope is done, and says the same thing.
	if out.settled == nil {
		t.Fatal("the order was never settled, so the reader cannot lay it out")
	}
	if out.settled.Stop != out.done.Stop || !Equal(out.settled.Watermark, out.done.Watermark) {
		t.Errorf("the order ended at %s/%s and the scope at %s/%s",
			out.settled.Stop, valueText(out.settled.Watermark),
			out.done.Stop, valueText(out.done.Watermark))
	}
	sound(t, c)
}

// An answer with no places in it settles its order at the end like any other,
// and says so once.
func TestAnAnswerWithNoPlacesSettlesItsOrderOnlyOnce(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, many(20))
	draw(t, src, byName(), &Scope{Count: 6})

	out := drawPlacing(t, src, byName(), &Scope{Count: 6})

	if got := out.placed(); got != "" {
		t.Errorf("it placed %s, and everything was known", got)
	}
	if out.settled != nil {
		t.Error("it settled the order separately with nothing to settle early")
	}
	if got := out.joined(); got != "0,1,2,3,4,5" {
		t.Errorf("it gave %s", got)
	}
}

// A place carries what is KNOWN, not what this query asked for. A place with
// nothing in it is still a place -- and one carrying the fields that decide the
// sequence is one a merging source can put somewhere.
func TestAPlaceCarriesWhatIsKnownRatherThanWhatWasAsked(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, many(20))

	// Read the whole records, then forget one of them entirely and teach the
	// cache one field of it back -- which is what a stretch read for one field
	// leaves behind.
	draw(t, src, byName(), &Scope{Count: 3})
	forget(c, src, 1)
	c.learnValues(src.key, []*cachedRecord{
		newRecord("", NewInt(1), Record{Named(".name", "f001")}, Totals{Named: 2}, 0),
	})

	// A query that names only `.size` still gets `.name` on the place.
	sized := &DataSetDescriptor{Sort: []SortLevel{{Field: ".name"}}, Fields: Record{{Name: ".size"}}}
	out := drawPlacing(t, src, sized, &Scope{Count: 3})

	if got := out.placed(); got != "1" {
		t.Fatalf("it placed %s", got)
	}
	if got := out.carried[0].String(); got != `{ .name "f001" }` {
		t.Errorf("the place carries %s", got)
	}
	// And one known of nothing at all is placed carrying nothing.
	forget(c, src, 2)
	if again := drawPlacing(t, src, sized, &Scope{Count: 3}); again.placed() != "1,2" {
		t.Errorf("it placed %s", again.placed())
	}
}

// stingy answers a narrow question about particular records and holds one of
// them back -- a record that has gone since the order was learned. Asked the
// ordinary way it answers in full, which is what makes the fallback visible.
type stingy struct {
	child Source
	holds int64
}

func (p *stingy) Open(descriptor *DataSetDescriptor) (DataSet, error) {
	v, err := p.child.Open(descriptor)
	if err != nil {
		return nil, err
	}
	return &stingySet{src: p, descriptor: descriptor, child: v}, nil
}

type stingySet struct {
	src        *stingy
	descriptor *DataSetDescriptor
	child      DataSet
}

func (s *stingySet) Close() { s.child.Close() }

func (s *stingySet) Read(sc *Scope, out Sink) error {
	if s.descriptor.Filter == nil || s.descriptor.Filter.Op != OpID {
		return s.child.Read(sc, out)
	}
	return s.child.Read(sc, &stingySink{src: s.src, out: out})
}

type stingySink struct {
	src *stingy
	out Sink
}

func (k *stingySink) Ordered() { k.out.Ordered() }
func (k *stingySink) Record(id *Value, f Record) error {
	if Equal(id, NewInt(k.src.holds)) {
		return nil
	}
	return k.out.Record(id, f)
}
func (k *stingySink) Subset(id *Value, f Record, has Totals) error {
	if Equal(id, NewInt(k.src.holds)) {
		return nil
	}
	return k.out.Subset(id, f, has)
}
func (k *stingySink) Done(c Complete) { k.out.Done(c) }

// A narrow question that comes back short leaves the scope to be read the
// ordinary way. It was only ever an optimisation, so falling short of one costs
// the reader an ordinary read and never a hollow record.
func TestATopUpThatFallsShortIsReadTheOrdinaryWay(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	held := &stingy{child: mustPSL(t, many(20)), holds: 99} // nothing held back
	src := NewCachedSource(held)

	draw(t, src, byName(), &Scope{Count: 6})
	forget(c, src, 2, 4)
	held.holds = 4 // and now it will not say anything about record 4

	out, done := draw(t, src, byName(), &Scope{Count: 6})

	if out.joined() != "0,1,2,3,4,5" || done.Stop != StopFilled {
		t.Errorf("it gave %s / %s", out.joined(), done.Stop)
	}
	sound(t, c)
}

// What a query asked to leave out is left out of its places too. A place says
// less than a result about how much of the record there is; it does not say
// more about which fields the asker wanted to see.
func TestAPlaceLeavesOutWhatTheQueryExcluded(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, many(20))

	draw(t, src, byName(), &Scope{Count: 2})
	forget(c, src, 1)
	// Two of its three named members known, so a question about the third is a
	// question -- and the record is placed rather than sent.
	c.learnValues(src.key, []*cachedRecord{newRecord("", NewInt(1),
		Record{Named(".name", "f001"), Named(".size", 1)}, Totals{Named: 3}, 0)})

	out := drawPlacing(t, src, &DataSetDescriptor{
		Sort:    []SortLevel{{Field: ".name"}},
		Fields:  Record{{Name: ".modified"}},
		Exclude: Record{{Name: ".name"}},
	}, &Scope{Count: 2})

	if got := out.placed(); got != "1" {
		t.Fatalf("it placed %s", got)
	}
	if got := out.carried[0].String(); got != "{ .size 1 }" {
		t.Errorf("the place carries %s, and .name was asked against", got)
	}
}

// **A run with no bound reaches the end of the sequence, and an answer that stopped
// short has not earned one.**
//
// A missing watermark means the answer ran out of records: it has nowhere to point
// at. So an answer that stopped for any other reason and named no watermark was filed
// as reaching the end -- and a run bounded at neither end is read as the whole
// sequence in one piece.
//
// The cost is quiet and large. A source of a thousand rows answering a window of
// three, saying `filled` and naming no watermark, was counted as a sequence of
// three -- so every reader drew a true thumb against a figure wrong by a factor of
// three hundred.
func TestAnAnswerThatStoppedShortIsNotTheWholeSequence(t *testing.T) {
	ownCache(t, 64, 4096)

	body := &quietStopper{n: 1000}
	src := NewCachedSource(body)
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var got collector
	if err := set.Read(&Scope{Count: 3}, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.keys) != 3 {
		t.Fatalf("the window holds %d rows", len(got.keys))
	}
	if n := CountOf(set); n.Exact {
		t.Errorf("after a window of three of a thousand it counts %v exactly; the"+
			" answer said it had stopped short and named no watermark", n)
	}

	// And the run is still USABLE: the three rows are held, so asking for them again
	// does not reach the source.
	was := body.reads
	var again collector
	if err := set.Read(&Scope{Count: 3}, &again); err != nil {
		t.Fatal(err)
	}
	if body.reads != was {
		t.Errorf("the window was asked for again: %d reads became %d", was, body.reads)
	}
	if len(again.keys) != 3 {
		t.Errorf("reading it again holds %d rows", len(again.keys))
	}
}

// quietStopper answers a window and says it stopped short WITHOUT saying where --
// which an application may do, and which says less than it could rather than more.
type quietStopper struct {
	n     int
	reads int
}

func (q *quietStopper) Open(*DataSetDescriptor) (DataSet, error) {
	return &quietStopperSet{src: q}, nil
}

type quietStopperSet struct{ src *quietStopper }

func (s *quietStopperSet) Close() {}

func (s *quietStopperSet) Read(sc *Scope, out Sink) error {
	s.src.reads++
	out.Ordered()
	sent := 0
	for i := 0; i < s.src.n; i++ {
		if sc != nil && sc.Count > 0 && sent >= sc.Count {
			// Stopped short, and saying nothing about where.
			out.Done(Complete{Stop: StopFilled})
			return nil
		}
		if err := out.Record(NewInt(int64(i)), Record{Named("name", fmt.Sprint(i))}); err != nil {
			return err
		}
		sent++
	}
	out.Done(Complete{Stop: StopExhausted})
	return nil
}
