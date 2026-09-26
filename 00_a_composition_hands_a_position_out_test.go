package serval

// Dragging a thumb on a list made of several sources.
//
// With no sort a composition lays its includes out end to end, in the order
// their names put them, so a position in the whole falls inside exactly one of
// them and the arithmetic is subtraction. That is the case a list view is in by
// default, and it lands exactly on the first try.
//
// With a sort they interleave by value and no such arithmetic exists. This is
// also about what happens then, which is that nothing is guessed.

import "testing"

func composedOver(t *testing.T, descriptor *DataSetDescriptor, in ...Include) (*ComposedSource, DataSet) {
	t.Helper()
	c, err := NewComposedSource(in...)
	if err != nil {
		t.Fatal(err)
	}
	set, err := c.Open(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	return c, set
}

// A position falls in one include, and that include is handed it.
func TestACompositionHandsAPositionToTheIncludeThatHoldsIt(t *testing.T) {
	_, set := composedOver(t, &DataSetDescriptor{},
		Include{Name: "a", Source: numbered(100)},
		Include{Name: "b", Source: numbered(100)},
		Include{Name: "c", Source: numbered(100)})
	defer set.Close()

	for _, c := range []struct {
		from int
		want string // the composed key the answer should begin with
	}{
		{0, "a/0"},
		{50, "a/50"},
		{99, "a/99"}, // the last of the first block
		{100, "b/0"}, // and the first of the second
		{150, "b/50"},
		{200, "c/0"},
		{299, "c/99"}, // the last record there is
	} {
		got := placeRead(t, set, &Scope{From: c.from, Count: 3})
		if len(got.ids) == 0 {
			t.Errorf("from %d carried nothing: %q", c.from, got.done.Error)
			continue
		}
		if got.ids[0].Str != c.want {
			t.Errorf("from %d began with %q, want %q", c.from, got.ids[0].Str, c.want)
		}
		if !got.done.First.Exact || got.done.First.N != c.from {
			t.Errorf("from %d reported First %v", c.from, got.done.First)
		}
	}
}

// And it is a true run of the sequence from there: what follows the position is
// what follows it, across a block boundary like anywhere else.
func TestWhatFollowsAPositionIsWhatFollowsIt(t *testing.T) {
	_, set := composedOver(t, &DataSetDescriptor{},
		Include{Name: "a", Source: numbered(3)},
		Include{Name: "b", Source: numbered(3)})
	defer set.Close()

	got := placeRead(t, set, &Scope{From: 2, Count: 4})
	want := []string{"a/2", "b/0", "b/1", "b/2"}
	if len(got.ids) != len(want) {
		t.Fatalf("it carried %d records, want %d", len(got.ids), len(want))
	}
	for i, w := range want {
		if got.ids[i].Str != w {
			t.Errorf("record %d is %q, want %q", i, got.ids[i].Str, w)
		}
	}
}

// Past the end is the last record, as it is for a source holding its own.
func TestACompositionClampsAPositionPastTheEnd(t *testing.T) {
	_, set := composedOver(t, &DataSetDescriptor{},
		Include{Name: "a", Source: numbered(5)},
		Include{Name: "b", Source: numbered(5)})
	defer set.Close()

	got := placeRead(t, set, &Scope{From: 900, Count: 3})
	if len(got.ids) != 1 || got.ids[0].Str != "b/4" {
		t.Fatalf("it carried %v", got.ids)
	}
	if !got.done.First.Exact || got.done.First.N != 9 {
		t.Errorf("it reported First %v, want exactly 9", got.done.First)
	}
}

// Walking backwards from a position turns the shares over: the include holding
// the place walks back inside itself, the ones before it walk back from their
// ends, and the ones after it have nothing to say.
func TestACompositionSharesAPositionOutBackwards(t *testing.T) {
	_, set := composedOver(t, &DataSetDescriptor{},
		Include{Name: "a", Source: numbered(4)},
		Include{Name: "b", Source: numbered(4)})
	defer set.Close()

	got := placeRead(t, set, &Scope{From: 5, Count: 4, Reversed: true})
	want := []string{"b/1", "b/0", "a/3", "a/2"}
	if len(got.ids) != len(want) {
		t.Fatalf("it carried %v", got.ids)
	}
	for i, w := range want {
		if got.ids[i].Str != w {
			t.Errorf("record %d is %q, want %q", i, got.ids[i].Str, w)
		}
	}
	if !got.done.First.Exact || got.done.First.N != 5 {
		t.Errorf("it reported First %v, want exactly 5", got.done.First)
	}
}

// The order the SEQUENCE puts the includes in is the order their names put
// them, and not the order they were declared in. Sharing a position out by
// declaration order would hand it to the wrong include.
func TestTheBlocksAreInNameOrderAndNotDeclarationOrder(t *testing.T) {
	_, set := composedOver(t, &DataSetDescriptor{},
		Include{Name: "zulu", Source: numbered(10)},
		Include{Name: "alpha", Source: numbered(10)})
	defer set.Close()

	got := placeRead(t, set, &Scope{From: 0, Count: 1})
	if len(got.ids) == 0 || got.ids[0].Str != "alpha/0" {
		t.Fatalf("the sequence begins with %v, want alpha/0", got.ids)
	}
	got = placeRead(t, set, &Scope{From: 12, Count: 1})
	if len(got.ids) == 0 || got.ids[0].Str != "zulu/2" {
		t.Errorf("position 12 is %v, want zulu/2", got.ids)
	}
}

// A sorted composition interleaves its includes, so there is no position
// arithmetic and none is invented. It starts where it would have started
// anyway and says so, which is what lets the reader find out.
//
// The alternative is worse than being a little off. Halving each of 1,2,3 and
// 100,200,300 to reach the fourth of six gives 2,3,200,300: it begins at the
// second record, skips the fourth altogether, and reports neither. An Extent
// with holes cannot be corrected by a reader that cannot see them.
func TestASortedCompositionSharesNothingOut(t *testing.T) {
	_, set := composedOver(t, &DataSetDescriptor{Sort: []SortLevel{{Field: "n"}}},
		Include{Name: "a", Source: numbered(50)},
		Include{Name: "b", Source: numbered(50)})
	defer set.Close()

	got := placeRead(t, set, &Scope{From: 60, Count: 3})
	if len(got.ids) == 0 {
		t.Fatalf("it carried nothing: %q", got.done.Error)
	}
	if !got.done.First.Exact || got.done.First.N != 0 {
		t.Errorf("it reported First %v, want exactly 0 -- where it really began",
			got.done.First)
	}
}

// An include whose length is only a floor leaves every boundary after it a
// guess, so the position is not shared out at all.
func TestAnIncludeThatCannotCountItselfStopsTheSharing(t *testing.T) {
	_, set := composedOver(t, &DataSetDescriptor{},
		Include{Name: "a", Source: numbered(50)},
		Include{Name: "b", Source: uncounted{numbered(50)}})
	defer set.Close()

	got := placeRead(t, set, &Scope{From: 60, Count: 3})
	if len(got.ids) == 0 {
		t.Fatalf("it carried nothing: %q", got.done.Error)
	}
	if !got.done.First.Exact || got.done.First.N != 0 {
		t.Errorf("it reported First %v, want exactly 0", got.done.First)
	}
}

// uncounted is a source that answers every scope and will not say how many
// records it has -- which is every source that has to ask somebody else.
type uncounted struct{ Source }

func (u uncounted) Open(descriptor *DataSetDescriptor) (DataSet, error) {
	set, err := u.Source.Open(descriptor)
	if err != nil {
		return nil, err
	}
	return uncountedSet{set}, nil
}

type uncountedSet struct{ DataSet }

func (uncountedSet) RecordCount() RecordCount { return Unknown() }
