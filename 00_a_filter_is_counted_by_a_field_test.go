package serval

// A census: how many records this filter admits, for each value of this field.
//
// The thing being checked throughout is that a census makes THREE claims and
// keeps them apart -- each group's count, whether those are all the groups, and
// what it left out -- because a slice of exact counts that cannot say "there may
// be more groups" is the failure this type exists to prevent.

import (
	"fmt"
	"testing"
)

// people is a small population with a `parent`, a `kind` and one row that has
// neither -- the undefined group having to be a group like any other.
func people() *ListSource {
	row := func(id int64, fields ...*Field) Row {
		return NewRow(NewInt(id), Record(fields))
	}
	return NewListSource([]Row{
		row(1, Named("parent", 10), Named("kind", "file")),
		row(2, Named("parent", 10), Named("kind", "file")),
		row(3, Named("parent", 10), Named("kind", "dir")),
		row(4, Named("parent", 20), Named("kind", "file")),
		row(5, Named("kind", "dir")), // no parent at all
	})
}

func censusOn(t *testing.T, src Source, spec *Spec, field string) Census {
	t.Helper()
	set, err := src.Open(spec)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer set.Close()
	c, err := CensusOf(set, field)
	if err != nil {
		t.Fatalf("taking a census of %s: %v", field, err)
	}
	return c
}

// shows is a census as readable text, so a test says what it expects rather
// than reaching into three fields per group.
func census1(c Census) string {
	out := ""
	for _, g := range c.Groups {
		if out != "" {
			out += " "
		}
		n := fmt.Sprint(g.Count.N)
		if !g.Count.Exact {
			n = "≥" + n
		}
		out += g.Value.String() + "=" + n
	}
	if c.Total.Exact {
		return out + fmt.Sprintf(" (%d groups)", c.Total.N)
	}
	return out + fmt.Sprintf(" (≥%d groups)", c.Total.N)
}

// The whole of it, over records that are here: exact counts, an exact group
// total, and the records with no `parent` gathered under undefined.
func TestACensusCountsEachValueExactly(t *testing.T) {
	c := censusOn(t, people(), &Spec{}, "parent")
	if got, want := census1(c), "undefined=1 10=3 20=1 (3 groups)"; got != want {
		t.Errorf("the census reads\n  %s\nwant\n  %s", got, want)
	}
}

// **A census belongs to the FILTER**, so the filter decides what is counted.
func TestACensusCountsTheFilteredSequence(t *testing.T) {
	spec := &Spec{Filter: &Filter{
		Op: OpEq, Field: "kind", Values: []*Value{NewText("file")},
	}}
	c := censusOn(t, people(), spec, "parent")
	if got, want := census1(c), "10=2 20=1 (2 groups)"; got != want {
		t.Errorf("filtered to files the census reads\n  %s\nwant\n  %s", got, want)
	}
}

// **And the SORT is ignored**, for the same reason a count ignores it: sorting
// the same records cannot make there be more or fewer of them. Two orders of one
// filter are one census.
func TestASortDoesNotChangeACensus(t *testing.T) {
	one := censusOn(t, people(), &Spec{}, "parent")
	src := people()
	other := censusOn(t, src, &Spec{Sort: []SortLevel{
		{Field: "kind", Level: Level{Descending: true}},
	}}, "parent")
	if census1(one) != census1(other) {
		t.Errorf("sorting changed the census:\n  %s\n  %s", census1(one), census1(other))
	}
}

// Groups are told apart by Key and Equal and by nothing else, so 3 and "3" are
// two groups, and 3 and 3.0 are two groups. A census that grouped by a spelling
// would merge values the rest of the library keeps apart.
func TestGroupsAreToldApartByIdentityAndNotBySpelling(t *testing.T) {
	row := func(id int64, v *Value) Row {
		return NewRow(NewInt(id), Record{&Field{Name: "at", Value: v}})
	}
	src := NewListSource([]Row{
		row(1, NewInt(3)),
		row(2, NewText("3")),
		row(3, NewFloat(3.0)),
		row(4, NewSymbol("3")),
		row(5, NewInt(3)),
	})
	c := censusOn(t, src, &Spec{}, "at")
	if c.Total.N != 4 {
		t.Fatalf("it made %d groups of four different values: %s", c.Total.N, census1(c))
	}
	if got := c.CountOfGroup(NewInt(3)); got != Exactly(2) {
		t.Errorf("the integer 3 counts %v, want two", got)
	}
	for _, v := range []*Value{NewText("3"), NewFloat(3.0), NewSymbol("3")} {
		if got := c.CountOfGroup(v); got != Exactly(1) {
			t.Errorf("%v counts %v, want one", v, got)
		}
	}
}

// A value no record holds counts NOUGHT where the census is complete -- there
// being nothing left for it to be -- and is Unknown where it is not. That
// difference is the whole reason the group total is a claim of its own.
func TestAValueNobodyHoldsIsNoughtOrUnknown(t *testing.T) {
	c := censusOn(t, people(), &Spec{}, "parent")
	if got := c.CountOfGroup(NewInt(99)); got != Exactly(0) {
		t.Errorf("a complete census says %v of a value nobody holds, want exactly none", got)
	}

	partial := Census{
		Groups: []Group{{Value: NewInt(10), Count: AtLeast(3)}},
		Total:  AtLeast(1),
	}
	if got := partial.CountOfGroup(NewInt(99)); got != Unknown() {
		t.Errorf("an incomplete census says %v of a value it has not seen, want unknown", got)
	}
	if got := partial.CountOfGroup(NewInt(10)); got != AtLeast(3) {
		t.Errorf("a group it HAS seen reads %v, want the floor it was given", got)
	}
}

// Undefined is asked about like any other value, `Equal` holding nil to be nil.
// A tree asking whether a row has children asks exactly this question, and a row
// whose key is undefined must not fall through to the "nobody holds it" answer
// when one record does.
func TestUndefinedIsAskedAboutLikeAnyOtherValue(t *testing.T) {
	c := censusOn(t, people(), &Spec{}, "parent")
	if got := c.CountOfGroup(nil); got != Exactly(1) {
		t.Errorf("undefined counts %v, want the one row with no parent", got)
	}
}

// The order of the groups is the order of their VALUES, so two censuses of one
// sequence are one answer rather than two arrangements of it.
func TestTheGroupsComeBackInOneOrder(t *testing.T) {
	src := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("at", 40)}),
		NewRow(NewInt(2), Record{Named("at", 10)}),
		NewRow(NewInt(3), Record{Named("at", 30)}),
		NewRow(NewInt(4), Record{Named("at", 20)}),
	})
	if got, want := census1(censusOn(t, src, &Spec{}, "at")),
		"10=1 20=1 30=1 40=1 (4 groups)"; got != want {
		t.Errorf("the groups read\n  %s\nwant\n  %s", got, want)
	}
}

// A data set that cannot take a census SAYS SO, rather than walking its
// sequence to produce one. A census that quietly became a full scan would be
// fast in testing and ruinous in use.
func TestADataSetThatCannotSaysSo(t *testing.T) {
	set, err := plainSet{}.Open(&Spec{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if _, err := CensusOf(set, "parent"); err == nil {
		t.Fatal("it produced a census from a data set that cannot take one")
	}
}

// plainSet is the least a source can do: Read and Close and nothing else.
type plainSet struct{}

func (plainSet) Open(*Spec) (DataSet, error) { return plainSet{}, nil }
func (plainSet) Read(*Scope, Sink) error     { return nil }
func (plainSet) Close()                      {}

// A closed data set has let its ordering go, so it refuses rather than
// answering out of nothing -- the same posture RecordCount takes.
func TestAClosedDataSetRefuses(t *testing.T) {
	set, err := people().Open(&Spec{})
	if err != nil {
		t.Fatal(err)
	}
	set.Close()
	if _, err := CensusOf(set, "parent"); err == nil {
		t.Fatal("a closed data set produced a census")
	}
}

// **A census is a sequence, so it is a source** -- and then a short list of
// large counts is a sort and a scope, and needs nothing written for it.
func TestACensusReadsBackAsRecords(t *testing.T) {
	c := censusOn(t, people(), &Spec{}, "parent")

	set, err := CensusSource(c).Open(&Spec{
		Sort: []SortLevel{{Field: CensusCount, Level: Level{Descending: true}}},
	})
	if err != nil {
		t.Fatalf("opening the census as a source: %v", err)
	}
	defer set.Close()

	if got := CountOf(set); got != Exactly(3) {
		t.Errorf("the census source counts %v records, want its three groups", got)
	}

	var out censusRows
	if err := set.Read(&Scope{Count: 1}, &out); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if len(out.ids) != 1 {
		t.Fatalf("the biggest group came back as %d records", len(out.ids))
	}
	// Parent 10 has three children, which is more than either of the others.
	if !Equal(out.ids[0], NewInt(10)) {
		t.Errorf("the biggest group is %v, want the parent with three", out.ids[0])
	}
	if got := out.fields[0].Get(CensusCount); !Equal(got, NewInt(3)) {
		t.Errorf("it carries a count of %v, want three", got)
	}
}

// rows is a sink that keeps the identities AND the fields, and that survives an
// identity of `undefined` -- which the census has, for the records with no value
// under the field it counted.
type censusRows struct {
	ids    []*Value
	fields []Record
}

func (r *censusRows) Ordered() {}
func (r *censusRows) Record(id *Value, f Record) error {
	r.ids = append(r.ids, id)
	r.fields = append(r.fields, f)
	return nil
}
func (r *censusRows) Subset(id *Value, f Record, _ Totals) error { return r.Record(id, f) }
func (r *censusRows) Done(Complete)                              {}

// The undefined group survives being made into a record: its identity is the
// undefined value, which has a Key like anything else.
func TestTheUndefinedGroupIsARecordToo(t *testing.T) {
	c := censusOn(t, people(), &Spec{}, "parent")
	set, err := CensusSource(c).Open(&Spec{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out censusRows
	if err := set.Read(&Scope{Count: 10}, &out); err != nil {
		t.Fatalf("reading: %v", err)
	}
	found := false
	for i, id := range out.ids {
		if id != nil {
			continue
		}
		found = true
		if got := out.fields[i].Get(CensusCount); !Equal(got, NewInt(1)) {
			t.Errorf("the undefined group counts %v, want the one row with no parent", got)
		}
	}
	if !found {
		t.Errorf("the undefined group is not among the records: %v", out.ids)
	}
}
