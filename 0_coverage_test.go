package serval

// What a sequence depends on: which fields decide it, and which stretches of it
// are held.

import (
	"strings"
	"testing"
)

func names(ss []string) string { return strings.Join(ss, ",") }

// The three groups, and what falls in each.
func TestFieldsAreGroupedByThePartTheyPlay(t *testing.T) {
	spec := &Spec{
		Sort: []SortLevel{{Field: ".name"}, {Field: ".size"}},
		Filter: &Filter{Op: OpAnd, Children: []*Filter{
			{Op: OpEq, Field: ".kind"},
			{Op: OpNot, Children: []*Filter{{Op: OpStarts, Field: ".name"}}},
			{Op: OpHas, Field: ".tags"},
			{Op: OpID}, // names no field: an identity is not one
		}},
		Fields: Record{
			{Name: ".name"}, {Name: ".size"}, {Name: ".kind"}, // all decided by
			{Name: ".modified"}, {Name: ".thumbnail"}, // shown, and nothing more
		},
	}
	r := spec.Roles()

	if got := names(r.Sort); got != ".name,.size" {
		t.Errorf("the sort levels are %s", got)
	}
	// In the order the tree names them, once each, and `id` names none.
	if got := names(r.Filter); got != ".kind,.name,.tags" {
		t.Errorf("the filter's fields are %s", got)
	}
	// `.name` is sorted AND filtered, and is a detail field in neither case: a
	// field that decides where a record stands is not decoration.
	if got := names(r.Detail); got != ".modified,.thumbnail" {
		t.Errorf("the detail fields are %s", got)
	}
	if r.Whole {
		t.Error("a query naming its fields was taken as asking for whole records")
	}
}

// A field appears once per group however often it is named, because this is
// stated over and over as a sequence is read and a list that reshuffles itself
// makes every statement look like a change.
func TestAFieldIsNamedOncePerGroup(t *testing.T) {
	spec := &Spec{
		Sort: []SortLevel{{Field: ".name"}, {Field: ".name"}},
		Filter: &Filter{Op: OpAnd, Children: []*Filter{
			{Op: OpEq, Field: ".kind"},
			{Op: OpNe, Field: ".kind"},
			{Op: OpOr, Children: []*Filter{{Op: OpGt, Field: ".kind"}}},
		}},
	}
	r := spec.Roles()
	if got := names(r.Sort); got != ".name" {
		t.Errorf("one level named twice came out as %s", got)
	}
	if got := names(r.Filter); got != ".kind" {
		t.Errorf("one field tested three times came out as %s", got)
	}
}

// A query that named no fields asked for whole records, so what it shows cannot
// be listed -- only what it asked to be left out.
func TestAQueryForWholeRecordsShowsWhatItDidNotExclude(t *testing.T) {
	r := (&Spec{
		Sort:    []SortLevel{{Field: ".name"}},
		Exclude: Record{{Name: ".blob"}},
	}).Roles()

	if !r.Whole {
		t.Fatal("a query naming no fields was not taken as asking for whole records")
	}
	if got := names(r.Detail); got != ".blob" {
		t.Errorf("the nameable detail fields are %s, and only the exclusion is", got)
	}
	for _, c := range []struct {
		name  string
		shows bool
	}{
		{".name", false},    // decided by, so not shown-and-nothing-more
		{".modified", true}, // never named, and a whole record carries it
		{".blob", false},    // asked for, and asked against
		{".anything", true}, // likewise unnamed
	} {
		if got := r.Shows(c.name); got != c.shows {
			t.Errorf("a whole-record query shows %s: %v", c.name, got)
		}
	}
}

// Decides is the skeleton question: could a change to this field move a record,
// or take it out of the sequence?
func TestDecidesIsTheSkeletonQuestion(t *testing.T) {
	r := (&Spec{
		Sort:   []SortLevel{{Field: ".name"}},
		Filter: &Filter{Op: OpAnd, Children: []*Filter{{Op: OpGe, Field: ".size"}}},
		Fields: Record{{Name: ".name"}, {Name: ".size"}, {Name: ".modified"}},
	}).Roles()

	for _, c := range []struct {
		name    string
		decides bool
		shows   bool
	}{
		{".name", true, false},       // sorted
		{".size", true, false},       // filtered
		{".modified", false, true},   // carried and shown, deciding nothing
		{".thumbnail", false, false}, // not asked for at all
	} {
		if got := r.Decides(c.name); got != c.decides {
			t.Errorf("%s decides: %v", c.name, got)
		}
		if got := r.Shows(c.name); got != c.shows {
			t.Errorf("%s shows: %v", c.name, got)
		}
	}
}

// `id` names no field however it was built. An identity travels beside a
// record's fields rather than among them, so a change to what a record CONTAINS
// never changes what it is called -- and a record that happens to carry a field
// called `key` is answering an ordinary question about an ordinary field.
func TestAnIdentityTestNamesNoField(t *testing.T) {
	r := (&Spec{Filter: &Filter{Op: OpAnd, Children: []*Filter{
		{Op: OpID, Field: "key", Values: []*Value{NewSymbol("left/1")}},
		{Op: OpEq, Field: ".kind"},
	}}}).Roles()

	if got := names(r.Filter); got != ".kind" {
		t.Errorf("an identity test named %s", got)
	}
	if r.Decides("key") {
		t.Error("a field called key was taken for the identity that was tested")
	}
}

// A field both asked for and asked against is not shown: an exclusion takes it
// out of what crosses, so nothing depends on it.
func TestAFieldAskedForAndAgainstIsNotShown(t *testing.T) {
	r := (&Spec{
		Sort:    []SortLevel{{Field: ".name"}},
		Fields:  Record{{Name: ".name"}, {Name: ".size"}, {Name: ".blob"}},
		Exclude: Record{{Name: ".blob"}},
	}).Roles()

	if got := names(r.Detail); got != ".size" {
		t.Errorf("the detail fields are %s", got)
	}
	if r.Shows(".blob") {
		t.Error("a field taken out by an exclusion is shown")
	}
}

// A spec that is not there depends on nothing.
func TestNoSpecDependsOnNothing(t *testing.T) {
	var s *Spec
	r := s.Roles()
	if len(r.Sort) != 0 || len(r.Filter) != 0 || len(r.Detail) != 0 || r.Whole {
		t.Errorf("a spec that is not there came out as %+v", r)
	}
}

// --- what is held ---------------------------------------------------------

// covered is the extents of one sequence, as text, so a case reads as what it
// is about.
func covered(t *testing.T, src *CachedSource, spec *Spec) string {
	t.Helper()
	parts := []string{}
	for _, e := range src.Covers(spec) {
		parts = append(parts, valueText(e.First)+".."+valueText(e.Last))
	}
	return strings.Join(parts, " ")
}

// What is covered is what is held: the records at the ends of each run, both
// ends inclusive.
func TestCoverageIsWhatIsHeld(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, many(20))

	if got := covered(t, src, byName()); got != "" {
		t.Errorf("a sequence nothing has read covers %s", got)
	}

	draw(t, src, byName(), &Scope{Count: 4})
	if got := covered(t, src, byName()); got != "0..3" {
		t.Errorf("after four records it covers %s", got)
	}

	// Scrolling on extends the one run rather than making another.
	draw(t, src, byName(), &Scope{After: NewInt(3), Count: 4})
	if got := covered(t, src, byName()); got != "0..7" {
		t.Errorf("after eight it covers %s", got)
	}

	// And a stretch somewhere else is a second extent.
	draw(t, src, byName(), &Scope{After: NewInt(14), Count: 3})
	if got := covered(t, src, byName()); got != "0..7 15..17" {
		t.Errorf("with a gap between them it covers %s", got)
	}
}

// Every sequence covers its own stretch. Two orders over one source hold two
// different sets of records in two different orders, and neither is the other's
// business.
func TestEachSequenceCoversItsOwn(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, twoWays)

	draw(t, src, byName(), &Scope{Count: 2})
	if got := covered(t, src, bySize()); got != "" {
		t.Errorf("a sequence nothing has read covers %s", got)
	}
	if got := covered(t, src, byName()); got != "0..1" {
		t.Errorf("the sequence that was read covers %s", got)
	}
}

// The same coverage comes out the same way every time. It is stated over and
// over as a sequence is read, and an arrangement that shuffled would make every
// statement look like a revision.
func TestCoverageIsStatedTheSameWayTwice(t *testing.T) {
	ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, many(20))

	for _, sc := range []*Scope{
		{Count: 2},
		{After: NewInt(9), Count: 2},
		{After: NewInt(14), Count: 2},
		{After: NewInt(4), Count: 2},
	} {
		draw(t, src, byName(), sc)
	}
	want := covered(t, src, byName())
	if strings.Count(want, "..") != 4 {
		t.Fatalf("four stretches apart came out as %s", want)
	}
	for i := 0; i < 8; i++ {
		if got := covered(t, src, byName()); got != want {
			t.Fatalf("stated again it came out %s, where it was %s", got, want)
		}
	}
}

// Coverage follows what is held, so what is given up stops being covered.
func TestWhatIsGivenUpStopsBeingCovered(t *testing.T) {
	c := ownCache(t, 1<<20, 1<<20)
	src, _ := cached(t, many(20))

	draw(t, src, byName(), &Scope{Count: 6})
	if got := covered(t, src, byName()); got != "0..5" {
		t.Fatalf("it covers %s", got)
	}

	// The front of the run gives way, and the extent moves in with it -- which
	// is the rule the whole arrangement stands on: what is stated is a superset
	// of what is depended on, never less.
	c.mu.Lock()
	c.drop(c.sets[setKeyOf(src, byName()).set][0], true)
	c.mu.Unlock()

	if got := covered(t, src, byName()); got != "1..5" {
		t.Errorf("after the first record went it covers %s", got)
	}
}
