package serval

// Where a row stands in a hierarchy, and how it says so.
//
// **A record keeps its own identity and a tree does not rewrite it.** A path is
// a different thing standing beside it: a file's identity may be an inode and
// its path `/usr/local/bin`, a message's identity is a message id and its place
// in a thread is somewhere else entirely, a row's key may be a database primary
// key while what makes it a child is a `parent` column. None of those paths are
// keys and none of those keys are paths.
//
// # Three readings, and the third is the mechanism
//
//	its container, plus its name  two fields saying where it LIVES and what it is
//	                              called, joined
//	read whole                    the record carries its own full path in one field
//	derived                       the tree builds it as it descends: the parent's
//	                              path, joined with this row's segment
//
// The first is the best of the three and the third is the one that always works,
// which is the same fact from two ends. A tree can ALWAYS build a path as it
// descends, whatever the data is shaped like; what the other two buy is knowing
// where a row belongs without having descended to it, which is what jumping
// straight to a node needs and what a deep filter needs. So derived is the
// mechanism and the others are shortcuts past it.
//
// # And a path is optional entirely
//
// The zero Standing is no path at all, which is not a degenerate case but the
// commonest hierarchy there is: a table of `id` and `parent`, integers both,
// with nothing anywhere that looks like an address. Nothing is built, nothing is
// concatenated, and no two spellings of one node can arise because there are no
// spellings. See `docs/trees.md`.

import (
	"fmt"
	"strconv"
	"strings"
)

// A Standing says how a row of one source tells where it stands.
//
// **It belongs to a CHILD TYPE and not to a tree.** A child type names a source,
// and how a row says where it stands is a property of that source rather than of
// whatever is reading it -- so a tree that is addresses at the top and an
// adjacency list beneath has two standings, and its capability degrades at the
// boundary rather than for the whole of it.
type Standing struct {
	// Location is the field holding the row's CONTAINER -- where it lives, not
	// what it is. With Name, a row's own path is the two joined.
	//
	// This is the field that makes children one equality, and the name is
	// `location` rather than `path` for a reason: `path` can mean where a row
	// lives or the row's own full address, and the two behave completely
	// differently. Whatever the data calls it -- `directory`, `container`,
	// `folder`, `thread` -- is what goes here.
	Location string

	// Name is the field holding this row's own segment. Empty means the
	// record's key, rendered as text.
	Name string

	// Whole is the field holding the row's own full path, materialised. It
	// suits data that already knows its whole address, and it is read as the
	// record spells it.
	Whole string

	// Delimiter goes between segments: `/` for something filesystem-like, `.`
	// for a namespace, `::`, whatever the data uses.
	//
	// **A segment containing it makes a path that means two things**, and that
	// is the caller's to avoid exactly as a duplicate key is. Where the tree
	// DERIVES a path, choosing one the segments do not contain is the whole of
	// what keeps it unambiguous.
	Delimiter string
}

// Paths reports whether this standing produces a path at all. The zero Standing
// does not, and a tree over one keys its marks by identity instead.
func (st Standing) Paths() bool {
	return st.Whole != "" || st.Location != "" || st.Delimiter != ""
}

// Check refuses a standing that cannot mean what it says.
//
// Two ways to be wrong, and both are configuration rather than data: naming two
// standings at once, and asking for a path to be BUILT with nothing to build it
// out of. A standing that builds no path is not among them -- that is the
// adjacency list, and it is a shape and not a mistake.
func (st Standing) Check() error {
	if st.Whole != "" && st.Location != "" {
		return fmt.Errorf("standing: %q is a whole path and %q is a container, "+
			"which are two standings and not one", st.Whole, st.Location)
	}
	if st.Delimiter == "" && (st.Location != "" || st.Name != "") {
		return fmt.Errorf("standing: joining %q needs a delimiter, "+
			"or the segments run together and the path means two things",
			st.Location+st.Name)
	}
	return nil
}

// Join puts a container and a name together.
//
// **No trailing delimiter is assumed.** Data writes `/usr/local` and data writes
// `/usr/local/`, both are common, and one rule reads both: the delimiter goes
// between them and is not doubled where it is already there. So `/usr` and
// `local` join as `/usr/local`, `/usr/` and `local` join the same way, and a
// root spelled `/` does not produce `//usr`.
//
// An empty container is the name alone, which is what the top level of a derived
// path wants: the first segment stands by itself rather than under a delimiter
// that leads nowhere.
func (st Standing) Join(container, name string) string {
	if container == "" {
		return name
	}
	if st.Delimiter != "" && strings.HasSuffix(container, st.Delimiter) {
		return container + name
	}
	return container + st.Delimiter + name
}

// Under is a path with the delimiter on it, for asking what lies beneath.
//
// **This is where the missing trailing delimiter bites**, and it is why the tree
// appends one rather than hoping the data did. Descendants of `/usr/local` are
// `starts location "/usr/local/"` and NOT `starts location "/usr/local"`,
// because the second also matches `/usr/locally` -- a sibling whose name begins
// with the same letters, dragged into a subtree it has nothing to do with. The
// delimiter is what makes a prefix a BOUNDARY instead of a spelling.
//
// Equality needs no such care, comparing the whole of the field, which is why
// only this one has a method.
//
// The empty path stays empty: beneath the root is everything, and every string
// starts with nothing.
func (st Standing) Under(path string) string {
	if path == "" || st.Delimiter == "" || strings.HasSuffix(path, st.Delimiter) {
		return path
	}
	return path + st.Delimiter
}

// PathOf is where this row stands, by whichever of the three readings is
// configured. `above` is the path of the row it was found beneath, which only
// the derived standing uses -- and which is exactly why the other two can say
// where a row belongs without having descended to it.
func (st Standing) PathOf(above string, key *Value, r Record) string {
	switch {
	case st.Whole != "":
		return Segment(r.Get(st.Whole))
	case st.Location != "":
		return st.Join(Segment(r.Get(st.Location)), st.segmentOf(key, r))
	case st.Delimiter != "":
		return st.Join(above, st.segmentOf(key, r))
	}
	return "" // no path, which is the adjacency list
}

// segmentOf is this row's own name: the Name field, or the record's key where
// no field was named. A path derived out of keys is never compared against
// anything the data wrote, nothing having written one -- so it needs only to be
// unambiguous, which the delimiter's choice is what settles.
func (st Standing) segmentOf(key *Value, r Record) string {
	if st.Name != "" {
		return Segment(r.Get(st.Name))
	}
	return Segment(key)
}

// Segment is a value as a piece of a path: the characters themselves, and not
// the readable rendering.
//
// Value.String quotes text, which is right for standing and wrong here -- a path
// segment is what the field HOLDS. Nothing is parsed back out of this: it is
// compared, and joined, and used to key a mark.
func Segment(v *Value) string {
	if v == nil {
		return ""
	}
	switch v.Kind {
	case TextValue, SymbolValue, BytesValue:
		return v.Str
	case NumberValue:
		if v.IsInt {
			return strconv.FormatInt(v.Int, 10)
		}
		return strconv.FormatFloat(v.Num, 'g', -1, 64)
	}
	return v.String()
}
