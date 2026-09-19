package serval

// Wrapping must not cost a capability.
//
// A `Source` has one method, so a wrapper that forwards `Open` compiles and looks
// finished. Every optional interface it does not forward is something the child could
// do and the wrapper cannot -- and it fails quietly: nothing refuses, a hierarchy
// just reads flat, or a tree draws no twisties.
//
// So each one is asked of each wrapper here, including the two cases where the right
// answer is to say nothing.

import (
	"strings"
	"testing"
)

// shaped is a source that says what its records are, so a wrapper over it can be
// asked whether it still says so.
func shaped() *ListSource {
	src := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "usr"), Named("kids", 1)}),
		NewRow(NewInt(2), Record{Named("name", "local"), Named("up", 1)}),
		NewRow(NewInt(3), Record{Named("name", "etc")}),
	})
	src.SetTreeHint(TreeHint{Parent: "up", Label: "name", Children: "kids"})
	return src
}

// A cache and an amendment go on saying what shape the records are.
//
// It is the case that matters most in practice: a source across a connection gets
// wrapped so its reader can edit a cell, and if the wrapper swallowed the hint the
// hierarchy would silently become one flat level.
func TestAWrapperGoesOnSayingWhatShapeTheRecordsAre(t *testing.T) {
	child := shaped()
	want, _ := TreeHintOf(child)
	if want.Nothing() {
		t.Fatal("the child says nothing, so nothing is being tested")
	}

	for _, over := range []struct {
		what string
		src  Source
	}{
		{"a cache", NewCachedSource(child)},
		{"an amendment", NewAmendedSource(child)},
	} {
		got, said := TreeHintOf(over.src)
		if !said {
			t.Errorf("%s says nothing about shape", over.what)
			continue
		}
		if got != want {
			t.Errorf("%s says %+v, want the child's %+v", over.what, got, want)
		}
	}
}

// An amendment's OWN saying wins, because it is the more specific one: whoever
// assembled the layer and described it knows what the layer is. A bundle is exactly
// that -- its document describes the assembled whole.
func TestAnAmendmentsOwnHintBeatsItsChilds(t *testing.T) {
	a := NewAmendedSource(shaped())
	a.SetTreeHint(TreeHint{Location: "dir", Name: "name", Delimiter: "/"})

	got, said := TreeHintOf(a)
	if !said || got.Location != "dir" {
		t.Errorf("it says %+v, want its own", got)
	}
	if got.Parent != "" {
		t.Error("it mixed its own saying with the child's")
	}
}

// **A composition says nothing about shape, and that is the right answer.**
//
// Its records are a union of differently shaped includes under keys it prefixes, so
// one include's hint does not describe what comes out: `up: 1` names a key that is
// now `left/1`. A bundle's hint is said on the amendment OVER the composition for
// exactly this reason.
func TestACompositionSaysNothingAboutShape(t *testing.T) {
	c, err := NewComposedSource(
		Include{Name: "left", Source: shaped()},
		Include{Name: "right", Source: shaped()},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, said := TreeHintOf(c); said && !got.Nothing() {
		t.Errorf("a composition claims its records are %+v", got)
	}

	// One include is no different: a union of one is still a union, and its keys
	// still wear a prefix.
	one, err := NewComposedSource(Include{Name: "only", Source: shaped()})
	if err != nil {
		t.Fatal(err)
	}
	if got, said := TreeHintOf(one); said && !got.Nothing() {
		t.Errorf("a composition of one claims %+v", got)
	}
}

// A wrapper over a TREE goes on saying what that tree writes its own fields under. A
// field name is not a shape, so even a composition may hand this on.
func TestAWrapperGoesOnSayingATreesFieldNames(t *testing.T) {
	tree, err := NewTreeSource(TreeOptions{
		Source:     shaped(),
		Descriptor: &DataSetDescriptor{Filter: &Filter{Op: OpEq, Field: "up", Values: []*Value{nil}}},
		Types:      NodeTypes{Default: &NodeType{Children: ChildrenByKey("up")}},
		Fields: TreeFields{Depth: "tree:depth", Path: "tree:path",
			Expandable: "tree:kids", State: "tree:state", Kind: "tree:kind"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := TreeFieldsOf(tree)

	for _, over := range []struct {
		what string
		src  Source
	}{
		{"a cache", NewCachedSource(tree)},
		{"an amendment", NewAmendedSource(tree)},
	} {
		got, said := TreeFieldsOf(over.src)
		if !said || got != want {
			t.Errorf("%s says %+v, want the tree's %+v", over.what, got, want)
		}
	}

	// A composition of one tree has one answer and gives it.
	one, err := NewComposedSource(Include{Name: "only", Source: tree})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := TreeFieldsOf(one); got != want {
		t.Errorf("a composition of one tree says %+v, want %+v", got, want)
	}

	// Two trees under DIFFERENT names have no one answer, and nothing is what that
	// gets -- rather than one of them offered for rows that are not its.
	other, err := NewTreeSource(TreeOptions{
		Source:     shaped(),
		Descriptor: &DataSetDescriptor{Filter: &Filter{Op: OpEq, Field: "up", Values: []*Value{nil}}},
		Types:      NodeTypes{Default: &NodeType{Children: ChildrenByKey("up")}},
		Fields:     TreeFields{Depth: "deep"},
	})
	if err != nil {
		t.Fatal(err)
	}
	two, err := NewComposedSource(
		Include{Name: "a", Source: tree}, Include{Name: "b", Source: other})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := TreeFieldsOf(two); got.Depth != "" {
		t.Errorf("two trees under two names answered %+v", got)
	}
}

// --- the census -----------------------------------------------------------

// countable is three records over two kinds, so a census of `kind` has two groups.
func countable() *ListSource {
	return NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("kind", "folder")}),
		NewRow(NewInt(2), Record{Named("kind", "file")}),
		NewRow(NewInt(3), Record{Named("kind", "file")}),
	})
}

// censusOver is what a source says when its sequence is partitioned by a field.
func censusOver(t *testing.T, src Source, field string) (Census, error) {
	t.Helper()
	set, err := src.Open(&DataSetDescriptor{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	return CensusOf(set, field)
}

// A cache hands a census on: what it holds is records and their places, and a census
// is neither -- it is an answer about the whole sequence, and the child knows it.
func TestACacheHandsACensusOn(t *testing.T) {
	got, err := censusOver(t, NewCachedSource(countable()), "kind")
	if err != nil {
		t.Fatalf("a cache refused a census its child would take: %v", err)
	}
	if n := got.CountOfGroup(NewText("file")); !n.Exact || n.N != 2 {
		t.Errorf("it counted %v files, want exactly two", n)
	}
}

// An UNAMENDED amendment is its child, so it hands one on too -- a layer nobody has
// written in has no business costing anything.
func TestAnUnamendedSourceHandsACensusOn(t *testing.T) {
	got, err := censusOver(t, NewAmendedSource(countable()), "kind")
	if err != nil {
		t.Fatalf("an unamended source refused a census: %v", err)
	}
	if n := got.CountOfGroup(NewText("file")); !n.Exact || n.N != 2 {
		t.Errorf("it counted %v files, want exactly two", n)
	}
}

// **And one that holds an amendment refuses**, rather than passing on an answer that
// is no longer the sequence's.
//
// Every kind of amendment can break a partition: a deletion takes a record out of its
// group, an addition puts one in, a replacement may move it, and an alteration may
// change the very member being counted. Refused rather than corrected, because the
// callers all have a fallback and none of them has a way to notice a wrong count.
func TestAnAmendedSourceRefusesACensus(t *testing.T) {
	for _, amend := range []struct {
		what string
		do   func(*AmendedSource)
	}{
		{"a replacement", func(a *AmendedSource) {
			a.Replace(NewInt(1), Record{Named("kind", "file")})
		}},
		{"an alteration", func(a *AmendedSource) {
			a.Alter(NewInt(1), Record{Named("kind", "file")})
		}},
		{"an addition", func(a *AmendedSource) {
			a.Add(NewInt(9), Record{Named("kind", "file")})
		}},
		{"a deletion", func(a *AmendedSource) {
			a.Delete(NewInt(2), Record{Named("kind", "file")})
		}},
	} {
		a := NewAmendedSource(countable())
		amend.do(a)
		_, err := censusOver(t, a, "kind")
		if err == nil {
			t.Errorf("with %s held, the census was answered as though nothing had changed",
				amend.what)
			continue
		}
		if !strings.Contains(err.Error(), "amendments") {
			t.Errorf("with %s held, it refused with %q", amend.what, err)
		}
	}
}

// A composition SUMS its includes' censuses, because a group is a value and a value
// means the same thing in every include -- unlike a key, which is prefixed, and which
// is why a composition can say nothing about shape but can say this.
func TestACompositionSumsItsIncludesCensuses(t *testing.T) {
	c, err := NewComposedSource(
		Include{Name: "left", Source: countable()},
		Include{Name: "right", Source: countable()},
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := censusOver(t, c, "kind")
	if err != nil {
		t.Fatalf("a composition of countable includes refused: %v", err)
	}
	// Two includes of two files each, and the same value in both is ONE group.
	if n := got.CountOfGroup(NewText("file")); !n.Exact || n.N != 4 {
		t.Errorf("it counted %v files, want exactly four", n)
	}
	if n := got.CountOfGroup(NewText("folder")); !n.Exact || n.N != 2 {
		t.Errorf("it counted %v folders, want exactly two", n)
	}
	if !got.Total.Exact || got.Total.N != 2 {
		t.Errorf("it says there are %v distinct kinds, want exactly two", got.Total)
	}
	// By value, so a merged census and a plain one are one answer.
	for i := 1; i < len(got.Groups); i++ {
		if Compare(got.Groups[i-1].Value, got.Groups[i].Value, "") > 0 {
			t.Errorf("group %d sorts after %d", i-1, i)
		}
	}
}

// One include that cannot census refuses the whole, because a census short by an
// unknown amount is not a floor -- it is a wrong answer, and `CountOfGroup` would
// report fewer children than a node has.
func TestOneIncludeThatCannotCensusRefusesTheComposition(t *testing.T) {
	blocked := NewAmendedSource(countable())
	blocked.Alter(NewInt(1), Record{Named("kind", "file")}) // now it refuses

	c, err := NewComposedSource(
		Include{Name: "left", Source: countable()},
		Include{Name: "right", Source: blocked},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := censusOver(t, c, "kind"); err == nil {
		t.Error("the composition answered although one include could not")
	}
}

// --- and whether anything under it actually arrives -----------------------

// **Implementing `Arriving` is not the same as answering late**, and the difference
// is what `Arrives` is for.
//
// A wrapper hands the notice on, so it satisfies the interface whatever its child
// does: a cache over records in hand can be asked to tell, and will never tell,
// because there is nothing under it to tell about. Asserting the interface to decide
// whether to WAIT is therefore wrong -- and wrong expensively. A tree that concluded
// its levels might answer late walked on a goroutine and returned no rows at all,
// waiting for a notice that could not come.
func TestAWrapperOverRecordsInHandDoesNotArrive(t *testing.T) {
	held := countable()
	for _, over := range []struct {
		what string
		src  Source
	}{
		{"a bare list", held},
		{"a cache", NewCachedSource(held)},
		{"an amendment", NewAmendedSource(held)},
	} {
		if Arrives(over.src) {
			t.Errorf("%s says it may answer late, over records in hand", over.what)
		}
	}

	// A composition arrives when any include does, and not otherwise.
	quiet, err := NewComposedSource(
		Include{Name: "a", Source: held}, Include{Name: "b", Source: held})
	if err != nil {
		t.Fatal(err)
	}
	if Arrives(quiet) {
		t.Error("a composition of records in hand says it may answer late")
	}
}

// arriver answers after its read returns, which is what a source across a
// connection does.
type arriver struct{ tells []func() }

func (a *arriver) Open(*DataSetDescriptor) (DataSet, error) { return &arriverSet{}, nil }
func (a *arriver) WhenArrived(tell func())                  { a.tells = append(a.tells, tell) }

type arriverSet struct{}

func (s *arriverSet) Read(sc *Scope, out Sink) error { return nil } // later
func (s *arriverSet) Close()                         {}

// And a wrapper over one that DOES arrive says so, however deep it is -- which is
// the half that was already true and must stay true.
func TestAWrapperOverSomethingThatArrivesSaysSo(t *testing.T) {
	late := &arriver{}
	if !Arrives(late) {
		t.Fatal("the source that arrives says it does not")
	}
	for _, over := range []struct {
		what string
		src  Source
	}{
		{"a cache", NewCachedSource(late)},
		{"an amendment", NewAmendedSource(late)},
		{"an amendment over a cache", NewAmendedSource(NewCachedSource(late))},
	} {
		if !Arrives(over.src) {
			t.Errorf("%s says it answers at once, over something that does not", over.what)
		}
	}

	mixed, err := NewComposedSource(
		Include{Name: "here", Source: countable()}, Include{Name: "late", Source: late})
	if err != nil {
		t.Fatal(err)
	}
	if !Arrives(mixed) {
		t.Error("a composition with one late include says it answers at once")
	}
}

// **A tree over a WRAPPER of records in hand flattens on the thread that asked**, so
// its rows are there when the read returns. This is the case that failed: the tree
// went asynchronous because its source implemented an interface, and a reader saw an
// empty tree with no way to learn otherwise.
func TestATreeOverAWrappedListFlattensAtOnce(t *testing.T) {
	over := NewAmendedSource(NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "alpha")}),
		NewRow(NewInt(2), Record{Named("name", "beta")}),
	}))
	tree, err := NewTreeSource(TreeOptions{
		Source: over,
		Types:  NodeTypes{Default: &NodeType{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := tree.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	var got collector
	if err := set.Read(&Scope{Count: 10}, &got); err != nil {
		t.Fatal(err)
	}
	if got.joined() != "1,2" {
		t.Errorf("the tree reads %q, want both rows on the thread that asked", got.joined())
	}
}
