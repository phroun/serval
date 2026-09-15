package serval

// What a filter lets through.
//
// The cases are the ones two implementations could disagree about: an empty
// operator, a field a record has not got, a value of the wrong kind for the
// predicate asking about it, and a collation on an op that has no order to
// apply one to. Ordering and matching are the two things two holders of one
// sequence have to agree on without conferring, so both are written down here
// case by case rather than left to read alike.
//
// The filters are BUILT. serval has no text form of its own, so a test says
// what it means in the shapes the library takes -- and `p`, `pv` and the rest
// are only there to keep a table of thirty cases readable.

import "testing"

// rec builds a record out of pairs, so a case reads as the record it is about.
func rec(pairs ...any) Record {
	out := make(Record, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Named(pairs[i].(string), pairs[i+1]))
	}
	return out
}

// p is one predicate over a field, and pv one with more than one value.
func p(op, field string, v any) *Filter {
	return &Filter{Op: op, Field: field, Values: []*Value{Val(v)}}
}

func pv(op, field string, vs ...any) *Filter {
	f := &Filter{Op: op, Field: field}
	for _, v := range vs {
		f.Values = append(f.Values, Val(v))
	}
	return f
}

// collated is a text predicate under a collation.
func collated(op, field string, v any, collation string) *Filter {
	f := p(op, field, v)
	f.Collate = collation
	return f
}

// block is what a filter written as a block of predicates parses to: an AND,
// whether it held one predicate or twenty.
func block(cs ...*Filter) *Filter { return &Filter{Op: OpAnd, Children: cs} }

func anyID(ids ...*Value) *Filter { return &Filter{Op: OpID, Values: ids} }

func TestAFilterDecidesWhatIsIn(t *testing.T) {
	file := rec("name", "src/parser.go", "size", 1024, "kind", NewSymbol("source"))

	for _, c := range []struct {
		what string
		f    *Filter
		want bool
	}{
		// A block is an AND, and an empty one holds everything.
		{"an empty block", block(), true},
		{"eq on the name", block(p(OpEq, "name", "src/parser.go")), true},
		{"eq on another name", block(p(OpEq, "name", "other")), false},
		{"two that both hold", block(p(OpEq, "name", "src/parser.go"), p(OpGe, "size", 1024)), true},
		{"two where one does not", block(p(OpEq, "name", "src/parser.go"), p(OpGt, "size", 1024)), false},

		// A symbol and text are different values, which is what lets a filter
		// carry no type annotations.
		{"a symbol against a symbol", block(p(OpEq, "kind", NewSymbol("source"))), true},
		{"text against a symbol", block(p(OpEq, "kind", "source")), false},

		// undefined is a value with a rank, so presence needs no operator.
		{"eq undefined on a field that is not there", block(p(OpEq, "thumbnail", nil)), true},
		{"ne undefined on a field that is", block(p(OpNe, "name", nil)), true},
		{"undefined is below every number", block(p(OpLt, "thumbnail", 0)), true},

		{"or, one branch holding", block(&Filter{Op: OpOr, Children: []*Filter{
			p(OpEq, "size", 1), p(OpEq, "size", 1024)}}), true},
		{"or, neither branch holding", block(&Filter{Op: OpOr, Children: []*Filter{
			p(OpEq, "size", 1), p(OpEq, "size", 2)}}), false},
		{"not, of something false", block(&Filter{Op: OpNot, Children: []*Filter{
			p(OpStarts, "name", ".")}}), true},
		{"not, of something true", block(&Filter{Op: OpNot, Children: []*Filter{
			p(OpStarts, "name", "src/")}}), false},

		{"in, with the value among them", block(pv(OpIn, "size", 1, 1024, 4096)), true},
		{"in, without it", block(pv(OpIn, "size", 1, 2, 3)), false},
		{"in, over symbols", block(pv(OpIn, "kind", NewSymbol("source"), NewSymbol("folder"))), true},
		{"in, over other symbols", block(pv(OpIn, "kind", NewSymbol("folder"), NewSymbol("disk"))), false},

		{"contains", block(p(OpContains, "name", "parser")), true},
		{"contains, wrong case", block(p(OpContains, "name", "PARSER")), false},
		{"contains, folded", block(collated(OpContains, "name", "PARSER", CollateFold)), true},
		{"starts", block(p(OpStarts, "name", "src/")), true},
		{"ends", block(p(OpEnds, "name", ".go")), true},
		{"ends, folded", block(collated(OpEnds, "name", ".GO", CollateFold)), true},

		// Text is text. A number has no inside for text to sit in, and is not
		// rendered into any to pretend it has.
		{"contains, against a number", block(p(OpContains, "size", "102")), false},
		{"starts, against a symbol", block(p(OpStarts, "kind", "sou")), false},
	} {
		if got := Match(nil, file, c.f); got != c.want {
			t.Errorf("%s: %s matched %v", c.what, c.f, got)
		}
	}
}

// A nil filter is an unfiltered sequence.
func TestNoFilterHoldsEverything(t *testing.T) {
	if !Match(nil, rec("name", "anything"), nil) {
		t.Error("a record fell out of a filter that was not there")
	}
}

// Each operator's identity: a conjunction of nothing holds, a disjunction of
// nothing does not. `not` of nothing is not a filter anyone can build meaning
// for, so there is no third case.
func TestAnEmptyOperatorIsItsOwnIdentity(t *testing.T) {
	if !Match(nil, rec(), block(&Filter{Op: OpAnd})) {
		t.Error("an empty AND excluded a record")
	}
	if Match(nil, rec(), block(&Filter{Op: OpOr})) {
		t.Error("an empty OR admitted a record")
	}
}

// A block is an AND wherever one appears, so a `not` over two predicates
// negates both together rather than negating each of them. With one predicate
// inside, which is how a negation is nearly always written, the two readings
// agree.
func TestNotNegatesTheBlockAsAWhole(t *testing.T) {
	f := block(&Filter{Op: OpNot, Children: []*Filter{
		p(OpStarts, "name", "src/"), p(OpGt, "size", 5000)}})
	if !Match(nil, rec("name", "src/parser.go", "size", 1024), f) {
		t.Error("a record matching one half of the negated block was excluded")
	}
	if Match(nil, rec("name", "src/parser.go", "size", 9000), f) {
		t.Error("a record matching the whole negated block was admitted")
	}
}

// The text predicates are text's alone, and the empty needle is what shows it:
// every piece of text contains it, and a value that is not text contains
// nothing, because it has no inside for text to sit in.
func TestATextPredicateNeedsText(t *testing.T) {
	r := rec("name", "parser.go", "size", 1024,
		"kind", NewSymbol("source"), "data", []byte("hello"))

	for _, c := range []struct {
		what string
		f    *Filter
		want bool
	}{
		{"the empty needle, in text", block(p(OpContains, "name", "")), true},
		{"the empty needle, in a number", block(p(OpContains, "size", "")), false},
		{"the empty needle, in a symbol", block(p(OpContains, "kind", "")), false},
		{"the empty needle, in a field that is not there", block(p(OpContains, "missing", "")), false},

		// Bytes are not text, and take no collation. A text needle is not
		// looked for in them, whatever the bytes happen to spell.
		{"text inside bytes", block(p(OpContains, "data", "ell")), false},
		{"text at the start of bytes", block(p(OpStarts, "data", "hel")), false},
	} {
		if got := Match(nil, r, c.f); got != c.want {
			t.Errorf("%s: %s matched %v", c.what, c.f, got)
		}
	}
}

// A list has no order of its own, so nothing that compares can answer about
// one -- and it is not quietly rendered into something that can.
func TestAListCannotBeCompared(t *testing.T) {
	tagged := rec("name", "a", "tags", NewList(rec("red", true)))
	plain := rec("name", "b")

	for _, f := range []*Filter{
		block(p(OpEq, "tags", nil)),
		block(p(OpNe, "tags", nil)),
		block(p(OpEq, "tags", "red")),
		block(p(OpLt, "tags", 3)),
		block(p(OpGe, "tags", 3)),
		block(pv(OpIn, "tags", NewSymbol("red"), NewSymbol("blue"))),
		block(p(OpContains, "tags", "red")),
	} {
		if Match(nil, tagged, f) {
			t.Errorf("%s matched a record whose field is a list", f)
		}
	}

	// And the field being absent is a different thing from being a list, so
	// comparisons against a record that has not got it are untouched.
	if !Match(nil, plain, block(p(OpEq, "tags", nil))) {
		t.Error("a record without the field stopped comparing as undefined")
	}
}

// Which is why presence has operators of its own: they ask whether the record
// carries the field, whatever it holds, and take no value.
func TestHasAndLacksAskWhetherTheFieldIsThere(t *testing.T) {
	tagged := rec("name", "a", "tags", NewList(rec("red", true)))
	plain := rec("name", "b")

	has := func(op, field string) *Filter { return &Filter{Op: op, Field: field} }

	for _, c := range []struct {
		what string
		f    *Filter
		on   Record
		want bool
	}{
		{"has, on a record that does", block(has(OpHas, "tags")), tagged, true},
		{"has, on one that does not", block(has(OpHas, "tags")), plain, false},
		{"lacks, on a record that has it", block(has(OpLacks, "tags")), tagged, false},
		{"lacks, on one that does not", block(has(OpLacks, "tags")), plain, true},

		// Whatever the field holds, including a plain value.
		{"has, over a plain value", block(has(OpHas, "name")), tagged, true},
		{"lacks, over a plain value", block(has(OpLacks, "name")), tagged, false},

		// And they compose like any other predicate.
		{"has, beside a predicate", block(has(OpHas, "tags"), p(OpEq, "name", "a")), tagged, true},
		{"not lacks, on a record that has it",
			block(&Filter{Op: OpNot, Children: []*Filter{has(OpLacks, "tags")}}), tagged, true},
		{"not lacks, on one that does not",
			block(&Filter{Op: OpNot, Children: []*Filter{has(OpLacks, "tags")}}), plain, false},
	} {
		if got := Match(nil, c.on, c.f); got != c.want {
			t.Errorf("%s: %s matched %v", c.what, c.f, got)
		}
	}
}

// `id` asks about a record's identity, which is not a field.
//
// It travels beside the record rather than among its fields, so it comes into
// Match as its own argument -- and a record that happens to carry a field
// called `key` is answering an ordinary question about an ordinary field.
func TestIDAsksAboutTheIdentityAndNotAField(t *testing.T) {
	id := NewSymbol("left/1")
	// The record's own `key` field says something else entirely.
	r := rec("key", "not-really-a-key", "name", "gamma")

	for _, c := range []struct {
		what string
		f    *Filter
		want bool
	}{
		{"the identity it has", block(anyID(NewSymbol("left/1"))), true},
		{"an identity it has not", block(anyID(NewSymbol("left/0"))), false},
		{"a set holding it", block(anyID(
			NewSymbol("left/0"), NewSymbol("left/1"), NewSymbol("right/9"))), true},
		{"negated", block(&Filter{Op: OpNot,
			Children: []*Filter{anyID(NewSymbol("left/1"))}}), false},

		// A field called `key` is a field. These ask about the record's
		// contents, and nothing about its identity.
		{"the key field, as text", block(p(OpEq, "key", "not-really-a-key")), true},
		{"the key field, against the identity",
			block(p(OpEq, "key", NewSymbol("left/1"))), false},
	} {
		if got := Match(id, r, c.f); got != c.want {
			t.Errorf("%s: %s matched %v", c.what, c.f, got)
		}
	}

	// A record with no identity matches no set of them.
	if Match(nil, r, block(anyID(NewSymbol("left/1")))) {
		t.Error("a record with no identity matched one")
	}
}
