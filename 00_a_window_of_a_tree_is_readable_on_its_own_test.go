package serval

// What a reader holding a WINDOW of a tree has to be told, over and above what a
// reader holding the whole of it needs.
//
// A flattening stops where its budget runs out, and the two facts that follow
// from that are separate and both had to be said. How long the sequence is
// becomes a FLOOR, because rows the walk never reached are rows it cannot count.
// And a row's ancestry stops being derivable, because the trick that derives it
// -- pre-order puts every ancestor above its descendant -- only works for a
// reader that kept everything above. A window of rows forty to eighty kept none
// of it, and clicking a twisty is `Marks.Open(chain...)`.

import (
	"fmt"
	"strings"
	"testing"
)

// chainOf is a row's chain as one readable line, the segments barred apart --
// a bar and not the delimiter, because a path-keyed chain's segments contain
// that and the two would be indistinguishable.
func chainOf(f Record, under string) string {
	v := f.Get(under)
	if v == nil || v.Kind != ListValue {
		return "<none>"
	}
	segs := make([]string, len(v.List))
	for i, m := range v.List {
		segs[i] = Segment(m.Value)
	}
	return strings.Join(segs, "|")
}

// segments is a row's chain as the marks take it.
func segments(f Record, under string) []string {
	v := f.Get(under)
	if v == nil || v.Kind != ListValue {
		return nil
	}
	segs := make([]string, len(v.List))
	for i, m := range v.List {
		segs[i] = Segment(m.Value)
	}
	return segs
}

// **A count the walk did not earn is a floor.** A reader that asked for four
// rows of a tree with fifty is told AT LEAST four, and draws a thumb that
// shrinks as it learns more rather than a true one it invented.
//
// `RecordCount` said this from the start. What it did not do was say it to the
// reader that ASKED -- `Complete.Total` came back exact whatever the walk had
// seen, so a view believing the answer it was handed would put the last row of a
// hundred thousand at position four and refuse to scroll past it.
func TestAWindowedReadSaysTheCountIsAFloor(t *testing.T) {
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
	if len(out.lines) != 4 {
		t.Fatalf("the window holds %d rows, want four: %v", len(out.lines), out.lines)
	}
	if got := out.done.Total; got != AtLeast(4) {
		t.Errorf("it says the sequence holds %v, want at least four", got)
	}
	// The two ways of asking cannot disagree: they are one claim about one walk.
	if got := CountOf(set); got != out.done.Total {
		t.Errorf("the set counts %v and the answer said %v", got, out.done.Total)
	}

	// And a reader that asks for the whole thing gets the figure exactly, which
	// is what every reader of a tree had before there was another way to ask.
	var all treeTook
	if err := set.Read(&Scope{Count: 1000}, &all); err != nil {
		t.Fatal(err)
	}
	if got := all.done.Total; got != Exactly(51) {
		t.Errorf("reading the whole tree it says %v, want exactly fifty-one", got)
	}
}

// **A row carries the mark segments from the root down to it**, which is what
// makes a window clickable.
//
// The chain is the walk's own: a segment appended per level, so it costs one list
// per row and nothing is searched for it.
func TestAFlattenedRowCarriesTheChainTheWalkSpelled(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.ExpandAll()
	set, _ := wholeTree(t, src)
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	var says []string
	for i, f := range out.fields {
		says = append(says, fmt.Sprintf("%s=%s", out.lines[i], chainOf(f, "chain")))
	}
	// **Spelled as `Key` spells an identity**, and not as the number reads. The
	// chain is handed to Marks verbatim, and Marks is keyed by what the walk put
	// there -- so a test that wrote `1` would be describing a segment that opens
	// nothing.
	k := func(n int64) string { return Key(NewInt(n)) }
	want := fmt.Sprintf("alpha/0=%s beta/1=%s|%s delta/2=%s|%s|%s gamma/0=%s epsilon/1=%s|%s",
		k(1), k(1), k(2), k(1), k(2), k(4), k(3), k(3), k(5))
	if got := strings.Join(says, " "); got != want {
		t.Errorf("the chains read\n  %s\nwant\n  %s", got, want)
	}
}

// And a row read in a window that holds NONE of its ancestors carries the whole
// of it anyway. This is the case the field exists for: everything above is what
// the reader declined to hold, so deriving the chain would mean fetching back
// exactly what was windowed away in order to click on what is already there.
func TestARowKeepsItsChainWhenItsAncestorsAreOutsideTheWindow(t *testing.T) {
	src := treeOf(t, TreeOptions{})
	src.ExpandAll()
	set, _ := wholeTree(t, src)
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{From: 2, Count: 1}, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(out.lines, " "), "delta/2"; got != want {
		t.Fatalf("the window reads %q, want %q", got, want)
	}
	want := strings.Join([]string{Key(NewInt(1)), Key(NewInt(2)), Key(NewInt(4))}, "|")
	if got := chainOf(out.fields[0], "chain"); got != want {
		t.Errorf("its chain reads %q, want %q -- neither ancestor is in the window", got, want)
	}

	// And the chain is what `Marks` takes, which is the whole point of holding
	// it: a reader with this row and nothing else can close its grandparent.
	src.Collapse(segments(out.fields[0], "chain")[0])
	if got, want := drawn(t, set, &Scope{Count: 100}), "alpha/0 gamma/0 epsilon/1"; got != want {
		t.Errorf("after closing the chain's root the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// The chain is one of the tree's own fields, so it shadows a record's like the
// rest of them and its name is the caller's to move.
func TestTheChainShadowsTheRecordsOwn(t *testing.T) {
	clash := NewListSource([]Row{
		NewRow(NewInt(1), Record{
			Named("name", "alpha"), Named("chain", "the record's own"),
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
	if got, want := chainOf(out.fields[0], "chain"), Key(NewInt(1)); got != want {
		t.Errorf("chain is %q, want the tree's own %q", got, want)
	}
	// One member under the name, not two.
	n := 0
	for _, m := range out.fields[0] {
		if m.Name == "chain" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the row carries %d members called chain", n)
	}

	moved := treeOf(t, TreeOptions{Source: clash, Descriptor: &DataSetDescriptor{},
		Fields: TreeFields{Chain: "ancestry"}})
	set2, err := moved.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set2.Close()
	var out2 treeTook
	if err := set2.Read(&Scope{Count: 10}, &out2); err != nil {
		t.Fatal(err)
	}
	if got := out2.fields[0].Get("chain"); !Equal(got, NewText("the record's own")) {
		t.Errorf("with the tree's name moved, chain is %v", got)
	}
	if got, want := chainOf(out2.fields[0], "ancestry"), Key(NewInt(1)); got != want {
		t.Errorf("and ancestry is %q, want %q", got, want)
	}
}

// **Where a level has a standing the chain is spelled in PATHS**, because that
// is what the marks are keyed by there -- and a chain of identities would name a
// node the walk never visited, which opens nothing and says nothing.
//
// So the chain is not "the keys above this row". It is what the WALK spelled,
// which is the only thing `Marks` answers to.
func TestTheChainIsSpelledTheWayTheMarksAre(t *testing.T) {
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
	src.ExpandAll()
	set, _ := wholeTree(t, src)
	defer set.Close()

	var out treeTook
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatal(err)
	}
	var says []string
	for i, f := range out.fields {
		says = append(says, fmt.Sprintf("%s=%s", out.lines[i], chainOf(f, "chain")))
		// The last segment IS the row's own path, the standing having built one.
		segs := segments(f, "chain")
		if len(segs) == 0 {
			t.Errorf("row %s carries no chain at all", out.lines[i])
			continue
		}
		if got, want := segs[len(segs)-1], Segment(f.Get("path")); got != want {
			t.Errorf("row %s: the chain ends in %q and its path is %q",
				out.lines[i], got, want)
		}
	}
	want := "usr/0=/usr local/1=/usr|/usr/local bin/2=/usr|/usr/local|/usr/local/bin " +
		"share/2=/usr|/usr/local|/usr/local/share locally/1=/usr|/usr/locally"
	if got := strings.Join(says, " "); got != want {
		t.Errorf("the chains read\n  %s\nwant\n  %s", got, want)
	}
}
