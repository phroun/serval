package serval

// What a source says its own records ARE.
//
// A tree is CONFIGURED: a source, a criterion naming one node's children, a
// standing saying how a row tells where it stands, a field saying how many
// children a row has. Somebody has to know all that, and until now that somebody
// was whoever wrote the Go -- which means a document whose author knew perfectly
// well that its records form a hierarchy could not say so, and every reader had
// to be told again by hand.
//
// A TreeHint is the author saying it once. It is **about the records** and not
// about any view: which field holds a parent, which holds a container, which
// says how many children there are. Nothing here is a column, a width, an
// alignment or an expansion -- those are a view's and a view's alone, and a
// format that let a document reach into them would be a format that lets a
// document lay out somebody else's window.
//
// # Descent is Parent or Location, exactly one
//
//	Parent      the parent's own key, in a field of the child      an adjacency list
//	Location    the container the row lives in                     an address
//
// Those are the two shapes that make a LEVEL out of one equality, and a tree is
// built a level at a time. Everything else the hint carries rides along with one
// of them.
//
// **`Whole` is not a descent, which is not obvious.** A row carrying its own full
// address knows where it is without anything having descended to it -- which is
// what jumping to a node and what a deep filter want -- but there is no equality
// that turns a parent's path into its children's, a full path having the parent's
// on the FRONT of it rather than in a field of its own. `DescendantsByLocation` is
// the prefix form and it answers a whole subtree, not a level. So `Whole` is a
// path reading that may accompany `Parent`, and never a way down on its own.
//
// # What it deliberately cannot say
//
// One hint describes ONE shape of record, because a document is one document. A
// tree whose second level comes from somewhere else entirely -- applications under
// hosts, windows under applications -- is several node types over several sources,
// and naming those means naming the other sources, which a document cannot do
// without reaching outside itself. That configuration stays Go's. What this buys
// is the common case: a document that is a hierarchy, read as one.

import "fmt"

// A TreeHint is what a source says about the shape of its own records.
//
// Every field is a FIELD NAME, not a value, and the zero hint says nothing.
type TreeHint struct {
	// Parent is the field holding the parent's key. A row with nothing under it
	// stands at the top, `undefined` being what a record lacking a field reads
	// as everywhere else.
	Parent string

	// Location is the field holding the container the row lives in -- where it
	// lives, not what it is. Whatever the data calls it: `directory`,
	// `container`, `folder`, `thread`.
	Location string

	// Root is how the TOP LEVEL's container is spelled, for a Location descent.
	//
	// It is here because only the author knows: data writes a root as `/`, as
	// `.`, as the empty string, and as nothing at all. Left out, the top level is
	// the rows whose container is **absent or empty** -- two predicates, because
	// `undefined` and `""` are two values and both are ordinary ways to say "at
	// the top".
	Root string

	// Name is the field holding the row's own segment. Empty means the record's
	// key, rendered as text.
	Name string

	// Whole is the field holding the row's own full address, where it carries
	// one. A path reading and never a descent -- see above.
	Whole string

	// Delimiter goes between segments, where a path is built or read.
	Delimiter string

	// Children is the field saying whether a row has children and how many:
	// `undefined` or `false` is a leaf, `true` is children of unknown number, a
	// number is that many. Empty means no row says, and every twisty is counted.
	Children string

	// Order is the field a row's place among its SIBLINGS comes from.
	//
	// It earns a field of its own because sibling order is so often not any
	// datum's order: the order a document listed them in, the order a store
	// decided about them. For a document whose records are positional, `key` is
	// that order, the key of a positional record being its index.
	Order string

	// Label is the field holding the row's own name, for whatever draws it.
	//
	// It is about the record -- which of its fields is what this record is
	// CALLED -- rather than about a column, and a reader with no use for it may
	// ignore it.
	Label string
}

// Nothing reports whether this hint says anything at all.
func (h TreeHint) Nothing() bool { return h == TreeHint{} }

// Standing is the path reading this hint describes.
func (h TreeHint) Standing() Standing {
	return Standing{
		Location:  h.Location,
		Name:      h.Name,
		Whole:     h.Whole,
		Delimiter: h.Delimiter,
	}
}

// Check refuses a hint that cannot mean what it says.
//
// Every one of these is CONFIGURATION rather than data, which is why it is worth
// finding when a hint is read rather than when a row is drawn: a document with a
// contradictory hint is a document to be fixed, and a tree assembled from it
// would be wrong in a way that looks like the data being wrong.
func (h TreeHint) Check() error {
	// An EQUIVALENT mutant and kept anyway: a zero hint names no way down either,
	// so the branch below refuses it regardless. This one exists for the message,
	// "it says nothing" being what somebody who wrote an empty block needs to read
	// rather than a complaint about a field they never reached for.
	if h.Nothing() {
		return fmt.Errorf("tree hint: it says nothing")
	}
	if h.Parent != "" && h.Location != "" {
		return fmt.Errorf("tree hint: %q is a parent's key and %q is a container, "+
			"which are two ways down and not one", h.Parent, h.Location)
	}
	if h.Parent == "" && h.Location == "" {
		// Named apart from the general case, because this is the mistake somebody
		// makes having read that `Whole` is a path: it is, and a path is not a way
		// down.
		if h.Whole != "" {
			return fmt.Errorf("tree hint: %q is a whole address, which says where a "+
				"row IS and not which rows are under it; name a parent or a container",
				h.Whole)
		}
		return fmt.Errorf("tree hint: nothing here says how to go down -- " +
			"a parent's key, or the container a row lives in")
	}
	if h.Root != "" && h.Location == "" {
		return fmt.Errorf("tree hint: a root of %q, and no container field for "+
			"anything to be at the root OF", h.Root)
	}
	return h.Standing().Check()
}

// Options is the tree this hint describes, over the records of one source.
//
// **This is the whole payoff.** A hint is worth having because it becomes a
// configured tree without anybody restating it, so the assembling lives here,
// next to what a hint means, rather than in each of the readers.
func (h TreeHint) Options(src Source) (TreeOptions, error) {
	if src == nil {
		return TreeOptions{}, fmt.Errorf("tree hint: a tree is over a source, and none was given")
	}
	if err := h.Check(); err != nil {
		return TreeOptions{}, err
	}

	st := h.Standing()
	var children Criterion
	if h.Parent != "" {
		children = ChildrenByKey(h.Parent)
	} else {
		children = st.ChildrenByLocation(h.Location)
	}
	if h.Order != "" {
		children = Sorted(children, SortLevel{Field: h.Order})
	}

	descriptor := &DataSetDescriptor{Filter: h.top()}
	if h.Order != "" {
		descriptor.Sort = []SortLevel{{Field: h.Order}}
	}
	return TreeOptions{
		Source:       src,
		Descriptor:   descriptor,
		SaysChildren: h.Children,
		Types: NodeTypes{Default: &NodeType{
			Children: children,
			Standing: st,
		}},
	}, nil
}

// top is which rows stand at the TOP level.
//
// For an adjacency list it is the rows with no parent, and `undefined` is what a
// record lacking the field reads as -- one predicate, and no ambiguity.
//
// For a container it depends on how the data spells its root, which is why the
// hint can say. Where it does not, both ordinary spellings are taken: a row with
// no container field at all, and one whose container is empty. **An OR and not a
// choice**, because guessing which of the two a document meant would put half a
// tree's top level out of sight, and there is nothing to guess from.
func (h TreeHint) top() *Filter {
	if h.Parent != "" {
		return &Filter{Op: OpEq, Field: h.Parent, Values: []*Value{nil}}
	}
	if h.Root != "" {
		return &Filter{Op: OpEq, Field: h.Location, Values: []*Value{NewText(h.Root)}}
	}
	return &Filter{Op: OpOr, Children: []*Filter{
		{Op: OpEq, Field: h.Location, Values: []*Value{nil}},
		{Op: OpEq, Field: h.Location, Values: []*Value{NewText("")}},
	}}
}

// A Hinting source says what its own records are.
//
// Optional, as every one of these is: a source that says nothing about its shape
// is answered for by TreeHintOf, which is what a reader asks.
type Hinting interface {
	TreeHint() TreeHint
}

// A HintSaid is what a source embeds to be able to say what its records are.
//
// It is embedded rather than wrapped, and that is the point. A wrapper forwarding
// `Open` would have to forward every OPTIONAL interface too -- Counting,
// Censusing, TreeFielded, and whichever is added next -- and the one it forgot
// would silently degrade the source it wrapped. Embedding adds a method and
// changes nothing else.
//
// **The hint is said when the source is assembled, before anybody reads it.** So
// there is no lock: whoever built the source is the only one holding it at that
// moment. A source whose shape changes after it has been handed out is not a thing
// this describes.
type HintSaid struct{ hint TreeHint }

// SetTreeHint says what this source's records are.
func (h *HintSaid) SetTreeHint(hint TreeHint) { h.hint = hint }

// TreeHint is what it was told, and the zero hint where nobody said.
func (h *HintSaid) TreeHint() TreeHint { return h.hint }

// TreeHintOf is what a source says about the shape of its records, and false for
// one that says nothing.
//
// **Asking the SOURCE rather than being handed a hint beside it** is the same
// move TreeFieldsOf makes, and for the same reason: whatever produced the source
// is what knows, and a reader that has been given only a source should not have
// to have been given a second thing as well.
func TreeHintOf(src Source) (TreeHint, bool) {
	h, ok := src.(Hinting)
	if !ok {
		return TreeHint{}, false
	}
	hint := h.TreeHint()
	if hint.Nothing() {
		return TreeHint{}, false
	}
	return hint, true
}
