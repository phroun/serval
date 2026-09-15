package serval

// How many members a record has, and what knowing that settles.
//
// Three ways a question about a field gets an answer without anyone being
// asked: the record carries it, the record carries it as an ABSENCE, or the
// TOTALS leave nothing for it to be. The first was always there; the other two
// are what these are about.

import "testing"

func TestAnOrderedMemberIsNamedByItsPosition(t *testing.T) {
	for _, c := range []struct {
		name string
		want int
		is   bool
	}{
		{"0", 0, true},
		{"1", 1, true},
		{"42", 42, true},
		{".0", 0, true}, // a source that marks its members with a dot
		{".7", 7, true},

		// The canonical spelling and no other: `007` is a name that happens to
		// be digits, the same distinction a head makes between the two.
		{"007", 0, false},
		{".007", 0, false},
		{"-1", 0, false},
		{"+1", 0, false},
		{"1x", 0, false},
		{"", 0, false},
		{".", 0, false},
		{".name", 0, false},
		{"name", 0, false},
	} {
		got, is := MemberIndex(c.name)
		if is != c.is || (is && got != c.want) {
			t.Errorf("%q read as index %d/%v, want %d/%v", c.name, got, is, c.want, c.is)
		}
	}
}

// Tally counts the two apart, and counts no absence: a member sent as undefined
// is a name the record has NOT got, which is knowledge about it rather than a
// member of it.
func TestTallyCountsMembersAndNotAbsences(t *testing.T) {
	for _, c := range []struct {
		what string
		r    Record
		want Totals
	}{
		{"nothing", Record{}, Totals{}},
		{"two named", Record{Named(".name", "a"), Named(".size", 1)}, Totals{Named: 2}},
		{"two ordered", Record{Named(".0", "a"), Named(".1", "b")}, Totals{Ordered: 2}},
		{"some of each", Record{Named(".0", "a"), Named("key", 1), Named(".size", 2)},
			Totals{Ordered: 1, Named: 2}},
		{"an absence among them",
			Record{Named(".name", "a"), {Name: ".size"}}, Totals{Named: 1}},
		{"nothing but absences",
			Record{{Name: ".name"}, {Name: ".0"}}, Totals{}},
	} {
		if got := Tally(c.r); got != c.want {
			t.Errorf("%s: %s tallies %+v, want %+v", c.what, c.r, got, c.want)
		}
	}
}

// --- what the totals settle ----------------------------------------------

// held is one record as the cache knows it: what it carries, and how many
// members it has altogether.
func held(fields Record, has Totals) *cachedRecord {
	return newRecord("s", NewInt(1), fields, has, 0)
}

func TestARecordAnswersWhatItsTotalsSettle(t *testing.T) {
	// Eight named members, four of them known. Nothing is settled but what is
	// here: four more could be anything.
	some := held(Record{
		Named(".a", 1), Named(".b", 2), Named(".c", 3), Named(".d", 4),
	}, Totals{Named: 8})

	for _, c := range []struct {
		what string
		want string
		yes  bool
	}{
		{"one it carries", ".a", true},
		{"one it has not seen", ".e", false},
	} {
		if got := some.answers(Record{{Name: c.want}}); got != c.yes {
			t.Errorf("four of eight: %s (%s) answered %v", c.what, c.want, got)
		}
	}

	// The same four, and the record has only four. Now every other name is
	// settled: there is nothing left for one to be.
	all := held(Record{
		Named(".a", 1), Named(".b", 2), Named(".c", 3), Named(".d", 4),
	}, Totals{Named: 4})

	if !all.answers(Record{{Name: ".e"}}) {
		t.Error("four of four did not settle a fifth name")
	}
	if !all.entire() {
		t.Error("four of four is not the record entire")
	}
	if !all.answers(nil) {
		t.Error("a record known entire would not answer for the whole record")
	}
}

// An ordered member is settled by the count alone, there being nothing to know
// about which ones they are: three of them are 0, 1 and 2.
func TestAnOrderedMemberIsSettledByTheCountAlone(t *testing.T) {
	// Three ordered members and five named, of which ONE named is known -- so
	// nothing about the names is settled, and everything about the positions
	// past the third is.
	r := held(Record{Named(".name", "x")}, Totals{Ordered: 3, Named: 5})

	for _, c := range []struct {
		name string
		yes  bool
	}{
		{".0", false}, // inside the count, and not here: a real question
		{".2", false},
		{".3", true}, // past the count: not there, and said so without asking
		{".9", true},
		{".name", true},   // carried
		{".other", false}, // a name, and four more could be anything
	} {
		if got := r.answers(Record{{Name: c.name}}); got != c.yes {
			t.Errorf("three ordered and one of five named: %s answered %v",
				c.name, got)
		}
	}
}

// A name asked about and not there is kept as an absence, and answers the next
// question about it -- without being counted as a member, which would say the
// record had one it has not.
func TestAnAbsenceIsAnAnswerAndNotAMember(t *testing.T) {
	r := held(Record{Named(".name", "x"), {Name: ".thumbnail"}}, Totals{Named: 8})

	if !r.answers(Record{{Name: ".thumbnail"}}) {
		t.Error("a name filed as not there was asked about again")
	}
	if r.knows.Named != 1 {
		t.Errorf("one member and one absence count as %d members", r.knows.Named)
	}
	if r.entire() {
		t.Error("one of eight, with an absence beside it, counts as the lot")
	}
}

// The two counts answer two questions, and neither stands in for the other: a
// record whose named members are all known still has ordered ones nobody has
// seen, and is not entire -- while any NAME is settled all the same, there
// being nothing left for one to be.
func TestTheTwoCountsAnswerTwoQuestions(t *testing.T) {
	r := held(Record{Named(".a", 1), Named(".b", 2)}, Totals{Ordered: 3, Named: 2})

	if r.entire() {
		t.Error("three ordered members, none of them seen, and it counts as the lot")
	}
	if r.answers(nil) {
		t.Error("a record not known entire answered for the whole record")
	}
	if !r.answers(Record{{Name: ".c"}}) {
		t.Error("two of two named did not settle a third name")
	}
	if r.answers(Record{{Name: ".1"}}) {
		t.Error("it answered for an ordered member it has never seen")
	}
	if !r.answers(Record{{Name: ".3"}}) {
		t.Error("it did not settle an index past the count")
	}
}

// The newer statement of how many members there are stands. A source counting a
// record it has just read is better placed than one that counted it last time,
// and neither is guessing.
func TestTheNewerCountStands(t *testing.T) {
	c := roomy()
	ds := over("files")

	filed(c, ds, nil, slimsOf(Totals{Named: 5}, 1, 2, ".name"))
	if known(c, "files", 1).entire() {
		t.Fatal("one of five counts as the lot")
	}

	// The second answer says there are two, and brings the second.
	filed(c, ds, nil, slimsOf(Totals{Named: 2}, 1, 2, ".size"))
	r := known(c, "files", 1)
	if r.has.Named != 2 {
		t.Errorf("after an answer saying two, it says the record has %d", r.has.Named)
	}
	if !r.entire() {
		t.Errorf("two of two left %s of %+v", r.fields, r.has)
	}
	sound(t, c)
}

// Subsets that between them cover a record leave it entire, without anybody
// deciding to: it is what the two counts say.
func TestSubsetsThatCoverARecordLeaveItEntire(t *testing.T) {
	c := roomy()
	ds := over("files")

	filed(c, ds, nil, slimsOf(Totals{Named: 2}, 1, 3, ".name"))
	if known(c, "files", 1).entire() {
		t.Fatal("one of two counts as both")
	}
	if _, _, ok := asked(c, ds, nil, &Scope{Count: 3}); ok {
		t.Fatal("a record known in part answered for the whole record")
	}

	filed(c, ds, nil, slimsOf(Totals{Named: 2}, 1, 3, ".size"))
	r := known(c, "files", 1)
	if !r.entire() {
		t.Errorf("two answers covering two members left %s of %+v", r.fields, r.has)
	}
	if _, _, ok := asked(c, ds, nil, &Scope{Count: 3}); !ok {
		t.Error("a record its answers had covered would not answer for the whole")
	}
	sound(t, c)
}

// The totals cross every source that passes a record on.
//
// A source built on others says of a record it relayed exactly what the source
// below said of it, and how many members that record has is part of what was
// said. A subset that arrived at the top saying nothing is a subset nobody can
// ever finish.
func TestTheTotalsCrossASourceThatRelays(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	narrow := &Spec{
		Sort:   []SortLevel{{Field: ".name"}},
		Fields: Record{{Name: ".name"}},
	}
	// A `twoWays` record carries three members: key, .name and .size.
	want := Totals{Named: 3}

	for _, c := range []struct {
		what string
		of   func() Source
	}{
		{"straight from the source", func() Source { return mustPSL(t, twoWays) }},
		{"through an amended source", func() Source {
			return NewAmendedSource(mustPSL(t, twoWays))
		}},
		{"through a composed source", func() Source {
			s, err := NewComposedSource(Include{Name: "a", Source: mustPSL(t, twoWays)})
			if err != nil {
				t.Fatal(err)
			}
			return s
		}},
		{"through a cached source", func() Source {
			return NewCachedSource(mustPSL(t, twoWays))
		}},
	} {
		out, _ := draw(t, c.of(), narrow, &Scope{Count: 4})
		if len(out.keys) != 4 {
			t.Errorf("%s: %d records came out", c.what, len(out.keys))
			continue
		}
		for i := range out.keys {
			if out.whole[i] {
				t.Errorf("%s: record %s came out whole, and one member of three "+
					"was asked for", c.what, out.keys[i])
			}
			if out.has[i] != want {
				t.Errorf("%s: record %s says it has %+v, and it has %+v",
					c.what, out.keys[i], out.has[i], want)
			}
		}
	}
}

// An answer that says the record has MORE than it sent is a subset however many
// fields it carries, and stays one.
func TestASubsetThatSaysThereIsMoreStaysASubset(t *testing.T) {
	c := roomy()
	filed(c, over("files"), nil, slimsOf(Totals{Named: 9}, 1, 3, ".name", ".size"))

	r := known(c, "files", 1)
	if r.entire() {
		t.Error("two of nine counts as the lot")
	}
	if r.answers(Record{{Name: ".other"}}) {
		t.Error("two of nine settled a name it has never seen")
	}
}
