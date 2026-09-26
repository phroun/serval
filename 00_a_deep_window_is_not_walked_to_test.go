package serval

// Reaching a position deep in a flattening without walking to it.
//
// A tree is a sequence, so a reader names a position in it and expects the rows
// standing there. The flattening used to get them the only way it knew: from the
// top, a row at a time, every one of them held. A reader at row ninety-nine
// thousand paid for ninety-nine thousand rows to look at ten -- read out of the
// source, turned into records, and kept.
//
// What makes the short cut sound is arithmetic on the MARKS, not on the tree. Where
// nothing beneath a level is open, each of its rows stands for exactly one row of
// the flattening, so the nth row of the level IS the nth row of the subtree and the
// level can be entered at a position. One node open anywhere beneath and that stops
// being true -- its children stand between its siblings -- and the walk is the only
// way again. See Marks.Flat.

import (
	"fmt"
	"strings"
	"testing"
)

// countedRows is a flat source that remembers what it was asked and what it sent,
// which is the whole of what this measures.
type countedRows struct {
	n      int
	honour bool // whether it acts on `from`, a source honouring one being optional

	asked []Scope // one entry per Read
	sent  int     // records handed over, all reads together
}

func (s *countedRows) Open(descriptor *DataSetDescriptor) (DataSet, error) {
	return &countedSet{src: s}, nil
}

type countedSet struct{ src *countedRows }

func (v *countedSet) Close()                   {}
func (v *countedSet) RecordCount() RecordCount { return Exactly(v.src.n) }
func (v *countedSet) TreeFields() TreeFields   { return TreeFields{} }
func (v *countedSet) Read(s *Scope, out Sink) error {
	v.src.asked = append(v.src.asked, *s)
	from := 0
	if v.honouring() && s.From > 0 {
		from = s.From
	}
	out.Ordered()
	for i := from; i < v.src.n; i++ {
		if i-from >= s.Count {
			break
		}
		v.src.sent++
		if err := out.Record(NewInt(int64(i)), Record{
			Named("name", fmt.Sprintf("row%05d", i)),
			Named("parent", nil),
		}); err != nil {
			return err
		}
	}
	// Where it began, which is the whole of what a source owes a reader that asked
	// for a position: a source that would not jump says it started at the top.
	out.Done(Complete{First: Exactly(from), Total: Exactly(v.src.n)})
	return nil
}

func (v *countedSet) honouring() bool { return v.src.honour }

// flatTree is a tree over one flat level: every row a leaf, nothing to open.
func flatTree(t *testing.T, src Source) *TreeSource {
	t.Helper()
	tree, err := NewTreeSource(TreeOptions{
		Source: src,
		Types:  NodeTypes{Default: &NodeType{}},
	})
	if err != nil {
		t.Fatalf("making the tree: %v", err)
	}
	return tree
}

// **A window a long way down costs one question and the rows in it.**
func TestADeepWindowIsAskedForRatherThanWalkedTo(t *testing.T) {
	const rows = 100000
	src := &countedRows{n: rows, honour: true}
	set, err := flatTree(t, src).Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{From: rows - 10, Count: 10}, &out); err != nil {
		t.Fatal(err)
	}
	if out.done.Error != "" {
		t.Fatalf("the answer refused: %s", out.done.Error)
	}

	// The right rows, which is the part none of the rest is worth anything without.
	if got, want := len(out.lines), 10; got != want {
		t.Fatalf("it answered %d rows, want %d", got, want)
	}
	if got, want := out.lines[0], "row99990/0"; got != want {
		t.Errorf("the window begins at %q, want %q", got, want)
	}
	if got, want := out.lines[9], "row99999/0"; got != want {
		t.Errorf("the window ends at %q, want %q", got, want)
	}
	// And it says where it began, which is what a reader places them by.
	if got := out.done.First; !got.Exact || got.N != rows-10 {
		t.Errorf("the answer began at %v, want exactly %d", got, rows-10)
	}

	// **The cost.** The level was asked to begin at the position, and what crossed
	// is the window and not the sequence.
	if len(src.asked) != 1 {
		t.Fatalf("the level was read %d times, want once", len(src.asked))
	}
	if got := src.asked[0].From; got != rows-10 {
		t.Errorf("the level was asked from row %d, want %d", got, rows-10)
	}
	if src.sent > 20 {
		t.Errorf("%d records crossed to answer a window of ten", src.sent)
	}
}

// A source that will NOT jump is answered from the beginning, and the walk passes
// over the rows itself. Slower, never wrong -- which is the whole contract of a
// position being best effort.
func TestASourceThatWillNotJumpIsStillRight(t *testing.T) {
	const rows = 2000
	src := &countedRows{n: rows} // honour: false
	set, err := flatTree(t, src).Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{From: rows - 3, Count: 3}, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(out.lines, " "),
		"row01997/0 row01998/0 row01999/0"; got != want {
		t.Errorf("the window reads %q, want %q", got, want)
	}
	if got := out.done.First; !got.Exact || got.N != rows-3 {
		t.Errorf("the answer began at %v, want exactly %d", got, rows-3)
	}

	// **The retry is paid once.** Which sources will jump is a fact about the
	// source, so a tree that has been answered from the beginning stops asking --
	// otherwise every walk of a naive source reads its prefix twice for ever.
	asked := len(src.asked)
	var again treeTook
	if err := set.Read(&Scope{From: rows - 6, Count: 3}, &again); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(again.lines, " "),
		"row01994/0 row01995/0 row01996/0"; got != want {
		t.Errorf("the second window reads %q, want %q", got, want)
	}
	if got := len(src.asked) - asked; got != 1 {
		t.Errorf("the second window cost %d reads of a source that will not jump,"+
			" want one", got)
	}
}

// **An open node beneath is what makes the arithmetic wrong**, so it is not used.
// The rows a reader gets are the same either way; what changes is the price.
func TestAnOpenNodeBelowStopsTheShortCut(t *testing.T) {
	// The ordinary test tree: alpha, gamma at the top, with children beneath.
	src := treeOf(t, TreeOptions{})
	src.Expand(Key(NewInt(1))) // alpha, whose child stands between alpha and gamma
	set, err := src.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	// Row two is gamma, and it is row two only because beta stands at row one.
	if got, want := drawn(t, set, &Scope{From: 2, Count: 1}), "gamma/0"; got != want {
		t.Errorf("row 2 of the flattening reads %q, want %q", got, want)
	}
	// And the marks say so, which is what the descent asks before skipping.
	if src.Marks().Flat() {
		t.Error("an open node is held and the marks call the flattening flat")
	}
	src.Collapse(Key(NewInt(1)))
	if !src.Marks().Flat() {
		t.Error("nothing is open and the marks will not say the flattening is flat")
	}

	// **An expand-all is the same answer arrived at from above.** Nothing is marked
	// beneath the top level, and every row of it still shows its children: what a
	// level INHERITS decides this as much as what is marked under it.
	src.ExpandAll()
	if src.Marks().Flat() {
		t.Error("everything is open and the marks call the flattening flat")
	}
	all := drawn(t, set, &Scope{Count: 100})
	rows := strings.Fields(all)
	if len(rows) < 4 {
		t.Fatalf("the expanded tree reads %q, which is too short to tell anything", all)
	}
	// The third row of the flattening, which is a CHILD -- the third row of the top
	// level is a different row, and asking the level for a position would find it.
	if got, want := drawn(t, set, &Scope{From: 2, Count: 1}), rows[2]; got != want {
		t.Errorf("row 2 of the expanded flattening reads %q, want %q", got, want)
	}
}

// --- what a window costs a reader that was holding a prefix ---------------

// **A floor does not come down.**
//
// What is held is a window, so a walk further down can hold FEWER rows than one
// before it. A length taken from the window alone then shrinks as a reader scrolls
// -- a thumb that grows and jumps back, and a sequence that loses rows nobody
// removed. A row seen once is a row that exists.
func TestAFloorDoesNotComeDownAsTheWindowMovesOn(t *testing.T) {
	const rows = 500
	src := &countedRows{n: rows, honour: true}
	// **A tree nobody can count without walking it**, which is what leaves the
	// length a floor at all: a top level that counts itself floors there whatever
	// the window holds, and there is then no floor to come down. See reckon.go.
	tree := flatTree(t, uncounted{src})
	set, err := tree.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	floor := func(from, count int) RecordCount {
		t.Helper()
		var out treeTook
		if err := set.Read(&Scope{From: from, Count: count}, &out); err != nil {
			t.Fatal(err)
		}
		return out.done.Total
	}

	far := floor(300, 40) // reaches row 340
	if far.Exact || far.N < 340 {
		t.Fatalf("a window reaching row 340 floors at %v", far)
	}
	// A window somewhere else entirely, and nearer the top: the rows it holds say
	// nothing about the ones it saw and let go, and those rows are still there.
	near := floor(100, 4)
	if near.N < far.N {
		t.Errorf("the floor fell from %v to %v as the window moved on", far, near)
	}
}

// **And where a row stands is remembered past the window that showed it.**
//
// A reader asks for what comes after a record it holds, and it holds rows the set
// has since slid past. An index rebuilt each walk loses the one row the question is
// about, and a scope that names a row nobody can place starts again at the top --
// which is the window undone, and reads as a reader that cannot scroll.
func TestARowsPlaceSurvivesTheWindowSlidingPastIt(t *testing.T) {
	const rows = 500
	src := &countedRows{n: rows, honour: true}
	set, err := flatTree(t, src).Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	// A window at the top, and the last row of it is what the reader will name.
	var first treeTook
	if err := set.Read(&Scope{Count: 10}, &first); err != nil {
		t.Fatal(err)
	}
	held := first.done.Watermark
	if held == nil {
		t.Fatal("the first window named no row to carry on from")
	}

	// The reader moves on, and the window slides away from that row.
	var moved treeTook
	if err := set.Read(&Scope{From: 200, Count: 10}, &moved); err != nil {
		t.Fatal(err)
	}

	// And now it asks for what came after the row it still holds.
	var back treeTook
	if err := set.Read(&Scope{After: held, Count: 3}, &back); err != nil {
		t.Fatal(err)
	}
	if back.done.Error != "" {
		t.Fatalf("a row it was shown is a row it cannot name: %s", back.done.Error)
	}
	if got, want := strings.Join(back.lines, " "),
		"row00010/0 row00011/0 row00012/0"; got != want {
		t.Errorf("after the tenth row it reads %q, want %q", got, want)
	}
	if got := back.done.First; !got.Exact || got.N != 10 {
		t.Errorf("the answer began at %v, want exactly row 10", got)
	}
}

// **Two readers of one sequence are not one reader.**
//
// A view asks about the rows on screen and, a moment later, about a row somebody
// dragged a thumb to. A window that simply replaced what was held would answer each
// by throwing the other's away, and the two asks would walk over each other for
// ever -- neither ever there when it was looked for. What that reads as is a list
// that will not scroll.
func TestARunThatTouchesWhatIsHeldJoinsIt(t *testing.T) {
	const rows = 5000
	src := &countedRows{n: rows, honour: true}
	set, err := flatTree(t, src).Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	read := func(from, count int) treeTook {
		t.Helper()
		var out treeTook
		if err := set.Read(&Scope{From: from, Count: count}, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	read(0, 20)  // the viewport
	read(20, 20) // and on down: a run that carries on from what is held
	walks := len(src.asked)

	// Both are answered now, and neither costs a walk: what is held covers them.
	if got := read(0, 20); got.lines[0] != "row00000/0" {
		t.Errorf("the first window reads %q", got.lines[0])
	}
	if got := read(30, 4); got.lines[0] != "row00030/0" {
		t.Errorf("the second window reads %q", got.lines[0])
	}
	if len(src.asked) != walks {
		t.Errorf("answering what was already held took %d more reads of the source",
			len(src.asked)-walks)
	}

	// And a row of the joined run is where the run says it is, which is what a
	// reader carrying on from a record it holds depends on.
	var back treeTook
	if err := set.Read(&Scope{After: NewInt(24), Count: 2}, &back); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(back.lines, " "), "row00025/0 row00026/0"; got != want {
		t.Errorf("after row 24 it reads %q, want %q", got, want)
	}
	if got := back.done.First; !got.Exact || got.N != 25 {
		t.Errorf("the answer began at %v, want exactly row 25", got)
	}
}

// A walk landing somewhere else entirely starts again, which is what keeps a jump
// to the far end from dragging the rows between along with it.
func TestAWalkLandingElsewhereStartsAgain(t *testing.T) {
	const rows = 5000
	src := &countedRows{n: rows, honour: true}
	set, err := flatTree(t, src).Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var near treeTook
	if err := set.Read(&Scope{Count: 20}, &near); err != nil {
		t.Fatal(err)
	}
	var out treeTook
	if err := set.Read(&Scope{From: 4000, Count: 20}, &out); err != nil {
		t.Fatal(err)
	}
	// Nothing of the sequence between was read to get there: two windows of twenty
	// and the spare row each answer carries, and nothing else.
	if src.sent > 44 {
		t.Errorf("%d records crossed for two windows of twenty", src.sent)
	}
	if got := out.lines[0]; got != "row04000/0" {
		t.Errorf("the far window begins at %q", got)
	}
}
