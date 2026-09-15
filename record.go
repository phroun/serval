package serval

// A record: a run of named values, in the order they stand.
//
// A run and not a map, because the order a source produced them in is worth
// keeping and because a record is small enough that looking a name up by
// walking it beats hashing it. A name may repeat, and the first one wins --
// which is what `Get` means and what a source that appends an override relies
// on.
//
// A member may have no name at all. That is how a list is held: the positional
// members of a nested thing are fields with an empty name, so one type serves
// both a record of named fields and a list of unnamed ones, and a thing that is
// both -- which is what a PSL node is -- needs no second shape.

import "strings"

// A Field is one member of a record: a name, and what is under it.
type Field struct {
	// Name is what the member is called, or "" for a member that has no name of
	// its own and stands by its position.
	Name string

	// Value is what is under the name. Nil is undefined, which is a value a
	// field can have: a name present with nothing under it.
	Value *Value
}

// A Record is a run of fields.
type Record []*Field

// Named is one field, built from an ordinary Go value.
func Named(name string, v any) *Field { return &Field{Name: name, Value: Val(v)} }

// At is one positional member, which is a field with no name.
func At(v any) *Field { return &Field{Value: Val(v)} }

// Get is the value under a name, or nil where the record does not name it. A
// field present with no value reads as nil too: a bare name is a name, not a
// value of its own.
func (r Record) Get(name string) *Value {
	for _, f := range r {
		if f.Name == name {
			return f.Value
		}
	}
	return nil
}

// Has reports whether the record names something, whatever is under it.
func (r Record) Has(name string) bool {
	for _, f := range r {
		if f.Name == name {
			return true
		}
	}
	return false
}

// Names is the names the record carries, in order, including repeats and
// including the empty name of a positional member.
func (r Record) Names() []string {
	out := make([]string, 0, len(r))
	for _, f := range r {
		out = append(out, f.Name)
	}
	return out
}

// String is the record as readable text: each member's name, then its value,
// and nothing at all for a member that has no value under its name. See
// Value.String -- readable, never parsed back, and nothing turns on it.
func (r Record) String() string {
	if len(r) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(r))
	for _, f := range r {
		switch {
		case f.Name == "":
			parts = append(parts, f.Value.String())
		case f.Value == nil:
			parts = append(parts, f.Name)
		default:
			parts = append(parts, f.Name+" "+f.Value.String())
		}
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}
