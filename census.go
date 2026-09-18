package serval

// How many records this filter admits, for each value of this field.
//
// One question and one answer, where a caller would otherwise ask a question per
// value and get a number back each time. The case it was built for is a tree
// drawing a page of twisties: thirty visible rows, each needing to know whether
// it has children and how many, which is thirty counts and therefore thirty
// questions -- or one census of the child filter by the parent field, answering
// all thirty exactly.
//
// # It is RecordCount, partitioned
//
// That is the whole of the claim, and it is why this fits rather than being
// bolted on. A count already belongs to the FILTER and not to the order --
// sorting the same records cannot make there be more or fewer, which is why the
// cache keys one by the source and the filter with the sort left out. A census is
// that same claim, partitioned by one field's value, so it is keyed by the same
// two things and the field name, and the sort is ignored for the same reason.
//
// # Three claims, not one
//
// A census says more than it looks like it says, and the second of these is the
// one that would get fudged:
//
//   - each group's count, which is a RecordCount like any other;
//   - whether those are ALL the groups, which is a RecordCount of the GROUPS and
//     is a separate statement;
//   - what it left out, where it left anything out.
//
// The second is separate and must be. A source that has seen parents 3, 7 and 9
// can report those three with honest counts while having no idea whether there
// is a fourth. Rolling that into the per-group counts would lose it: three exact
// counts with nothing said about completeness reads as a complete answer, which
// is the silent wrong answer this library refuses everywhere else.

import (
	"fmt"
	"sort"
)

// A Group is one distinct value of a census field, and how many records of the
// sequence hold it.
type Group struct {
	// Value is what the field held. Groups are told apart by Key and Equal, so
	// 3 and 3.0 are two groups because they are two numbers, and 3 and "3" are
	// two groups because one is a number and one is text. Nothing else decides
	// this and nothing may -- a census that grouped by a spelling would merge
	// values the rest of the library keeps apart.
	//
	// Nil is `undefined`, which is a group like any other. A field a record has
	// not got reads as undefined rather than being left out, so the records
	// missing the field are countable and nameable rather than a silence to be
	// inferred from an arithmetic that does not add up.
	Value *Value

	// Count is how many records of the sequence hold it: Exactly where the
	// whole sequence was seen, AtLeast where only part of it was.
	Count RecordCount
}

// A Census is the answer: the groups, and whether they are all of them.
type Census struct {
	// Groups, in the order of their values -- which is an order and not an
	// arrangement, so that two censuses of one sequence are the same answer and
	// a test can say what it expects. A caller wanting them by size sorts them,
	// or reads them through CensusSource and says `sort={ count desc }`.
	Groups []Group

	// Total is how many distinct values there are: Exactly(n) meaning these n
	// and no others, AtLeast(n) meaning at least these.
	//
	// It is the second of the three claims, and it is why this is a struct
	// rather than a slice. A slice of exact counts cannot say "and there may be
	// more groups I have not seen", which is precisely what a source that has
	// read part of a sequence has to be able to say.
	Total RecordCount
}

// Group finds one value's group, and reports false for a value this census has
// not got -- which, where Total is exact, is a count of nought and not an
// ignorance.
func (t Census) Group(v *Value) (Group, bool) {
	for _, g := range t.Groups {
		if Equal(g.Value, v) {
			return g, true
		}
	}
	return Group{}, false
}

// CountOfGroup is the count for one value, and the honest answer for a value
// this census has not got: nought exactly where the census is complete, because
// then there is nothing left for it to be, and Unknown where it is not.
//
// This is the question a tree actually asks -- has this row any children, and
// how many -- so it is worth answering here rather than at every caller, where
// the difference between "no children" and "I did not see" would get lost.
func (t Census) CountOfGroup(v *Value) RecordCount {
	if g, ok := t.Group(v); ok {
		return g.Count
	}
	if t.Total.Exact {
		return Exactly(0)
	}
	return Unknown()
}

// A Censusing data set can partition its sequence by a field and count each part.
//
// Optional, and deliberately so, on the model of Counting: the least a data set
// can do stays one method, and one that cannot take one simply is not one of these.
// Ask with CensusOf.
//
// **A data set that cannot take one says so rather than walking.** Over records in
// hand the work is a pass over records already here, which is exact and cheap.
// Over a source that must be ASKED and cannot, walking the sequence to
// count it is precisely what the caller was avoiding -- and a census that quietly
// became a full scan would be fast in testing and ruinous in use.
type Censusing interface {
	Census(field string) (Census, error)
}

// CensusOf partitions a data set's sequence by a field, and says it cannot for a
// data set that is not Censusing.
//
// The error is the point. CountOf answers Unknown for a set that cannot count,
// because a floor of nought is a true statement about every sequence; there is
// no such harmless answer here, an empty census claiming there are no groups at
// all. So this refuses instead, and a caller falls back to whatever it had.
func CensusOf(v DataSet, field string) (Census, error) {
	t, ok := v.(Censusing)
	if !ok {
		return Census{}, fmt.Errorf("census %s: this data set does not take one", field)
	}
	return t.Census(field)
}

// --- a census as records -------------------------------------------------

// The fields a census record carries. They are bare rather than dotted because
// a census MAKES its records, and a dot is how a name says it is a member of a
// record somebody else wrote.
const (
	CensusValue = "value" // the distinct value, which is also the record's identity
	CensusCount = "count" // how many records hold it
	CensusExact = "exact" // whether that count is the whole figure or a floor
)

// CensusSource presents a census as a body of records: one per group, carrying
// the value and the count.
//
// **A census is a sequence, so it is a source**, and then it needs no vocabulary
// of its own for anything a caller will want. A short list of large counts is
// `sort={ count desc }` with `count=20`; the biggest group is the same with
// `count=1`; groups over a threshold are a filter; how many distinct values
// there are is RecordCount. None of that is written here because none of it is
// new.
//
// A group's identity is the value itself, which is unique by Key -- so nothing
// is synthesised, and a scope naming where to carry on from names a value the
// caller was already holding.
func CensusSource(t Census) *ListSource {
	rows := make([]Row, 0, len(t.Groups))
	for _, g := range t.Groups {
		rows = append(rows, NewRow(g.Value, Record{
			Named(CensusValue, g.Value),
			Named(CensusCount, NewInt(int64(g.Count.N))),
			Named(CensusExact, NewBool(g.Count.Exact)),
		}))
	}
	return NewListSource(rows)
}

// --- counting records that are here -------------------------------------

// Census partitions this sequence by a field and counts each part.
//
// Exact throughout: the sequence was ordered when it was opened, so every record
// in it is here to be read, and how many hold a value is arrived at by looking
// rather than by estimating. Which also makes the group total exact -- a value
// not among the groups is held by no record of this sequence, and that is a
// statement rather than an ignorance.
//
// It is the filtered sequence that is walked and not the source's rows, because
// a census is a count and a count belongs to the filter.
func (v *listDataSet) Census(field string) (Census, error) {
	if v.ord == nil {
		return Census{}, fmt.Errorf("census %s: the data set is closed", field)
	}

	// Keyed by the value's Key, which is what decides whether two values are the
	// same value. The value itself is kept beside the count because the key is
	// canonical and not reversible.
	type bucket struct {
		value *Value
		n     int
	}
	seen := map[string]*bucket{}
	order := make([]string, 0, 16)
	for _, row := range v.ord.rows {
		val := v.src.rows[row].Field(field)
		key := Key(val)
		b := seen[key]
		if b == nil {
			b = &bucket{value: val}
			seen[key] = b
			order = append(order, key)
		}
		b.n++
	}

	// By value, so that two censuses of one sequence are one answer. The order
	// the records happened to be in is not an order anybody asked for.
	sort.SliceStable(order, func(i, j int) bool {
		return Compare(seen[order[i]].value, seen[order[j]].value, "") < 0
	})

	out := Census{
		Groups: make([]Group, 0, len(order)),
		Total:  Exactly(len(order)),
	}
	for _, key := range order {
		b := seen[key]
		out.Groups = append(out.Groups, Group{Value: b.value, Count: Exactly(b.n)})
	}
	return out, nil
}
