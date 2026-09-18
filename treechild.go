package serval

// What a node's children are.
//
// A **child type** is a source and a way of deriving a Spec from the parent
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
// **It is a Go function from a record to a Spec, not a small language.** Two or
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

// A Node is a parent, as a child type sees it: what identifies it, what it
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

// A ChildType says where a node's children live, how to ask for them, and how
// a row of that source says where it stands.
type ChildType struct {
	// Source is where the children are read from. Nil is the tree's own
	// source, which is the nested case: a hierarchy inside one body of records.
	Source Source

	// Children is the question. The zero Criterion makes every node of this
	// type a leaf, which is a legitimate type for a row to name and is how a
	// tree says "nothing hangs off this kind".
	Children Criterion

	// Standing is how a row of that source says where it stands, and it belongs
	// here rather than on the tree because it is a property of the SOURCE.
	Standing Standing
}

// A Criterion says which rows are a node's children -- and, where it can, how a
// whole LEVEL of that question is answered at once.
//
// The three census parts are here rather than on the child type because they
// have to agree with Of and with each other, and a place where three things must
// agree is a place to put them together. A criterion that cannot be censused
// leaves them out, and every node is then counted on its own.
type Criterion struct {
	// Of is the question for one node: a Spec naming that node's children.
	Of func(of Node) *Spec

	// Over and By are how a whole level is counted in one question: the
	// sequence every one of these children is drawn from, and the field saying
	// which parent each belongs to. Group is the value naming one node's part
	// of that census.
	//
	// **Only a criterion that PARTITIONS can be censused.** A census puts each
	// row in exactly one group, so a criterion that puts a row in several
	// nodes' answers -- a subtree, where every ancestor claims it -- cannot use
	// one, and says so by leaving these out rather than by counting wrongly.
	Over  *Spec
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
		Of: func(of Node) *Spec {
			return &Spec{Filter: &Filter{
				Op: OpEq, Field: field, Values: []*Value{of.Key},
			}}
		},
		Over:  &Spec{},
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
		Of: func(of Node) *Spec {
			return &Spec{Filter: &Filter{
				Op: OpEq, Field: field, Values: []*Value{NewText(of.Path)},
			}}
		},
		Over:  &Spec{},
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
	return Criterion{Of: func(of Node) *Spec {
		return &Spec{Filter: &Filter{Op: OpOr, Children: []*Filter{
			{Op: OpEq, Field: field, Values: []*Value{NewText(of.Path)}},
			{Op: OpStarts, Field: field, Values: []*Value{NewText(st.Under(of.Path))}},
		}}}
	}}
}

// Sorted puts a sort on whatever a criterion produces.
//
// **A child type may order its children differently from its parents, and this
// costs nothing** -- a child type produces a whole Spec, and a Spec is a source,
// a filter and a sort. Windows in z-order under applications in name order needs
// nothing added. It is written down because a capability nobody notices gets
// reinvented.
// The census parts come through untouched, a count belonging to the filter and
// not to the order: sorting the same records cannot make there be more or fewer.
func Sorted(by Criterion, levels ...SortLevel) Criterion {
	inner := by.Of
	by.Of = func(of Node) *Spec {
		spec := inner(of)
		if spec == nil {
			return nil
		}
		out := *spec
		out.Sort = levels
		return &out
	}
	return by
}

// ChildTypes is the set a tree knows: one default, and any number named.
//
// **That is what lets one tree mix kinds**: a host whose children are
// applications, an application whose children are windows, read out of three
// different places.
type ChildTypes struct {
	// Default is what a row that says nothing gets.
	Default *ChildType

	// Named are the others, by the name a row uses to ask for one.
	Named map[string]*ChildType

	// Field is the field a row names a different type in. Empty means every
	// row gets the default, which is the single-shape tree.
	Field string
}

// For is the type this row's children come from, and nil for a leaf.
//
// Three answers, and the middle one is the one to get right:
//
//   - the field is absent, or undefined, which is a row saying nothing: the
//     DEFAULT. A field a record has not got reads as undefined everywhere in this
//     library, so "says nothing" and "has not got it" are one case and must be.
//   - the field says `false` or `nil`: no children, whatever its siblings do.
//   - the field names a type: that one -- or, where nothing is registered under
//     the name, a leaf.
//
// **An unknown name is a leaf and not a refusal.** One mistyped field must not
// empty a view, which is the same posture a sort takes towards a field a record
// has not got.
func (c ChildTypes) For(r Record) *ChildType {
	if c.Field == "" {
		return c.Default
	}
	v := r.Get(c.Field)
	if v == nil {
		return c.Default
	}
	if v.Kind == NilValue || (v.Kind == BoolValue && !v.Bool) {
		return nil
	}
	return c.Named[Segment(v)]
}

// Check refuses a set that cannot be used, which is a configuration mistake and
// is worth finding when the tree is built rather than when a row is drawn.
func (c ChildTypes) Check() error {
	if c.Default == nil && len(c.Named) == 0 {
		return fmt.Errorf("child types: none given, so every row is a leaf")
	}
	if c.Field == "" && len(c.Named) > 0 {
		return fmt.Errorf("child types: %d named, and no field for a row to name one in",
			len(c.Named))
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
