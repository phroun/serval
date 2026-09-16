package serval

// Reading a PSL list as records.
//
// A PSL list holds two independent collections: an ordered sequence of items,
// and a keyed map that sits outside that sequence entirely. Both are records
// here. An item's key is its index and a keyed member's key is its name, and
// because an integer ranks below a string in the comparison core, the two
// spaces fall into one total order with no rule of their own: the items first,
// in their order, then the names.
//
// This is a flat reading. A `_bundle` member is a record like any other, and
// nothing here follows an include, applies an amendment or resolves a hash --
// that is a layer above this one, and it is built on this rather than into it.
// That layer is designed and unbuilt; the design is KittyTK's
// docs/data-sources-and-bundles.md, and where it comes to live is a question
// for whoever builds it.
//
// What it is for is access: a scope of a sorted, filtered sequence, found
// without walking the records that precede it. The sequence is ordered once per
// spec, and a scope is a binary search for the boundary and a walk forward as
// far as the scope is long -- so scrolling to the end of a large list costs
// what scrolling to the start of it costs.

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/phroun/pawscript"
)

// orderingsKept is how many orderings of one source are held at once. Each is a
// column header somebody clicked, and clicking back is the next thing they do,
// so the previous few are worth keeping and an unbounded pile of them is not.
const orderingsKept = 4

// The two names the Whole reading gives a record's own key and value.
//
// These are fields, and only fields. A record read this way carries `key`
// because the document gave it one and somebody may want to sort or show it --
// not because this end works by it. What identifies the record travels beside
// the bag, and a document is free to hold a member called `key` of its own
// without the two ever meaning the same thing.
const (
	KeyField   = "key"
	ValueField = "value"
)

// A Reading is how a record's contents are named. Both are useful and neither
// is a subset of the other's behaviour, so it is said once, when the source is
// made, rather than guessed per record.
type Reading int

const (
	// Whole exposes the record entire. `key` is the document's own key for it
	// and `value` is its value, and every member wears a dot: `.size` is the
	// member called size, `.0` is the item at position 0.
	//
	// Nothing can shadow anything, so a member called `key` -- which the bundle
	// format's own `_bundle: (key: "figaro")` has -- is `.key` and is reachable
	// like any other. It is the reading for data whose records are not all the
	// same shape, and for anything that has to survive a round trip.
	//
	// `key` here is a field. It is what the document called this record, handy
	// to sort or show; what this end identifies the record by travels beside
	// the bag and is nobody's field.
	Whole Reading = iota

	// Members exposes the members alone, under their own names: `size`, not
	// `.size`. It is the shorter reading, and it suits a source whose records
	// are all lists of named fields -- which is most of them.
	//
	// It is deliberately not complete: the record's own key and value have no
	// name here, and positions have none either, a bare `0` being the number it
	// spells rather than a name. A member called `key` is a
	// member like any other and is reached and sent as one -- the point of this
	// reading is the document at its face value, and nothing in the bag is an
	// identity for it to collide with.
	Members
)

// A PSLSource is a data source backed by one parsed PSL list.
//
// It is read-only and its records do not move, so everything computed from them
// stays true: an ordering is built once per spec and reused for every scope
// drawn from it.
// ParsePSLSource reads PSL text and presents it as a data source.
func ParsePSLSource(text string, reading Reading) (*ListSource, error) {
	n, err := pawscript.ParsePSL(text)
	if err != nil {
		return nil, err
	}
	return NewPSLSource(n, reading), nil
}

// NewPSLSource presents an already-parsed PSL list as a data source: its items as
// records keyed by index, its keyed members as records keyed by name.
func NewPSLSource(n *pawscript.PSLNode, reading Reading) *ListSource {
	rows := make([]Row, 0, n.Len()+len(n.Map()))
	for i := 0; i < n.Len(); i++ {
		v, _ := n.Item(i)
		rows = append(rows, pslRecord{reading: reading, key: NewInt(int64(i)), value: v})
	}
	for _, k := range sortedKeys(n) {
		v, _ := n.Get(k)
		rows = append(rows, pslRecord{reading: reading, key: NewText(k), value: v})
	}
	return NewListSource(rows)
}

// A pslRecord is one record: its key, and the PSL value it stands for.
type pslRecord struct {
	reading Reading
	key     *Value
	value   any // *pawscript.PSLNode for a list, the value itself otherwise
}

// Field is one of the record's values by name, as this source's reading names
// them: `key`, `value` and `.member` under Whole, and `member` alone under
// Members.
func (r pslRecord) Field(name string) *Value {
	node, isList := r.value.(*pawscript.PSLNode)
	if r.reading == Members {
		if !isList {
			return nil
		}
		// A member called `key` is a member. Nothing here is the record's
		// identity, so there is nothing for it to collide with and no reason
		// to hide it -- the point of this reading is the document at its face
		// value.
		if v, ok := node.Get(name); ok {
			return pslValue(v)
		}
		return nil
	}

	switch {
	case name == KeyField:
		return r.key
	case name == ValueField:
		if isList {
			// A list is its members. There is no value beside them, and a
			// record that answered with the whole list here would carry it
			// twice -- once under this name and once member by member.
			return nil
		}
		return pslValue(r.value)
	case len(name) < 2 || name[0] != '.':
		return nil
	}
	if !isList {
		return nil
	}
	if i, digits := itemIndex(name); digits {
		if v, ok := node.Item(i); ok {
			return pslValue(v)
		}
		return nil
	}
	if v, ok := node.Get(name[1:]); ok {
		return pslValue(v)
	}
	return nil
}

// fields is everything the record carries, once.
//
// Under Whole that is `key`, `value`, and a list's members each wearing a dot.
// Under Members it is the keyed members alone, under their own names: a
// record's positions have no name to go out under.
//
// Nothing is dropped either way. A member called `key` is a member like the
// rest, because the record's identity is not in this bag at all and there is
// nothing for it to shadow.
func (r pslRecord) Key() *Value { return r.key }

// Fields is the record entire, under this reading's names.
func (r pslRecord) Fields() Record {
	node, isList := r.value.(*pawscript.PSLNode)
	if r.reading == Members {
		if !isList {
			return nil
		}
		out := make(Record, 0, len(node.Map()))
		for _, k := range sortedKeys(node) {
			v, _ := node.Get(k)
			out = append(out, &Field{Name: k, Value: pslValue(v)})
		}
		return out
	}

	if !isList {
		return Record{
			{Name: KeyField, Value: r.key},
			{Name: ValueField, Value: pslValue(r.value)},
		}
	}
	out := make(Record, 0, node.Len()+len(node.Map())+1)
	out = append(out, &Field{Name: KeyField, Value: r.key})
	for i := 0; i < node.Len(); i++ {
		v, _ := node.Item(i)
		out = append(out, &Field{Name: itemName(i), Value: pslValue(v)})
	}
	for _, k := range sortedKeys(node) {
		v, _ := node.Get(k)
		out = append(out, &Field{Name: memberName(k), Value: pslValue(v)})
	}
	return out
}

// pslValue is a PSL value as one of ours.
//
// A nested list has no order of its own, so it takes the unordered rank, and it
// crosses as a block of its own members -- the same shape a record's fields
// take, which is what it is. Its contents are written the Whole way whatever
// the source's reading, because a position inside it has no other spelling.
func pslValue(v any) *Value {
	switch x := v.(type) {
	case nil:
		return NewNil()
	case bool:
		if x {
			return NewBool(true)
		}
		return NewBool(false)
	case int64:
		return NewInt(x)
	case int:
		return NewInt(int64(x))
	case float64:
		return NewFloat(x)
	case string:
		return NewText(x)
	case pawscript.Symbol:
		// PSL writes an absent value as the bare word `undefined`, which is a
		// value and not a name: a member present with nothing under it. So it
		// is read as nothing, and a filter asking about undefined finds it.
		//
		// That is a fact about PSL and is decided here, where PSL is read.
		// Anywhere past this a symbol is only ever a name.
		if x == "undefined" {
			return nil
		}
		// Every other bare word is a symbol, which ranks between a number and
		// text: a name, compared exactly and under no collation. So
		// `kind: text` and `kind: "text"` reach a filter as the two different
		// questions they were written as, and neither turns into the other.
		return NewSymbol(string(x))
	case *pawscript.PSLNode:
		return NewList(pslRecord{reading: Whole, value: x}.Fields())
	}
	return NewText(fmt.Sprintf("%v", v))
}

// itemName and memberName are how a record's own contents are written: a dot,
// and then the position or the name.
func itemName(i int) string      { return "." + strconv.Itoa(i) }
func memberName(k string) string { return "." + k }

// itemIndex reads a member name that addresses a position rather than a key:
// after the dot, ASCII digits and nothing else. A run of digits too long to be
// an index is still one, and is simply past the end of every list there could
// be.
func itemIndex(name string) (int, bool) {
	digits := name[1:]
	if digits == "" {
		return 0, false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, false
		}
	}
	i, err := strconv.Atoi(digits)
	if err != nil {
		return -1, true
	}
	return i, true
}

// sortedKeys is a node's keyed members in a settled order. Go's map iteration
// is deliberately unordered, and a record's fields have to come out the same
// way twice; PSL's own serializer sorts them for the same reason.
func sortedKeys(n *pawscript.PSLNode) []string {
	m := n.Map()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- the ordering -------------------------------------------------------

// An ordering is the sequence one spec names: which records are in it, and in
// what order.
//
// The sort tuples are kept beside the rows because they are what the sort and
// every later binary search compare. Extracting a field is a map lookup and a
// conversion; doing it once per record rather than once per comparison is the
// difference between a sort that reads the data n log n times and one that
// reads it once.
