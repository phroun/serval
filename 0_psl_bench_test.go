package serval

// What a scope costs.
//
// The claim the flat source makes is that a scope is the log of the sequence's
// length and then the length of the scope -- so reaching the last screenful of
// a hundred thousand records costs what reaching the first one costs. These
// measure it rather than asserting it, and the pair at the two ends of the
// sequence is the comparison that matters: if they diverge, the boundary is
// being walked to rather than found.

import (
	"fmt"
	"strings"
	"testing"
)

const benchRecords = 100000

// benchDoc is a PSL list of records with a name, a size and a kind.
func benchDoc(n int) string {
	var b strings.Builder
	b.Grow(n * 48)
	b.WriteString("(")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "(name: \"file%d.go\", size: %d, kind: %d),", i, (i*7919)%100000, i%5)
	}
	b.WriteString(")")
	return b.String()
}

func benchSource(tb testing.TB, n int) *PSLSource {
	tb.Helper()
	src, err := ParsePSLSource(benchDoc(n), Members)
	if err != nil {
		tb.Fatal(err)
	}
	return src
}

// byNameOver is the sequence the benchmarks read: names in natural order, and
// a floor under the size so that every spec is a different sequence and the
// orderings the source keeps are never the answer.
func byNameOver(size int) *Spec {
	return &Spec{
		Sort: []SortLevel{{Field: "name", Level: Level{Collation: CollateNatural}}},
		Filter: &Filter{Op: OpAnd, Children: []*Filter{
			{Op: OpGe, Field: "size", Values: []*Value{NewInt(int64(size))}},
		}},
	}
}

func byNameNatural() *Spec {
	return &Spec{Sort: []SortLevel{{Field: "name", Level: Level{Collation: CollateNatural}}}}
}

type counter struct {
	n    int
	last Record
	key  *Value
}

func (c *counter) Record(key *Value, fields Record) error {
	c.n++
	c.last, c.key = fields, key
	return nil
}

func (c *counter) Subset(key *Value, fields Record) error {
	return c.Record(key, fields)
}

func (c *counter) Ordered()      {}
func (c *counter) Done(Complete) {}

// Reading the file: parsing the PSL and taking its records off it.
func BenchmarkReadThePSL(b *testing.B) {
	text := benchDoc(benchRecords)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParsePSLSource(text, Members); err != nil {
			b.Fatal(err)
		}
	}
}

// Stating the sequence: the filter over every record, and the sort of what is
// left. Paid once, however many scopes are drawn from it.
func BenchmarkStateTheSequence(b *testing.B) {
	src := benchSource(b, benchRecords)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A spec of its own each time, so the cache never answers.
		spec := byNameOver(i)
		if _, err := src.Open(spec); err != nil {
			b.Fatal(err)
		}
	}
}

// A scope at the start of the sequence, and a scope at the end of it. These
// are the two numbers to compare.
func BenchmarkScopeAtTheStart(b *testing.B) { benchScope(b, 0) }
func BenchmarkScopeAtTheEnd(b *testing.B)   { benchScope(b, benchRecords-40) }

func benchScope(b *testing.B, after int) {
	src := benchSource(b, benchRecords)
	set, err := src.Open(byNameNatural())
	if err != nil {
		b.Fatal(err)
	}

	// Walk to the boundary once, outside the timer, so what is measured is one
	// scope drawn from where somebody has scrolled to.
	var from *Value
	if after > 0 {
		c := &counter{}
		if err := set.Read(scopeOf(b, after), c); err != nil {
			b.Fatal(err)
		}
		from = c.key
	}

	f := scopeOf(b, 30)
	f.After = from
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := &counter{}
		if err := set.Read(f, c); err != nil {
			b.Fatal(err)
		}
		if c.n != 30 {
			b.Fatalf("the scope came back %d records long", c.n)
		}
	}
}

func scopeOf(tb testing.TB, count int) *Scope {
	tb.Helper()
	return &Scope{Count: count}
}
