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

// --- how many members a record has ---------------------------------------

// Totals is how many members a record has, whether or not they were sent.
//
// Two counts and not one, because they settle different questions. ORDERED
// members are named by their position, so their count settles them entirely: n
// of them are the members `0` through `n-1`, and a query naming an index past
// that is answered without anyone being asked. NAMED members need the count AND
// the names -- knowing there are three says nothing about which three -- but the
// count still settles the question the moment three are known, there being
// nothing left for a fourth name to be.
//
// **An absence does not count.** A member sent as undefined is a name the
// record has NOT got, said out loud so that nobody asks again. That is
// knowledge about the record rather than a member of it, and counting it would
// say the record had a member it has not.
type Totals struct {
	Ordered int
	Named   int
}

// MemberIndex reads a field name as an ordered member's index.
//
// An ordered member is named by its position written out -- `0`, `1`, `2`, or
// `.0`, `.1`, `.2` where the source marks its members with a leading dot -- and
// only in that spelling: no sign, and no leading zeros but `0` itself. So `007`
// is a NAME that happens to be digits rather than the member at seven, which is
// the same distinction everything else here makes between a name and a number.
//
// **This is the rule, and a source counting its members has to count by it.**
// Totals that said a record had no ordered members while naming one `.0` would
// have this answer "index 0 is not there" about a member that is, which is the
// one way a total can be worse than no total at all. Tally counts by this rule
// and is what a source should use.
func MemberIndex(name string) (int, bool) {
	if name != "" && name[0] == '.' {
		name = name[1:]
	}
	if name == "" || (len(name) > 1 && name[0] == '0') {
		return 0, false
	}
	n := 0
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		if n > 1<<40 {
			return 0, false // past any record anyone has, and past overflowing
		}
	}
	return n, true
}

// Tally counts what a record carries: how many members stand by position and
// how many by name. Absences are not counted -- see Totals.
func Tally(r Record) Totals {
	var t Totals
	for _, f := range r {
		if f.Value == nil {
			continue
		}
		if _, ok := MemberIndex(f.Name); ok {
			t.Ordered++
		} else {
			t.Named++
		}
	}
	return t
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
