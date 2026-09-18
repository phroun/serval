package serval

// Where a row stands, and what its children are.
//
// Two things checked together because they cooperate through one value: a
// criterion asking for "everything whose container is this row's own path" needs
// somebody to have worked out that path, and which of the three readings did it
// is the standing's business and not the criterion's.

import (
	"strings"
	"testing"
)

// The filesystem-shaped case from the note, spelled WITHOUT trailing
// delimiters, because real data often is.
func folders() *ListSource {
	row := func(id int64, location, name string) Row {
		return NewRow(NewInt(id), Record{
			Named("location", location), Named("name", name),
		})
	}
	return NewListSource([]Row{
		row(1, "/", "usr"),
		row(2, "/usr", "local"),
		row(3, "/usr/local", "bin"),
		row(4, "/usr/local", "share"),
		row(5, "/usr", "locally"), // the sibling a bare prefix would drag in
	})
}

var here = Standing{Location: "location", Name: "name", Delimiter: "/"}

// A row's own path is its container and its name joined -- **and it is worked
// out from the ROW ALONE**, which is what this reading buys over the derived
// one: a tree can say where a row belongs before it has walked there.
func TestAContainerAndANameMakeAPath(t *testing.T) {
	for _, c := range []struct {
		location, name, want string
	}{
		{"/", "usr", "/usr"},
		{"/usr", "local", "/usr/local"},
		{"/usr/local", "bin", "/usr/local/bin"},
	} {
		r := Record{Named("location", c.location), Named("name", c.name)}
		// The empty `above` is the point: nothing was descended through.
		if got := here.PathOf("", nil, r); got != c.want {
			t.Errorf("%q + %q is %q, want %q", c.location, c.name, got, c.want)
		}
	}
}

// **No trailing delimiter is assumed, and one rule reads both conventions.**
func TestJoiningDoesNotDoubleADelimiter(t *testing.T) {
	for _, c := range []struct{ container, name, want string }{
		{"/usr", "local", "/usr/local"},
		{"/usr/", "local", "/usr/local"}, // already there, not doubled
		{"/", "usr", "/usr"},             // the root does not make `//usr`
		{"", "usr", "usr"},               // nothing above, so the name alone
	} {
		if got := here.Join(c.container, c.name); got != c.want {
			t.Errorf("joining %q and %q gives %q, want %q",
				c.container, c.name, got, c.want)
		}
	}
}

// And the delimiter is whatever the data uses, this package knowing nothing
// about slashes.
func TestTheDelimiterIsTheCallersOwn(t *testing.T) {
	dotted := Standing{Delimiter: "."}
	if got := dotted.Join("com.example", "thing"); got != "com.example.thing" {
		t.Errorf("a namespace joins as %q", got)
	}
	doubled := Standing{Delimiter: "::"}
	if got := doubled.Join("std::vector", "iterator"); got != "std::vector::iterator" {
		t.Errorf("a scope joins as %q", got)
	}
	if got := doubled.Under("std::vector"); got != "std::vector::" {
		t.Errorf("beneath it is %q, want the two-character delimiter on the end", got)
	}
}

// **`eq` gives children.** One equality, no depth field and no prefix
// arithmetic -- and, crucially, the sibling whose name merely begins the same
// way is not among them.
func TestChildrenAreOneEquality(t *testing.T) {
	keys := childrenOf(t, folders(), here.ChildrenByLocation("location"),
		Node{Path: "/usr/local"})
	if got, want := keys, "3 4"; got != want {
		t.Errorf("the children of /usr/local are %q, want %q", got, want)
	}

	// And at the level above: `locally` IS a child of /usr, being in it.
	keys = childrenOf(t, folders(), here.ChildrenByLocation("location"),
		Node{Path: "/usr"})
	if got, want := keys, "2 5"; got != want {
		t.Errorf("the children of /usr are %q, want %q", got, want)
	}
}

// **Descendants are TWO predicates**, and this is the test that found it. The
// children's location is the parent's path exactly, so a prefix test with the
// delimiter on misses every one of them -- while a prefix test without it drags
// `/usr/locally` into `/usr/local`. Both halves, and each does its own job.
func TestDescendantsAreBoundedByTheDelimiter(t *testing.T) {
	keys := childrenOf(t, folders(), here.DescendantsByLocation("location"),
		Node{Path: "/usr"})
	if got, want := keys, "2 3 4 5"; got != want {
		t.Errorf("everything under /usr is %q, want %q", got, want)
	}

	// The narrower one is where it shows: `/usr/local/` must not admit a row
	// whose location is `/usr/locally`.
	deeper := NewListSource([]Row{
		NewRow(NewInt(1), Record{Named("location", "/usr/local")}),
		NewRow(NewInt(2), Record{Named("location", "/usr/local/bin")}),
		NewRow(NewInt(3), Record{Named("location", "/usr/locally")}),
	})
	keys = childrenOf(t, deeper, here.DescendantsByLocation("location"),
		Node{Path: "/usr/local"})
	if got, want := keys, "1 2"; got != want {
		t.Errorf("everything under /usr/local is %q, want %q -- "+
			"a bare prefix has taken in a sibling", got, want)
	}
}

// Under is what does it, and it is not fooled by a path that already carries
// one.
func TestUnderPutsTheDelimiterOnExactlyOnce(t *testing.T) {
	for _, c := range []struct{ path, want string }{
		{"/usr/local", "/usr/local/"},
		{"/usr/local/", "/usr/local/"},
		{"", ""}, // beneath the root is everything, and every string starts with nothing
	} {
		if got := here.Under(c.path); got != c.want {
			t.Errorf("beneath %q is %q, want %q", c.path, got, c.want)
		}
	}
}

// **An adjacency list needs no path at all.** Children are the parent's KEY
// compared against a column, and nothing is built, concatenated or delimited.
func TestAnAdjacencyListNeedsNoPath(t *testing.T) {
	var none Standing
	if none.Paths() {
		t.Error("the zero standing claims to produce a path")
	}
	if got := none.PathOf("anything", NewInt(3), Record{}); got != "" {
		t.Errorf("it produced the path %q", got)
	}

	keys := childrenOf(t, people(), ChildrenByKey("parent"), Node{Key: NewInt(10)})
	if got, want := keys, "1 2 3"; got != want {
		t.Errorf("the children of 10 are %q, want %q", got, want)
	}
}

// A whole path is read as the record spells it, and nothing is joined.
func TestAWholePathIsReadAsItIsWritten(t *testing.T) {
	whole := Standing{Whole: "path", Delimiter: "/"}
	r := Record{Named("path", "/usr/local/bin"), Named("name", "ignored")}
	if got := whole.PathOf("/somewhere/else", NewInt(9), r); got != "/usr/local/bin" {
		t.Errorf("a whole path reads as %q", got)
	}
}

// A derived path is built by descending, and defaults its segment to the
// record's key where no field names one.
func TestADerivedPathIsBuiltByDescending(t *testing.T) {
	derived := Standing{Delimiter: "/"}
	if got := derived.PathOf("", NewInt(3), Record{}); got != "3" {
		t.Errorf("the top level derives %q, want the key alone", got)
	}
	if got := derived.PathOf("3", NewInt(7), Record{}); got != "3/7" {
		t.Errorf("one level down derives %q", got)
	}

	named := Standing{Name: "slug", Delimiter: "/"}
	r := Record{Named("slug", "things")}
	if got := named.PathOf("3", NewInt(7), r); got != "3/things" {
		t.Errorf("with a name field it derives %q", got)
	}
}

// A path segment is what the field HOLDS, not how it reads: Value.String quotes
// text, and a quoted path would match nothing.
func TestASegmentIsTheCharactersAndNotTheRendering(t *testing.T) {
	if got := Segment(NewText("usr")); got != "usr" {
		t.Errorf("text segments as %q, want it unquoted", got)
	}
	if got := Segment(NewInt(42)); got != "42" {
		t.Errorf("a number segments as %q", got)
	}
	if got := Segment(NewSymbol("bin")); got != "bin" {
		t.Errorf("a symbol segments as %q", got)
	}
	if got := Segment(nil); got != "" {
		t.Errorf("undefined segments as %q, want nothing", got)
	}
}

// A standing that cannot mean what it says is refused when the tree is built,
// rather than when a row is drawn.
func TestAStandingThatCannotMeanWhatItSaysIsRefused(t *testing.T) {
	if err := (Standing{Whole: "p", Location: "d", Delimiter: "/"}).Check(); err == nil {
		t.Error("two readings at once were accepted")
	}
	if err := (Standing{Location: "d", Name: "n"}).Check(); err == nil {
		t.Error("joining with no delimiter was accepted")
	}
	// And the one that looks like a mistake and is not.
	if err := (Standing{}).Check(); err != nil {
		t.Errorf("the adjacency list was refused: %v", err)
	}
}

// --- child types --------------------------------------------------------

// **A child type may order its children differently from its parents**, which
// costs nothing because it produces a whole Spec.
func TestAChildTypeMayBringItsOwnSort(t *testing.T) {
	by := Sorted(ChildrenByKey("parent"),
		SortLevel{Field: "kind", Level: Level{Descending: true}})
	spec := by.Of(Node{Key: NewInt(10)})
	if len(spec.Sort) != 1 || spec.Sort[0].Field != "kind" {
		t.Fatalf("the sort did not come through: %v", spec.Sort)
	}
	// 1 and 2 are files, 3 is a dir; descending by kind puts the files first,
	// and the identity settles the rest.
	if got, want := readKeys(t, people(), spec), "1 2 3"; got != want {
		t.Errorf("sorted children are %q, want %q", got, want)
	}
}

// A row says nothing, and gets the default. A field a record has not got reads
// as undefined everywhere here, so "says nothing" and "has not got it" are one
// case and must be.
func TestARowThatSaysNothingGetsTheDefault(t *testing.T) {
	def := &ChildType{Children: ChildrenByKey("parent")}
	types := ChildTypes{Default: def, Field: "childType",
		Named: map[string]*ChildType{"windows": {}}}

	if got := types.For(Record{Named("kind", "file")}); got != def {
		t.Error("a row with no childType field did not get the default")
	}
	// `Named(name, nil)` is UNDEFINED and not nil, so it is a row saying
	// nothing and gets the default -- which is why saying "no children" needs a
	// value that is actually there.
	if got := types.For(Record{Named("childType", nil)}); got != def {
		t.Error("a row whose childType is undefined did not get the default")
	}
	// And `false` and `nil` mean no children even where something is registered
	// under that spelling -- they are a row refusing, not a row naming.
	saying := ChildTypes{Default: def, Field: "childType",
		Named: map[string]*ChildType{"false": def, "nil": def}}
	if got := saying.For(Record{Named("childType", NewNil())}); got != nil {
		t.Error("a row naming nil got a type")
	}
	if got := saying.For(Record{Named("childType", false)}); got != nil {
		t.Error("a row naming false got the type registered under that spelling")
	}
	if got := types.For(Record{Named("childType", false)}); got != nil {
		t.Error("a row naming false got a type")
	}
	if got := types.For(Record{Named("childType", "windows")}); got != types.Named["windows"] {
		t.Error("a row naming a registered type did not get it")
	}
}

// **An unknown name is a leaf and not a refusal.** One mistyped field must not
// empty a view, which is the same posture a sort takes towards a field a record
// has not got.
func TestAnUnknownChildTypeNameIsALeaf(t *testing.T) {
	types := ChildTypes{
		Default: &ChildType{Children: ChildrenByKey("parent")},
		Field:   "childType",
		Named:   map[string]*ChildType{"windows": {}},
	}
	if got := types.For(Record{Named("childType", "windoows")}); got != nil {
		t.Errorf("a mistyped name got %v, want a leaf", got)
	}
}

// And a set that cannot be used is refused where it is built.
func TestAChildTypeSetIsChecked(t *testing.T) {
	if err := (ChildTypes{}).Check(); err == nil {
		t.Error("a set with no types at all was accepted")
	}
	if err := (ChildTypes{
		Default: &ChildType{},
		Named:   map[string]*ChildType{"windows": {}},
	}).Check(); err == nil {
		t.Error("named types with no field to name them in were accepted")
	}
	bad := ChildTypes{Default: &ChildType{Standing: Standing{Location: "d"}}}
	if err := bad.Check(); err == nil {
		t.Error("a default type with an unusable standing was accepted")
	} else if !strings.Contains(err.Error(), "delimiter") {
		t.Errorf("the refusal does not say what is wrong: %v", err)
	}
}

// --- helpers ------------------------------------------------------------

// childrenOf asks a criterion for one node's children and lists their keys.
func childrenOf(t *testing.T, src Source, by Criterion, of Node) string {
	t.Helper()
	return readKeys(t, src, by.Of(of))
}

func readKeys(t *testing.T, src Source, spec *Spec) string {
	t.Helper()
	set, err := src.Open(spec)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer set.Close()
	var out censusRows
	if err := set.Read(&Scope{Count: 100}, &out); err != nil {
		t.Fatalf("reading: %v", err)
	}
	keys := make([]string, 0, len(out.ids))
	for _, id := range out.ids {
		keys = append(keys, Segment(id))
	}
	return strings.Join(keys, " ")
}
