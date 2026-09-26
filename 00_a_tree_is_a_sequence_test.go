package serval

// A tree is a sequence.
//
// So the thing checked here is not that a tree is drawn correctly -- nothing
// here draws. It is that the FOUR THINGS a flat sequence gives a reader are all
// true of a tree: how many rows there are, where a row stands, where an answer
// began, and a scope of it read from anywhere. If those hold, a tree view is a
// list view and every piece of apparatus built for one works over the other.

import (
	"fmt"
	"strings"
	"testing"
)

// A small adjacency list, which is the hierarchy that needs no paths at all.
//
//	1 alpha          3 gamma
//	  2 beta           5 epsilon
//	    4 delta
func kin() *ListSource {
	row := func(id, parent int64, name string) Row {
		r := Record{Named("name", name)}
		if parent != 0 {
			r = append(r, Named("parent", parent))
		}
		return NewRow(NewInt(id), r)
	}
	return NewListSource([]Row{
		row(1, 0, "alpha"),
		row(2, 1, "beta"),
		row(3, 0, "gamma"),
		row(4, 2, "delta"),
		row(5, 3, "epsilon"),
	})
}

// treeOf is the adjacency tree, with the top level being the rows that have no
// parent and the children being `eq parent <the parent's key>`.
func treeOf(t *testing.T, o TreeOptions) *TreeSource {
	t.Helper()
	if o.Source == nil {
		o.Source = kin()
	}
	if o.Descriptor == nil {
		o.Descriptor = &DataSetDescriptor{Filter: &Filter{
			Op: OpEq, Field: "parent", Values: []*Value{nil},
		}}
	}
	if o.Types.Default == nil && o.Types.Named == nil {
		o.Types = NodeTypes{Default: &NodeType{Children: ChildrenByKey("parent")}}
	}
	src, err := NewTreeSource(o)
	if err != nil {
		t.Fatalf("making the tree: %v", err)
	}
	return src
}

// drawn is the visible sequence as one readable line, so a test says what it
// expects instead of reaching into four fields per row.
func drawn(t *testing.T, set DataSet, s *Scope) string {
	t.Helper()
	var out treeTook
	if err := set.Read(s, &out); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if out.done.Error != "" {
		t.Fatalf("the answer refused: %s", out.done.Error)
	}
	return strings.Join(out.lines, " ")
}

func wholeTree(t *testing.T, src *TreeSource) (DataSet, string) {
	t.Helper()
	set, err := src.Open(nil)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	return set, drawn(t, set, &Scope{Count: 100})
}

// Nothing expanded: the top level, and nothing deeper.
func TestATreeStartsAtItsTopLevel(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	set, got := wholeTree(t, src)
	defer set.Close()

	if want := "alpha/0 gamma/0"; got != want {
		t.Errorf("the tree reads\n  %s\nwant\n  %s", got, want)
	}
	if n := CountOf(set); n != Exactly(2) {
		t.Errorf("it counts %v rows, want two", n)
	}
}

// **Pre-order, and it is BUILT rather than sorted.** A child stands directly
// after its parent and before its parent's next sibling, which no comparison
// over tuples produces: CompareLevels stops where the shorter run ends, so
// `[a]` and `[a, b]` compare EQUAL rather than parent-before-child.
func TestTheVisibleRowsComeBackInPreOrder(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	if want := "alpha/0 beta/1 delta/2 gamma/0 epsilon/1"; got != want {
		t.Errorf("expanded, the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// **Expanding a million rows is one mark**, and the sequence it produces is the
// whole tree. One mark, five rows, and the mark set stays at one.
func TestExpandingEverythingIsStillOneMark(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	if n := CountOf(set); n != Exactly(5) {
		t.Errorf("it counts %v rows, want all five", n)
	}
	if held := src.Marks().Held(); held != 1 {
		t.Errorf("the whole tree expanded holds %d marks, want one", held)
	}
	if !strings.Contains(got, "delta/2") {
		t.Errorf("the deepest row is not there: %s", got)
	}
}

// Opening one node shows its children and not its grandchildren, the segments
// being the records' own keys because this standing builds no path.
func TestOpeningOneNodeShowsOneLevel(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.Expand(Key(NewInt(1)))
	set, got := wholeTree(t, src)
	defer set.Close()

	if want := "alpha/0 beta/1 gamma/0"; got != want {
		t.Errorf("with alpha open the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// **A mark change is TOLD, and the sequence rebuilds.** Nothing polls, nothing
// expires and no generation is compared: the source says a mark moved, and what
// is drawing on it is told.
func TestAMarkChangeReachesASequenceAlreadyOpen(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	if got, want := drawn(t, set, &Scope{Count: 100}), "alpha/0 gamma/0"; got != want {
		t.Fatalf("before: %s", got)
	}

	src.Expand(Key(NewInt(1)))

	if got, want := drawn(t, set, &Scope{Count: 100}), "alpha/0 beta/1 gamma/0"; got != want {
		t.Errorf("the open sequence reads\n  %s\nwant\n  %s", got, want)
	}
	if n := CountOf(set); n != Exactly(3) {
		t.Errorf("and counts %v, want three", n)
	}
}

// --- the four things a flat sequence gives a reader ----------------------

// **Scope.From is a position in the flattened sequence**, which is a thumb
// dragged -- and Complete.First is where the answer actually began, which is the
// calibration. Over records in hand both are exact.
func TestAScopeReadsATreeFromAPosition(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.ExpandAll()
	set, _ := wholeTree(t, src)
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{From: 3, Count: 2}, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(out.lines, " "), "gamma/0 epsilon/1"; got != want {
		t.Errorf("from the fourth row it reads\n  %s\nwant\n  %s", got, want)
	}
	if out.done.First != Exactly(3) {
		t.Errorf("it says it began at %v, want exactly the third", out.done.First)
	}
	if out.done.Total != Exactly(5) {
		t.Errorf("it says the sequence holds %v, want five", out.done.Total)
	}
}

// And a scope carries on from a record by IDENTITY, which is what keyset
// pagination over a tree amounts to.
func TestAScopeCarriesOnFromARow(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.ExpandAll()
	set, _ := wholeTree(t, src)
	defer set.Close()

	got := drawn(t, set, &Scope{After: NewInt(2), Count: 2})
	if want := "delta/2 gamma/0"; got != want {
		t.Errorf("after beta it reads\n  %s\nwant\n  %s", got, want)
	}
}

// A tree walks backwards like any other sequence, the pre-order simply read from
// the other end.
func TestATreeReadsBackwards(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.ExpandAll()
	set, _ := wholeTree(t, src)
	defer set.Close()

	got := drawn(t, set, &Scope{Reversed: true, Count: 3})
	if want := "epsilon/1 gamma/0 delta/2"; got != want {
		t.Errorf("backwards it reads\n  %s\nwant\n  %s", got, want)
	}
}

// **A sort is refused**, because the order is the tree's own. Each LEVEL is
// sorted by its own descriptor and the pre-order is laid over that.
func TestATreeRefusesASortOfItsOwnSequence(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	_, err := src.Open(&DataSetDescriptor{Sort: []SortLevel{{Field: "name"}}})
	if err == nil {
		t.Fatal("it accepted a sort of the flattened sequence")
	}
	if !strings.Contains(err.Error(), "pre-order") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// A level's OWN sort is honoured, which is where ordering belongs -- and the
// census parts come through it untouched, a count belonging to the FILTER and
// not to the order: sorting the same records cannot make there be more or fewer.
func TestEachLevelCarriesItsOwnSort(t *testing.T) {
	down := []SortLevel{{Field: "name", Level: Level{Descending: true}}}
	if !Sorted(ChildrenByKey("parent"), down...).counts() {
		t.Error("sorting a criterion cost it its census")
	}
	src := treeOf(t, TreeOptions{
		Descriptor: &DataSetDescriptor{
			Filter: &Filter{Op: OpEq, Field: "parent", Values: []*Value{nil}},
			Sort:   down,
		},
		Types: NodeTypes{Default: &NodeType{
			Children: Sorted(ChildrenByKey("parent"), down...),
		}},
	})
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	if want := "gamma/0 epsilon/1 alpha/0 beta/1 delta/2"; got != want {
		t.Errorf("sorted down, the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// --- what a tree adds to every row --------------------------------------

// Depth, path, state and how many children -- three things a tree knows and a
// record need not, plus the state a twisty is drawn from.
func TestEveryRowCarriesWhereItStands(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.Expand(Key(NewInt(1)))
	set, _ := wholeTree(t, src)
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	alpha, beta := out.fields[0], out.fields[1]

	if got := alpha.Get("depth"); !Equal(got, NewInt(0)) {
		t.Errorf("alpha's depth is %v", got)
	}
	if got := beta.Get("depth"); !Equal(got, NewInt(1)) {
		t.Errorf("beta's depth is %v", got)
	}
	if got := alpha.Get("state"); !Equal(got, NewSymbol("open")) {
		t.Errorf("alpha's state is %v, want open", got)
	}
	if got := beta.Get("state"); !Equal(got, NewSymbol("closed")) {
		t.Errorf("beta's state is %v, want closed", got)
	}
	if got := alpha.Get("expandable"); !Equal(got, NewInt(1)) {
		t.Errorf("alpha says it has %v children, want one", got)
	}
	// The record's own fields are still there beside them.
	if got := alpha.Get("name"); !Equal(got, NewText("alpha")) {
		t.Errorf("the record's own name is %v", got)
	}
}

// A leaf says nought children, which is a statement and not an ignorance: the
// child descriptor was counted and came back empty.
func TestALeafSaysNoughtAndNotNothing(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.ExpandAll()
	set, _ := wholeTree(t, src)
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	// delta is the deepest row and has nothing under it.
	for i, f := range out.fields {
		if !Equal(f.Get("name"), NewText("delta")) {
			continue
		}
		if got := f.Get("expandable"); !Equal(got, NewInt(0)) {
			t.Errorf("row %d (delta) says %v children, want nought exactly", i, got)
		}
		return
	}
	t.Fatal("delta is not in the sequence")
}

// **Expandability is a field, where the row can say** -- which is free, and is
// the only way to draw a twisty over records that must be asked for without
// opening every visible row.
func TestARowMaySayHowManyChildrenItHas(t *testing.T) {
	src := treeOf(t, TreeOptions{
		Source: NewListSource([]Row{
			NewRow(NewInt(1), Record{Named("name", "says a number"), Named("kids", 42)}),
			NewRow(NewInt(2), Record{Named("name", "says yes"), Named("kids", true)}),
			NewRow(NewInt(3), Record{Named("name", "says no"), Named("kids", false)}),
			NewRow(NewInt(4), Record{Named("name", "says nothing")}),
		}),
		Descriptor:   &DataSetDescriptor{},
		SaysChildren: "kids",
	})
	set, _ := wholeTree(t, src)
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	want := []*Value{NewInt(42), NewInt(1), NewInt(0), NewInt(0)}
	for i, w := range want {
		if got := out.fields[i].Get("expandable"); !Equal(got, w) {
			t.Errorf("row %d says %v children, want %v", i, got, w)
		}
	}
}

// --- paths --------------------------------------------------------------

// Over data shaped like an address the marks are keyed by PATH, and the rows
// carry theirs.
func TestATreeOverLocationsKeysItsMarksByPath(t *testing.T) {
	src, err := NewTreeSource(TreeOptions{
		Source:     folders(),
		Descriptor: &DataSetDescriptor{Filter: &Filter{Op: OpEq, Field: "location", Values: []*Value{NewText("/")}}},
		Types: NodeTypes{Default: &NodeType{
			Children: here.ChildrenByLocation("location"),
			Standing: here,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	src.Expand("/usr")
	src.Expand("/usr", "/usr/local")
	set, got := wholeTree(t, src)
	defer set.Close()

	if want := "usr/0 local/1 bin/2 share/2 locally/1"; got != want {
		t.Errorf("the tree reads\n  %s\nwant\n  %s", got, want)
	}

	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	if p := out.fields[2].Get("path"); !Equal(p, NewText("/usr/local/bin")) {
		t.Errorf("bin's path is %v", p)
	}
}

// And the tree SAYS which of the two a kind's segment is, because a caller
// reaching in from the side has to spell a chain the way the walk spells it.
//
// It cannot work it out: it holds a row's identity and its path and has no way to
// know which one a mark is filed under. Guessing is wrong exactly where it is
// hardest to see -- a chain naming a node that is not there opens nothing, reports
// nothing, and leaves a twisty that was drawn from a real child count sitting there
// unmoved.
func TestATreeSaysWhetherAKindIsMarkedByItsPath(t *testing.T) {
	byPath, err := NewTreeSource(TreeOptions{
		Source:     folders(),
		Descriptor: &DataSetDescriptor{Filter: &Filter{Op: OpEq, Field: "location", Values: []*Value{NewText("/")}}},
		Types: NodeTypes{Default: &NodeType{
			Children: here.ChildrenByLocation("location"),
			Standing: here,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !byPath.MarkedByPath("") {
		t.Error("a tree descending by location says its marks are keyed by identity")
	}

	// An adjacency list has no standing and no path, so its segment is the key --
	// which is also what every tree did before a standing existed.
	byKey, err := NewTreeSource(TreeOptions{
		Source:     people(),
		Descriptor: &DataSetDescriptor{Filter: &Filter{Op: OpEq, Field: "parent", Values: []*Value{nil}}},
		Types:      NodeTypes{Default: &NodeType{Children: ChildrenByKey("parent")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if byKey.MarkedByPath("") {
		t.Error("an adjacency list says its marks are keyed by path")
	}
	// A kind nobody declared is nobody's standing, so it is not by path either.
	if byPath.MarkedByPath("no such kind") {
		t.Error("a kind that does not exist says its marks are keyed by path")
	}
}

// --- cycles -------------------------------------------------------------

// looper is a node that is its own parent, which is the smallest cycle there is.
func looper() *ListSource {
	return NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "top")}),
		NewRow(NewInt(2), Record{Named("name", "loop"), Named("parent", 2)}),
	})
}

// **Revisits default to nought, so a node already on its own path is drawn but
// not expandable**, and the sequence is finite without anybody having to bound
// it.
//
// Drawn, and not hidden. The budget governs EXPANSION and never emission,
// because a row the criterion says is a child IS a child -- hiding it would make
// the tree lie about what is under a node, which is worse than showing a loop.
// In this fixture `loop` is its own parent, so it appears once at the top level
// and once beneath itself, and the second one has a dead twisty.
func TestACycleIsRefusedByDefault(t *testing.T) {
	src := treeOf(t, TreeOptions{Source: looper(), Descriptor: &DataSetDescriptor{}})
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	if want := "top/0 loop/0 loop/1"; got != want {
		t.Errorf("the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// And a budget above nought EXPANDS the loop that many more times, which is the
// whole reason to allow any: a cycle you can see round.
func TestARevisitBudgetShowsTheLoopThatManyTimes(t *testing.T) {
	for budget, want := range map[int]string{
		0: "top/0 loop/0 loop/1",
		1: "top/0 loop/0 loop/1 loop/2",
		2: "top/0 loop/0 loop/1 loop/2 loop/3",
	} {
		src := treeOf(t, TreeOptions{
			Source: looper(), Descriptor: &DataSetDescriptor{}, Revisits: budget,
		})
		src.ExpandAll()
		set, got := wholeTree(t, src)
		set.Close()
		if got != want {
			t.Errorf("with %d revisits the tree reads\n  %s\nwant\n  %s",
				budget, got, want)
		}
	}
}

// And the budget is clamped rather than refused, the way a count is.
func TestARevisitBudgetIsClamped(t *testing.T) {
	src := treeOf(t, TreeOptions{Source: looper(), Descriptor: &DataSetDescriptor{}, Revisits: 99})
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()
	// Three more expansions than the default's none, so three more rows.
	if want := "top/0 loop/0 loop/1 loop/2 loop/3 loop/4"; got != want {
		t.Errorf("clamped to %d the tree reads\n  %s\nwant\n  %s",
			MostRevisits, got, want)
	}
}

// --- mixed descent ------------------------------------------------------

// **One tree, several shapes.** A row names a different node type, and its
// children come out of a different source entirely -- which is the thing the
// whole child-type mechanism exists for.
func TestARowMayNameAChildTypeInAnotherSource(t *testing.T) {
	Extents := NewListSource([]Row{
		NewRow(NewInt(90), Record{Named("name", "an Extent"), Named("app", 1)}),
		NewRow(NewInt(91), Record{Named("name", "another"), Named("app", 1)}),
	})
	apps := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "an app"), Named("kind", "Extents")}),
		NewRow(NewInt(2), Record{Named("name", "a plain row")}),
	})

	src, err := NewTreeSource(TreeOptions{
		Source:     apps,
		Descriptor: &DataSetDescriptor{},
		Types: NodeTypes{
			Field:   "kind",
			Default: &NodeType{Children: ChildrenByKey("parent")},
			Named: map[string]*NodeType{
				"Extents": {Source: Extents, Children: ChildrenByKey("app")},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	if want := "an app/0 an Extent/1 another/1 a plain row/0"; got != want {
		t.Errorf("the grafted tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// A shallow filter is asked at every level: a parent that does not match is gone
// and its children with it.
func TestAFilterIsAskedAtEveryLevel(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.ExpandAll()

	set, err := src.Open(&DataSetDescriptor{Filter: &Filter{
		Op: OpNot, Children: []*Filter{
			{Op: OpEq, Field: "name", Values: []*Value{NewText("beta")}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	// beta goes, and delta with it: delta is only reachable through beta.
	if got, want := drawn(t, set, &Scope{Count: 100}), "alpha/0 gamma/0 epsilon/1"; got != want {
		t.Errorf("filtered, the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// --- helpers ------------------------------------------------------------

// treeTook keeps each row as `name/depth`, which is what a test wants to read.
type treeTook struct {
	lines  []string
	fields []Record
	done   Complete
}

func (k *treeTook) Ordered() {}
func (k *treeTook) Record(id *Value, f Record) error {
	k.lines = append(k.lines, fmt.Sprintf("%s/%s",
		Segment(f.Get("name")), Segment(f.Get("depth"))))
	k.fields = append(k.fields, f)
	return nil
}
func (k *treeTook) Subset(id *Value, f Record, _ Totals) error { return k.Record(id, f) }
func (k *treeTook) Done(c Complete)                            { k.done = c }

// --- the gaps a mutation sweep found ------------------------------------

// `uncounted` is the helper the composition tests already have: a source that
// reads but will not count. It is what keeps these off the easy path -- where a
// count is exact, a nought short-circuits the descent, so a bug in the descent
// itself would never be reached.

// **The shallow filter is asked of the LEVEL and not only of the count.** Over a
// source that counts, a nought stops the descent before it starts, so a filter
// that never reached the level read would look as though it worked. Over one
// that cannot count, the descent happens and the filter has to be there.
func TestTheShallowFilterReachesTheLevelAndNotOnlyTheCount(t *testing.T) {
	src := treeOf(t, TreeOptions{Source: uncounted{kin()}})
	src.ExpandAll()

	set, err := src.Open(&DataSetDescriptor{Filter: &Filter{
		Op: OpNot, Children: []*Filter{
			{Op: OpEq, Field: "name", Values: []*Value{NewText("beta")}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	if got, want := drawn(t, set, &Scope{Count: 100}), "alpha/0 gamma/0 epsilon/1"; got != want {
		t.Errorf("filtered over a source that cannot count, the tree reads\n  %s\nwant\n  %s",
			got, want)
	}
}

// And a row's expandability is the FILTERED count: alpha's only child is beta,
// so with beta filtered out alpha has nought children and says so.
func TestExpandabilityIsTheFilteredCount(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	set, err := src.Open(&DataSetDescriptor{Filter: &Filter{
		Op: OpNot, Children: []*Filter{
			{Op: OpEq, Field: "name", Values: []*Value{NewText("beta")}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.fields[0].Get("expandable"); !Equal(got, NewInt(0)) {
		t.Errorf("alpha says %v children with its only one filtered out, want nought", got)
	}
}

// Where a source cannot count and no row says, expandability is UNDEFINED --
// which means draw the twisty and find out on opening, rather than nought.
func TestWhatNobodyCanSayIsUndefinedAndNotNought(t *testing.T) {
	src := treeOf(t, TreeOptions{Source: uncounted{kin()}})
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.fields[0].Get("expandable"); got != nil {
		t.Errorf("a source that cannot count said %v children, want undefined", got)
	}
}

// **The tree's own fields win on a collision**, because a view cannot draw
// without them -- and that is exactly why their names are the caller's to move.
func TestTheTreesOwnFieldsShadowTheRecords(t *testing.T) {
	clash := NewListSource([]Row{
		NewRow(NewInt(1), Record{
			Named("name", "alpha"), Named("depth", "the record's own"),
			Named("path", "somewhere else"),
		}),
	})
	src := treeOf(t, TreeOptions{Source: clash, Descriptor: &DataSetDescriptor{}})
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 10}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.fields[0].Get("depth"); !Equal(got, NewInt(0)) {
		t.Errorf("depth is %v, want the tree's own", got)
	}
	// One member under the name, not two: the record's was dropped rather than
	// left for whoever reads first.
	n := 0
	for _, m := range out.fields[0] {
		if m.Name == "depth" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the row carries %d members called depth", n)
	}

	// And moving the tree's name gives the record its own back.
	moved := treeOf(t, TreeOptions{Source: clash, Descriptor: &DataSetDescriptor{},
		Fields: TreeFields{Depth: "level"}})
	set2, err := moved.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set2.Close()
	var out2 treeTook
	if err := set2.Read(&Scope{Count: 10}, &out2); err != nil {
		t.Fatal(err)
	}
	if got := out2.fields[0].Get("depth"); !Equal(got, NewText("the record's own")) {
		t.Errorf("with the tree's name moved, depth is %v", got)
	}
	if got := out2.fields[0].Get("level"); !Equal(got, NewInt(0)) {
		t.Errorf("and level is %v", got)
	}
}

// A From past the end is clamped to the LAST row, not round to the first: a
// thumb dragged off the bottom of its track stays at the bottom.
func TestAFromPastTheEndStaysAtTheEnd(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.ExpandAll()
	set, _ := wholeTree(t, src)
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{From: 900, Count: 2}, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(out.lines, " "), "epsilon/1"; got != want {
		t.Errorf("from past the end it reads\n  %s\nwant\n  %s", got, want)
	}
	if out.done.First != Exactly(4) {
		t.Errorf("it says it began at %v, want the last row", out.done.First)
	}
}

// --- the census ---------------------------------------------------------

// watched is a source that counts how many times it is asked anything, which is
// the only way to check that one question replaced many.
type watched struct {
	Source
	opens *int
}

func (w watched) Open(descriptor *DataSetDescriptor) (DataSet, error) {
	*w.opens++
	return w.Source.Open(descriptor)
}

// wide is one root with `n` children, all of them leaves -- the shape where a
// twisty per row costs the most.
func wide(n int) *ListSource {
	rows := []Row{NewRow(NewInt(1), Record{Named("name", "root")})}
	for i := 0; i < n; i++ {
		rows = append(rows, NewRow(NewInt(int64(100+i)), Record{
			Named("name", fmt.Sprintf("leaf%d", i)), Named("parent", 1),
		}))
	}
	return NewListSource(rows)
}

// **One census answers every twisty in the tree**, so the number of questions
// stops growing with the number of expandable rows. That is the whole of what it
// was built for, and it cannot be checked by looking at the answers -- only by
// counting the asking.
func TestOneCensusAnswersEveryTwisty(t *testing.T) {
	ask := func(n int, by Criterion) int {
		opens := 0
		src := treeOf(t, TreeOptions{
			Source:     watched{wide(n), &opens},
			Descriptor: &DataSetDescriptor{Filter: &Filter{Op: OpEq, Field: "parent", Values: []*Value{nil}}},
			Types:      NodeTypes{Default: &NodeType{Children: by}},
		})
		src.ExpandAll()
		set, got := wholeTree(t, src)
		defer set.Close()
		if want := n + 1; strings.Count(got, " ")+1 != want {
			t.Fatalf("with %d leaves the tree drew %q", n, got)
		}
		return opens
	}

	// Without the census parts, a twisty is a count and a count is a question.
	blind := ChildrenByKey("parent")
	blind.Over, blind.By, blind.Group = nil, "", nil

	// And a SORTED criterion keeps its census, which is the same claim from the
	// other side: a count belongs to the filter and not to the order.
	sorted := Sorted(ChildrenByKey("parent"),
		SortLevel{Field: "name", Level: Level{Descending: true}})

	for _, n := range []int{1, 5, 20} {
		withIt, without := ask(n, ChildrenByKey("parent")), ask(n, blind)
		if got := ask(n, sorted); got != 3 {
			t.Errorf("with %d leaves and a sorted census it asked %d, want 3", n, got)
		}
		// Three: the top level, the census, and the root's children. It does not
		// move, whatever `n` is.
		if withIt != 3 {
			t.Errorf("with %d leaves and a census it asked %d questions, want 3", n, withIt)
		}
		if want := 3 + n; without != want {
			t.Errorf("with %d leaves and no census it asked %d, want %d", n, without, want)
		}
	}
}

// And the answers are the same either way, which is the part that would be easy
// to lose while making it faster.
func TestACensusGivesTheSameAnswersAsCounting(t *testing.T) {
	blind := ChildrenByKey("parent")
	blind.Over, blind.By, blind.Group = nil, "", nil

	saw := func(by Criterion) []string {
		src := treeOf(t, TreeOptions{
			Types: NodeTypes{Default: &NodeType{Children: by}},
		})
		src.ExpandAll()
		set, err := src.Open(nil)
		if err != nil {
			t.Fatal(err)
		}
		defer set.Close()
		var out treeTook
		if err := set.Read(&Scope{Count: 100}, &out); err != nil {
			t.Fatal(err)
		}
		var says []string
		for i, f := range out.fields {
			says = append(says, fmt.Sprintf("%s=%s", out.lines[i],
				Segment(f.Get("expandable"))))
		}
		return says
	}

	withIt, without := saw(ChildrenByKey("parent")), saw(blind)
	if strings.Join(withIt, " ") != strings.Join(without, " ") {
		t.Errorf("the census says\n  %v\nand counting says\n  %v", withIt, without)
	}
	if want := "alpha/0=1 beta/1=1 delta/2=0 gamma/0=1 epsilon/1=0"; strings.Join(withIt, " ") != want {
		t.Errorf("it says\n  %s\nwant\n  %s", strings.Join(withIt, " "), want)
	}
}

// **A criterion that cannot partition is counted a node at a time**, and says so
// rather than censusing something that is not what was asked. A subtree puts
// every row in the group of every one of its ancestors, so no field's values are
// those answers.
func TestASubtreeCriterionTakesNoCensus(t *testing.T) {
	sub := here.DescendantsByLocation("location")
	if sub.counts() {
		t.Error("a subtree criterion claims it can be censused")
	}
	// All three parts or none, checked here as well as at construction: this is
	// the guard that decides whether a census is attempted at all, and half of
	// one would census the wrong thing rather than declining.
	for _, half := range []Criterion{
		{Over: &DataSetDescriptor{}},
		{By: "parent"},
		{Group: func(Node) *Value { return nil }},
		{Over: &DataSetDescriptor{}, By: "parent"},
	} {
		if half.counts() {
			t.Errorf("a criterion with part of a census claims it can be taken: %+v", half)
		}
	}
	// And it still works, by counting.
	src, err := NewTreeSource(TreeOptions{
		Source:     folders(),
		Descriptor: &DataSetDescriptor{Filter: &Filter{Op: OpEq, Field: "location", Values: []*Value{NewText("/")}}},
		Types:      NodeTypes{Default: &NodeType{Children: sub, Standing: here}},
	})
	if err != nil {
		t.Fatal(err)
	}
	src.Expand("/usr")
	set, got := wholeTree(t, src)
	defer set.Close()
	// Everything under /usr, flat beneath it: the subtree, deliberately.
	if want := "usr/0 local/1 bin/1 share/1 locally/1"; got != want {
		t.Errorf("the subtree tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// A source that will not take a census is asked for one ONCE, not once per node
// -- the whole point being to ask less, and a failed attempt repeated per row
// would be worse than not trying.
//
// The rest of the arithmetic is the honest cost of a source that will neither
// census nor count, and is worth spelling out because it is what the census
// exists to avoid. Nothing can say how many children a row has, so every row
// reads `undefined`, which means draw the twisty and FIND OUT on opening -- so
// an expand-all reads a level per row, leaves included. Six rows, and the tree
// opens a sequence for every one of them twice over.
func TestASourceThatWillNotCensusIsAskedOnce(t *testing.T) {
	opens := 0
	src := treeOf(t, TreeOptions{Source: watched{uncounted{wide(5)}, &opens},
		Descriptor: &DataSetDescriptor{Filter: &Filter{Op: OpEq, Field: "parent", Values: []*Value{nil}}}})
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	if want := 6; strings.Count(got, " ")+1 != want {
		t.Fatalf("it drew %q", got)
	}
	// One attempt at a census and not one per node, which is what pins this
	// number: the top level, the failed census, the root's count and the root's
	// children, then a count and a level read for each of the five leaves. Six
	// attempts at a census instead of one would make it twenty.
	if want := 1 + 1 + 2 + 5 + 5; opens != want {
		t.Errorf("it asked %d questions, want %d", opens, want)
	}
}

// Two of the three census parts is a caller who meant to have one and silently
// will not, so it is refused where the tree is built.
func TestHalfACensusIsRefused(t *testing.T) {
	half := ChildrenByKey("parent")
	half.Group = nil
	_, err := NewTreeSource(TreeOptions{
		Source: kin(),
		Types:  NodeTypes{Default: &NodeType{Children: half}},
	})
	if err == nil {
		t.Fatal("it accepted a criterion with half a census on it")
	}
	if !strings.Contains(err.Error(), "census") {
		t.Errorf("the refusal does not say what is wrong: %v", err)
	}
}

// The shallow filter goes on the census too, or a twisty would count rows the
// level itself would not show.
func TestTheCensusCountsWhatTheLevelWouldShow(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	set, err := src.Open(&DataSetDescriptor{Filter: &Filter{
		Op: OpNot, Children: []*Filter{
			{Op: OpEq, Field: "name", Values: []*Value{NewText("beta")}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.fields[0].Get("expandable"); !Equal(got, NewInt(0)) {
		t.Errorf("alpha says %v children with its only one filtered out, want nought", got)
	}
}

// --- kinds of row -------------------------------------------------------

// **A chain of kinds declares itself, and no row carries a type name.**
//
// This is what the rename bought. While a type described somebody's CHILDREN,
// every host row had to say `applications` and every application row had to say
// `Extents` -- which means the applications source carries the view's vocabulary
// as data. A type describes a KIND OF ROW and says what kind comes next, so the
// kinds are the tree's own business and the data is left alone.
func TestAChainOfKindsDeclaresItself(t *testing.T) {
	hosts := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "a host")}),
	})
	apps := NewListSource([]Row{
		NewRow(NewInt(10), Record{Named("name", "an app"), Named("host", 1)}),
		NewRow(NewInt(11), Record{Named("name", "another app"), Named("host", 1)}),
	})
	Extents := NewListSource([]Row{
		NewRow(NewInt(100), Record{Named("name", "an Extent"), Named("app", 10)}),
	})

	src, err := NewTreeSource(TreeOptions{
		Source:     hosts,
		Descriptor: &DataSetDescriptor{},
		Types: NodeTypes{
			Default: &NodeType{Then: Always("applications")},
			Named: map[string]*NodeType{
				"applications": {
					Source:   apps,
					Children: ChildrenByKey("host"),
					Then:     Always("Extents"),
				},
				"Extents": {Source: Extents, Children: ChildrenByKey("app")},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	if want := "a host/0 an app/1 an Extent/2 another app/1"; got != want {
		t.Errorf("the chain reads\n  %s\nwant\n  %s", got, want)
	}

	// Not one record anywhere names a kind. That is the claim.
	for _, s := range []*ListSource{hosts, apps, Extents} {
		set, err := s.Open(&DataSetDescriptor{})
		if err != nil {
			t.Fatal(err)
		}
		var out censusRows
		if err := set.Read(&Scope{Count: 100}, &out); err != nil {
			t.Fatal(err)
		}
		set.Close()
		for _, f := range out.fields {
			for _, m := range f {
				if m.Name == "kind" || m.Name == "childType" {
					t.Errorf("a record carries the view's vocabulary: %v", m.Name)
				}
			}
		}
	}
}

// And a row still OVERRIDES its kind where it is unusual, which is what the
// field is for now rather than being the only way to say anything.
func TestARowOverridesItsChildrensKind(t *testing.T) {
	elsewhere := NewListSource([]Row{
		NewRow(NewInt(90), Record{Named("name", "somewhere else"), Named("via", 2)}),
	})
	rows := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "ordinary")}),
		NewRow(NewInt(2), Record{Named("name", "a mount point"), Named("kind", "elsewhere")}),
		NewRow(NewInt(3), Record{Named("name", "a child"), Named("parent", 1)}),
	})

	src, err := NewTreeSource(TreeOptions{
		Source:     rows,
		Descriptor: &DataSetDescriptor{Filter: &Filter{Op: OpEq, Field: "parent", Values: []*Value{nil}}},
		Types: NodeTypes{
			Field:   "kind",
			Default: &NodeType{Children: ChildrenByKey("parent")},
			Named: map[string]*NodeType{
				"elsewhere": {Source: elsewhere, Children: ChildrenByKey("via")},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	if want := "ordinary/0 a child/1 a mount point/0 somewhere else/1"; got != want {
		t.Errorf("with an override the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// A Then naming a kind nothing is registered under is refused where the tree is
// built. Unlike a ROW naming one -- which is data and reads as a leaf -- this is
// configuration, and configuration that cannot work is worth finding early.
func TestAThenNamingNothingIsRefused(t *testing.T) {
	_, err := NewTreeSource(TreeOptions{
		Source: kin(),
		Types:  NodeTypes{Default: &NodeType{Then: Always("nothing registered")}},
	})
	if err == nil {
		t.Fatal("it accepted a chain that goes nowhere")
	}
	if !strings.Contains(err.Error(), "nothing registered") {
		t.Errorf("the refusal does not name it: %v", err)
	}

	// And a NAMED type's chain is walked too, not only the default's -- a chain
	// breaks in the middle as readily as at the start.
	_, err = NewTreeSource(TreeOptions{
		Source: kin(),
		Types: NodeTypes{
			Default: &NodeType{Then: Always("middle")},
			Named: map[string]*NodeType{
				"middle": {Children: ChildrenByKey("parent"), Then: Always("the missing end")},
			},
		},
	})
	if err == nil {
		t.Fatal("it accepted a chain that breaks in the middle")
	}
	if !strings.Contains(err.Error(), "the missing end") {
		t.Errorf("the refusal does not name the broken link: %v", err)
	}
}

// **The top level's rows are of the default kind**, so there has to be one.
func TestATreeWithNoDefaultKindIsRefused(t *testing.T) {
	_, err := NewTreeSource(TreeOptions{
		Source: kin(),
		Types: NodeTypes{
			Field: "kind",
			Named: map[string]*NodeType{"something": {}},
		},
	})
	if err == nil {
		t.Fatal("it accepted a tree whose top level is of no kind")
	}
	if !strings.Contains(err.Error(), "default") {
		t.Errorf("the refusal does not say what is missing: %v", err)
	}
}

// **One parent, two distinct kinds of children, out of two entirely different
// sources.** A host has applications and it has volumes, and neither is a
// special case of the other.
//
// This is what made `Then` a list. With one name a parent got one kind of child,
// so the second source was simply never asked -- and worse, `Check` let it
// through, because it only asked whether SOME chain existed rather than whether
// THIS kind was on one.
func TestOneParentHasTwoKindsOfChildren(t *testing.T) {
	hosts := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "a host")}),
		NewRow(NewInt(2), Record{Named("name", "another host")}),
	})
	apps := NewListSource([]Row{
		NewRow(NewInt(10), Record{Named("name", "an app"), Named("host", 1)}),
		NewRow(NewInt(11), Record{Named("name", "another app"), Named("host", 1)}),
		NewRow(NewInt(12), Record{Named("name", "a lone app"), Named("host", 2)}),
	})
	volumes := NewListSource([]Row{
		NewRow(NewInt(20), Record{Named("name", "a disk"), Named("on", 1)}),
		NewRow(NewInt(21), Record{Named("name", "a stick"), Named("on", 1)}),
	})

	src, err := NewTreeSource(TreeOptions{
		Source:     hosts,
		Descriptor: &DataSetDescriptor{},
		Types: NodeTypes{
			Default: &NodeType{Then: Always("applications", "volumes")},
			Named: map[string]*NodeType{
				"applications": {Source: apps, Children: ChildrenByKey("host")},
				"volumes":      {Source: volumes, Children: ChildrenByKey("on")},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	// Grouped in the order the type named them: every application, then every
	// volume. And the second host has applications and no volumes, which is not
	// a special case -- it is one of the two groups being empty.
	want := "a host/0 an app/1 another app/1 a disk/1 a stick/1 " +
		"another host/0 a lone app/1"
	if got != want {
		t.Errorf("the mixed level reads\n  %s\nwant\n  %s", got, want)
	}

	// **How many children is the SUM over the kinds.** A host with two apps and
	// two volumes has four, and a twisty drawn from one kind alone would be
	// wrong about it.
	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	if n := out.fields[0].Get("expandable"); !Equal(n, NewInt(4)) {
		t.Errorf("the first host says it has %v children, want four", n)
	}
	if n := out.fields[5].Get("expandable"); !Equal(n, NewInt(1)) {
		t.Errorf("the second host says it has %v children, want one", n)
	}

	// **And every row says WHICH KIND it is.** Nothing distinguishes the kinds by
	// looking at a row: the descent knows by construction, whatever the
	// `applications` criterion returned out of the applications source being an
	// application. This field is where it says so, and without it a view holding
	// a mapping per kind could not tell which mapping a row takes.
	var kinds []string
	for _, f := range out.fields {
		kinds = append(kinds, Segment(f.Get("kind")))
	}
	if got, want := strings.Join(kinds, " "),
		" applications applications volumes volumes  applications"; got != want {
		t.Errorf("the rows say they are\n  %q\nwant\n  %q", got, want)
	}
}

// And a kind that nothing can reach is refused, which is the check that let the
// mixed level fail silently before.
func TestAKindNothingNamesIsRefused(t *testing.T) {
	_, err := NewTreeSource(TreeOptions{
		Source: kin(),
		Types: NodeTypes{
			Default: &NodeType{Then: Always("reached")},
			Named: map[string]*NodeType{
				"reached":   {Children: ChildrenByKey("parent")},
				"forgotten": {Children: ChildrenByKey("parent")},
			},
		},
	})
	if err == nil {
		t.Fatal("it accepted a kind nothing names")
	}
	if !strings.Contains(err.Error(), "forgotten") {
		t.Errorf("the refusal does not name it: %v", err)
	}

	// Where a row CAN ask for a kind by name, every named kind is reachable and
	// nothing is dead.
	if _, err := NewTreeSource(TreeOptions{
		Source: kin(),
		Types: NodeTypes{
			Field:   "kind",
			Default: &NodeType{Children: ChildrenByKey("parent")},
			Named:   map[string]*NodeType{"asked for by a row": {}},
		},
	}); err != nil {
		t.Errorf("a kind a row can ask for was refused: %v", err)
	}
}

// **A branch may say WHEN it applies**, which is what a files list needs: a
// `.zip` takes its children from the archive, a `.ini` from its sections, a
// folder from the listing, and a plain file has none.
//
// All four are rows of ONE kind, out of one source, with one column mapping.
// What differs is decided by the row's own values -- so it is a predicate and
// not a second kind, and the predicate is a `*Filter` because that is what this
// library already says a predicate with. `ends name ".zip"` needs nothing new.
//
// The alternative that does NOT work is worth naming: the override field would
// need the filesystem listing to carry `childType: "zipEntries"` as data, and a
// filesystem has an extension and has never heard of a node type.
func TestABranchAppliesOnlyWhereItsConditionHolds(t *testing.T) {
	files := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "notes.ini"), Named("in", 0)}),
		NewRow(NewInt(2), Record{Named("name", "bundle.zip"), Named("in", 0)}),
		NewRow(NewInt(3), Record{Named("name", "readme.txt"), Named("in", 0)}),
	})
	entries := NewListSource([]Row{
		NewRow(NewInt(10), Record{Named("name", "inside.txt"), Named("archive", 2)}),
	})
	sections := NewListSource([]Row{
		NewRow(NewInt(20), Record{Named("name", "[general]"), Named("file", 1)}),
		NewRow(NewInt(21), Record{Named("name", "[paths]"), Named("file", 1)}),
	})

	endsWith := func(suffix string) *Filter {
		return &Filter{Op: OpEnds, Field: "name", Values: []*Value{NewText(suffix)}}
	}

	src, err := NewTreeSource(TreeOptions{
		Source:     files,
		Descriptor: &DataSetDescriptor{Sort: []SortLevel{{Field: "name"}}},
		Types: NodeTypes{
			Default: &NodeType{Then: []Branch{
				{Kind: "zipEntries", When: endsWith(".zip")},
				{Kind: "iniSections", When: endsWith(".ini")},
			}},
			Named: map[string]*NodeType{
				"zipEntries":  {Source: entries, Children: ChildrenByKey("archive")},
				"iniSections": {Source: sections, Children: ChildrenByKey("file")},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	want := "bundle.zip/0 inside.txt/1 notes.ini/0 [general]/1 [paths]/1 readme.txt/0"
	if got != want {
		t.Errorf("the files tree reads\n  %s\nwant\n  %s", got, want)
	}

	// The .txt is a leaf and says so exactly, having met no branch's condition --
	// which is a statement and not an ignorance.
	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	for i, f := range out.fields {
		want := Exactly(0)
		switch Segment(f.Get("name")) {
		case "bundle.zip":
			want = Exactly(1)
		case "notes.ini":
			want = Exactly(2)
		}
		if got := f.Get("expandable"); !Equal(got, NewInt(int64(want.N))) {
			t.Errorf("row %d (%s) says %v children, want %v",
				i, Segment(f.Get("name")), got, want.N)
		}
	}

	// And a `.zip` is never asked for ini sections: the condition decides which
	// question is asked, not which answer is kept.
	if kinds := src.opt.Types.Beneath("", Node{
		Key: NewInt(2), Fields: Record{Named("name", "bundle.zip")},
	}); len(kinds) != 1 || kinds[0] != "zipEntries" {
		t.Errorf("a .zip's branches are %v, want the archive alone", kinds)
	}
}

// **Two branches on two separate fields, each true on its own** -- and a row
// where both are true takes BOTH sets of children.
//
// This is the case the suffix test above does not reach, and the distinction
// matters: `ends name ".zip"` and `ends name ".ini"` cannot both hold, so that
// test would pass even if the branches were mutually exclusive by construction.
// Two independent booleans is what proves each condition is asked on its own.
func TestTwoBranchesOnSeparateFieldsBothApply(t *testing.T) {
	// A row can be a zip, an ini, both, or neither. `both.dat` is a container
	// that happens to hold sections too, which is contrived and is the point.
	things := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "plain"), Named("seq", 1)}),
		NewRow(NewInt(2), Record{Named("name", "zippy"), Named("seq", 2),
			Named("isZip", true)}),
		NewRow(NewInt(3), Record{Named("name", "config"), Named("seq", 3),
			Named("isIni", true)}),
		NewRow(NewInt(4), Record{Named("name", "both"), Named("seq", 4),
			Named("isZip", true), Named("isIni", true)}),
	})
	entries := NewListSource([]Row{
		NewRow(NewInt(20), Record{Named("name", "an entry"), Named("archive", 2)}),
		NewRow(NewInt(24), Record{Named("name", "both's entry"), Named("archive", 4)}),
	})
	sections := NewListSource([]Row{
		NewRow(NewInt(30), Record{Named("name", "[a section]"), Named("file", 3)}),
		NewRow(NewInt(34), Record{Named("name", "[both's section]"), Named("file", 4)}),
	})

	isTrue := func(field string) *Filter {
		return &Filter{Op: OpEq, Field: field, Values: []*Value{NewBool(true)}}
	}

	src, err := NewTreeSource(TreeOptions{
		Source:     things,
		Descriptor: &DataSetDescriptor{Sort: []SortLevel{{Field: "seq"}}},
		Types: NodeTypes{
			Default: &NodeType{Then: []Branch{
				{Kind: "zipEntries", When: isTrue("isZip")},
				{Kind: "iniSections", When: isTrue("isIni")},
			}},
			Named: map[string]*NodeType{
				"zipEntries":  {Source: entries, Children: ChildrenByKey("archive")},
				"iniSections": {Source: sections, Children: ChildrenByKey("file")},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	// `both` takes its entries AND its sections, grouped in the order the branches
	// were named.
	want := "plain/0 zippy/0 an entry/1 config/0 [a section]/1 " +
		"both/0 both's entry/1 [both's section]/1"
	if got != want {
		t.Errorf("the tree reads\n  %s\nwant\n  %s", got, want)
	}

	// Each condition really is asked on its own: four rows, four different
	// answers about which branches apply.
	for _, c := range []struct {
		name  string
		row   Record
		kinds []string
	}{
		{"plain", Record{}, nil},
		{"zippy", Record{Named("isZip", true)}, []string{"zipEntries"}},
		{"config", Record{Named("isIni", true)}, []string{"iniSections"}},
		{"both", Record{Named("isZip", true), Named("isIni", true)},
			[]string{"zipEntries", "iniSections"}},
	} {
		got := src.opt.Types.Beneath("", Node{Fields: c.row})
		if strings.Join(got, " ") != strings.Join(c.kinds, " ") {
			t.Errorf("%s takes %v, want %v", c.name, got, c.kinds)
		}
	}

	// And the count is the SUM across the branches that applied.
	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	for i, f := range out.fields {
		want := 0
		switch Segment(f.Get("name")) {
		case "zippy", "config":
			want = 1
		case "both":
			want = 2 // one entry and one section
		}
		if got := f.Get("expandable"); !Equal(got, NewInt(int64(want))) {
			t.Errorf("row %d (%s) says %v children, want %d",
				i, Segment(f.Get("name")), got, want)
		}
	}
}
