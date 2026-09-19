package serval

// A source saying what shape its own records are, and that becoming a tree.
//
// The interesting assertions are about what a hint REFUSES, because a hint is
// configuration: a document with a contradictory one is a document to be fixed,
// and a tree assembled out of it would be wrong in a way that reads like the data
// being wrong.

import "testing"

// fileRows is a flat list that is really a hierarchy two different ways over:
// each record carries both its parent's key and the directory it lives in, so one
// fixture serves both descents.
//
// **`rank` reverses key order at every level, deliberately.** A fixture whose
// sibling order agrees with its keys says nothing about whether the order was
// applied -- and every parent has TWO children, because ordering a level of one
// cannot come out wrong either.
//
//	etc                 rank 1, key 4
//	usr                 rank 2, key 1
//	  share             rank 1, key 5
//	  local             rank 2, key 2
//	    bin
func fileRows() []Row {
	return []Row{
		NewRow(NewInt(1), Record{
			Named("name", "usr"), Named("where", ""), Named("rank", 2),
		}),
		NewRow(NewInt(2), Record{
			Named("name", "local"), Named("up", 1), Named("where", "usr"),
			Named("rank", 2),
		}),
		NewRow(NewInt(3), Record{
			Named("name", "bin"), Named("up", 2), Named("where", "usr/local"),
			Named("rank", 1),
		}),
		NewRow(NewInt(4), Record{
			Named("name", "etc"), Named("where", ""), Named("rank", 1),
		}),
		NewRow(NewInt(5), Record{
			Named("name", "share"), Named("up", 1), Named("where", "usr"),
			Named("rank", 1),
		}),
	}
}

func fileList() *ListSource { return NewListSource(fileRows()) }

func hintedTree(t *testing.T, h TreeHint) *TreeSource {
	t.Helper()
	opt, err := h.Options(fileList())
	if err != nil {
		t.Fatalf("what the hint becomes: %v", err)
	}
	src, err := NewTreeSource(opt)
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	return src
}

// **An adjacency hint becomes a tree, and the top level is the rows with no
// parent.** `undefined` is what a record lacking a field reads as everywhere else,
// so one predicate says it and there is nothing to guess.
func TestAnAdjacencyHintBecomesATree(t *testing.T) {
	tree := hintedTree(t, TreeHint{Parent: "up", Order: "rank", Label: "name"})

	// Pre-order, with `rank` settling each level -- which reverses key order at
	// both of them, so an order that was never applied reads completely
	// differently.
	want := []string{"etc", "usr", "share", "local", "bin"}
	got := flat(t, tree)
	if len(got) != len(want) {
		t.Fatalf("expanded the tree reads %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the tree reads\n  %v\nwant\n  %v", got, want)
		}
	}
}

// A location hint descends the same data by where a row LIVES, and the top level
// is what is at the root.
func TestALocationHintBecomesATree(t *testing.T) {
	tree := hintedTree(t, TreeHint{
		Location: "where", Name: "name", Delimiter: "/", Order: "rank",
	})
	got := flat(t, tree)
	want := []string{"etc", "usr", "share", "local", "bin"}
	if len(got) != len(want) {
		t.Fatalf("the tree reads %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the tree reads\n  %v\nwant\n  %v", got, want)
		}
	}
}

// **Both spellings of "at the top" are taken where the author did not say.** A
// record with no container field at all and one whose container is empty are two
// values, and both are ordinary ways to say it -- so an OR, because guessing
// which was meant would put half a top level out of sight.
func TestWithNoRootSaidBothSpellingsStandAtTheTop(t *testing.T) {
	rows := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "said empty"), Named("where", "")}),
		NewRow(NewInt(2), Record{Named("name", "said nothing")}),
		NewRow(NewInt(3), Record{
			Named("name", "under one"), Named("where", "said empty"),
		}),
	})
	opt, err := TreeHint{Location: "where", Name: "name", Delimiter: "/"}.Options(rows)
	if err != nil {
		t.Fatalf("what the hint becomes: %v", err)
	}
	tree, err := NewTreeSource(opt)
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	got := flat(t, tree)
	if len(got) != 3 {
		t.Fatalf("the tree reads %v, want all three", got)
	}
	// Both are at the top, and the third hangs under the first of them.
	if got[0] != "said empty" || got[1] != "under one" || got[2] != "said nothing" {
		t.Errorf("the tree reads %v", got)
	}
}

// And a root the author DID say is the one place the top level is, rather than one
// of three.
func TestARootSaidIsTheOnlyTopLevel(t *testing.T) {
	rows := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("name", "at slash"), Named("where", "/")}),
		NewRow(NewInt(2), Record{Named("name", "said empty"), Named("where", "")}),
		NewRow(NewInt(3), Record{Named("name", "said nothing")}),
	})
	opt, err := TreeHint{
		Location: "where", Name: "name", Delimiter: "/", Root: "/",
	}.Options(rows)
	if err != nil {
		t.Fatalf("what the hint becomes: %v", err)
	}
	tree, err := NewTreeSource(opt)
	if err != nil {
		t.Fatalf("stating the tree: %v", err)
	}
	if got := flat(t, tree); len(got) != 1 || got[0] != "at slash" {
		t.Errorf("the tree reads %v, want the one row at the root the hint named", got)
	}
}

// The count field goes through, so a twisty is what a row SAYS rather than what a
// walk found.
func TestTheCountFieldReachesTheTree(t *testing.T) {
	h := TreeHint{Parent: "up", Children: "kids"}
	opt, err := h.Options(fileList())
	if err != nil {
		t.Fatalf("what the hint becomes: %v", err)
	}
	if opt.SaysChildren != "kids" {
		t.Errorf("the tree asks %q for a child count, want kids", opt.SaysChildren)
	}
}

// --- what a hint refuses -------------------------------------------------

func TestAHintThatCannotMeanWhatItSaysIsRefused(t *testing.T) {
	for _, c := range []struct {
		why  string
		hint TreeHint
	}{
		{"it says nothing at all", TreeHint{}},
		{"two ways down", TreeHint{Parent: "up", Location: "where", Delimiter: "/"}},
		{"a label and no way down", TreeHint{Label: "name"}},
		{"a whole address is not a way down",
			TreeHint{Whole: "path", Delimiter: "/"}},
		{"a root with nothing to be at the root of",
			TreeHint{Parent: "up", Root: "/"}},
		{"joining with no delimiter",
			TreeHint{Location: "where", Name: "name"}},
	} {
		if err := c.hint.Check(); err == nil {
			t.Errorf("%s: taken, and it cannot mean what it says", c.why)
		}
		if _, err := c.hint.Options(fileList()); err == nil {
			t.Errorf("%s: it became a tree anyway", c.why)
		}
	}
}

// **A whole address rides along with a parent.** It is a path reading and not a
// descent, so it is only wrong on its own -- and the tree then knows where a row
// is without having descended to it, which is the whole of what it buys.
func TestAWholeAddressRidesAlongWithAParent(t *testing.T) {
	h := TreeHint{Parent: "up", Whole: "path", Delimiter: "/"}
	if err := h.Check(); err != nil {
		t.Fatalf("refused: %v", err)
	}
	opt, err := h.Options(fileList())
	if err != nil {
		t.Fatalf("what the hint becomes: %v", err)
	}
	st := opt.Types.Default.Standing
	if st.Whole != "path" || !st.Paths() {
		t.Errorf("the tree's standing is %+v, want the whole path read from `path`", st)
	}
}

// A tree is over a source, and none being given is a refusal rather than a tree
// of nothing.
func TestAHintOverNoSourceIsRefused(t *testing.T) {
	if _, err := (TreeHint{Parent: "up"}).Options(nil); err == nil {
		t.Error("it made a tree over no source")
	}
}

// --- asking the source ---------------------------------------------------

type hintedList struct {
	*ListSource
	hint TreeHint
}

func (h hintedList) TreeHint() TreeHint { return h.hint }

// **The SOURCE is asked, rather than a hint being handed over beside it.** Whatever
// produced the source is what knows, and a reader given only a source should not
// have to have been given a second thing as well.
func TestASourceIsAskedWhatItsRecordsAre(t *testing.T) {
	plain := fileList()
	if _, ok := TreeHintOf(plain); ok {
		t.Error("a plain list says something about its shape")
	}

	said := hintedList{ListSource: plain, hint: TreeHint{Parent: "up", Label: "name"}}
	got, ok := TreeHintOf(said)
	if !ok {
		t.Fatal("a source that says so is not heard")
	}
	if got.Parent != "up" || got.Label != "name" {
		t.Errorf("it says %+v", got)
	}

	// A source that implements the interface and says NOTHING is the same as one
	// that does not implement it: the zero hint is a silence, not an answer.
	quiet := hintedList{ListSource: plain}
	if _, ok := TreeHintOf(quiet); ok {
		t.Error("an empty hint was heard as a hint")
	}
}

// --- wrapping must not cost a capability ---------------------------------

// arriving is a source that answers later, which is what a wrapper has to keep
// working.
type arriving struct {
	*ListSource
	tells []func()
}

func (a *arriving) WhenArrived(tell func()) { a.tells = append(a.tells, tell) }
func (a *arriving) landed() {
	for _, tell := range a.tells {
		tell()
	}
}

// **A wrapper hands the notice on.** A source across a connection wrapped in a
// cache, a composition or an amendment stopped being able to say its answer had
// landed -- so a reader over the wrapper waited forever for a notice the wrapper
// had swallowed. Wrapping adds a capability; it never takes one away.
func TestAWrapperHandsTheArrivalNoticeOn(t *testing.T) {
	for _, c := range []struct {
		what string
		wrap func(*arriving) Source
	}{
		{"a cache", func(a *arriving) Source { return NewCachedSource(a) }},
		{"an amendment", func(a *arriving) Source { return NewAmendedSource(a) }},
		{"a composition", func(a *arriving) Source {
			src, err := NewComposedSource(Include{Name: "rows", Source: a})
			if err != nil {
				t.Fatalf("composing: %v", err)
			}
			return src
		}},
	} {
		far := &arriving{ListSource: fileList()}
		wrapped := c.wrap(far)

		told := 0
		if !TellOnArrival(wrapped, func() { told++ }) {
			t.Errorf("%s says it cannot tell, and what it wraps can", c.what)
			continue
		}
		far.landed()
		if told != 1 {
			t.Errorf("%s passed on %d notices, want the one that landed", c.what, told)
		}
	}
}

// And a wrapper over a source that answers AT ONCE never fires, which is the right
// answer rather than a missing one: there is nothing to tell.
func TestAWrapperOverASynchronousSourceNeverFires(t *testing.T) {
	told := 0
	wrapped := NewCachedSource(fileList())
	// It reports that it can tell -- it will, if ever there is anything -- and
	// then there never is.
	TellOnArrival(wrapped, func() { told++ })
	if _, err := wrapped.Open(&Spec{}); err != nil {
		t.Fatalf("stating a sequence: %v", err)
	}
	if told != 0 {
		t.Errorf("it announced %d arrivals over a source that answers at once", told)
	}
}
