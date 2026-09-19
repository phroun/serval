package serval

// Restating a list, and what a sequence already stated goes on saying.
//
// The rule everywhere else is that a query is asked once and answered once, and
// a list that can be restated is the one place that could quietly have an
// exception: an ordering is a run of INDICES, and indices into a slice somebody
// replaced name different records, or none. So the ordering holds the rows it was
// built over, and the assertions here are about the seam that makes.

import "testing"

func namedList(names ...string) []Row {
	rows := make([]Row, 0, len(names))
	for i, n := range names {
		rows = append(rows, NewRow(NewInt(int64(i+1)), Record{Named("name", n)}))
	}
	return rows
}

// captions is a sequence read out in order, by one field.
func captions(t *testing.T, src Source, spec *Spec) []string {
	t.Helper()
	set, err := src.Open(spec)
	if err != nil {
		t.Fatalf("stating the sequence: %v", err)
	}
	defer set.Close()
	return captionsOf(t, set)
}

func captionsOf(t *testing.T, set DataSet) []string {
	t.Helper()
	var got sinkRows
	if err := set.Read(&Scope{Count: 100}, &got); err != nil {
		t.Fatalf("reading: %v", err)
	}
	return got.names
}

type sinkRows struct{ names []string }

func (s *sinkRows) Ordered() {}
func (s *sinkRows) Record(_ *Value, f Record) error {
	s.names = append(s.names, Segment(f.Get("name")))
	return nil
}
func (s *sinkRows) Subset(id *Value, f Record, _ Totals) error { return s.Record(id, f) }
func (s *sinkRows) Done(Complete)                              {}

// A list restated answers the new rows to anyone who asks after.
func TestARestatedListAnswersTheNewRows(t *testing.T) {
	src := NewListSource(namedList("alpha", "beta"))
	spec := &Spec{Sort: []SortLevel{{Field: "name"}}}

	if got := captions(t, src, spec); len(got) != 2 || got[0] != "alpha" {
		t.Fatalf("to begin with it reads %v", got)
	}
	src.Restate(namedList("delta", "gamma", "epsilon"))
	if got, want := len(captions(t, src, spec)), 3; got != want {
		t.Errorf("restated it holds %d records, want %d", got, want)
	}
	if got := captions(t, src, spec); got[0] != "delta" {
		t.Errorf("restated it reads %v, want the new rows sorted", got)
	}
	if got, want := src.Len(), 3; got != want {
		t.Errorf("it counts %d rows, want %d", got, want)
	}
}

// **The ordering is forgotten, and that is what the restatement costs.** The
// same spec asked again is worked out again rather than answered out of the
// cache, which is the whole reason a source has to be told at all.
func TestARestatedListForgetsWhatItWorkedOut(t *testing.T) {
	src := NewListSource(namedList("alpha"))
	spec := &Spec{Sort: []SortLevel{{Field: "name"}}}
	_ = captions(t, src, spec)

	src.mu.Lock()
	held := len(src.cache)
	src.mu.Unlock()
	if held != 1 {
		t.Fatalf("it kept %d orderings, want the one it worked out", held)
	}

	src.Restate(namedList("beta"))
	src.mu.Lock()
	held, recent := len(src.cache), len(src.recent)
	src.mu.Unlock()
	if held != 0 || recent != 0 {
		t.Errorf("restated it still holds %d orderings and %d keys, want none",
			held, recent)
	}
}

// **A sequence stated BEFORE the restatement goes on answering the one it
// stated.** Asked once and answered once: the rows it was built over are held by
// the ordering, so nothing it hands back is a record from a list it never read.
//
// A shorter list is what shows it -- indices into the source's slice would run
// off the end of it.
func TestASequenceStatedBeforeARestatementKeepsIt(t *testing.T) {
	src := NewListSource(namedList("alpha", "beta", "gamma"))
	set, err := src.Open(&Spec{Sort: []SortLevel{{Field: "name"}}})
	if err != nil {
		t.Fatalf("stating the sequence: %v", err)
	}
	defer set.Close()

	src.Restate(namedList("delta"))

	got := captionsOf(t, set)
	if len(got) != 3 {
		t.Fatalf("the sequence it stated now reads %v, want the three it opened over", got)
	}
	for i, want := range []string{"alpha", "beta", "gamma"} {
		if got[i] != want {
			t.Errorf("row %d reads %q, want %q -- it read a list it never stated",
				i, got[i], want)
		}
	}
	if n := CountOf(set); !n.Exact || n.N != 3 {
		t.Errorf("it counts %v records, want exactly three", n)
	}
}

// And a tree over a restated level is told, whereupon it flattens again. The
// notice is the source's to send and the rebuild is what it costs.
func TestATreeToldItIsStaleFlattensAgain(t *testing.T) {
	people := NewListSource(namedList("alpha", "beta"))
	tree, err := NewTreeSource(TreeOptions{
		Source: people,
		Spec:   &Spec{Sort: []SortLevel{{Field: "name"}}},
		Types:  NodeTypes{Default: &NodeType{}},
	})
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	set, err := tree.Open(nil)
	if err != nil {
		t.Fatalf("stating the sequence: %v", err)
	}
	defer set.Close()

	if got := captionsOf(t, set); len(got) != 2 {
		t.Fatalf("to begin with the tree reads %v", got)
	}

	people.Restate(namedList("delta", "gamma", "epsilon"))
	// Not yet: nothing polls, so the sequence still holds what it flattened.
	if got := captionsOf(t, set); len(got) != 2 {
		t.Errorf("the tree noticed on its own: it reads %v with nobody having said so", got)
	}

	tree.Stale()
	got := captionsOf(t, set)
	if len(got) != 3 {
		t.Fatalf("told, the tree reads %v, want the three rows now there", got)
	}
	if got[0] != "delta" {
		t.Errorf("it reads %v, want the new rows in their own order", got)
	}
}
