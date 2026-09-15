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
	child Source
	opens int
	reads int
	sent  int
}

func (c *counting) Open(spec *Spec) (DataSet, error) {
	v, err := c.child.Open(spec)
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
func (k *countingSink) Subset(id *Value, f Record) error {
	k.src.sent++
	return k.out.Subset(id, f)
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
func draw(t *testing.T, src Source, spec *Spec, sc *Scope) (*collector, Complete) {
	t.Helper()
	v, err := src.Open(spec)
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

	named := &Spec{Sort: []SortLevel{{Field: ".name"}}, Fields: Record{{Name: ".name"}}}
	draw(t, src, named, &Scope{Count: 4})
	was := n.reads

	// The same fields again: held.
	draw(t, src, named, &Scope{Count: 4})
	if n.reads != was {
		t.Error("the same question was asked of the source twice")
	}

	// One field more: not held, so it is asked -- and then it is.
	both := &Spec{Sort: []SortLevel{{Field: ".name"}},
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
	if _, _, ok := hot.serve(setKeyOf(src, byName()), nil, &Scope{Count: 99}); ok {
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

	out, done := draw(t, src, &Spec{}, &Scope{Count: 9})
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

func (nameless) Open(*Spec) (DataSet, error) { return namelessSet{}, nil }

type namelessSet struct{}

func (namelessSet) Close() {}

func (namelessSet) Read(sc *Scope, out Sink) error {
	out.Ordered()
	for _, id := range []*Value{NewInt(1), nil, NewInt(3)} {
		if err := out.Subset(id, Record{Named(".name", "x")}); err != nil {
			return err
		}
	}
	out.Done(Complete{Stop: StopExhausted})
	return nil
}

// setKeyOf and placedText reach into the wrapper the way a test may and nothing
// else should: by asking what it would have keyed things under.
func setKeyOf(src *CachedSource, spec *Spec) dataSet {
	return dataSet{source: src.key, set: src.key + "\x00" + dataSetKey(spec)}
}

func placedText(c *cache, src *CachedSource, spec *Spec, key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.at {
		if strings.HasPrefix(k, setKeyOf(src, spec).set) && valueText(e.id) == key {
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

	bad := &Spec{Sort: []SortLevel{{Field: ".name", Level: Level{Collation: "klingon"}}}}
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

	draw(t, src, &Spec{}, &Scope{Count: 2})
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

func (r *reusing) Open(*Spec) (DataSet, error) { return &reusingSet{src: r}, nil }

type reusingSet struct{ src *reusing }

func (s *reusingSet) Close() {}

func (s *reusingSet) Read(sc *Scope, out Sink) error {
	out.Ordered()
	for i, name := range []string{"first", "second"} {
		s.src.bag[0] = Named(".name", name)
		if err := out.Subset(NewInt(int64(i)), s.src.bag); err != nil {
			return err
		}
	}
	out.Done(Complete{Stop: StopExhausted})
	return nil
}
