package serval

// How long a flattening is, worked out rather than walked.
//
// The claim is not that it is faster. It is that a reader holding a window of a
// hundred thousand rows can be told how many there are -- so the thumb is true, the
// end of the sequence is reachable, and neither of those waits for a walk nobody
// asked for. See reckon.go for why it never needed the walk.

import (
	"fmt"
	"strings"
	"testing"
)

// **Nothing open: the length is the top level, counted.** One question, and the
// walk has already paid for it.
func TestALengthComesFromCountsRatherThanTheWalk(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	// ONE row read, so the walk has seen almost nothing.
	var out treeTook
	if err := set.Read(&Scope{Count: 1}, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(out.lines, " "), "alpha/0"; got != want {
		t.Fatalf("a window of one reads %q, want %q", got, want)
	}
	if got := out.done.Total; got != Exactly(2) {
		t.Errorf("it says the sequence holds %v, want exactly the two top rows", got)
	}
	// And the set says the same thing, the two being one claim about one walk.
	if got := CountOf(set); got != out.done.Total {
		t.Errorf("the set counts %v and the answer said %v", got, out.done.Total)
	}
}

// **And every open node adds its children**, out of the census -- which is one
// question per node type, so a page of open folders is a sum and not a walk.
func TestAnOpenNodeAddsItsChildrenToTheLength(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	window := func() RecordCount {
		t.Helper()
		var out treeTook
		if err := set.Read(&Scope{Count: 1}, &out); err != nil {
			t.Fatal(err)
		}
		return out.done.Total
	}

	// alpha, gamma at the top; alpha has beta; beta has delta; gamma has epsilon.
	for _, step := range []struct {
		open []string
		want int
	}{
		{nil, 2},
		{[]string{Key(NewInt(1))}, 3}, // alpha's one child
		{[]string{Key(NewInt(1)), Key(NewInt(2))}, 4}, // and beta's
		{[]string{Key(NewInt(3))}, 5},                 // and gamma's
		// A LEAF opened adds nothing, and nothing is a statement here rather than
		// an ignorance: the census saw the whole sequence, so a value it has not got
		// is nought children and not something it failed to see.
		{[]string{Key(NewInt(3)), Key(NewInt(5))}, 5},
	} {
		if step.open != nil {
			src.Expand(step.open...)
		}
		if got := window(); got != Exactly(step.want) {
			t.Errorf("with %v open it says %v, want exactly %d",
				step.open, got, step.want)
		}
	}

	// Which is the whole sequence, and reading it agrees.
	if got := drawn(t, set, &Scope{Count: 100}); got !=
		"alpha/0 beta/1 delta/2 gamma/0 epsilon/1" {
		t.Errorf("the tree reads %q", got)
	}
}

// The headline: one root of fifty children, one of them open, four rows read. The
// length is the lot, and no level was walked to find out.
func TestAWindowOfAWideTreeStillKnowsItsLength(t *testing.T) {
	src := treeOf(t, TreeOptions{
		Source: wide(50),
		Descriptor: &DataSetDescriptor{Filter: &Filter{
			Op: OpEq, Field: "parent", Values: []*Value{nil},
		}},
	})
	src.Expand(Key(NewInt(1))) // the root, by name and not by expanding everything

	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 4}, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.lines) != 4 {
		t.Fatalf("the window holds %d rows", len(out.lines))
	}
	if got := out.done.Total; got != Exactly(51) {
		t.Errorf("it says the sequence holds %v, want exactly the root and its fifty",
			got)
	}
}

// --- and what it will not claim ------------------------------------------

// **An expand-all stays a floor.** Under OpenAll the children inherit it, so an
// open node's contribution is its whole subtree -- and a subtree's size is not free.
func TestAnExpandAllStaysAFloor(t *testing.T) {
	src := treeOf(t, TreeOptions{
		Source: wide(50),
		Descriptor: &DataSetDescriptor{Filter: &Filter{
			Op: OpEq, Field: "parent", Values: []*Value{nil},
		}},
	})
	src.ExpandAll()

	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 4}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.done.Total; got != AtLeast(4) {
		t.Errorf("under an expand-all it says %v, want a floor of four", got)
	}

	// **The ROOT's own OpenAll is asked apart from the rest**, because Showing does
	// not yield the root: an expand-all with nothing touched inside it leaves the
	// trie empty, which would otherwise read as nothing being open. Here the top
	// level alone is two rows and the sequence is five, so taking the top level for
	// the whole would be a confident wrong answer rather than a short one.
	all := treeOf(t, TreeOptions{})
	all.ExpandAll()
	aset, err := all.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer aset.Close()
	var aout treeTook
	if err := aset.Read(&Scope{Count: 1}, &aout); err != nil {
		t.Fatal(err)
	}
	if got := aout.done.Total; got.Exact {
		t.Errorf("under an expand-all with nothing touched it says %v, want a floor",
			got)
	}
	if got := drawn(t, aset, &Scope{Count: 100}); got !=
		"alpha/0 beta/1 delta/2 gamma/0 epsilon/1" {
		t.Fatalf("the expanded tree reads %q, so the figure above is not the point",
			got)
	}

	// An OpenAll further down is the same refusal, reached the other way.
	plain := treeOf(t, TreeOptions{})
	plain.Marks().OpenAll(Key(NewInt(1)))
	pset, err := plain.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pset.Close()
	var pout treeTook
	if err := pset.Read(&Scope{Count: 1}, &pout); err != nil {
		t.Fatal(err)
	}
	if got := pout.done.Total; got.Exact {
		t.Errorf("with one node expanded wholesale it says %v, want a floor", got)
	}
}

// A top level that will not COUNT stays a floor, which is the case an application
// source across a connection is: the sum starts from the top level's own figure, and
// there is nothing to start from.
//
// The exactness check here IS killed by a sweep, this test being what kills it. Its
// partner -- the refusal of a sum that comes out below the floor -- is not, an
// uncounted top level being refused here before any sum is reached. It is kept as the
// net for one of the guards above it being wrong, and `exactlyOr` says so where it
// stands.
func TestATopLevelThatWillNotCountStaysAFloor(t *testing.T) {
	src := treeOf(t, TreeOptions{Source: uncounted{kin()}})
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 1}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.done.Total; got.Exact {
		t.Errorf("over a source that will not count it says %v, want a floor", got)
	}
}

// **More than one kind of row stays a floor**, one census per type being what makes
// the sum cheap and a mark being a chain of segments that does not carry a kind.
func TestTwoKindsOfRowStayAFloor(t *testing.T) {
	apps := NewListSource([]Row{
		NewRow(NewInt(10), Record{Named("name", "an app"), Named("host", 1)}),
	})
	src, err := NewTreeSource(TreeOptions{
		Source:     kin(),
		Descriptor: &DataSetDescriptor{},
		Types: NodeTypes{
			Default: &NodeType{Then: Always("apps")},
			Named: map[string]*NodeType{
				"apps": {Source: apps, Children: ChildrenByKey("host")},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 1}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.done.Total; got.Exact {
		t.Errorf("over two kinds of row it says %v, want a floor", got)
	}
}

// **A row that answers its own twisty stays a floor.** The walk believes the record
// over the census -- including when it says none, which stops the descent -- so a
// sum from the census would count rows the walk would not have read.
func TestARowAnsweringItsOwnTwistyStaysAFloor(t *testing.T) {
	// alpha says it has none although a child names it, so the descent skips a row
	// the census counts -- and gamma says nothing, which is what makes a census get
	// taken at all. Without both, the refusal would be masked: a tree where nobody
	// says nothing never takes one, and a census nobody took refuses anyway.
	rows := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "alpha"), Named("kids", false)}),
		NewRow(NewInt(2), Record{Named("name", "beta"), Named("parent", 1)}),
		NewRow(NewInt(3), Record{Named("name", "gamma")}),
	})
	src := treeOf(t, TreeOptions{
		Source:       rows,
		Descriptor:   &DataSetDescriptor{Filter: &Filter{Op: OpEq, Field: "parent", Values: []*Value{nil}}},
		SaysChildren: "kids",
	})
	src.Expand(Key(NewInt(1)))

	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	// TWO rows, so the walk reaches gamma and a census actually gets taken: a
	// census nobody took refuses anyway, which would mask the refusal being tested.
	var out treeTook
	if err := set.Read(&Scope{Count: 2}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.done.Total; got.Exact {
		t.Errorf("with a row answering its own twisty it says %v, want a floor", got)
	}
	// And the row's own word is what shows: it said no children, so none are drawn.
	// The census says one, which is exactly the figure a sum would have added.
	if got := drawn(t, set, &Scope{Count: 10}); got != "alpha/0 gamma/0" {
		t.Errorf("the tree reads %q, and alpha said it has no children", got)
	}
}

// **A loop stays a floor.** A node standing beneath itself is drawn and not
// descended into, so a mark on that occurrence contributes no rows while its census
// count says otherwise. For an adjacency list the loop IS a repeated segment.
func TestALoopStaysAFloor(t *testing.T) {
	if !loops([]string{"a", "b", "a"}) {
		t.Error("a chain naming the same node twice does not read as a loop")
	}
	if loops([]string{"a", "b", "c"}) {
		t.Error("a chain of three different nodes reads as a loop")
	}

	// A chain that SHOWS and repeats a segment, which is the only way the sum can
	// be reached with one: the node has to be open and so has its ancestor.
	src := treeOf(t, TreeOptions{})
	src.Marks().Open(Key(NewInt(1)))
	src.Marks().Open(Key(NewInt(1)), Key(NewInt(1)))

	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 1}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.done.Total; got.Exact {
		t.Errorf("with a looping chain open it says %v, want a floor", got)
	}
}

// A mark inside a CLOSED parent shows nothing, so it adds nothing -- which is the
// second rule of the mark set holding for the sum as well as for the sequence.
func TestAMarkBeneathAClosedParentAddsNothing(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	// beta open, inside alpha which is not.
	src.Marks().Open(Key(NewInt(1)), Key(NewInt(2)))
	src.Marks().Close(Key(NewInt(1)))

	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 1}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.done.Total; got != Exactly(2) {
		t.Errorf("it says %v, want exactly the two top rows", got)
	}
	if got := drawn(t, set, &Scope{Count: 10}); got != "alpha/0 gamma/0" {
		t.Errorf("the tree reads %q", got)
	}

	// Showing agrees: nothing is reachable.
	n := 0
	src.Marks().Showing(func([]string, Mark) { n++ })
	if n != 0 {
		t.Errorf("Showing yielded %d nodes beneath a closed parent", n)
	}
}

// Showing walks only what is reachable, and yields the state so a caller can tell
// an Open from an OpenAll.
func TestShowingYieldsWhatIsReachable(t *testing.T) {
	m := &Marks{}
	m.Open("a")
	m.Open("a", "b")
	m.OpenAll("c")

	seen := map[string]Mark{}
	m.Showing(func(chain []string, state Mark) {
		seen[strings.Join(chain, "/")] = state
	})
	want := map[string]Mark{"a": Open, "a/b": Open, "c": OpenAll}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Errorf("it yielded %v, want %v", seen, want)
	}

	// The ROOT is not yielded: the top level is in the sequence whatever the marks
	// say, and counting it here would count it twice.
	if _, named := seen[""]; named {
		t.Error("it yielded the root")
	}
}

// **Where a level has a standing the sum is keyed by PATHS**, because that is what
// the marks are keyed by there and what the census groups by -- and the two spell it
// differently, a mark segment being the path itself and a census group holding it as
// text. Getting that wrong is quiet: every lookup misses, every open node adds
// nought, and the length comes back plausibly too small.
func TestALengthOverAPathKeyedTreeIsCountedToo(t *testing.T) {
	src, err := NewTreeSource(TreeOptions{
		Source: folders(),
		Descriptor: &DataSetDescriptor{Filter: &Filter{
			Op: OpEq, Field: "location", Values: []*Value{NewText("/")},
		}},
		Types: NodeTypes{Default: &NodeType{
			Children: here.ChildrenByLocation("location"),
			Standing: here,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	window := func() RecordCount {
		t.Helper()
		var out treeTook
		if err := set.Read(&Scope{Count: 1}, &out); err != nil {
			t.Fatal(err)
		}
		return out.done.Total
	}

	// `/usr` alone at the top.
	if got := window(); got != Exactly(1) {
		t.Errorf("with nothing open it says %v, want exactly one", got)
	}
	// Opened: local and locally, which is two more.
	src.Expand("/usr")
	if got := window(); got != Exactly(3) {
		t.Errorf("with /usr open it says %v, want exactly three", got)
	}
	// And again: bin and share.
	src.Expand("/usr", "/usr/local")
	if got := window(); got != Exactly(5) {
		t.Errorf("with /usr/local open too it says %v, want exactly five", got)
	}
	if got := drawn(t, set, &Scope{Count: 100}); got !=
		"usr/0 local/1 bin/2 share/2 locally/1" {
		t.Errorf("the tree reads %q", got)
	}
}

// A criterion that cannot be CENSUSED is still counted while nothing is open: the
// sum needs a census only where there is something to look up, and a subtree
// criterion -- where every ancestor claims every row, so nothing partitions -- has a
// top level like any other.
func TestATreeThatCannotBeCensusedIsStillCountedWhileClosed(t *testing.T) {
	sub := ChildrenByKey("parent")
	sub.Over, sub.By, sub.Group = nil, "", nil // the census parts taken away
	src := treeOf(t, TreeOptions{
		Types: NodeTypes{Default: &NodeType{Children: sub}},
	})
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 1}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.done.Total; got != Exactly(2) {
		t.Errorf("with nothing open it says %v, want exactly the two top rows", got)
	}

	// And opening one falls back to the floor, there being no census to sum with.
	src.Expand(Key(NewInt(1)))
	var open treeTook
	if err := set.Read(&Scope{Count: 1}, &open); err != nil {
		t.Fatal(err)
	}
	if got := open.done.Total; got.Exact {
		t.Errorf("with a node open it says %v, want a floor", got)
	}
}

// **Where it cannot say exactly it still says at least the TOP LEVEL**, every one of
// whose rows is in the flattening however little of it anybody has read.
//
// It is the difference between an expand-all over two hundred folders flooring at two
// hundred and flooring at the thirty rows on screen -- which is a thumb that lurches
// to a tenth of its size the moment somebody expands everything, and then grows back
// as they scroll.
func TestAFloorIsAtLeastTheTopLevel(t *testing.T) {
	src := treeOf(t, TreeOptions{
		Source: wide(50),
		Descriptor: &DataSetDescriptor{Filter: &Filter{
			Op: OpEq, Field: "parent", Values: []*Value{nil},
		}},
	})
	src.ExpandAll()
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	// wide(50) is one root, so its top level is one row and the walk sees more than
	// that: the rows held are the better floor here.
	var out treeTook
	if err := set.Read(&Scope{Count: 4}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.done.Total; got != AtLeast(4) {
		t.Errorf("it says %v, want the four rows it holds", got)
	}

	// And the other way about: a wide TOP level, barely read.
	broad := treeOf(t, TreeOptions{Source: wideTop(200)})
	broad.ExpandAll()
	bset, err := broad.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bset.Close()
	var bout treeTook
	if err := bset.Read(&Scope{Count: 3}, &bout); err != nil {
		t.Fatal(err)
	}
	if got := bout.done.Total; got != AtLeast(200) {
		t.Errorf("with three rows read of two hundred at the top it says %v,"+
			" want a floor of the whole top level", got)
	}
}

// wideTop is `n` rows at the top with a child each, so the top level is large and a
// small window sees almost none of it.
func wideTop(n int) *ListSource {
	var rows []Row
	for i := 0; i < n; i++ {
		key := int64(1000 + i)
		rows = append(rows, NewRow(NewInt(key), Record{
			Named("name", fmt.Sprintf("top%03d", i)),
		}))
		rows = append(rows, NewRow(NewInt(key*100), Record{
			Named("name", fmt.Sprintf("kid%03d", i)), Named("parent", key),
		}))
	}
	return NewListSource(rows)
}
