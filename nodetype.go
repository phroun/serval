package serval

// What a node's children are.
//
// A **node type** is a source and a way of deriving a DataSetDescriptor from the parent
// record. Most of them are one predicate:
//
//	its parent by key      eq parent <the parent's key>                 an adjacency list
//	its container          eq location <the parent's own path>          a row that says where it LIVES
//	everything under it    starts location <that path, and a delimiter> the subtree, deliberately
//
// All three are said with what a filter already has, so nothing new was needed
// to SAY what a child is. `eq` gives children and `starts` gives descendants,
// which is the shallow and the deep distinction expressed in the criterion
// rather than added beside it.
//
// **It is a Go function from a record to a DataSetDescriptor, not a small language.** Two or
// three constructors cover the shapes above. A language for saying this belongs
// with bundles authoring trees, which is a real thing to want later and a parser
// to write when something needs it rather than now.
//
// A type naming the same source as the top level makes a hierarchy nested inside
// one body of records, by those same rules all the way down. A type naming a
// different source GRAFTS one body of records under another, and the rules a
// graft wants all turn out to be rules stated for other reasons -- see
// `docs/trees.md`.

import "fmt"

// A Node is a parent, as a node type sees it: what identifies it, what it
// holds, and where it stands.
//
// The path is here because two of the three criteria are about it, and it is a
// STRING rather than a value because that is what a delimiter joins and a prefix
// bounds. Where a standing produces no path it is empty, and the criteria that
// use one are not the ones such a tree is built with.
type Node struct {
	Key    *Value
	Fields Record
	Path   string
}

// A NodeType is a KIND OF ROW: where rows of that kind come from, how one says
// where it stands, and what kind the rows beneath it are.
//
// It was called a child type while the only thing it described was somebody's
// children, and the name was the parent's vantage point rather than the thing's
// own. A tree's parents are of a kind too -- a host row, an application row, a
// window row -- and anything a VIEW hangs off a type, a column mapping most of
// all, is about what a row IS and not about what hangs off it.
type NodeType struct {
	// Source is where rows of this kind are read from. Nil is the tree's own
	// source, which is the nested case: a hierarchy inside one body of records.
	Source Source

	// Children is how rows of this kind are found under a parent. The zero
	// Criterion makes every node of this type a leaf, which is a legitimate
	// kind for a row to be and is how a tree says "nothing hangs off this".
	Children Criterion

	// Standing is how a row of this kind says where it stands, and it belongs
	// here rather than on the tree because it is a property of the SOURCE.
	Standing Standing

	// Then are the kinds the rows BENEATH this one are, by name. Empty is the
	// default kind, which is what makes a tree of one shape need no names at
	// all.
	//
	// **This is what lets a chain declare itself.** Without it, host ->
	// application -> window needs every host row to carry `applications` and
	// every application row to carry `windows` -- which means the applications
	// SOURCE has to carry the view's type names as data. The kinds are the
	// tree's own business, so the tree says them, and a row speaks up only where
	// it departs from its kind.
	//
	// **It is a LIST because one parent can have two kinds of children.** A host
	// has applications and it has volumes, out of two entirely different
	// sources, and neither is a special case of the other. Each branch is asked
	// for its own children of this parent, and they come back grouped in the
	// order named -- which is predictable, is what a view usually wants (every
	// folder, then every file), and asks nothing of the two sources that they
	// cannot answer. Interleaving them would need a sort field the two sources
	// share, and nothing says they have one.
	//
	// **And each branch may say WHEN it applies**, which is what makes a files
	// list work: a `.zip` takes its children from the archive, a `.ini` from its
	// sections, a folder from the listing, and a plain file has none. All four
	// are rows of one kind, out of one source, with one column mapping -- what
	// differs is decided by the row's own values, so it is a predicate and not a
	// second kind.
	Then []Branch
}

// A Branch is one kind of thing that hangs off a row, and when it does.
type Branch struct {
	// Kind is the node type the children are.
	Kind string

	// When is a predicate on the PARENT. Nil is always, which is what makes the
	// unconditional case free -- Match passes a nil filter.
	//
	// It is a `*Filter` because that is what this library already says a
	// predicate with, down to the collation. `ends name ".zip"` needs nothing
	// new, and the alternative -- a Go function -- could not be written down in
	// a bundle later.
	//
	// **It tests the row's own values and not a field an author added.** The
	// override field exists for a row announcing something about itself, and
	// wanting the data to carry the view's vocabulary is what this avoids: a
	// filesystem listing has an extension and has never heard of a node type.
	When *Filter
}

// Always is the unconditional branches, which is the common case said shortly.
func Always(kinds ...string) []Branch {
	out := make([]Branch, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, Branch{Kind: k})
	}
	return out
}

// A Criterion says which rows are a node's children -- and, where it can, how a
// whole LEVEL of that question is answered at once.
//
// The three census parts are here rather than on the node type because they
// have to agree with Of and with each other, and a place where three things must
// agree is a place to put them together. A criterion that cannot be censused
// leaves them out, and every node is then counted on its own.
type Criterion struct {
	// Of is the question for one node: a DataSetDescriptor naming that node's children.
	Of func(of Node) *DataSetDescriptor

	// Over and By are how a whole level is counted in one question: the
	// sequence every one of these children is drawn from, and the field saying
	// which parent each belongs to. Group is the value naming one node's part
	// of that census.
	//
	// **Only a criterion that PARTITIONS can be censused.** A census puts each
	// row in exactly one group, so a criterion that puts a row in several
	// nodes' answers -- a subtree, where every ancestor claims it -- cannot use
	// one, and says so by leaving these out rather than by counting wrongly.
	Over  *DataSetDescriptor
	By    string
	Group func(of Node) *Value
}

// counts reports whether this criterion can be answered a whole level at a time.
func (c Criterion) counts() bool {
	return c.Over != nil && c.By != "" && c.Group != nil
}

// Check refuses a criterion whose census parts do not all agree to be there.
// Two of the three is a caller who meant to have one and will silently not.
func (c Criterion) Check() error {
	n := 0
	for _, has := range []bool{c.Over != nil, c.By != "", c.Group != nil} {
		if has {
			n++
		}
	}
	if n != 0 && n != 3 {
		return fmt.Errorf("criterion: %d of the three census parts, "+
			"which is a census that will never be taken", n)
	}
	return nil
}

// ChildrenByKey is `eq <field> <the parent's key>`: an adjacency list, and the
// commonest hierarchy in a database.
//
// It needs no path, no delimiter and no address field. The parent's identity is
// compared against the child's parent column, and that is the whole of it.
//
// It partitions -- every row has one value under the field, so it belongs to one
// parent -- so a census of the source by that field answers every node at once.
func ChildrenByKey(field string) Criterion {
	return Criterion{
		Of: func(of Node) *DataSetDescriptor {
			return &DataSetDescriptor{Filter: &Filter{
				Op: OpEq, Field: field, Values: []*Value{of.Key},
			}}
		},
		Over:  &DataSetDescriptor{},
		By:    field,
		Group: func(of Node) *Value { return of.Key },
	}
}

// ChildrenByLocation is `eq <field> <the parent's own path>`: every row that
// says it LIVES here.
//
// One equality, no depth field and no prefix arithmetic -- which is what a field
// holding the CONTAINER buys, and what a field holding a row's own full address
// cannot, no operator being able to take the parent off the front of one.
//
// The path is compared as TEXT, because `eq` compares by kind and a location
// field holding a symbol or a number is a different value from the text of the
// same characters. A source whose location field is not text wants a different
// criterion, and gets a refusal from the filter rather than a wrong answer.
// It partitions for the same reason ChildrenByKey does -- a row lives in one
// place -- so one census of the source by the location field answers every
// node's twisty.
func (st Standing) ChildrenByLocation(field string) Criterion {
	return Criterion{
		Of: func(of Node) *DataSetDescriptor {
			return &DataSetDescriptor{Filter: &Filter{
				Op: OpEq, Field: field, Values: []*Value{NewText(of.Path)},
			}}
		},
		Over:  &DataSetDescriptor{},
		By:    field,
		Group: func(of Node) *Value { return NewText(of.Path) },
	}
}

// DescendantsByLocation is the whole subtree in one question, deliberately.
//
// **It is TWO predicates and not one, which is not what it looks like.** The
// obvious form -- `starts <field> <the parent's path, and a delimiter>` -- is
// not descendants at all: a direct child's location is the parent's path
// EXACTLY, with no delimiter after it, so a prefix test misses every child and
// catches only what is deeper. And dropping the delimiter to let them in drags
// `/usr/locally` into `/usr/local`, which is what appending it prevents.
//
// So:
//
//	or { eq location "/usr/local" ; starts location "/usr/local/" }
//
// The first half is the children, and is the same predicate ChildrenByLocation
// uses; the second is everything below them, bounded. Each half does exactly the
// job it was described as doing, and descendants are the two TOGETHER rather
// than either alone.
//
// **It cannot be censused, and that is a property of the question rather than a
// gap.** A census partitions, putting each row in exactly one group; a subtree
// criterion puts every row in the group of every one of its ancestors. So there
// is no field whose values are these answers, and this one leaves the census
// parts out rather than counting something that is not what was asked.
func (st Standing) DescendantsByLocation(field string) Criterion {
	return Criterion{Of: func(of Node) *DataSetDescriptor {
		return &DataSetDescriptor{Filter: &Filter{Op: OpOr, Children: []*Filter{
			{Op: OpEq, Field: field, Values: []*Value{NewText(of.Path)}},
			{Op: OpStarts, Field: field, Values: []*Value{NewText(st.Under(of.Path))}},
		}}}
	}}
}

// Sorted puts a sort on whatever a criterion produces.
//
// **A node type may order its children differently from its parents, and this
// costs nothing** -- a node type produces a whole DataSetDescriptor, and a DataSetDescriptor is a source,
// a filter and a sort. Windows in z-order under applications in name order needs
// nothing added. It is written down because a capability nobody notices gets
// reinvented.
// The census parts come through untouched, a count belonging to the filter and
// not to the order: sorting the same records cannot make there be more or fewer.
func Sorted(by Criterion, levels ...SortLevel) Criterion {
	inner := by.Of
	by.Of = func(of Node) *DataSetDescriptor {
		descriptor := inner(of)
		if descriptor == nil {
			return nil
		}
		out := *descriptor
		out.Sort = levels
		return &out
	}
	return by
}

// NodeTypes is the set a tree knows: one default, and any number named.
//
// **That is what lets one tree mix kinds**: a host whose children are
// applications, an application whose children are windows, read out of three
// different places.
type NodeTypes struct {
	// Default is what a row that says nothing gets.
	Default *NodeType

	// Named are the others, by the name a row uses to ask for one.
	Named map[string]*NodeType

	// Field is the field a row OVERRIDES its children's kind in. Empty means no
	// row can, and every level takes the kind its parent's type named.
	//
	// It is an override and no longer the only way, which is the difference the
	// rename made: a row says something here when it is unusual -- a mount point
	// under a folder, an alias, a graft -- and says nothing when it is not.
	Field string
}

// all is every type this set holds, for whoever wants to ask something of each of
// them rather than of one. Unordered, because nothing about a SET of types is.
func (n NodeTypes) all() []*NodeType {
	out := make([]*NodeType, 0, len(n.Named)+1)
	if n.Default != nil {
		out = append(out, n.Default)
	}
	for _, nt := range n.Named {
		out = append(out, nt)
	}
	return out
}

// Get is the type of that name, and the default for an empty one.
func (c NodeTypes) Get(name string) *NodeType {
	if name == "" {
		return c.Default
	}
	return c.Named[name]
}

// Beneath are the kinds of the rows under this one: what its own type says comes
// next, unless the row itself says otherwise.
//
// Four answers, and the middle two are the ones to get right:
//
//   - no type at all above, which is the top level: the DEFAULT.
//   - the field is absent, or undefined, which is a row saying nothing: the kinds
//     its parent's type named. A field a record has not got reads as undefined
//     everywhere in this library, so "says nothing" and "has not got it" are one
//     case and must be.
//   - the field says `false` or `nil`: no children, whatever its siblings do.
//   - the field names a kind: that one alone -- or, where nothing is registered
//     under the name, a leaf.
//
// **An unknown name is a leaf and not a refusal.** One mistyped field must not
// empty a view, which is the same posture a sort takes towards a field a record
// has not got.
//
// A row's override names ONE kind, where a type's Then may name several. A row
// departing from its kind is saying something unusual about itself, and wanting
// two unusual things at once has not come up; a type declaring the shape of a
// tree is the place where several is ordinary.
// It answers in NAMES rather than in types, because the name is what a reader of
// the flattened sequence needs: the descent knows a row's kind by construction --
// whatever the `applications` criterion returned is an application -- and the
// name is how it says so, in the field the tree writes beside the depth.
func (c NodeTypes) Beneath(of string, node Node) []string {
	next := c.chain(of, node)
	if c.Field == "" {
		return next
	}
	v := node.Fields.Get(c.Field)
	if v == nil {
		return next
	}
	if v.Kind == NilValue || (v.Kind == BoolValue && !v.Bool) {
		return nil
	}
	if name := Segment(v); c.Named[name] != nil {
		return []string{name}
	}
	return nil
}

// chain is the kinds this one says come beneath it, dropping the branches whose
// condition this row does not meet -- and the default kind where it says
// nothing.
//
// The default kind's own name is empty, which is what makes a tree of one shape
// need no names at all -- and what a view with one mapping reads back.
func (c NodeTypes) chain(of string, node Node) []string {
	t := c.Get(of)
	if t == nil || len(t.Then) == 0 {
		return []string{""}
	}
	out := make([]string, 0, len(t.Then))
	for _, b := range t.Then {
		if c.Get(b.Kind) == nil {
			continue
		}
		if !Match(node.Key, node.Fields, b.When) {
			continue
		}
		out = append(out, b.Kind)
	}
	return out
}

// Check refuses a set that cannot be used, which is a configuration mistake and
// is worth finding when the tree is built rather than when a row is drawn.
// reachable is every kind a descent could arrive at through Then alone, walked
// from the default.
//
// It is a walk and not a "does anything chain at all", which is what the first
// version asked -- and that was too loose to catch the mistake it existed for: a
// tree naming two kinds and a Then reaching only one of them passed, and the
// other was silently dead. Asking whether SOME chain exists is not asking
// whether THIS kind is on one.
func (c NodeTypes) reachable() map[string]bool {
	seen := map[string]bool{}
	var walk func(t *NodeType)
	walk = func(t *NodeType) {
		if t == nil {
			return
		}
		for _, b := range t.Then {
			if seen[b.Kind] {
				continue
			}
			seen[b.Kind] = true
			walk(c.Named[b.Kind])
		}
	}
	walk(c.Default)
	return seen
}

// chainOk refuses a Then that names a kind nothing is registered under.
func (c NodeTypes) chainOk(who string, t *NodeType) error {
	if t == nil {
		return nil
	}
	for _, b := range t.Then {
		if c.Named[b.Kind] == nil {
			return fmt.Errorf("%s says its children are %q, "+
				"and nothing is registered under that name", who, b.Kind)
		}
	}
	return nil
}

func (c NodeTypes) Check() error {
	// **The top level's rows are the default kind**, so there must be one. A
	// tree whose top level is of no kind cannot say where its rows stand, what
	// fills a view's columns, or what lies beneath them.
	if c.Default == nil {
		return fmt.Errorf("node types: no default, and the top level's rows are of it")
	}
	// Every Then must name something. This is configuration, unlike a ROW naming
	// a kind, which is data and reads as a leaf.
	for name, t := range c.Named {
		if err := c.chainOk(fmt.Sprintf("the type %q", name), t); err != nil {
			return err
		}
	}
	if err := c.chainOk("the default type", c.Default); err != nil {
		return err
	}
	// And every named kind must be namABLE: on a Then chain from the default, or
	// asked for by a row where the override field exists at all.
	if c.Field == "" {
		can := c.reachable()
		for name := range c.Named {
			if !can[name] {
				return fmt.Errorf("the type %q is named by nothing -- "+
					"no Then reaches it and no field lets a row ask for it", name)
			}
		}
	}
	if c.Default != nil {
		if err := c.Default.Standing.Check(); err != nil {
			return fmt.Errorf("the default type's %w", err)
		}
		if err := c.Default.Children.Check(); err != nil {
			return fmt.Errorf("the default type's %w", err)
		}
	}
	for name, t := range c.Named {
		if t == nil {
			continue
		}
		if err := t.Standing.Check(); err != nil {
			return fmt.Errorf("the type %q: its %w", name, err)
		}
		if err := t.Children.Check(); err != nil {
			return fmt.Errorf("the type %q: its %w", name, err)
		}
	}
	return nil
}
