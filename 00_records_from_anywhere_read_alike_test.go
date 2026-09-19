package serval

// Rows that never were a PSL document answer the same questions the same way.
// The formats differ in what they read; nothing after that differs at all.

import "testing"

// rows is a small body of records built by hand, as a delimited loader or any
// other reader would hand them over.
func rows() []Row {
	return []Row{
		NewRow(NewInt(0), Record{Named("name", "parser.go"), Named("size", 2048)}),
		NewRow(NewInt(1), Record{Named("name", "lexer.go"), Named("size", 310)}),
		NewRow(NewInt(2), Record{Named("name", "audit.go"), Named("size", 9001)}),
	}
}

// gather reads a whole scope back as the keys that crossed, in order.
func gather(t *testing.T, src Source, descriptor *DataSetDescriptor, s *Scope) ([]string, Complete) {
	t.Helper()
	set, err := src.Open(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	c := &collector{}
	if err := set.Read(s, c); err != nil {
		t.Fatal(err)
	}
	return c.keys, c.done
}

// Sorted, filtered and scoped, with no PSL anywhere.
func TestRowsBuiltByHandAreASourceLikeAnyOther(t *testing.T) {
	src := NewListSource(rows())
	if src.Len() != 3 {
		t.Fatalf("it holds %d records", src.Len())
	}

	descriptor := &DataSetDescriptor{Sort: []SortLevel{{Field: "name"}}}
	got, done := gather(t, src, descriptor, &Scope{Count: 10})
	if len(got) != 3 {
		t.Fatalf("a full scope carried %v", got)
	}
	// audit, lexer, parser -- which is keys 2, 1, 0.
	if got[0] != "2" || got[1] != "1" || got[2] != "0" {
		t.Errorf("sorted by name it read %v", got)
	}
	if done.Stop != StopExhausted {
		t.Errorf("it ended %s", done.Stop)
	}
}

// The count is exact and free, the same as it is for a PSL source: the records
// are here and the filter has already run.
func TestAHandBuiltSourceCountsExactly(t *testing.T) {
	src := NewListSource(rows())
	set, err := src.Open(&DataSetDescriptor{Filter: &Filter{Op: OpGt, Field: "size", Values: []*Value{NewInt(500)}}})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	c, ok := set.(Counting)
	if !ok {
		t.Fatal("it does not count")
	}
	if n := c.RecordCount(); !n.Exact || n.N != 2 {
		t.Errorf("two records are over 500, and it said %s", n)
	}
}

// A scope resumes from an identity, not a position -- the property the whole
// engine rests on, and it does not care what read the records.
func TestAHandBuiltScopeResumesFromAnIdentity(t *testing.T) {
	src := NewListSource(rows())
	descriptor := &DataSetDescriptor{Sort: []SortLevel{{Field: "name"}}}

	first, done := gather(t, src, descriptor, &Scope{Count: 2})
	if len(first) != 2 || done.Stop != StopFilled {
		t.Fatalf("the first scope was %v, %s", first, done.Stop)
	}
	next, _ := gather(t, src, descriptor, &Scope{After: done.Watermark, Count: 2})
	if len(next) != 1 || next[0] != "0" {
		t.Errorf("resuming after %s read %v", done.Watermark, next)
	}
}

// An identity the sequence does not hold is refused rather than guessed at.
func TestAHandBuiltSourceRefusesAnAfterItDoesNotHold(t *testing.T) {
	src := NewListSource(rows())
	_, done := gather(t, src, &DataSetDescriptor{}, &Scope{After: NewInt(99), Count: 10})
	if done.Error == "" {
		t.Error("it answered from somewhere nobody asked about")
	}
}

// Rows from different readers stand side by side in one composition, and the
// composed key is built the same way over both.
func TestHandBuiltRowsComposeWithAPSLDocument(t *testing.T) {
	psl, err := ParsePSLSource(`( ("from the document") )`, Whole)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewComposedSource(
		Include{Name: "plain", Source: NewListSource(rows())},
		Include{Name: "doc", Source: psl},
	)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := gather(t, c, &DataSetDescriptor{}, &Scope{Count: 10})
	if len(got) != 4 {
		t.Fatalf("the composition carried %v", got)
	}
	for _, want := range []string{"plain/0", "doc/0"} {
		var found bool
		for _, k := range got {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is not in %v", want, got)
		}
	}
}
