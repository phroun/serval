package serval

// What is asked, and what comes back.
//
// A query names a sequence once and then reads stretches of it. The naming is a
// Spec -- a source, a filter and a sort -- and it is stated when the query is
// made and never again: a different filter or a different sort is a different
// sequence, which is a different query opened alongside this one. The reading
// is a Scope, and there is one per question asked.
//
// **A query is asked once and answered once.** Nothing addresses an open
// question, nothing amends one, and a Complete ends the answer rather than the
// interest. Every stretch is a new question.

import "strings"

// A Stop is why a scope ended, which the asker cannot work out for itself.
//
// A scope that filled and one that ran out of records look identical from the
// far end -- both are a run of records that stopped -- and they mean opposite
// things about whether there is any point asking again.
type Stop string

const (
	// StopFilled: the count was reached. There is more past the watermark.
	StopFilled Stop = "filled"

	// StopJoined: the walk reached `until`, so what the asker holds on this
	// side and what it holds on the other are now one run.
	StopJoined Stop = "joined"

	// StopExhausted: there are no more records this way. Nothing past the end
	// to be complete up to, so there is no watermark either.
	StopExhausted Stop = "exhausted"
)

// The operators a filter is built from.
const (
	OpAnd      = "and"
	OpOr       = "or"
	OpNot      = "not"
	OpEq       = "eq"
	OpNe       = "ne"
	OpLt       = "lt"
	OpLe       = "le"
	OpGt       = "gt"
	OpGe       = "ge"
	OpIn       = "in"
	OpContains = "contains"
	OpStarts   = "starts"
	OpEnds     = "ends"

	// OpHas and OpLacks ask whether a record carries a field at all, and take
	// no value. Every other operator compares one, and a field holding
	// something with no order of its own -- a nested list -- cannot be
	// compared, so presence needs an operator that does not try.
	OpHas   = "has"
	OpLacks = "lacks"

	// OpID matches a record's identity against a set of them, the way `in`
	// matches a field against a set of values. It names no field, because an
	// identity is not one: it travels beside a record's fields rather than
	// among them, and a field called `key` is a field like any other.
	OpID = "id"
)

// A Filter is a tree: a predicate over one field, a test of a record's
// identity, or an and/or/not over other nodes.
//
// The top of a built filter is an OpAnd, so there is one shape to walk whether
// the filter held one predicate or twenty.
type Filter struct {
	Op       string
	Field    string    // predicates: the field being tested
	Values   []*Value  // what it is tested against; more than one only for `in`
	Collate  string    // text predicates: the collation, "" for the default
	Children []*Filter // and, or, not
}

// Value is the single operand of a comparison, and nil where there is none.
func (f *Filter) Value() *Value {
	if f == nil || len(f.Values) == 0 {
		return nil
	}
	return f.Values[0]
}

// A SortLevel is one level of a sort: which field, which way, and -- for text
// -- under which collation.
type SortLevel struct {
	Field string
	Level
}

// Reverse turns every level over, which is what walking a sequence from its
// end amounts to: the same records, in the opposite order, with the level that
// settles ties turned over as well so that nothing is left facing the way it
// was.
func Reverse(levels []Level) []Level {
	out := make([]Level, len(levels))
	for i, l := range levels {
		out[i] = l
		out[i].Descending = !l.Descending
	}
	return out
}

// Levels drops the field names, leaving what CompareLevels compares tuples by.
func Levels(levels []SortLevel) []Level {
	out := make([]Level, 0, len(levels))
	for _, l := range levels {
		out = append(out, l.Level)
	}
	return out
}

// A Spec says what sequence a query names: which records, in which order.
//
// Fields and Exclude are what the asker WANTS. What a source carries back may
// be any superset of that -- sending more than was asked for is always
// allowed -- but what it CLAIMS to carry must be true, because an answer kept
// against a later question is answered from what it claims.
type Spec struct {
	Source  string
	Fields  Record // the fields asked for; empty means whatever the record has
	Exclude Record // the fields not wanted, valued the same way
	Filter  *Filter
	Sort    []SortLevel
}

// A Scope is the run of records a query asks for: where to start, which way to
// walk, how many, and where the asker's own knowledge picks up again.
//
// It is not a filter and it names no field. The sequence is already decided by
// the spec, and a scope only says which part of it to read -- so a source
// prepares one ordering and serves every scope of it cheaply, rather than
// preparing a new one because the reader scrolled.
//
// After and Until are identities, not positions. An identity means something
// only to the source that issued it, which is why a source made of several
// others never passes one down: it hands each of them that one's own.
type Scope struct {
	// After is the record to start past: the asker holds it already. Nil
	// starts at the first record in walk order.
	After *Value

	// Until is the record to stop before: the asker holds that one too, and
	// everything beyond it, so a walk that reaches it has joined two runs the
	// asker held separately. Nil walks until the count is reached or the
	// records run out.
	Until *Value

	// Count is how many records are wanted.
	Count int

	// Reversed walks the sequence from its end rather than its beginning.
	//
	// Every level turns over, the one the sort does not write included: an
	// identity settles what the named levels leave equal, and a sequence read
	// backwards settles it backwards too. That is what makes this the exact
	// mirror -- `size desc` turns one level over and leaves ties facing the way
	// they were, which is a different sequence again.
	//
	// It belongs to the scope rather than the sequence because it costs
	// nothing: one prepared ordering is read either way, where a reversed
	// *sequence* would be a second ordering of the same records. And it names
	// no field, so it is the one way to turn over a sequence whose records are
	// read in a way that cannot name their identity at all.
	Reversed bool
}

// A Complete ends a scope: which of the three ways it ended, and how far the
// answer is complete.
//
// Watermark says there is nothing between where the scope was asked from and
// that record that the asker does not now have. StopExhausted carries none,
// because there is no point past the end to be complete up to.
type Complete struct {
	Watermark *Value
	Stop      Stop

	// Error is a refusal, which is an answer: this scope cannot be produced,
	// the records are gone, whatever held them is no longer reachable.
	// Whoever asked carries on with what it has.
	Error string
}

// String is the filter as readable text, in the same shape Record uses: each
// node as an operator, the field it names and what it is tested against, and a
// braced block where a node has children. For an error message, a log line, or
// a test saying what it expected in one line -- never parsed back.
func (f *Filter) String() string {
	if f == nil {
		return "{}"
	}
	switch f.Op {
	case OpAnd, OpOr, OpNot:
		return f.block()
	}
	return "{ " + f.statement() + " }"
}

func (f *Filter) block() string {
	parts := make([]string, 0, len(f.Children))
	for _, c := range f.Children {
		parts = append(parts, c.statement())
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

func (f *Filter) statement() string {
	switch f.Op {
	case OpAnd, OpOr, OpNot:
		return f.Op + " " + f.block()
	}
	var sb strings.Builder
	sb.WriteString(f.Op)
	if f.Op != OpID {
		// Every other operator names the field it tests. This one tests an
		// identity, which is not a field and has no name to write.
		sb.WriteByte(' ')
		sb.WriteString(f.Field)
	}
	for _, v := range f.Values {
		sb.WriteByte(' ')
		sb.WriteString(v.String())
	}
	if f.Collate != "" {
		sb.WriteString(" collate=")
		sb.WriteString(f.Collate)
	}
	return sb.String()
}
