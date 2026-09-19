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
func captions(t *testing.T, src Source, descriptor *DataSetDescriptor) []string {
	t.Helper()
	set, err := src.Open(descriptor)
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
	descriptor := &DataSetDescriptor{Sort: []SortLevel{{Field: "name"}}}

	if got := captions(t, src, descriptor); len(got) != 2 || got[0] != "alpha" {
		t.Fatalf("to begin with it reads %v", got)
	}
	src.Restate(namedList("delta", "gamma", "epsilon"))
	if got, want := len(captions(t, src, descriptor)), 3; got != want {
		t.Errorf("restated it holds %d records, want %d", got, want)
	}
	if got := captions(t, src, descriptor); got[0] != "delta" {
		t.Errorf("restated it reads %v, want the new rows sorted", got)
	}
	if got, want := src.Len(), 3; got != want {
		t.Errorf("it counts %d rows, want %d", got, want)
	}
}

// **The ordering is forgotten, and that is what the restatement costs.** The
// same descriptor asked again is worked out again rather than answered out of the
// cache, which is the whole reason a source has to be told at all.
func TestARestatedListForgetsWhatItWorkedOut(t *testing.T) {
	src := NewListSource(namedList("alpha"))
	descriptor := &DataSetDescriptor{Sort: []SortLevel{{Field: "name"}}}
	_ = captions(t, src, descriptor)

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
	set, err := src.Open(&DataSetDescriptor{Sort: []SortLevel{{Field: "name"}}})
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
		Source:     people,
		Descriptor: &DataSetDescriptor{Sort: []SortLevel{{Field: "name"}}},
		Types:      NodeTypes{Default: &NodeType{}},
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

// --- and a tree's levels can be ordered otherwise ------------------------

// twoLevels is hosts and the applications on them, each level with an order its
// configuration chose.
func twoLevels(t *testing.T) (*TreeSource, *ListSource) {
	t.Helper()
	hosts := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "kestrel"), Named("size", 2)}),
		NewRow(NewInt(2), Record{Named("name", "merlin"), Named("size", 1)}),
	})
	apps := NewListSource([]Row{
		NewRow(NewInt(10), Record{
			Named("name", "browser"), Named("host", 1), Named("size", 9),
		}),
		NewRow(NewInt(11), Record{
			Named("name", "editor"), Named("host", 1), Named("size", 3),
		}),
	})
	tree, err := NewTreeSource(TreeOptions{
		Source:     hosts,
		Descriptor: &DataSetDescriptor{Sort: []SortLevel{{Field: "name"}}},
		Types: NodeTypes{
			Default: &NodeType{Then: Always("apps")},
			Named: map[string]*NodeType{
				"apps": {
					Source:   apps,
					Children: Sorted(ChildrenByKey("host"), SortLevel{Field: "name"}),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	return tree, apps
}

func flat(t *testing.T, tree *TreeSource) []string {
	t.Helper()
	tree.ExpandAll()
	set, err := tree.Open(nil)
	if err != nil {
		t.Fatalf("stating the sequence: %v", err)
	}
	defer set.Close()
	return captionsOf(t, set)
}

// **A tree sorts LEVEL by level, which is why an order is said per kind.** The
// pre-order is built rather than sorted, so "by size, descending" over a tree is
// each level's own sequence put in that order and the walk laid over it.
func TestATreesLevelsCanBeOrderedOtherwise(t *testing.T) {
	tree, _ := twoLevels(t)
	if got := flat(t, tree); got[0] != "kestrel" || got[1] != "browser" {
		t.Fatalf("as configured the tree reads %v", got)
	}

	by := []SortLevel{{Field: "size", Level: Level{Descending: true}}}
	tree.SortBy(map[string][]SortLevel{"": by, "apps": by})

	got := flat(t, tree)
	want := []string{"kestrel", "browser", "editor", "merlin"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("ordered by size the tree reads\n  %v\nwant\n  %v", got, want)
		}
	}
}

// A kind nobody named keeps the order its configuration chose, so one level can
// be reordered without the rest being restated.
func TestAKindNobodyNamedKeepsItsOwnOrder(t *testing.T) {
	tree, _ := twoLevels(t)
	tree.SortBy(map[string][]SortLevel{
		"apps": {{Field: "size", Level: Level{Descending: true}}},
	})
	got := flat(t, tree)
	want := []string{"kestrel", "browser", "editor", "merlin"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("the tree reads\n  %v\nwant\n  %v -- only the apps were named", got, want)
		}
	}
}

// **A kind named with NO levels is ordered the way its source answers**, which is
// how a sort is turned off. The map's keys are the question; an empty slice is an
// answer and not a silence.
func TestAKindNamedWithNoLevelsTakesTheSourcesOwnOrder(t *testing.T) {
	tree, _ := twoLevels(t)
	tree.SortBy(map[string][]SortLevel{"": {}})
	got := flat(t, tree)
	// The hosts source holds kestrel then merlin, which is also what `name`
	// gives -- so the assertion that means anything is about the SIZE order the
	// configuration did not ask for either way. Reordering the list is what
	// shows it.
	if len(got) == 0 || got[0] != "kestrel" {
		t.Fatalf("unsorted the tree reads %v", got)
	}
	tree.SortBy(map[string][]SortLevel{"": {{Field: "size"}}})
	if got := flat(t, tree); got[0] != "merlin" {
		t.Errorf("by size the tree reads %v, want the smaller host first", got)
	}
	tree.SortBy(map[string][]SortLevel{"": {}})
	if got := flat(t, tree); got[0] != "kestrel" {
		t.Errorf("with the sort turned off the tree reads %v, want the source's "+
			"own order back", got)
	}
}

// Restating a level's rows and reordering it are two different sayings, and both
// reach a sequence somebody is holding.
func TestAnOrderSaidIsToldToEverySequence(t *testing.T) {
	tree, apps := twoLevels(t)
	tree.ExpandAll()
	set, err := tree.Open(nil)
	if err != nil {
		t.Fatalf("stating the sequence: %v", err)
	}
	defer set.Close()

	if got := len(captionsOf(t, set)); got != 4 {
		t.Fatalf("to begin with the tree holds %d rows", got)
	}
	apps.Restate([]Row{
		NewRow(NewInt(12), Record{Named("name", "shell"), Named("host", 2)}),
	})
	tree.Stale()
	got := captionsOf(t, set)
	want := []string{"kestrel", "merlin", "shell"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("told, the tree reads\n  %v\nwant\n  %v", got, want)
		}
	}
	tree.SortBy(map[string][]SortLevel{"": {{Field: "name", Level: Level{Descending: true}}}})
	if got := captionsOf(t, set); got[0] != "merlin" {
		t.Errorf("reordered, the sequence somebody holds reads %v", got)
	}
}

// **The configuration is not the tree's to rewrite.** An order said for a kind is
// laid over a COPY of that level's descriptor, so saying nothing afterwards gives the
// order the configuration chose rather than the last thing anybody asked for.
//
// The top level is where it shows: a node type's `Of` builds a fresh descriptor every
// time, but `TreeOptions.Descriptor` is one pointer read on every build.
func TestAnOrderSaidAndThenUnsaidGivesTheConfigurationBack(t *testing.T) {
	tree, _ := twoLevels(t)
	if got := flat(t, tree); got[0] != "kestrel" {
		t.Fatalf("as configured the tree reads %v", got)
	}
	tree.SortBy(map[string][]SortLevel{"": {{Field: "size"}}})
	if got := flat(t, tree); got[0] != "merlin" {
		t.Fatalf("by size the tree reads %v", got)
	}

	tree.SortBy(nil)
	if got := flat(t, tree); got[0] != "kestrel" {
		t.Errorf("with nothing said the tree reads %v, want the order its "+
			"configuration chose -- which was written over", got)
	}
}
