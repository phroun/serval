package serval

// A tree is a sequence.
//
// That is the whole of the idea, and everything else follows from taking it
// seriously. A TreeSource wraps a source and presents the rows that are VISIBLE,
// flat, in pre-order, each carrying its depth, its path and whether it can be
// expanded -- three things a tree knows and a record need not.
//
// Everything downstream then works unchanged, which is the point of the design
// rather than a happy accident: RecordCount is how many rows are visible and
// therefore a scrollbar's scale, Scope.From is a position in the flattened
// sequence and therefore a thumb dragged, Complete.First is the calibration, and
// a CachedSource over it caches runs of a tree like any other. A tree VIEW is
// then a list view that draws indentation from a field and a twisty from
// another.
//
// # Where expansion lives, and why it is not a view's business
//
// The obvious place for expansion is the view: which nodes are open is surely
// about how somebody is LOOKING at the data. That was the first answer and it is
// the wrong one. AmendedSource already does what that answer says cannot be
// done -- Add, Replace and Delete are a caller reaching in and changing what a
// sequence holds, with everyone reading it told by invalidation -- and expansion
// is the same shape. See `docs/trees.md`.
//
// # What is built here, and what is not
//
// **The flattening is eager.** Opening a data set walks every visible row and
// holds it, which makes the count exact, the positions exact and From honoured
// exactly -- and which is right for records in hand, the case this serves. A
// source that must be ASKED wants the window and not the whole, and that is the
// next piece rather than a different design: the same interface, answered
// lazily.
//
// **The census is not wired in.** Expandability asks the row, and then counts
// the child spec one node at a time. `docs/census.md` is how a page of those
// counts becomes one question, and joining it needs a child type to say which
// field a census of its children would partition by -- an interface worth
// getting right rather than guessing at now.

import (
	"fmt"
	"sync"
)

// TreeFields names the three things a tree ADDS to every row.
//
// They are the caller's to change because they shadow: a tree over filesystem
// records whose own field is called `path` would otherwise lose it, and the
// tree's own must win or a view could not draw. Moving the tree's name is the
// way out, and it is why these are not constants.
type TreeFields struct {
	Depth      string // how deep the row stands, 0 at the top
	Path       string // where it stands, empty where the standing builds no path
	Expandable string // how many children: a number, or a floor, or undefined
	State      string // `closed`, `open` or `openAll`, as a symbol
}

// TreeFieldsDefault is what a tree writes where the caller says nothing.
var TreeFieldsDefault = TreeFields{
	Depth:      "depth",
	Path:       "path",
	Expandable: "expandable",
	State:      "state",
}

func (f TreeFields) orElse(d TreeFields) TreeFields {
	if f.Depth == "" {
		f.Depth = d.Depth
	}
	if f.Path == "" {
		f.Path = d.Path
	}
	if f.Expandable == "" {
		f.Expandable = d.Expandable
	}
	if f.State == "" {
		f.State = d.State
	}
	return f
}

// TreeOptions is everything a tree is made of.
type TreeOptions struct {
	// Source and Spec are the TOP LEVEL: where its rows come from and which of
	// them, in what order. A nil Spec is all of them, unsorted.
	Source Source
	Spec   *Spec

	// Standing is how a row of that source says where it stands. The zero one
	// builds no path, which is the adjacency list, and then marks are keyed by
	// identity instead.
	Standing Standing

	// Types is where children come from, by the name a row asks for one under.
	Types ChildTypes

	// SaysChildren is the field a row uses to say whether it has children and
	// how many: `undefined` or `false` is a leaf, `true` is children of unknown
	// number, and a number is that many. Empty means no row says, and every
	// twisty is a count.
	SaysChildren string

	// Revisits is how many times a node may be EXPANDED again beneath itself on
	// one root-to-leaf path: 0 refuses, which is the default, and 3 is the most
	// that can be asked for. It is a setting because a tree that refuses says
	// the data is a tree and one that allows says it is a graph, and seeing
	// round the loop is how somebody understands it. See `docs/trees.md`.
	//
	// **It governs expansion and never emission.** A row the criterion says is
	// a child IS a child, so a node standing on its own path is still drawn --
	// with a dead twisty. Hiding it would make the tree lie about what is under
	// a node, which is worse than showing a loop, and the budget exists to bound
	// the walk rather than to censor the data.
	Revisits int

	// ByPath identifies rows by their PATH rather than by the record's own key.
	//
	// Not the default, and a choice a caller makes knowing why. A record's
	// identity is unique across a tree nested inside ONE source, and a path is
	// longer than a key. Where a tree grafts a second source under rows of the
	// first, or allows a node to stand beneath itself, two rows can carry one
	// key and the path is what tells them apart.
	ByPath bool

	// Fields names what the tree adds. Empty names take TreeFieldsDefault.
	Fields TreeFields
}

// The most laps anybody may ask for. The reason to allow any is to make a cycle
// VISIBLE, and nobody has ever needed a fourth to see one.
const MostRevisits = 3

// A TreeSource presents the visible rows of a hierarchy as one flat sequence.
type TreeSource struct {
	opt TreeOptions

	mu   sync.Mutex
	mark Marks
	live map[*treeDataSet]bool
}

// NewTreeSource makes one, and refuses what cannot be used.
//
// The checks are configuration and are worth finding here rather than when a row
// is drawn: a standing that names two readings, named child types with no field
// to name them in, and a sort on the top level, which the next paragraph is
// about.
func NewTreeSource(o TreeOptions) (*TreeSource, error) {
	if o.Source == nil {
		return nil, fmt.Errorf("tree: no source")
	}
	if err := o.Standing.Check(); err != nil {
		return nil, fmt.Errorf("tree: the top level's %w", err)
	}
	if err := o.Types.Check(); err != nil {
		return nil, fmt.Errorf("tree: %w", err)
	}
	if o.Revisits < 0 || o.Revisits > MostRevisits {
		// Clamped rather than refused, the way a count is.
		if o.Revisits < 0 {
			o.Revisits = 0
		} else {
			o.Revisits = MostRevisits
		}
	}
	o.Fields = o.Fields.orElse(TreeFieldsDefault)
	return &TreeSource{opt: o, live: map[*treeDataSet]bool{}}, nil
}

// Marks is the expansion, for a caller that wants to set it directly. The tree's
// own four verbs are the usual way, because they also tell the sequences that
// what they hold has changed.
func (t *TreeSource) Marks() *Marks { return &t.mark }

// Expand, ExpandAll, Collapse and CollapseAll are the mark verbs, and each tells
// every live sequence afterwards.
//
// **Invalidation is told, never decided.** Nothing here polls, nothing expires
// and no generation is compared: a mark moved, so the source says so, and the
// sequences drawing on it rebuild. That the saying costs a rebuild is this
// version's price for an eager flattening, and it is bounded by the rows that
// are visible rather than by the size of the tree.
func (t *TreeSource) Expand(chain ...string)    { t.moved(func() { t.mark.Open(chain...) }) }
func (t *TreeSource) ExpandAll(chain ...string) { t.moved(func() { t.mark.OpenAll(chain...) }) }
func (t *TreeSource) Collapse(chain ...string)  { t.moved(func() { t.mark.Close(chain...) }) }
func (t *TreeSource) CollapseAll()              { t.moved(t.mark.Clear) }

func (t *TreeSource) moved(do func()) {
	do()
	t.mu.Lock()
	sets := make([]*treeDataSet, 0, len(t.live))
	for s := range t.live {
		sets = append(sets, s)
	}
	t.mu.Unlock()
	for _, s := range sets {
		s.rebuild()
	}
}

// Open states a sequence over the visible rows.
//
// **A sort is refused**, because the order is the tree's own and is BUILT rather
// than sorted. It does not fall out of CompareLevels either: that stops where
// the shorter run ends and returns 0, so `[a]` and `[a, b]` compare EQUAL rather
// than parent-before-child, and a tuple per level cannot express a
// varying-depth path. Each LEVEL is sorted, by its own spec, and the pre-order
// is laid over that.
//
// A filter is the SHALLOW one: it is asked of the rows at every level, and a
// parent that does not match is gone and its children with it. Deep filtering --
// keeping a parent that does not match where a descendant does -- is eager by
// nature and is not here yet.
func (t *TreeSource) Open(spec *Spec) (DataSet, error) {
	if spec != nil && len(spec.Sort) > 0 {
		return nil, fmt.Errorf(
			"tree: a sort of %q, and a tree's order is its own: pre-order is built "+
				"rather than sorted, and each level carries its own sort",
			spec.Sort[0].Field)
	}
	set := &treeDataSet{tree: t}
	if spec != nil {
		set.shallow = spec.Filter
	}
	if err := set.build(); err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.live[set] = true
	t.mu.Unlock()
	return set, nil
}

// --- the flattened sequence ---------------------------------------------

// a treeRow is one visible row: the record, and what the tree knows about where
// it stands.
type treeRow struct {
	id     *Value
	fields Record
}

type treeDataSet struct {
	tree    *TreeSource
	shallow *Filter

	mu   sync.Mutex
	rows []treeRow
	at   map[string]int // where each row stands, by identity
	err  error          // what the last build ran into, answered on the next Read
}

func (v *treeDataSet) Close() {
	v.tree.mu.Lock()
	delete(v.tree.live, v)
	v.tree.mu.Unlock()

	v.mu.Lock()
	v.rows, v.at = nil, nil
	v.mu.Unlock()
}

// RecordCount is how many rows are visible, and it is EXACT: the flattening
// walked them all, so the figure is a slice's length. That is a scrollbar's
// scale over a tree nobody has scrolled.
func (v *treeDataSet) RecordCount() RecordCount {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.at == nil {
		return Unknown() // closed
	}
	return Exactly(len(v.rows))
}

// rebuild is what a mark change costs. It is told rather than noticed.
func (v *treeDataSet) rebuild() {
	v.mu.Lock()
	closed := v.at == nil
	v.mu.Unlock()
	if closed {
		return
	}
	_ = v.build()
}

func (v *treeDataSet) build() error {
	d := &descent{set: v, tree: v.tree}
	err := d.level(nil, v.tree.opt.Source, v.tree.opt.Spec, v.tree.opt.Standing,
		Node{}, 0, nil, nil)

	v.mu.Lock()
	defer v.mu.Unlock()
	v.err = err
	if err != nil {
		v.rows, v.at = nil, map[string]int{}
		return err
	}
	v.rows = d.rows
	v.at = make(map[string]int, len(d.rows))
	for i, r := range d.rows {
		v.at[Key(r.id)] = i
	}
	return nil
}

// Read produces one scope of the flattened sequence.
//
// It is the same walk a source holding its records does, because that is what
// this is: the rows are in a slice, in order, so `after` is a map lookup, `from`
// is honoured exactly, and the count and the first position are both facts.
func (v *treeDataSet) Read(s *Scope, out Sink) error {
	v.mu.Lock()
	rows, at, buildErr := v.rows, v.at, v.err
	v.mu.Unlock()

	if at == nil {
		return fmt.Errorf("this data set has been closed")
	}
	if buildErr != nil {
		out.Done(Complete{Error: buildErr.Error()})
		return nil
	}
	if err := bothEnds(s); err != nil {
		out.Done(Complete{Error: err.Error()})
		return nil
	}

	step, i := 1, 0
	if s.Reversed {
		step, i = -1, len(rows)-1
	}
	if s.After != nil {
		j, ok := at[Key(s.After)]
		if !ok {
			out.Done(Complete{Error: fmt.Sprintf(
				"after %s: no row of mine is in this sequence under that identity",
				s.After.String())})
			return nil
		}
		i = j + step
	} else if s.From != 0 {
		i = s.From
		if i < 0 {
			i = 0
		}
		if i >= len(rows) {
			i = len(rows) - 1
		}
	}

	stop := -1
	if s.Until != nil {
		if j, ok := at[Key(s.Until)]; ok {
			stop = j
		}
	}

	out.Ordered()
	done := Complete{Total: Exactly(len(rows))}
	last := s.After
	sent := 0
	for ; i >= 0 && i < len(rows); i += step {
		if i == stop {
			done.Stop = StopJoined
			break
		}
		if sent >= s.Count {
			done.Stop = StopFilled
			break
		}
		if sent == 0 {
			done.First = Exactly(i)
		}
		// Entire, because the flattening read whole records and added to them.
		// When the window is read lazily instead, this is the line that has to
		// learn to say Subset.
		if err := out.Record(rows[i].id, rows[i].fields); err != nil {
			return err
		}
		last = rows[i].id
		sent++
	}

	if done.Stop == "" {
		done.Stop = StopExhausted
	} else {
		done.Watermark = last
	}
	out.Done(done)
	return nil
}

// --- the descent --------------------------------------------------------

// a descent is one flattening in progress.
type descent struct {
	set  *treeDataSet
	tree *TreeSource
	rows []treeRow
}

// a standingAt is one node on the path from the root, for the revisit budget.
//
// **Which source is part of it, because an identity is unique within a SOURCE
// and not across sources.** Two grafted sources may each hold a record keyed 3,
// and without the qualifier they would count as one node standing twice.
//
// The source is named by the child type that BROUGHT one, which is what `from`
// works out: the top level and a type that grafts nothing both read the tree's
// own source, so a record reached either way is one node. Getting that wrong is
// what the first version of this did, and the cost was a cycle counted as two
// nodes and given twice the laps.
type standingAt struct {
	from *ChildType
	key  string
}

// from is the token for the source a level came from. Nil is the tree's own,
// which the top level and every type that grafts nothing all read.
//
// Two DIFFERENT types grafting the same source count as two, which lets a cycle
// alternating between them take twice the laps. Still bounded, still finite, and
// an unusual enough configuration to be worth a sentence rather than a scheme
// for comparing sources -- a Source is an interface, and a type that is not
// comparable would panic on being compared rather than being told off.
func (d *descent) from(ct *ChildType) *ChildType {
	if ct == nil || ct.Source == nil {
		return nil
	}
	return ct
}

// level reads one level's rows and, for each that shows, the levels beneath it.
//
// `seen` is the chain of ancestors, for the revisit budget; `chain` is the same
// walk as mark segments. They are two lists rather than one because they are
// keyed differently on purpose: a mark segment is a path where there is one, and
// a revisit is counted on the record's IDENTITY always -- a cycle appends a
// segment each lap, so the path is the thing GROWING and cannot be what notices.
func (d *descent) level(ct *ChildType, src Source, spec *Spec, st Standing,
	above Node, depth int, chain []string, seen []standingAt) error {

	spec = d.withShallow(spec)
	set, err := src.Open(spec)
	if err != nil {
		return err
	}
	var got levelRows
	err = set.Read(&Scope{Count: everyRow}, &got)
	set.Close()
	if err != nil {
		return err
	}
	if got.err != "" {
		return fmt.Errorf("tree: reading a level: %s", got.err)
	}

	for i := range got.ids {
		id, fields := got.ids[i], got.fields[i]
		path := st.PathOf(above.Path, id, fields)
		node := Node{Key: id, Fields: fields, Path: path}

		seg := Key(id)
		if st.Paths() {
			seg = path
		}
		mine := append(append([]string{}, chain...), seg)

		// The revisit budget governs DESCENT and not emission: a node standing
		// on its own path is still drawn, it is simply not expandable.
		next := d.tree.opt.Types.For(fields)
		here := standingAt{from: d.from(ct), key: Key(id)}
		looped := laps(seen, here) > d.tree.opt.Revisits

		children := d.reach(next, node, looped)
		d.emit(node, depth, d.tree.mark.Mark(mine...), children)

		if looped || children.Exact && children.N == 0 {
			continue
		}
		if !d.tree.mark.Shows(mine...) || next == nil || next.Children == nil {
			continue
		}
		under := next.Source
		if under == nil {
			under = d.tree.opt.Source
		}
		if err := d.level(next, under, next.Children(node), next.Standing,
			node, depth+1, mine, append(seen, here)); err != nil {
			return err
		}
	}
	return nil
}

// everyRow is the count a level is read with. The flattening is eager, so a
// level is read entire; the number is a ceiling against a source that would
// otherwise answer forever, not a window.
const everyRow = 1 << 30

// laps is how many times this node already stands on the path above it.
func laps(seen []standingAt, at standingAt) int {
	n := 0
	for _, s := range seen {
		if s == at {
			n++
		}
	}
	return n
}

// withShallow puts the sequence's own filter on a level's spec.
//
// A shallow filter is asked at every level independently, so a parent that does
// not match is gone and its children with it. Deep filtering -- keeping a parent
// where a DESCENDANT matches -- is a claim about everything beneath and cannot
// be made without looking, so it is eager by nature and is not here.
func (d *descent) withShallow(spec *Spec) *Spec {
	if d.set.shallow == nil {
		if spec == nil {
			return &Spec{}
		}
		return spec
	}
	out := Spec{}
	if spec != nil {
		out = *spec
	}
	if out.Filter == nil {
		out.Filter = d.set.shallow
	} else {
		out.Filter = &Filter{Op: OpAnd, Children: []*Filter{out.Filter, d.set.shallow}}
	}
	return &out
}

// reach is how many children this node has: **a field, or a count, or nothing
// said.**
//
// The field first, where the row can say, which is free. Then a count of the
// child spec, which answers without reading a record -- Open, count, Close --
// and which is exact over records in hand. Then Unknown, which CountOf answers
// by itself for anything that is not Counting, and which means draw the twisty
// and find out on opening.
//
// A count per visible row is a real cost over a source that must be asked, and
// `docs/census.md` is how a whole page of them becomes one question.
func (d *descent) reach(ct *ChildType, of Node, looped bool) RecordCount {
	if looped || ct == nil || ct.Children == nil {
		return Exactly(0)
	}
	if f := d.tree.opt.SaysChildren; f != "" {
		switch v := of.Fields.Get(f); {
		case v == nil: // undefined, which is the row saying nothing
		case v.Kind == BoolValue && !v.Bool, v.Kind == NilValue:
			return Exactly(0)
		case v.Kind == BoolValue:
			return AtLeast(1) // children of unknown number
		case v.Kind == NumberValue && v.IsInt:
			return Exactly(int(v.Int))
		}
	}
	src := ct.Source
	if src == nil {
		src = d.tree.opt.Source
	}
	set, err := src.Open(d.withShallow(ct.Children(of)))
	if err != nil {
		return Unknown()
	}
	defer set.Close()
	return CountOf(set)
}

// emit adds one row, with the record's own fields and the tree's beside them.
//
// The tree's WIN on a collision, because a view cannot draw without them -- and
// that is why their names are the caller's to move.
func (d *descent) emit(of Node, depth int, state Mark, children RecordCount) {
	f := d.tree.opt.Fields
	out := make(Record, 0, len(of.Fields)+4)
	for _, m := range of.Fields {
		switch m.Name {
		case f.Depth, f.Path, f.Expandable, f.State:
		default:
			out = append(out, m)
		}
	}
	out = append(out,
		Named(f.Depth, depth),
		Named(f.Path, of.Path),
		Named(f.State, NewSymbol(state.String())))
	if children.Exact || children.N > 0 {
		out = append(out, Named(f.Expandable, int(children.N)))
	} else {
		// Nothing known, which is undefined rather than nought: a twisty is
		// drawn and the answer found on opening.
		out = append(out, Named(f.Expandable, nil))
	}

	id := of.Key
	if d.tree.opt.ByPath {
		id = NewText(of.Path)
	}
	d.rows = append(d.rows, treeRow{id: id, fields: out})
}

// levelRows takes one level entire.
type levelRows struct {
	ids    []*Value
	fields []Record
	err    string
}

func (l *levelRows) Ordered() {}
func (l *levelRows) Record(id *Value, f Record) error {
	l.ids = append(l.ids, id)
	l.fields = append(l.fields, f)
	return nil
}
func (l *levelRows) Subset(id *Value, f Record, _ Totals) error { return l.Record(id, f) }
func (l *levelRows) Done(c Complete)                            { l.err = c.Error }
