package serval

import "strconv"

// How many records a sequence has.
//
// Not how many were asked for and not how many came back -- a scope already
// knows both of those, having stated one and counted the other. This is the
// figure a reader needs to size a bar it has not scrolled: the whole of the
// sequence, which is to say the whole of the DATA SET.
//
// # It belongs to the filter, not to the order
//
// A data set is a source, a sort and a filter, and only two of those three can
// change this number. Sorting the same records cannot make there be more or
// fewer of them, and the fields a query asks for change what a scope carries
// rather than which records are in it.
//
// So every data set over one source with one filter has the same count, and a
// reader that clicks a column header -- a new sort, a new order, every
// placement thrown away -- already knows it.
//
// # A floor is worth more than a silence
//
// Some sources know the figure for nothing: one that ordered its records when
// the sequence was opened has them in a slice, and the count is its length.
// Some cannot know it at all. And in between is the common case, where part of
// the sequence has been read and what is known is a LOWER BOUND -- which is
// most of what a bar wants anyway, and is exactly what an incremental reader
// has.
//
// So a count is a number and whether that number is the whole story. Nothing
// known at all is a floor of zero, which is true of every sequence and says
// nothing, and is how a source that cannot count answers.
type RecordCount struct {
	// N is how many records there are, or where the count is not Exact, how
	// many there are AT LEAST.
	N int

	// Exact says N is the whole figure rather than a floor.
	Exact bool
}

// Exactly and AtLeast are the two things that can be said, and Unknown is
// saying nothing -- a floor of zero, which holds for every sequence there is.
func Exactly(n int) RecordCount { return RecordCount{N: n, Exact: true} }
func AtLeast(n int) RecordCount { return RecordCount{N: n} }
func Unknown() RecordCount      { return RecordCount{} }

// Nothing reports whether this says anything at all.
func (c RecordCount) Nothing() bool { return !c.Exact && c.N == 0 }

// Add and Take are records certainly arriving and certainly leaving, which is
// what a notice about one identity says. Both keep whatever the figure was: a
// floor with one more record above it is a floor one higher, and an exact
// count that gains one is exact.
//
// **This is why counting needs no deltas of its own.** A source reporting an
// append has already said +1 by saying Added, and one reporting a deletion has
// said -1; there is no second channel to invent and nothing to resynchronise,
// because the figure is only ever moved by a statement that was being sent
// anyway.
func (c RecordCount) Add(n int) RecordCount {
	c.N += n
	return c
}

func (c RecordCount) Take(n int) RecordCount {
	if c.N -= n; c.N < 0 {
		c.N = 0
	}
	return c
}

// Doubt is n records that MAY no longer be in the sequence -- replaced, or
// altered in a field the filter tests, either of which can take a record out of
// it or leave it where it was.
//
// The true figure is then somewhere between N-n and N, so N-n is a floor and
// nothing here is exact any more. Which is the whole reason a floor is worth
// carrying: without one, a single doubtful record would cost the entire count,
// and the next honest thing to say would be nothing at all.
func (c RecordCount) Doubt(n int) RecordCount {
	c = c.Take(n)
	c.Exact = false
	return c
}

// And is two counts added, for a sequence made of several -- exact only where
// both halves are, because a floor plus an exact figure is still only a floor.
func (c RecordCount) And(d RecordCount) RecordCount {
	return RecordCount{N: c.N + d.N, Exact: c.Exact && d.Exact}
}

// String is the count as readable text, for a message or a test saying what it
// expected. Never parsed back.
func (c RecordCount) String() string {
	switch {
	case c.Nothing():
		return "unknown"
	case c.Exact:
		return strconv.Itoa(c.N)
	}
	return "at least " + strconv.Itoa(c.N)
}

// A Counting data set knows how many records its sequence has.
//
// Optional, and deliberately so: the least a source can do stays one method,
// and a source that cannot count simply is not one of these. Ask with
// CountOf, which answers Unknown for anything that is not.
type Counting interface {
	RecordCount() RecordCount
}

// CountOf is what a data set says its sequence holds, and Unknown for one that
// does not count.
func CountOf(v DataSet) RecordCount {
	if c, ok := v.(Counting); ok {
		return c.RecordCount()
	}
	return Unknown()
}
