package serval

// Which nodes of a tree are open.
//
// The property under test throughout is that the SET STAYS SMALL. Every one of
// these could be made to pass by a table with an entry per node, and every one
// of them checks the count as well as the answer, because a mark set that is
// right and unbounded is the failure this type exists to prevent.

import "testing"

// held says how many exceptions are being carried, which is the number that must
// not grow with the size of the tree.
func heldIs(t *testing.T, m *Marks, want int, why string) {
	t.Helper()
	if got := m.Held(); got != want {
		t.Errorf("%s: it holds %d marks, want %d", why, got, want)
	}
}

func showsIs(t *testing.T, m *Marks, want bool, chain ...string) {
	t.Helper()
	if got := m.Shows(chain...); got != want {
		t.Errorf("%v shows %v, want %v", chain, got, want)
	}
}

// Nothing said: the top level shows and nothing deeper does.
func TestByDefaultTheTopLevelShowsAndNothingElse(t *testing.T) {
	var m Marks
	showsIs(t, &m, true) // the root, whose children are the top level
	showsIs(t, &m, false, "a")
	showsIs(t, &m, false, "a", "b")
	heldIs(t, &m, 0, "having been told nothing")
}

// Open governs exactly one level: the node's children show, and each of them is
// closed until it says otherwise. That is what makes Open and OpenAll two states
// rather than one with a flag.
func TestOpenGovernsOneLevel(t *testing.T) {
	var m Marks
	m.Open("a")
	showsIs(t, &m, true, "a")
	showsIs(t, &m, false, "a", "b")
	if got := m.Mark("a", "b"); got != Closed {
		t.Errorf("a child of an open node is %v, want closed", got)
	}
	heldIs(t, &m, 1, "one node opened")
}

// **Expanding a million rows is one mark.** OpenAll reaches all the way down
// until something deeper says otherwise, and costs one entry however deep it is
// asked about.
func TestOpenAllIsOneMarkHoweverDeep(t *testing.T) {
	var m Marks
	m.OpenAll("a")
	for _, chain := range [][]string{
		{"a"}, {"a", "b"}, {"a", "b", "c"}, {"a", "b", "c", "d", "e", "f"},
	} {
		showsIs(t, &m, true, chain...)
	}
	heldIs(t, &m, 1, "a whole subtree expanded")
}

// And marking the ROOT expands the whole tree, which is the same move at the top.
func TestMarkingTheRootExpandsEverything(t *testing.T) {
	var m Marks
	m.OpenAll()
	showsIs(t, &m, true, "anything")
	showsIs(t, &m, true, "anything", "at", "all")
	heldIs(t, &m, 1, "the whole tree expanded")
}

// **A mark wipes what it governs.** Closing a node discards every mark beneath
// it, because each was an exception to a state that no longer applies -- so
// collapsing and re-opening shows one level and not whatever was open before.
func TestClosingWipesWhatWasOpenBeneath(t *testing.T) {
	var m Marks
	m.Open("a")
	m.Open("a", "b")
	m.Open("a", "b", "c")
	heldIs(t, &m, 3, "three levels opened")

	// Nought, not one: the three beneath were exceptions to a state that is
	// gone, and `a` itself is now in the state it inherits, which is no
	// exception either. Both rules, in one move.
	m.Close("a")
	heldIs(t, &m, 0, "closing the top of them")

	m.Open("a")
	showsIs(t, &m, true, "a")
	showsIs(t, &m, false, "a", "b")
	heldIs(t, &m, 1, "and opening it again")
}

// So does marking one OpenAll, and for the same reason.
func TestOpenAllWipesTheExceptionsBeneathIt(t *testing.T) {
	var m Marks
	m.Open("a", "b")
	m.Open("a", "b", "c")
	heldIs(t, &m, 2, "two nodes opened")

	m.OpenAll("a")
	heldIs(t, &m, 1, "and the lot expanded from above them")
}

// **Setting a node to what it would inherit removes its mark rather than adding
// one.** Opening a node inside an OpenAll region is not "add an open mark", it is
// "drop the closed mark", and the OpenAll resumes beneath it.
func TestOpeningInsideAnOpenAllDropsTheMark(t *testing.T) {
	var m Marks
	m.OpenAll("a")
	m.Close("a", "b")
	heldIs(t, &m, 2, "one subtree expanded and one exception closed")
	showsIs(t, &m, false, "a", "b")
	showsIs(t, &m, false, "a", "b", "c")

	m.Open("a", "b")
	heldIs(t, &m, 1, "and the exception taken back")
	showsIs(t, &m, true, "a", "b")
	// The whole point: what resumes beneath is the OpenAll, not a bare Open.
	showsIs(t, &m, true, "a", "b", "c")
	if got := m.Mark("a", "b"); got != OpenAll {
		t.Errorf("re-opened inside an expand-all the node is %v, want the inherited openAll", got)
	}
}

// The consequence of that reading, checked because it is a real limit and not
// an accident: **Open on a node that is already OpenAll is a no-op.** Narrowing
// an expand-all to one level is a state this cannot express, the verb that would
// say it being spent saying the thing above -- which is wanted constantly, where
// this has not been wanted at all.
func TestOpenDoesNotNarrowAnOpenAll(t *testing.T) {
	var m Marks
	m.OpenAll("a")
	m.Open("a", "b", "c")
	m.Open("a")

	if got := m.Mark("a"); got != OpenAll {
		t.Errorf("opening an expanded node made it %v, want the openAll left alone", got)
	}
	showsIs(t, &m, true, "a", "b", "c", "d")
	heldIs(t, &m, 1, "two redundant opens inside an expand-all")
}

// The same rule at the top: closing a node nobody opened says nothing, so
// nothing is written down.
func TestClosingSomethingAlreadyClosedSaysNothing(t *testing.T) {
	var m Marks
	m.Close("a")
	m.Close("a", "b", "c")
	heldIs(t, &m, 0, "closing what was closed already")
}

// **A click on an already open node must not wipe what is beneath it**, which is
// the way a redundant set would do the most damage.
func TestSettingTheStateANodeIsAlreadyInChangesNothing(t *testing.T) {
	var m Marks
	m.Open("a")
	m.Open("a", "b")
	heldIs(t, &m, 2, "two nodes opened")

	m.Open("a") // again
	showsIs(t, &m, true, "a", "b")
	heldIs(t, &m, 2, "opening the outer one again")
}

// A mark that stops saying anything is not merely ignored, it is let go of --
// otherwise a set bounded by what has been touched would grow by one for every
// state anybody ever set back.
//
// **Held is not enough to check this**, and that is the point of counting the
// trie as well. Held counts exceptions, and an emptied marker is not one; but it
// is still a node, still holding a map, and a set that never let go of them
// would grow without bound while reporting nought. So both numbers, because they
// are two different claims.
func TestAMarkThatSaysNothingIsLetGoOf(t *testing.T) {
	var m Marks
	for i := 0; i < 50; i++ {
		m.Open("a", "b", "c")
		m.Close("a", "b", "c")
	}
	heldIs(t, &m, 0, "fifty opens and fifty closes back")
	if got := nodes(&m.root); got != 1 {
		t.Errorf("the trie kept %d nodes, want only its root: the emptied ones are still there", got)
	}
	// And the emptied map goes too. Nothing behaves differently for an empty map
	// against a nil one -- which is exactly why this has to be said here rather
	// than found by asking the type a question.
	if m.root.kids != nil {
		t.Errorf("the root kept an emptied map of %d children", len(m.root.kids))
	}
}

// nodes is how many markers the trie holds at all, set or not -- the memory,
// where Held is the meaning.
func nodes(at *marker) int {
	n := 1
	for _, k := range at.kids {
		n += nodes(k)
	}
	return n
}

// Emptying one branch does not take a sibling's marks with it, which is the way
// a pruning walk most easily goes wrong: it climbs while a node is empty, and
// one that still has another child is not.
func TestPruningStopsAtABranchThatIsStillNeeded(t *testing.T) {
	var m Marks
	m.Open("a", "b")
	m.Open("a", "c")
	heldIs(t, &m, 2, "two siblings opened")

	m.Close("a", "b")
	heldIs(t, &m, 1, "and one of them closed back")
	showsIs(t, &m, true, "a", "c")
	if nodes(&m.root) != 3 { // the root, `a`, and `a/c`
		t.Errorf("the trie holds %d nodes, want the root, `a` and the sibling still open", nodes(&m.root))
	}
}

// Clear is collapse-all, and it is its own verb: setting the root Open would be
// a no-op exactly when a hundred marks beneath it are the thing to be rid of.
func TestClearIsCollapseAll(t *testing.T) {
	var m Marks
	m.Open("a")
	m.OpenAll("b")
	m.Open("c", "d")
	heldIs(t, &m, 3, "three things expanded")

	m.Clear()
	heldIs(t, &m, 0, "collapsed")
	showsIs(t, &m, true)       // the top level still shows
	showsIs(t, &m, false, "a") // and nothing deeper does
	showsIs(t, &m, false, "b")
}

// Clearing after the ROOT was expanded also works, which is the case the verb
// exists for: the root is back to showing one level.
func TestClearAfterExpandingEverything(t *testing.T) {
	var m Marks
	m.OpenAll()
	m.Close("a", "b")
	m.Clear()
	heldIs(t, &m, 0, "collapsed after an expand-all")
	showsIs(t, &m, true)
	showsIs(t, &m, false, "a")
}

// Marks nest: an exception inside an exception inside an expand-all is three
// marks and stays three, however big the tree beneath them is.
func TestExceptionsNestAndStayFew(t *testing.T) {
	var m Marks
	m.OpenAll()      // everything
	m.Close("a")     // except a
	m.Open("a", "b") // but a/b after all -- and only one level of it
	heldIs(t, &m, 3, "a rule and two exceptions")

	showsIs(t, &m, true, "z", "y", "x") // untouched, so the expand-all
	showsIs(t, &m, false, "a")
	showsIs(t, &m, true, "a", "b")
	// a is Closed, so a/b inherits Closed, so the Open mark on a/b is real and
	// governs one level only.
	showsIs(t, &m, false, "a", "b", "c")
	if got := m.Mark("a", "b"); got != Open {
		t.Errorf("a/b is %v, want a real open mark: it is not inside an openAll", got)
	}
}

// The segments are strings and this package does not care what they spell: a
// tree of one source uses each record's Key, and one that grafts uses a path.
// Both are checked here because the type must not learn to parse either.
func TestSegmentsAreOpaqueStrings(t *testing.T) {
	var m Marks
	m.OpenAll(Key(NewInt(7)))
	showsIs(t, &m, true, Key(NewInt(7)), Key(NewInt(9)))
	showsIs(t, &m, false, Key(NewInt(8)))

	var p Marks
	p.OpenAll("/usr/local/")
	showsIs(t, &p, true, "/usr/local/", "/usr/local/bin")
	// Nothing is inferred from the spelling: a sibling with a shared prefix is
	// a different node here, whatever a path reading would make of it.
	showsIs(t, &p, false, "/usr/locally")
}

// Two views sharing one tree share its expansion, which is the reason marks live
// on the source. Nothing here is per-reader.
func TestMarksAreSharedBecauseTheyAreOneSet(t *testing.T) {
	m := &Marks{}
	one, two := m, m
	one.OpenAll("a")
	if !two.Shows("a", "b") {
		t.Error("the second reader does not see what the first expanded")
	}
	heldIs(t, m, 1, "one set, not two")
}
