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
// **The census is wired in, and it is worth more than it was meant to be.** It
// was invented to answer a page of thirty twisties in one question. What it
// actually replaces is a question per expandable row in the whole TREE: the
// criterion partitions the source by one field, a level is a handful of that
// field's values, so one census serves the top level and every level beneath it.
// A criterion that cannot partition -- a subtree, where every ancestor claims
// every row -- says so and is counted a node at a time.

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

	// Kind is the NAME of the node type this row is, as a symbol, and empty for
	// the default kind.
	//
	// **This is how a reader tells one kind of row from another**, and nothing
	// else can: the descent knows a row's kind by construction -- whatever the
	// `applications` criterion returned out of the applications source is an
	// application -- and this is where it says so. A view holding a column
	// mapping per kind needs exactly this to know which mapping a row takes.
	Kind string
}

// TreeFieldsDefault is what a tree writes where the caller says nothing.
var TreeFieldsDefault = TreeFields{
	Depth:      "depth",
	Path:       "path",
	Expandable: "expandable",
	State:      "state",
	Kind:       "kind",
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
	if f.Kind == "" {
		f.Kind = d.Kind
	}
	return f
}

// TreeOptions is everything a tree is made of.
type TreeOptions struct {
	// Source and Spec are the TOP LEVEL: where its rows come from and which of
	// them, in what order. A nil Spec is all of them, unsorted.
	Source Source
	Spec   *Spec

	// Types is what kind each row is. **The top level's rows are the DEFAULT
	// kind**, so where the top level's rows come from is here and what they ARE
	// is there -- including how one says where it stands, which used to be said
	// twice and is now said once, by the type.
	Types NodeTypes

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

	// order is how each KIND of row is ordered among its siblings, where
	// somebody has said something other than what the configuration says. Keyed
	// by kind name, the empty name being the default kind, which is the top
	// level's. See SortBy.
	order map[string][]SortLevel

	// tells are the readers of this tree, told when a flattening has COMPLETED.
	//
	// **A tree is Arriving in its own right, and that is the layering.** A level
	// answering is news to the TREE -- it must walk again to take the records in.
	// A walk finishing is news to whoever is reading the tree, because that is
	// when the flattened rows exist. Telling readers what a level said, or
	// rebuilding because a walk ended, both collapse those two into one and loop.
	tells []func()

	// later says at least one of this tree's levels answers AFTER its read
	// returns, which is settled once when the tree is stated: the sources are
	// fixed for its life, so whether any of them can arrive is too.
	//
	// A tree of sources that all answer at once flattens exactly as it always
	// did -- on the thread that asked, with the rows there when build returns.
	// One that may wait does the walk on a goroutine of its own, because the
	// answer it waits for arrives on a thread that may be waiting for this one.
	// See build.
	later bool
}

// NewTreeSource makes one, and refuses what cannot be used.
//
// The checks are configuration and are worth finding here rather than when a row
// is drawn: a standing that names two readings, named node types with no field
// to name them in, and a sort on the top level, which the next paragraph is
// about.
func NewTreeSource(o TreeOptions) (*TreeSource, error) {
	if o.Source == nil {
		return nil, fmt.Errorf("tree: no source")
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
	t := &TreeSource{
		opt:   o,
		live:  map[*treeDataSet]bool{},
		later: answersLater(o),
	}
	if t.later {
		// A level answering means the tree must walk again to take the records
		// in -- unless it is already walking, in which case the answer IS what
		// the walk asked for. See levelArrived.
		TellOnArrival(o.Source, t.levelArrived)
		for _, nt := range o.Types.all() {
			if nt != nil && nt.Source != nil {
				TellOnArrival(nt.Source, t.levelArrived)
			}
		}
	}
	return t, nil
}

// answersLater reports whether any level of this tree may answer after its read
// returns.
//
// Asked of every source the tree will read -- the top level's and each node
// type's -- because one of them being across a connection is enough to make the
// whole flattening wait somewhere.
func answersLater(o TreeOptions) bool {
	if _, ok := o.Source.(Arriving); ok {
		return true
	}
	for _, nt := range o.Types.all() {
		if nt == nil || nt.Source == nil {
			continue
		}
		if _, ok := nt.Source.(Arriving); ok {
			return true
		}
	}
	return false
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

// MarkedByPath reports whether one KIND of row's mark segment is its path rather
// than its identity.
//
// **A chain handed to Expand has to be spelled the way the walk spells it**, and
// the walk spells a level with a standing by path -- see `descent.level`, and
// `Standing.Paths`, which is where the two answers are decided. A caller reaching
// in from the side has neither in hand, so it asks.
//
// It is per kind because a standing is: a tree of applications under hosts
// descends by key at one level and might descend by address at the next, and there
// is one right answer per level rather than one per tree.
//
// Without this a reader guesses, and the guess is wrong exactly where it is hard
// to see: a location-descended tree drew its twisties from the child counts, took
// an Expand keyed by identity, matched nothing, and opened nothing -- with no error
// anywhere, because a chain naming a node that is not there is an ordinary thing to
// ask about.
func (t *TreeSource) MarkedByPath(kind string) bool {
	nt := t.opt.Types.Get(kind)
	return nt != nil && nt.Standing.Paths()
}

// SortBy restates how each KIND of row is ordered among its siblings, and tells
// every sequence stated over this tree.
//
// **A tree sorts LEVEL by level, which is why this is per kind and not one
// order.** The pre-order is built rather than sorted -- `Open` refuses a sort for
// that reason -- so "sort by size" over a tree is a sort of each level's own
// sequence, laid out in the order the walk visits them. A kind spelling its size
// `bytes` where another spells it `size` is exactly the case, and the caller who
// knows both is the one holding the columns.
//
// A kind not named here is ordered as its configuration says. A kind named with
// no levels is ordered the way its source answers, which is how a sort is turned
// OFF -- so the map's keys are the question and an empty slice is an answer.
//
// The empty name is the default kind, which is the top level's.
func (t *TreeSource) SortBy(byKind map[string][]SortLevel) {
	t.mu.Lock()
	t.order = byKind
	t.mu.Unlock()
	t.tell()
}

// sortFor is the spec a level is read by, with whatever was said about this
// kind's order in place of what the configuration says.
//
// The spec is COPIED where there is something to say, because it is the caller's
// -- a node type's `Of` may hand back the same one every time, and writing a sort
// into it would be rewriting the configuration.
func (t *TreeSource) sortFor(kind string, spec *Spec) *Spec {
	t.mu.Lock()
	levels, said := t.order[kind]
	t.mu.Unlock()
	if !said {
		return spec
	}
	out := Spec{}
	if spec != nil {
		out = *spec
	}
	out.Sort = levels
	return &out
}

// WhenArrived adds a reader to be told once a flattening has completed
// (serval.Arriving).
//
// What a reader of a tree wants to know is that the ROWS are there, which is a
// different moment from a level answering: one level of several has said its piece
// and the walk goes on. So this fires when the walk is done, and nothing before.
func (t *TreeSource) WhenArrived(tell func()) {
	if tell == nil {
		return
	}
	t.mu.Lock()
	t.tells = append(t.tells, tell)
	t.mu.Unlock()
}

// arrived tells this tree's readers that a flattening has completed.
func (t *TreeSource) arrived() {
	t.mu.Lock()
	tells := make([]func(), len(t.tells))
	copy(tells, t.tells)
	t.mu.Unlock()
	for _, tell := range tells {
		tell()
	}
}

// levelArrived is one of this tree's sources saying its answer has landed.
//
// **An answer that arrives while a walk is in flight is that walk's own**, and
// telling the tree to walk again for it is how asking turns into a loop: the walk
// asks, the answer comes, the answer is called news, a new walk asks the same
// question. That is exactly what happened -- eight hundred thousand queries, each
// one answered, each answer starting the next.
//
// So an answer during a walk is left to the walk that asked for it, and an answer
// at any other time is something that changed and the flattening is out of date.
func (t *TreeSource) levelArrived() {
	t.mu.Lock()
	for set := range t.live {
		set.mu.Lock()
		walking := set.walking
		set.mu.Unlock()
		if walking {
			t.mu.Unlock()
			return
		}
	}
	t.mu.Unlock()
	t.tell()
}

// Stale says that what this tree flattens has changed: a level's source was
// restated, a record was altered, one arrived or one went.
//
// **Told, never decided.** A tree reads several sources at several levels and
// holds the flattening; nothing here asks any of them whether they still say
// what they said. Whoever changed one says so, and every sequence stated over
// this tree is built again.
//
// **It takes no Notice, because a tree's flattening is total.** The pre-order is
// one walk over every level, so a record altered anywhere can change which rows
// are visible, how deep they stand and what their twisties say -- there is no run
// of one level kept apart from the rest for a reason to narrow. Taking the fields
// would be taking a parameter this version does not read. Narrowing the walk is
// what a later one would want a Notice for, and it can have one then.
func (t *TreeSource) Stale() { t.tell() }

func (t *TreeSource) moved(do func()) {
	do()
	t.tell()
}

// tell is the rebuild every live sequence owes, once something has said so.
func (t *TreeSource) tell() {
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
	set := &treeDataSet{tree: t, at: map[string]int{}}
	if spec != nil {
		set.shallow = spec.Filter
	}
	// **Stating a sequence does not walk it.** How far to flatten is the SCOPE's
	// question, and no scope has been asked yet -- so a walk here could only guess,
	// and the guess it used to make was "all of it". A reader's first Read says what
	// it wants and the flattening goes that far. See treeDataSet.reach.

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

	// walking says a flattening is in flight, for a tree whose levels may answer
	// later. It is what stops a second walk asking every level the same question
	// again while the first is still being answered.
	walking bool

	// budget is how many rows the last walk was asked for, and `whole` says it ran
	// out of TREE before it ran out of budget -- so what is held is the lot.
	//
	// **That is the difference between an exact count and a floor.** A flattening
	// that stopped because it had what it came for knows there may be more; one
	// that stopped because there was no more knows there is not.
	budget int
	whole  bool
}

// needs is how many flattened rows a scope requires before it can be answered.
//
// A scope starting at a position needs everything up to it and then its count; one
// starting `after` a row it holds needs that row's place and then the count. A
// scope that names neither starts at the beginning.
//
// **Asking for an enormous count is a reader saying it wants the whole thing**,
// which is what every reader of a tree did before there was any other way to ask.
func (v *treeDataSet) needs(s *Scope) int {
	if s == nil || s.Count <= 0 {
		return 0
	}
	from := 0
	switch {
	case s.From > 0:
		from = s.From
	case s.After != nil:
		if at, ok := v.at[Key(s.After)]; ok {
			from = at + 1
		}
	}
	if s.Count >= everyRow-from {
		return everyRow
	}
	return from + s.Count
}

func (v *treeDataSet) Close() {
	v.tree.mu.Lock()
	delete(v.tree.live, v)
	v.tree.mu.Unlock()

	v.mu.Lock()
	v.rows, v.at = nil, nil
	v.mu.Unlock()
}

// RecordCount is how many rows are visible.
//
// **Exact where the flattening saw the whole tree, and a FLOOR where it stopped
// at what it was asked for.** Those are two different facts and RecordCount is
// built to say either: a reader that wanted forty rows out of a hundred thousand
// is told at least forty, and draws a thumb that shrinks as it learns more rather
// than a true one it has not earned.
//
// A reader that asked for the whole sequence gets the exact figure, which is what
// every reader of a tree did before there was another way to ask.
func (v *treeDataSet) RecordCount() RecordCount {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.at == nil {
		return Unknown() // closed
	}
	if v.whole {
		return Exactly(len(v.rows))
	}
	return AtLeast(len(v.rows))
}

// reach flattens far enough to answer this scope, where it is not far enough
// already.
//
// It does nothing in the two ordinary cases: the walk already saw the whole tree,
// or it already holds more rows than the scope needs. So a reader stepping through
// a sequence pays for the walk once and then for nothing, and one that jumps
// further than it has been pays for the difference.
func (v *treeDataSet) reach(s *Scope) error {
	v.mu.Lock()
	if v.at == nil || v.whole {
		v.mu.Unlock()
		return nil
	}
	need := v.needs(s)
	if need <= len(v.rows) || need <= v.budget {
		v.mu.Unlock()
		return nil
	}
	v.budget = need
	walking := v.walking
	v.mu.Unlock()
	if walking {
		// One in flight already, and it will tell when it lands.
		return nil
	}
	return v.build()
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

// build flattens the tree.
//
// **A tree whose levels all answer at once flattens HERE**, on the thread that
// asked, with the rows in place when this returns -- which is every tree over
// records in hand, and is what every reader of one has always seen.
//
// A tree that may WAIT walks on a goroutine of its own and this returns at once,
// because the answer it waits for arrives on a thread that may in turn be waiting
// for this one: a display's connection reader hands a batch to the drawing thread
// and waits for it, so a flattening that blocked the drawing thread for an answer
// would be waiting for the thread that has to deliver it. Nothing here knows that
// -- it is simply why waiting is done somewhere of its own.
//
// When such a walk finishes it TELLS, which is how a reader learns to read again.
// It is the same saying a mark moved uses, and the reason there is no polling
// anywhere in this.
func (v *treeDataSet) build() error {
	if !v.tree.later {
		return v.walk()
	}
	v.mu.Lock()
	if v.walking {
		// One walk is enough. A second would ask every level the same question
		// again while the first was still being answered, which is how asking
		// turns into a loop.
		v.mu.Unlock()
		return nil
	}
	v.walking = true
	if v.at == nil {
		// **An empty sequence and not a closed one.** A nil index is how this says
		// it has been let go, and a walk that has not finished has not been let go
		// -- it holds nothing YET. Reading it before the first answer is ordinary
		// and answers no rows, which is what a view draws while it waits.
		v.rows, v.at = nil, map[string]int{}
	}
	v.mu.Unlock()

	go func() {
		err := v.walk()
		v.mu.Lock()
		v.walking = false
		closed := v.at == nil
		v.mu.Unlock()
		if err != nil || closed {
			return
		}
		// The ROWS are there now, so whoever is reading this tree can read them.
		// Not `tell`: that rebuilds every live sequence, which would walk this one
		// again for the answer it just finished taking in.
		v.tree.arrived()
	}()
	return nil
}

// walk is the flattening itself, wherever it is being done.
func (v *treeDataSet) walk() error {
	v.mu.Lock()
	budget := v.budget
	v.mu.Unlock()

	d := &descent{set: v, tree: v.tree, want: budget, left: budget}
	// The top level's rows are of the default kind, read out of the tree's own
	// source by the tree's own spec.
	top := v.tree.opt.Types.Default
	err := d.level("", v.tree.opt.Source, v.tree.opt.Spec, top.Standing,
		Node{}, 0, nil, nil)

	v.mu.Lock()
	defer v.mu.Unlock()
	v.err = err
	if err != nil {
		v.rows, v.at = nil, map[string]int{}
		v.whole = false
		return err
	}
	// It ran out of TREE before it ran out of budget, so this is the lot -- which
	// is what makes the count exact rather than a floor.
	v.whole = !d.enough()
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
	// **The scope is what says how far to flatten.** A reader asking for a window
	// needs the pre-order up to the end of it and no further, so that is what the
	// walk is asked for -- and every level inside it is asked for no more than
	// that. A reader asking for the whole sequence gets the whole walk, which is
	// what one over records in hand has always had.
	if err := v.reach(s); err != nil {
		out.Done(Complete{Error: err.Error()})
		return nil
	}

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

	// want is how many rows this walk was asked for, and `left` how many of them
	// it still needs.
	//
	// **It is what makes a level's question targeted.** A level is asked for the
	// children of one node -- that part was always exact -- but it used to be asked
	// for all of them, however many that is. A flat level of a hundred thousand
	// siblings then answered a hundred thousand rows to fill a screen of forty.
	//
	// No level can usefully answer more than the walk still needs: every row it
	// sends past that is a row nobody will look at. So `left` is the count each
	// level is read with, and a walk that has what it came for stops asking.
	want int
	left int

	// counts is one census per node type, taken when a twisty first needs one
	// and kept for the rest of this build. A nil value means it was tried and
	// could not be had.
	counts map[*NodeType]*Census
}

// a standingAt is one node on the path from the root, for the revisit budget.
//
// **Which source is part of it, because an identity is unique within a SOURCE
// and not across sources.** Two grafted sources may each hold a record keyed 3,
// and without the qualifier they would count as one node standing twice.
//
// The source is named by the node type that BROUGHT one, which is what `from`
// works out: the top level and a type that grafts nothing both read the tree's
// own source, so a record reached either way is one node. Getting that wrong is
// what the first version of this did, and the cost was a cycle counted as two
// nodes and given twice the laps.
type standingAt struct {
	from *NodeType
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
func (d *descent) from(nt *NodeType) *NodeType {
	if nt == nil || nt.Source == nil {
		return nil
	}
	return nt
}

// level reads one level's rows and, for each that shows, the levels beneath it.
//
// `seen` is the chain of ancestors, for the revisit budget; `chain` is the same
// walk as mark segments. They are two lists rather than one because they are
// keyed differently on purpose: a mark segment is a path where there is one, and
// a revisit is counted on the record's IDENTITY always -- a cycle appends a
// segment each lap, so the path is the thing GROWING and cannot be what notices.
func (d *descent) level(kind string, src Source, spec *Spec, st Standing,
	above Node, depth int, chain []string, seen []standingAt) error {

	nt := d.tree.opt.Types.Get(kind)

	spec = d.tree.sortFor(kind, d.withShallow(spec))
	set, err := src.Open(spec)
	if err != nil {
		return err
	}
	// **Read, then WAIT, and only then close.** A source holding its records has
	// filled the sink and called Done on the way out, so the wait returns at once
	// and this is the same three lines it always was. One across a connection has
	// only sent a question -- and closing before the answer came hung up on it,
	// which is a question asked and deliberately not listened for.
	got := newLevelRows()
	err = set.Read(&Scope{Count: d.ask()}, got)
	if err == nil {
		got.wait()
	}
	set.Close()
	if err != nil {
		return err
	}
	ids, fields1, readErr := got.reading()
	if readErr != "" {
		return fmt.Errorf("tree: reading a level: %s", readErr)
	}

	for i := range ids {
		// **The budget is spent, so the walk stops.** Every row past what was asked
		// for is a row nobody is going to look at, and a deeper level opened to
		// find it is a question asked for nothing.
		if d.enough() {
			return nil
		}
		id, fields := ids[i], fields1[i]
		path := st.PathOf(above.Path, id, fields)
		node := Node{Key: id, Fields: fields, Path: path}

		seg := Key(id)
		if st.Paths() {
			seg = path
		}
		mine := append(append([]string{}, chain...), seg)

		// The revisit budget governs DESCENT and not emission: a node standing
		// on its own path is still drawn, it is simply not expandable.
		next := d.tree.opt.Types.Beneath(kind, node)
		here := standingAt{from: d.from(nt), key: Key(id)}
		looped := laps(seen, here) > d.tree.opt.Revisits

		// How many children, over ALL the kinds beneath this row. RecordCount.And
		// sums and degrades exactness, so a kind that cannot count leaves the
		// whole figure a floor rather than making the others a lie.
		//
		// **It starts at exactly NONE and not at unknown.** A row that met no
		// branch's condition has nowhere for children to come from, and that is a
		// statement rather than an ignorance -- the same answer a kind with no
		// criterion gives, arrived at from the other side. Starting at Unknown
		// drew a live twisty on every leaf of a conditional tree.
		children := Exactly(0)
		for _, name := range next {
			children = children.And(d.reach(d.tree.opt.Types.Get(name), node, looped))
		}
		d.emit(node, kind, depth, d.tree.mark.Mark(mine...), children)

		if looped || children.Exact && children.N == 0 {
			continue
		}
		if !d.tree.mark.Shows(mine...) {
			continue
		}
		// **One parent, one level per kind beneath it, grouped in the order the
		// type named them.** Every folder then every file, or every application
		// then every volume: predictable, and asking nothing of two sources that
		// they cannot answer.
		for _, name := range next {
			if d.enough() {
				return nil
			}
			below := d.tree.opt.Types.Get(name)
			if below == nil || below.Children.Of == nil {
				continue
			}
			if err := d.level(name, d.under(below), below.Children.Of(node),
				below.Standing, node, depth+1, mine, append(seen, here)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ask is how many rows to read a level with: what this walk still needs.
//
// **One more than it needs, deliberately.** A level that answers exactly the
// budget leaves the walk unable to tell "that is all there is" from "there is more
// and you stopped" -- and the difference between an exact count and a floor is
// exactly that. One spare row settles it without fetching a page to find out.
//
// A walk with no budget asks for everything, which is what a reader wanting the
// whole sequence gets: `Read` with an enormous count is a reader saying so.
func (d *descent) ask() int {
	if d.want <= 0 || d.left <= 0 {
		return everyRow
	}
	if d.left >= everyRow-1 {
		return everyRow
	}
	return d.left + 1
}

// enough reports whether this walk has what it was asked for, so the descent can
// stop rather than walk a tree nobody is reading the rest of.
func (d *descent) enough() bool { return d.want > 0 && d.left <= 0 }

// everyRow is the count a level is read with where nobody set a budget. The
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
func (d *descent) reach(nt *NodeType, of Node, looped bool) RecordCount {
	if looped || nt == nil || nt.Children.Of == nil {
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
	if c := d.census(nt); c != nil {
		return c.CountOfGroup(nt.Children.Group(of))
	}
	set, err := d.under(nt).Open(d.withShallow(nt.Children.Of(of)))
	if err != nil {
		return Unknown()
	}
	defer set.Close()
	return CountOf(set)
}

// census is this node type's counts, taken once and kept.
//
// **One census answers every node of a type, not merely every node of a level.**
// The criterion partitions the whole source by one field, and a level is a
// handful of that field's values -- so the same answer serves the top level, and
// every level beneath it, and any level a mark opens later in this build. The
// case it was invented for was a page of thirty twisties; what it actually
// replaces is a question per expandable row in the tree.
//
// A type that cannot be censused, or a source that will not take one, is
// remembered as such: the fallback is a count per node and asking again for
// every one of them would be worse than not trying.
func (d *descent) census(nt *NodeType) *Census {
	if c, tried := d.counts[nt]; tried {
		return c
	}
	if d.counts == nil {
		d.counts = map[*NodeType]*Census{}
	}
	d.counts[nt] = nil // tried, and nothing came of it unless the rest succeeds
	if !nt.Children.counts() {
		return nil
	}
	// The shallow filter goes on, or the census would count rows the level
	// itself would not show.
	set, err := d.under(nt).Open(d.withShallow(nt.Children.Over))
	if err != nil {
		return nil
	}
	defer set.Close()
	c, err := CensusOf(set, nt.Children.By)
	if err != nil {
		return nil
	}
	d.counts[nt] = &c
	return &c
}

// under is where this type's children are read from: its own source, or the
// tree's where it grafts nothing.
func (d *descent) under(nt *NodeType) Source {
	if nt.Source != nil {
		return nt.Source
	}
	return d.tree.opt.Source
}

// emit adds one row, with the record's own fields and the tree's beside them.
//
// The tree's WIN on a collision, because a view cannot draw without them -- and
// that is why their names are the caller's to move.
func (d *descent) emit(of Node, kind string, depth int, state Mark, children RecordCount) {
	f := d.tree.opt.Fields
	out := make(Record, 0, len(of.Fields)+5)
	for _, m := range of.Fields {
		switch m.Name {
		case f.Depth, f.Path, f.Expandable, f.State, f.Kind:
		default:
			out = append(out, m)
		}
	}
	out = append(out,
		Named(f.Depth, depth),
		Named(f.Path, of.Path),
		Named(f.State, NewSymbol(state.String())),
		Named(f.Kind, NewSymbol(kind)))
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
	if d.want > 0 {
		d.left--
	}
}

// levelRows takes one level entire.
// A levelRows takes one level's answer, and knows when it has all of it.
//
// **Knowing when matters, because not every source answers before Read returns.**
// One holding its records fills this and calls Done on the way out; one across a
// connection has only sent a question, and Done comes later on whatever thread
// the answer arrives on. A descent that closed the level in between hung up before
// being answered -- which is what `over` is here to stop.
type levelRows struct {
	mu     sync.Mutex
	ids    []*Value
	fields []Record
	err    string

	done bool
	over chan struct{} // closed once, when Done comes
}

func newLevelRows() *levelRows { return &levelRows{over: make(chan struct{})} }

func (l *levelRows) Ordered() {}
func (l *levelRows) Record(id *Value, f Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ids = append(l.ids, id)
	l.fields = append(l.fields, f)
	return nil
}
func (l *levelRows) Subset(id *Value, f Record, _ Totals) error { return l.Record(id, f) }
func (l *levelRows) Done(c Complete) {
	l.mu.Lock()
	if l.done {
		l.mu.Unlock()
		return
	}
	l.done, l.err = true, c.Error
	l.mu.Unlock()
	close(l.over)
}

// wait holds until the whole of this level has arrived.
//
// It returns at once for a source that answered on the way out, which is every
// synchronous one -- so nothing waits that had no reason to. Where it does wait it
// is never on a thread that the answer needs: an asynchronous tree walks on a
// goroutine of its own, for exactly this. See build.
func (l *levelRows) wait() {
	l.mu.Lock()
	done := l.done
	l.mu.Unlock()
	if done {
		return
	}
	<-l.over
}

// reading is what arrived, under the lock, because a record may still be landing
// as this is read.
func (l *levelRows) reading() (ids []*Value, fields []Record, err string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ids, l.fields, l.err
}

// A TreeFielded source says what names it writes its tree fields under.
//
// Optional, on the model of Counting and Censusing: a source that is not a tree
// is not one of these, and a reader asking gets told so rather than having to
// guess from whether a field happens to be there. That difference matters --
// "a tree that could not count this row's children" and "not a tree at all" are
// two answers, and sniffing for a field conflates them.
//
// A wrapper over a tree may forward it, and one that does not simply reads as not
// a tree, which is a missing capability rather than a wrong answer.
type TreeFielded interface {
	TreeFields() TreeFields
}

// TreeFields is what this source writes its added fields under.
func (t *TreeSource) TreeFields() TreeFields { return t.opt.Fields }

// TreeFieldsOf asks a source what it writes its tree fields under, and reports
// false for one that is not a tree.
func TreeFieldsOf(src Source) (TreeFields, bool) {
	if t, ok := src.(TreeFielded); ok {
		return t.TreeFields(), true
	}
	return TreeFields{}, false
}
