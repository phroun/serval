package serval

// Which nodes of a tree are open.
//
// Expanding everything must not mean iterating everything, so expansion is a
// STATE and not a walk: a node's state is the nearest mark at or above it, the
// root default is that its children show and nothing deeper does, and expanding
// a million rows is one mark.
//
// # Two rules keep the set small, and they are the same rule twice
//
// **A mark wipes what it governs.** Setting a node's state discards every mark
// beneath it, because each of those was an exception to a state that no longer
// applies. Closing a node and expanding one wholesale are the same move here.
//
// **Setting a node to what it would inherit removes its mark rather than adding
// one.** Opening a node inside an OpenAll region is not "add an Open mark", it
// is "drop the Closed mark", and the inherited OpenAll resumes beneath it. So
// what is held is bounded by what somebody has TOUCHED since the enclosing
// change rather than by the size of the tree.
//
// It is the selection's shape made hierarchical -- everything, except these
// named ones -- and both places are the better for saying so.
//
// # A trie, because a mark has to be findable from above and from below
//
// A flat table keyed by node would answer "what is this node's mark" and could
// not answer "wipe everything beneath it" without knowing every key's ancestry.
// So the marks are held in the shape of the tree they mark, sparsely: a node
// appears here only if it or something beneath it has been touched.
//
// The segments are strings and this package does not care what they spell. A
// tree of one source uses each record's Key; one that grafts sources or allows
// a node to stand beneath itself uses a path. Either way a descent walks this
// alongside the records, a segment at a time, and never searches.

import "sync"

// A Mark is what a node's state can be.
type Mark int

const (
	// Closed: this node's children do not show. Nothing beneath a closed node
	// is reachable to be marked, which is why closing wipes.
	Closed Mark = iota

	// Open: this node's children show. Theirs follow their own marks, which
	// default to Closed -- so Open governs exactly one level.
	Open

	// OpenAll: this node and everything beneath it, until a deeper mark says
	// otherwise. It is a state and not an iteration: one mark, whatever the
	// size of the subtree.
	OpenAll
)

func (m Mark) String() string {
	switch m {
	case Open:
		return "open"
	case OpenAll:
		return "openAll"
	}
	return "closed"
}

// shows reports whether a node in this state has its children visible.
func (m Mark) shows() bool { return m == Open || m == OpenAll }

// satisfies reports whether a node already in state `have` is doing what `want`
// asks of it.
//
// It is equality with one exception, and that exception is the second rule
// working: **an OpenAll already does what an Open asks**, the children showing
// either way. So opening a node inside an expand-all region drops its Closed
// mark rather than adding an Open one, and the OpenAll resumes beneath -- which
// is what somebody re-opening a folder they collapsed inside an expand-all
// expects to see, and is why this is not `have == want`.
//
// The same reading makes Open a no-op on a node that is already OpenAll, which
// is the consequence named on Open: the verb is spent saying the other thing.
func satisfies(have, want Mark) bool {
	return have == want || (want == Open && have == OpenAll)
}

// given is what a child of a node in this state inherits.
//
// Only OpenAll reaches past one level. Under Open the children show but each of
// them is closed until it says otherwise, which is what makes Open and OpenAll
// two states rather than one with a flag.
func (m Mark) given() Mark {
	if m == OpenAll {
		return OpenAll
	}
	return Closed
}

// a marker is one node of the sparse mark tree: its own state if it has been
// given one, and whatever is held beneath it.
type marker struct {
	mark Mark
	set  bool
	kids map[string]*marker
}

// Marks is a tree's expansion: which nodes are open, held sparsely.
//
// It lives on the SOURCE rather than on a data set, so two data sets over one
// descriptor cannot disagree about what the sequence contains. Two views sharing a
// tree share its expansion, which is sometimes exactly right; a view wanting its
// own wraps its own tree, which is cheap because wrapping is all it is.
//
// The zero value is ready, with the top level showing and nothing deeper.
type Marks struct {
	mu   sync.Mutex
	root marker
}

// state is a node's own marker and the state it inherits, walked from the root.
//
// The root is a node too, and its default is Open: the top level of a tree
// shows, always, and its rows are closed until somebody says otherwise. Marking
// the root OpenAll is how everything expands at once.
func (m *Marks) walk(chain []string) (at *marker, given Mark) {
	at, given = &m.root, Open
	here := m.rootState()
	for _, seg := range chain {
		given = here.given()
		next := at.kids[seg]
		if next == nil {
			return nil, given
		}
		at, here = next, given
		if at.set {
			here = at.mark
		}
	}
	return at, given
}

// rootState is the top level's own state, which shows by default: a tree's top
// rows are visible without anybody having said so, and its children are closed
// until they do. Marking the root OpenAll is how a whole tree expands at once.
func (m *Marks) rootState() Mark {
	if m.root.set {
		return m.root.mark
	}
	return Open
}

// Mark is a node's state: its own if it has one, and what it inherits if it has
// not.
//
// The chain is the segments from the root down to it, which a descent has in
// hand -- it got there by walking them.
func (m *Marks) Mark(chain ...string) Mark {
	m.mu.Lock()
	defer m.mu.Unlock()
	at, given := m.walk(chain)
	if at != nil && at.set {
		return at.mark
	}
	return given
}

// Shows reports whether this node's children are visible, which is the question
// a tree actually asks of every row it draws.
func (m *Marks) Shows(chain ...string) bool { return m.Mark(chain...).shows() }

// Set puts a node into a state, and is the one mutator the three verbs are made
// of.
//
// **Nothing happens where the state does not change.** A click on an already
// open node must not wipe what is beneath it, and asking for the state a node is
// already in is the commonest way that would happen.
//
// **Where it does change, the subtree is wiped and the mark is only kept if it
// says something.** Those are the two rules, and they are applied in that order:
// the marks beneath were exceptions to the state being replaced, and a mark
// equal to what would be inherited is not an exception at all.
func (m *Marks) Set(state Mark, chain ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	at, given := m.dig(chain)
	was := given
	if at.set {
		was = at.mark
	}
	if satisfies(was, state) {
		return
	}

	at.kids = nil // every mark beneath was an exception to `was`
	at.mark = state
	at.set = !satisfies(given, state)
	if !at.set {
		m.prune(chain)
	}
}

// Open makes a node's children show. Inside an OpenAll region this drops the
// node's Closed mark rather than adding an Open one, and the OpenAll resumes
// beneath it -- which is what somebody re-opening a folder they collapsed inside
// an expand-all expects to see.
//
// The consequence worth naming: "open exactly one level, here, inside an OpenAll
// region" is a state this cannot express, because the verb that would say it is
// spent saying the other thing. It has not been wanted, and the alternative
// reading costs the interaction above, which is wanted constantly.
func (m *Marks) Open(chain ...string) { m.Set(Open, chain...) }

// OpenAll expands a node and everything beneath it, in one mark.
func (m *Marks) OpenAll(chain ...string) { m.Set(OpenAll, chain...) }

// Close hides a node's children, and discards the expansion of everything held
// beneath it -- so collapsing a node and opening it again shows one level, not
// whatever was open before.
func (m *Marks) Close(chain ...string) { m.Set(Closed, chain...) }

// Clear is collapse-all: the top level shows and nothing deeper does.
//
// It is its own verb rather than a coincidence of the root's state. Setting the
// root Open would be a no-op where it is Open already, which is exactly when a
// hundred marks beneath it are the thing to be rid of.
func (m *Marks) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.root = marker{} // unset, which rootState reads as Open: the default, not an exception
}

// Held is how many nodes carry a mark of their OWN -- exceptions, not trie
// nodes. It is what "bounded by what somebody has touched" means when a test
// asks it, and the number that must not grow with the size of the tree.
func (m *Marks) Held() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	if m.root.set {
		n++
	}
	for _, k := range m.root.kids {
		n += heldBy(k)
	}
	return n
}

func heldBy(at *marker) int {
	n := 0
	if at.set {
		n++
	}
	for _, k := range at.kids {
		n += heldBy(k)
	}
	return n
}

// dig walks to a node, making the markers it passes through. Unlike walk it
// cannot fail, a mark being settable on a node nothing has touched yet.
func (m *Marks) dig(chain []string) (at *marker, given Mark) {
	at, given = &m.root, Open
	here := m.rootState()
	for _, seg := range chain {
		given = here.given()
		if at.kids == nil {
			at.kids = map[string]*marker{}
		}
		next := at.kids[seg]
		if next == nil {
			next = &marker{}
			at.kids[seg] = next
		}
		at, here = next, given
		if at.set {
			here = at.mark
		}
	}
	return at, given
}

// prune drops markers that say nothing and hold nothing, from the node up.
//
// Without it the trie would grow by a node for every state anybody ever set back
// to what it inherits, which is the opposite of the point: a set bounded by what
// has been touched has to let go of what has been untouched again.
func (m *Marks) prune(chain []string) {
	// The path down, so it can be walked back up.
	spine := make([]*marker, 0, len(chain)+1)
	at := &m.root
	spine = append(spine, at)
	for _, seg := range chain {
		next := at.kids[seg]
		if next == nil {
			return
		}
		at = next
		spine = append(spine, at)
	}
	for i := len(spine) - 1; i > 0; i-- {
		node := spine[i]
		if node.set || len(node.kids) > 0 {
			return
		}
		delete(spine[i-1].kids, chain[i-1])
		if len(spine[i-1].kids) == 0 {
			spine[i-1].kids = nil
		}
	}
}
