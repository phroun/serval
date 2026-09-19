package serval

// What a wrapper hands on, and what it must not.
//
// Three of the four kinds of source WRAP another: a cache, a composition, an
// amendment. Each forwards `Open`, which is the one method a `Source` has -- and
// every OPTIONAL interface it does not forward is a capability the child had and the
// wrapper silently lost.
//
// That is not a small thing. A source across a connection wrapped in an amendment so
// its reader can edit a cell must not thereby stop saying what shape its records
// are, or stop being able to say that its answer has landed. The one that was
// forgotten degrades the source it wraps, and it does so quietly: nothing refuses,
// a tree simply draws no twisties, or a hint is never found and a hierarchy reads
// flat.
//
// `arriving.go` is the first of these and says the same thing about `Arriving`. This
// is the rest of them, kept together so the question "does a wrapper hand this on"
// has one place to be answered:
//
//	Counting     on each wrapper's DATA SET, and each already had its own -- a
//	             count is not the child's answer, it is the child's answer plus or
//	             minus what the wrapper does to the sequence
//	Censusing    forwarded where it is still true, refused where it is not
//	TreeFielded  forwarded: a wrapper over a tree is reading a tree's rows, and
//	             the field names are the tree's whatever is layered over them
//	Hinting      forwarded by a cache and an amendment, and NOT by a composition
//
// # Why a composition says nothing about shape
//
// It is the one place where refusing is the right answer rather than the lazy one.
// A composition's records are a UNION of differently shaped includes, under keys it
// prefixes with the include's own name. So one include's hint does not describe what
// comes out: a `parent` field naming a sibling's key is naming a key that no longer
// exists under that spelling, and two includes that are both hierarchies need not be
// the same hierarchy.
//
// A bundle is exactly this shape, and it is why a bundle's hint is said on the
// AMENDMENT over the composition rather than found underneath it -- the document
// describing the assembled whole, which is the only thing that can.

import (
	"fmt"
	"sort"
)

// --- what shape are these records ----------------------------------------

// TreeHint: a cache holds the same records under the same keys, so whatever the
// child says about their shape is true of what comes out.
func (c *CachedSource) TreeHint() TreeHint {
	hint, _ := TreeHintOf(c.child)
	return hint
}

// TreeHint: an amendment's own where one was said, and otherwise the child's.
//
// **Amending a record does not reshape it.** A replacement is a record in the same
// world as the ones around it -- that is what makes it a replacement rather than a
// different source -- so a child that says its records are a hierarchy is describing
// the amended ones too.
//
// Its own wins because it is the more specific saying: whoever assembled this layer
// and described it knows what the layer is, and a bundle is exactly that case.
func (a *AmendedSource) TreeHint() TreeHint {
	if own := a.HintSaid.TreeHint(); !own.Nothing() {
		return own
	}
	hint, _ := TreeHintOf(a.child)
	return hint
}

// --- what a tree writes its own fields under ------------------------------

// A wrapper over a TREE is reading a tree's flattened rows, and the names those rows
// carry their depth and kind under are the tree's. So each of these hands the
// question on, and answers nothing where the child is not a tree.
//
// A composition may hand it on too, unlike a hint: a field NAME is not a shape. Two
// includes that are trees under different names is the case with no answer, and
// saying nothing is what it gets -- rather than one include's names offered for the
// whole, which would read the wrong member of half the rows.

func (c *CachedSource) TreeFields() TreeFields {
	f, _ := TreeFieldsOf(c.child)
	return f
}

func (a *AmendedSource) TreeFields() TreeFields {
	f, _ := TreeFieldsOf(a.child)
	return f
}

func (c *ComposedSource) TreeFields() TreeFields {
	var agreed TreeFields
	said := false
	for _, in := range c.includes {
		f, ok := TreeFieldsOf(in.Source)
		if !ok {
			continue
		}
		if said && f != agreed {
			return TreeFields{} // two answers, so this has none
		}
		agreed, said = f, true
	}
	return agreed
}

// --- partitioning the sequence by a field ---------------------------------

// Census: a cache's is the child's. What is held here is records and their places,
// and a census is neither -- it is an answer about the whole filtered sequence, and
// the child is who knows it.
func (s *cachedSet) Census(field string) (Census, error) {
	return CensusOf(s.child, field)
}

// Census: the child's where nothing is amended, and refused where something is.
//
// **A census is a statement about every record in the sequence**, so one amendment
// anywhere in it makes the child's answer wrong: a deletion takes a record out of
// its group, an addition puts one in, a replacement may move it between groups, and
// an alteration may change the very member being counted.
//
// Refused rather than corrected, and rather than quietly passed on. Correcting it
// means knowing which group each amended record was in, which is the child's copy of
// a record this source may never have seen. `CensusOf` refuses for a set that cannot
// take one and every caller has a fallback for it -- a tree counts each node
// instead -- so a refusal costs work and a wrong answer costs correctness.
//
// An unamended AmendedSource is its child, and there is no reason for a layer nobody
// has written in to cost anything at all.
func (s *amendedSet) Census(field string) (Census, error) {
	if s.src.amends() {
		return Census{}, fmt.Errorf("census %s: this source holds amendments, "+
			"so the child's partition is no longer the sequence's", field)
	}
	return CensusOf(s.child, field)
}

// Census: the includes', summed.
//
// **A union's partition by a field is the sum of its parts' partitions**, which is
// true because a group is a VALUE and a value means the same thing in every include.
// That is not so of a key -- which a composition prefixes, and which is why it can
// say nothing about shape -- and it is why this one merges where the other refuses.
//
// Every include has to answer. One that cannot leaves a group short by however many
// records it holds, and a census short by an unknown amount is not a floor, it is a
// wrong answer: `CountOfGroup` would report fewer children than a node has and a
// twisty would go missing. So one refusal refuses the whole.
func (s *composedSet) Census(field string) (Census, error) {
	out := Census{Total: Exactly(0)}
	for _, part := range s.parts {
		from, err := CensusOf(part.set, field)
		if err != nil {
			return Census{}, fmt.Errorf("census %s: %q cannot take one, "+
				"so the composition cannot: %w", field, part.name, err)
		}
		out = mergeCensus(out, from)
	}
	return out, nil
}

// mergeCensus adds one census into another: counts summed per value, and the total
// counting the DISTINCT values rather than summing the two figures -- the same value
// in two includes being one group, not two.
//
// Exactness degrades on the way, as everywhere else: a group summed with a floor is
// a floor, and a total is exact only where every part of it was.
func mergeCensus(into, from Census) Census {
	for _, g := range from.Groups {
		found := false
		for i, had := range into.Groups {
			if Equal(had.Value, g.Value) {
				into.Groups[i].Count = had.Count.And(g.Count)
				found = true
				break
			}
		}
		if !found {
			into.Groups = append(into.Groups, g)
			into.Total = into.Total.Add(1)
		}
	}
	if !from.Total.Exact {
		// A part that did not see the whole of its own sequence may hold groups
		// nobody here has heard of, so this is a floor whatever the arithmetic.
		into.Total = AtLeast(into.Total.N)
	}
	// By value, the same order a census built from records comes out in -- so that a
	// merged one and a plain one are one answer and a caller can say what it expects.
	sort.SliceStable(into.Groups, func(i, j int) bool {
		return Compare(into.Groups[i].Value, into.Groups[j].Value, "") < 0
	})
	return into
}
