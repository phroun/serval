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

	// Children is the question -- a Spec naming this parent's children. Nil
	// makes every node of this type a leaf, which is a legitimate type for a
	// row to name and is how a tree says "nothing hangs off this kind".
	Children func(of Node) *Spec

	// Standing is how a row of that source says where it stands, and it belongs
	// here rather than on the tree because it is a property of the SOURCE.
	Standing Standing
}

// ChildrenByKey is `eq <field> <the parent's key>`: an adjacency list, and the
// commonest hierarchy in a database.
//
// It needs no path, no delimiter and no address field. The parent's identity is
// compared against the child's parent column, and that is the whole of it.
func ChildrenByKey(field string) func(Node) *Spec {
	return func(of Node) *Spec {
		return &Spec{Filter: &Filter{
			Op: OpEq, Field: field, Values: []*Value{of.Key},
		}}
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
func (st Standing) ChildrenByLocation(field string) func(Node) *Spec {
	return func(of Node) *Spec {
		return &Spec{Filter: &Filter{
			Op: OpEq, Field: field, Values: []*Value{NewText(of.Path)},
		}}
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
func (st Standing) DescendantsByLocation(field string) func(Node) *Spec {
	return func(of Node) *Spec {
		return &Spec{Filter: &Filter{Op: OpOr, Children: []*Filter{
			{Op: OpEq, Field: field, Values: []*Value{NewText(of.Path)}},
			{Op: OpStarts, Field: field, Values: []*Value{NewText(st.Under(of.Path))}},
		}}}
	}
}

// Sorted puts a sort on whatever a criterion produces.
//
// **A child type may order its children differently from its parents, and this
// costs nothing** -- a child type produces a whole Spec, and a Spec is a source,
// a filter and a sort. Windows in z-order under applications in name order needs
// nothing added. It is written down because a capability nobody notices gets
// reinvented.
func Sorted(by func(Node) *Spec, levels ...SortLevel) func(Node) *Spec {
	return func(of Node) *Spec {
		spec := by(of)
		if spec == nil {
			return nil
		}
		out := *spec
		out.Sort = levels
		return &out
	}
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
	}
	for name, t := range c.Named {
		if t == nil {
			continue
		}
		if err := t.Standing.Check(); err != nil {
			return fmt.Errorf("the type %q: its %w", name, err)
		}
	}
	return nil
}
