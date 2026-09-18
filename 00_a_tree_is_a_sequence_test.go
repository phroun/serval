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
	if o.Spec == nil {
		o.Spec = &Spec{Filter: &Filter{
			Op: OpEq, Field: "parent", Values: []*Value{nil},
		}}
	}
	if o.Types.Default == nil && o.Types.Named == nil {
		o.Types = ChildTypes{Default: &ChildType{Children: ChildrenByKey("parent")}}
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
// sorted by its own spec and the pre-order is laid over that.
func TestATreeRefusesASortOfItsOwnSequence(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	_, err := src.Open(&Spec{Sort: []SortLevel{{Field: "name"}}})
	if err == nil {
		t.Fatal("it accepted a sort of the flattened sequence")
	}
	if !strings.Contains(err.Error(), "pre-order") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// A level's OWN sort is honoured, which is where ordering belongs.
func TestEachLevelCarriesItsOwnSort(t *testing.T) {
	down := []SortLevel{{Field: "name", Level: Level{Descending: true}}}
	src := treeOf(t, TreeOptions{
		Spec: &Spec{
			Filter: &Filter{Op: OpEq, Field: "parent", Values: []*Value{nil}},
			Sort:   down,
		},
		Types: ChildTypes{Default: &ChildType{
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
// child spec was counted and came back empty.
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
		Spec:         &Spec{},
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
		Source:   folders(),
		Spec:     &Spec{Filter: &Filter{Op: OpEq, Field: "location", Values: []*Value{NewText("/")}}},
		Standing: here,
		Types: ChildTypes{Default: &ChildType{
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
	src := treeOf(t, TreeOptions{Source: looper(), Spec: &Spec{}})
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
			Source: looper(), Spec: &Spec{}, Revisits: budget,
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
	src := treeOf(t, TreeOptions{Source: looper(), Spec: &Spec{}, Revisits: 99})
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

// **One tree, several shapes.** A row names a different child type, and its
// children come out of a different source entirely -- which is the thing the
// whole child-type mechanism exists for.
func TestARowMayNameAChildTypeInAnotherSource(t *testing.T) {
	windows := NewListSource([]Row{
		NewRow(NewInt(90), Record{Named("name", "a window"), Named("app", 1)}),
		NewRow(NewInt(91), Record{Named("name", "another"), Named("app", 1)}),
	})
	apps := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "an app"), Named("kind", "windows")}),
		NewRow(NewInt(2), Record{Named("name", "a plain row")}),
	})

	src, err := NewTreeSource(TreeOptions{
		Source: apps,
		Spec:   &Spec{},
		Types: ChildTypes{
			Field:   "kind",
			Default: &ChildType{Children: ChildrenByKey("parent")},
			Named: map[string]*ChildType{
				"windows": {Source: windows, Children: ChildrenByKey("app")},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	src.ExpandAll()
	set, got := wholeTree(t, src)
	defer set.Close()

	if want := "an app/0 a window/1 another/1 a plain row/0"; got != want {
		t.Errorf("the grafted tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// A shallow filter is asked at every level: a parent that does not match is gone
// and its children with it.
func TestAFilterIsAskedAtEveryLevel(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.ExpandAll()

	set, err := src.Open(&Spec{Filter: &Filter{
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

	set, err := src.Open(&Spec{Filter: &Filter{
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
	set, err := src.Open(&Spec{Filter: &Filter{
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
	src := treeOf(t, TreeOptions{Source: clash, Spec: &Spec{}})
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
	moved := treeOf(t, TreeOptions{Source: clash, Spec: &Spec{},
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
