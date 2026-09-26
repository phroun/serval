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
// source that must be ASKED wants the Extent and not the whole, and that is the
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

// TreeFields names the things a tree ADDS to every row.
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

	// Chain is the mark segments from the root down to this row, as a list of
	// positional members -- the walk's own `mine`, written down.
	//
	// **It is what lets a reader hold a WINDOW of a tree and still open a row in
	// it.** Expanding is `Marks.Open(chain...)`, and a reader that holds the whole
	// pre-order can spell the chain for itself: the ancestors of any row precede
	// it, so walking back up the rows it has is enough. A reader holding rows
	// forty to eighty cannot -- the ancestors are above forty, which is exactly
	// what it declined to hold -- so it would have to fetch what it let go
	// in order to click on what it has.
	//
	// `Path` is this same fact for a level with a Standing, and is empty for the
	// two commonest trees: a source that is its own adjacency list, and a flat
	// source read as one generation. Neither configures a standing, so neither has
	// a path, and both still have to be clickable.
	//
	// The descent holds the chain already, a segment appended per level, so the
	// row costs one list and nothing is walked to find it.
	Chain string

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
	Chain:      "chain",
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
	if f.Chain == "" {
		f.Chain = d.Chain
	}
	return f
}

// TreeOptions is everything a tree is made of.
type TreeOptions struct {
	// Source and DataSetDescriptor are the TOP LEVEL: where its rows come from and which of
	// them, in what order. A nil DataSetDescriptor is all of them, unsorted.
	Source     Source
	Descriptor *DataSetDescriptor

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

	// noJump says a level of this tree has answered from the beginning after being
	// asked to begin somewhere else, so this tree stops asking. See jumps.
	noJump bool

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
	// **Asked with `Arrives` and not with a type assertion**, because a wrapper
	// implements `Arriving` whether or not anything under it does -- it hands the
	// notice on, which is its job. A tree that took the assertion for an answer
	// concluded that a cache over records in hand might answer late, walked on a
	// goroutine, returned no rows, and waited for a notice that could never come.
	if Arrives(o.Source) {
		return true
	}
	for _, nt := range o.Types.all() {
		if nt == nil || nt.Source == nil {
			continue
		}
		if Arrives(nt.Source) {
			return true
		}
	}
	return false
}

// Marks is the expansion, for a caller that wants to set it directly. The tree's
// own four verbs are the usual way, because they also tell the sequences that
// what they hold has changed.
func (t *TreeSource) Marks() *Marks { return &t.mark }

// jumps reports whether this tree's levels have ever been seen to honour a
// position, which they are taken to until one says otherwise.
//
// A source that walks its own body has no index into a sequence somebody else
// named, so it answers from the beginning however it is asked. That is allowed --
// `Scope.From` is best effort -- and it is a fact about the SOURCE rather than
// about any one walk, so it is learned once and remembered. The cost of learning it
// is one read; the cost of not remembering it would be one per walk for ever.
func (t *TreeSource) jumps() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.noJump
}

// sawNoJump records that a level answered from the beginning after being asked to
// begin somewhere else.
func (t *TreeSource) sawNoJump() {
	t.mu.Lock()
	t.noJump = true
	t.mu.Unlock()
}

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

// sortFor is the descriptor a level is read by, with whatever was said about this
// kind's order in place of what the configuration says.
//
// The descriptor is COPIED where there is something to say, because it is the caller's
// -- a node type's `Of` may hand back the same one every time, and writing a sort
// into it would be rewriting the configuration.
func (t *TreeSource) sortFor(kind string, descriptor *DataSetDescriptor) *DataSetDescriptor {
	t.mu.Lock()
	levels, said := t.order[kind]
	t.mu.Unlock()
	if !said {
		return descriptor
	}
	out := DataSetDescriptor{}
	if descriptor != nil {
		out = *descriptor
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
// varying-depth path. Each LEVEL is sorted, by its own descriptor, and the pre-order
// is laid over that.
//
// A filter is the SHALLOW one: it is asked of the rows at every level, and a
// parent that does not match is gone and its children with it. Deep filtering --
// keeping a parent that does not match where a descendant does -- is eager by
// nature and is not here yet.
func (t *TreeSource) Open(descriptor *DataSetDescriptor) (DataSet, error) {
	if descriptor != nil && len(descriptor.Sort) > 0 {
		return nil, fmt.Errorf(
			"tree: a sort of %q, and a tree's order is its own: pre-order is built "+
				"rather than sorted, and each level carries its own sort",
			descriptor.Sort[0].Field)
	}
	set := &treeDataSet{tree: t, at: map[string]int{}}
	if descriptor != nil {
		set.shallow = descriptor.Filter
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

	// topCount is how many rows the TOP LEVEL holds, and counts is one census per
	// node type, both as the last walk found them.
	//
	// **Both are free.** The walk opens the top level anyway, so counting it there
	// costs one question the set was already open for; the censuses it took are
	// thrown away at the end of a walk and are kept instead. They are what lets
	// `reckon` work out how long the flattening is without walking it, which is the
	// difference between a true thumb and a floor.
	//
	// They go stale when the DATA changes, and a source saying so is what rebuilds
	// the walk and refreshes them -- invalidation being told rather than decided.
	topCount RecordCount
	counts   map[*NodeType]*Census

	// base is where the rows this set holds BEGIN: the flat position of the first
	// of them, and nought for a walk that started at the top.
	//
	// **A flattening is an Extent now, not a prefix.** A reader a hundred thousand
	// rows down is answered by a walk that skipped to there, so what is held is a
	// stretch of the sequence rather than the front of it, and every position here
	// -- the index, a scope's `from`, the `first` an answer reports -- is the
	// position in the SEQUENCE and not the offset into the slice.
	base int

	// from is where the last walk was asked to begin and budget is how many rows it
	// was asked to keep; `whole` says it ran out of TREE before it ran out of
	// budget -- so what is held reaches the end of the sequence.
	//
	// **That is the difference between an exact count and a floor.** A flattening
	// that stopped because it had what it came for knows there may be more; one
	// that stopped because there was no more knows there is not.
	from   int
	budget int
	whole  bool

	// reshaped says the flattening changed SHAPE under the rows this set is
	// holding -- a mark moved, or a source said what has stopped being true -- so
	// they belong to a sequence that no longer exists. They are kept until the next
	// walk lands, because a view drawing the old rows for a moment is better than
	// one drawing nothing, and they are dropped when it does rather than joined to
	// what it brings: two shapes joined would read as one sequence holding rows
	// twice.
	reshaped bool

	// seen is the furthest this set has ever reached: the position past the last
	// row any walk of it has produced.
	//
	// **A floor must not come down.** What is held is an Extent now, so a walk
	// further down can hold FEWER rows than one before it, and a length taken from
	// the Extent alone would shrink as a reader scrolled -- a thumb that grew and
	// then jumped back, and a sequence that lost rows nobody removed. A row seen
	// once is a row that exists, so the floor is the furthest anybody has been.
	//
	// It is reset when the flattening changes SHAPE -- a mark moved, or a source
	// saying what has stopped being true -- because then the rows it stands on may
	// not be there any more. Told, not decided, like everything else here.
	seen int
}

// needs is how many flattened rows a scope requires before it can be answered.
//
// A scope starting at a position needs everything up to it and then its count; one
// starting `after` a row it holds needs that row's place and then the count. A
// scope that names neither starts at the beginning.
//
// **Asking for an enormous count is a reader saying it wants the whole thing**,
// which is what every reader of a tree did before there was any other way to ask.
func (v *treeDataSet) needs(s *Scope) (from, count int) {
	if s == nil || s.Count <= 0 {
		return 0, 0
	}
	switch {
	case s.From > 0:
		from = s.From
	case s.After != nil:
		// **A row this set does not hold names no position here.** It may be above
		// the Extent or below it, and the flattening is the only thing that knows
		// which -- so the walk starts again at the top, which is where it can be
		// found. A reader stepping through what it holds never reaches this.
		if at, ok := v.at[Key(s.After)]; ok {
			from = at + 1
		}
	}
	if s.Count >= everyRow-from {
		return from, everyRow
	}
	return from, s.Count
}

// holds reports whether the rows this set has cover the Extent a scope wants.
//
// Called under the lock.
func (v *treeDataSet) holds(from, count int) bool {
	if from < v.base {
		return false
	}
	if from+count <= v.base+len(v.rows) {
		return true
	}
	// The walk ran out of tree, so there is nothing past what is held: an Extent
	// reaching beyond the end is answered by the end.
	return v.whole
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
	closed, whole, held := v.at == nil, v.whole, v.seen
	if end := v.base + len(v.rows); end > held {
		held = end
	}
	if whole {
		held = v.base + len(v.rows)
	}
	v.mu.Unlock()

	switch {
	case closed:
		return Unknown()
	case whole:
		// The walk ran out of tree, so its last row is the sequence's last row --
		// and the position of that row is what the sequence is long. Nothing beats
		// having seen the end of it.
		return Exactly(held)
	}
	// **A walk that stopped short can still be counted, and usually can.** See
	// reckon: the top level counts itself and every open node adds its children, so
	// a flattening nobody has walked to the end of still has a length wherever no
	// expand-all is in force -- and where one is, a floor of at least the top level
	// rather than of the rows on screen.
	return v.reckon(held)
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
	if v.at == nil {
		v.mu.Unlock()
		return nil
	}
	from, count := v.needs(s)
	if v.holds(from, count) {
		v.mu.Unlock()
		return nil
	}
	// One already in flight for this Extent, which will tell when it lands.
	if v.walking && from >= v.from && from+count <= v.from+v.budget {
		v.mu.Unlock()
		return nil
	}
	v.from, v.budget = from, count
	walking := v.walking
	v.mu.Unlock()
	if walking {
		return nil
	}
	return v.build()
}

// rebuild is what a mark change costs. It is told rather than noticed.
func (v *treeDataSet) rebuild() {
	v.mu.Lock()
	closed := v.at == nil
	// The shape changed, so how far anybody has been says nothing about how long
	// this is any more, and where a row stood says nothing about where it stands.
	// See treeDataSet.seen.
	v.seen = 0
	if !closed {
		v.at = map[string]int{}
		v.reshaped = true
	}
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
	from, budget := v.from, v.budget
	v.mu.Unlock()

	d := &descent{set: v, tree: v.tree, want: budget, left: budget, skip: from}
	// The top level's rows are of the default kind, read out of the tree's own
	// source by the tree's own descriptor.
	top := v.tree.opt.Types.Default
	err := d.level("", v.tree.opt.Source, v.tree.opt.Descriptor, top.Standing,
		Node{}, 0, nil, nil)

	v.mu.Lock()
	defer v.mu.Unlock()
	v.err = err
	if err != nil {
		v.rows, v.at = nil, map[string]int{}
		v.whole = false
		return err
	}
	// It ran out of TREE before it ran out of budget, so what it holds reaches the
	// end -- which is what makes the count exact rather than a floor.
	//
	// **Where these rows begin.** Every row before the Extent was passed over, so
	// what is left of the skip is what the tree ran out before reaching: a walk that
	// skipped the lot holds nothing and begins at the end of the sequence.
	if v.reshaped {
		// The rows held are of a shape that is gone. This walk is the new one.
		v.rows, v.base, v.whole, v.reshaped = nil, 0, false, false
	}
	// Where THIS walk's rows begin, which is not where the joined run begins: the
	// rows below it were already held, and their positions were written down then.
	began := from - d.skip
	v.join(began, d.rows, !d.enough())
	if end := v.base + len(v.rows); end > v.seen {
		v.seen = end
	}
	// What the walk learned on the way, which `reckon` needs and which no separate
	// question has to be asked for. Both go stale when the DATA changes, and a
	// source saying so is what brings the walk round again.
	v.topCount, v.counts = d.topCount, d.counts
	// **Where a row stands is remembered past the Extent that showed it.** A reader
	// asks for what comes after a record it holds, and it holds rows this set has
	// since slid past -- so an index rebuilt each walk would lose the one row the
	// question was about and send the walk back to the top, which is the Extent
	// undone. What it costs is an entry per row anybody has actually been shown:
	// a reader that jumped to row ninety-nine thousand was shown ten of them.
	//
	// It is thrown away when the SHAPE changes, with `seen`, for the same reason:
	// a position is a fact about one flattening.
	if v.at == nil {
		v.at = make(map[string]int, len(d.rows))
	}
	for i, r := range d.rows {
		v.at[Key(r.id)] = began + i
	}
	return nil
}

// join puts a walk's rows together with what this set already held.
//
// **Two readers of one data set are not one reader.** A view asks about the rows on
// screen and, a moment later, about a row somebody dragged a thumb to -- and a data
// range that simply REPLACED what was held would answer each by throwing away the
// other's, so the two asks walk over each other for ever and neither is ever there
// when it is looked for. That is not a hypothetical: it is a list that will not
// scroll.
//
// So a run that touches what is held joins it, and only a walk landing somewhere
// else entirely starts again. What that costs is what the old flattening always
// cost -- the rows between, held -- and it is paid only where a reader really is
// reading both. A jump to the far end of a hundred thousand still lands on its own.
//
// Called with the lock held.
func (v *treeDataSet) join(base int, rows []treeRow, whole bool) {
	end, was, wasEnd := base+len(rows), v.base, v.base+len(v.rows)
	switch {
	case len(v.rows) == 0 || len(rows) == 0:
		// Nothing to join to, or nothing to join: the walk stands as it is.
	case base >= was && base <= wasEnd:
		// It begins inside what is held, or just past the end of it, so what is
		// held above it stands and this run carries on from there.
		if end < wasEnd {
			// Held rows reach further than this run, so the end is still theirs.
			whole = v.whole
			rows = append(rows, v.rows[end-was:]...)
		}
		rows = append(append([]treeRow{}, v.rows[:base-was]...), rows...)
		base = was
	case end >= was && end <= wasEnd:
		// It ends inside what is held, so this run leads into it.
		whole = v.whole
		rows = append(rows, v.rows[end-was:]...)
	case base < was && end > wasEnd:
		// It covers what was held whole, and is the better answer for every row of
		// it: nothing of the old run is worth keeping.
	}
	v.base, v.rows, v.whole = base, rows, whole
}

// Read produces one scope of the flattened sequence.
//
// It is the same walk a source holding its records does, because that is what
// this is: the rows are in a slice, in order, so `after` is a map lookup, `from`
// is honoured exactly, and the count and the first position are both facts.
func (v *treeDataSet) Read(s *Scope, out Sink) error {
	// **The scope is what says how far to flatten.** A reader asking for an Extent
	// needs the pre-order up to the end of it and no further, so that is what the
	// walk is asked for -- and every level inside it is asked for no more than
	// that. A reader asking for the whole sequence gets the whole walk, which is
	// what one over records in hand has always had.
	if err := v.reach(s); err != nil {
		out.Done(Complete{Error: err.Error()})
		return nil
	}

	v.mu.Lock()
	rows, at, buildErr, whole, base := v.rows, v.at, v.err, v.whole, v.base
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

	// Positions here are the SEQUENCE's, and the rows held are an Extent of it: `i`
	// walks positions and `i-base` is where to find one. An Extent that begins at
	// nought, which is every walk that was not asked to start elsewhere, makes the
	// two the same number and this reads as it always did.
	step, i := 1, base
	if s.Reversed {
		step, i = -1, base+len(rows)-1
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
		if i < base {
			i = base
		}
		if i >= base+len(rows) {
			i = base + len(rows) - 1
		}
	}

	stop := -1
	if s.Until != nil {
		if j, ok := at[Key(s.Until)]; ok {
			stop = j
		}
	}

	out.Ordered()
	// **How long the sequence is, and whether that is a count or a floor.** The
	// walk stops where its budget ran out, so the rows it holds are all there are
	// only where it ran out of TREE first -- and a reader told forty exactly, out of
	// a hundred thousand, draws a true thumb it has not earned and cannot scroll
	// past the fortieth row.
	//
	// It is RecordCount's own answer and not a second arithmetic, because the two
	// are one claim about one walk and a reader that asked cannot be told something
	// different from a reader that asks separately.
	done := Complete{Total: v.RecordCount()}
	last := s.After
	sent := 0
	for ; i >= base && i < base+len(rows); i += step {
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
		// When the Extent is read lazily instead, this is the line that has to
		// learn to say Subset.
		if err := out.Record(rows[i-base].id, rows[i-base].fields); err != nil {
			return err
		}
		last = rows[i-base].id
		sent++
	}

	switch {
	case done.Stop != "":
		done.Watermark = last
	case whole:
		// The walk saw the end of the tree, so the end of what it holds is the end
		// of the sequence: there is nothing past this.
		done.Stop = StopExhausted
	case last != nil:
		// **The WINDOW ran out, and the sequence did not.** Saying exhausted here
		// is the one thing that must not be said -- a reader told there is nothing
		// past the rows it just got stops asking, which is a list that ends
		// wherever the last walk happened to stop. What is true is that this run
		// reaches the watermark and there is more beyond it.
		done.Stop = StopFilled
		done.Watermark = last
	default:
		done.Stop = StopExhausted
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

	// skip is how many rows of the flattening are still to be passed over before
	// the first one this walk keeps.
	//
	// **An Extent of a tree begins somewhere.** A reader a hundred thousand rows
	// down does not want the hundred thousand above it, and the walk that produced
	// them held every one. So the rows before the Extent are counted rather than
	// kept -- and where a level can prove each of its rows stands for exactly one
	// row of the flattening, they are not even read: the skip becomes that level's
	// `Scope.From` and the source is asked to begin there. See Marks.Flat.
	skip int

	// counts is one census per node type, taken when a twisty first needs one
	// and kept for the rest of this build. A nil value means it was tried and
	// could not be had.
	counts map[*NodeType]*Census

	// topCount is how many rows the top level holds, taken off the set the walk
	// opened for it. **Free**, that set being open either way, and it is half of
	// what `reckon` needs to say how long the flattening is without walking it.
	topCount RecordCount
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
func (d *descent) level(kind string, src Source, descriptor *DataSetDescriptor, st Standing,
	above Node, depth int, chain []string, seen []standingAt) error {

	nt := d.tree.opt.Types.Get(kind)

	descriptor = d.tree.sortFor(kind, d.withShallow(descriptor))
	set, err := src.Open(descriptor)
	if err != nil {
		return err
	}
	// **Read, then WAIT, and only then close.** A source holding its records has
	// filled the sink and called Done on the way out, so the wait returns at once
	// and this is the same three lines it always was. One across a connection has
	// only sent a question -- and closing before the answer came hung up on it,
	// which is a question asked and deliberately not listened for.
	if depth == 0 {
		// The top level, counted while its set is open. See descent.topCount.
		d.topCount = CountOf(set)
	}
	// **Where every row of this level stands for exactly one row of the
	// flattening, the level is entered at the position rather than walked to it.**
	// One open node anywhere beneath breaks the arithmetic -- its children stand
	// between its siblings -- and Marks.Flat is that question asked of the marks
	// rather than of the tree.
	at := 0
	if d.skip > 0 && d.tree.jumps() && d.tree.mark.Flat(chain...) {
		at = d.skip
	}
	got, err := d.read(set, at)
	if err != nil {
		set.Close()
		return err
	}
	// **What the source passed over on this walk's behalf.** `From` is best effort:
	// a source that would not jump answers from the beginning and says so, and then
	// the rows it did not send are rows this walk still has to pass over itself --
	// so it is asked again, for the stretch it will actually answer.
	//
	// Once. Which sources will jump is a fact about the source rather than about
	// this walk, so it is remembered: the one that will not pays this retry the
	// first time a reader jumps and never again.
	if at > 0 {
		began := got.began()
		if began > at {
			set.Close()
			return fmt.Errorf(
				"tree: a level asked to begin at row %d answered from row %d,"+
					" and its rows cannot be placed", at, began)
		}
		if began < at {
			d.tree.sawNoJump()
			if got, err = d.read(set, 0); err != nil {
				set.Close()
				return err
			}
			began = got.began()
		}
		d.skip -= began
	}
	set.Close()
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
		d.emit(node, kind, depth, d.tree.mark.Mark(mine...), children, mine)

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

// read takes one level, entered at a position or at the beginning.
//
// **Read, then WAIT.** A source holding its records has filled the sink and called
// Done on the way out, so the wait returns at once; one across a connection has only
// sent a question, and the answer comes later on whatever thread brings it.
func (d *descent) read(set DataSet, at int) (*levelRows, error) {
	got := newLevelRows()
	if err := set.Read(&Scope{From: at, Count: d.ask(at)}, got); err != nil {
		return nil, err
	}
	got.wait()
	return got, nil
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
func (d *descent) ask(skipped int) int {
	if d.want <= 0 || d.left <= 0 {
		return everyRow
	}
	// What is still to be passed over counts too, for a level that cannot be entered
	// at a position: its rows have to arrive to be counted. What the SOURCE is
	// passing over does not -- that is the whole saving, and asking for it anyway
	// would hand back the rows the position was meant to skip.
	need := d.left + d.skip - skipped
	if need >= everyRow-1 || need < 0 {
		return everyRow
	}
	return need + 1
}

// enough reports whether this walk has what it was asked for, so the descent can
// stop rather than walk a tree nobody is reading the rest of.
func (d *descent) enough() bool { return d.want > 0 && d.left <= 0 }

// everyRow is the count a level is read with where nobody set a budget. The
// level is read entire; the number is a ceiling against a source that would
// otherwise answer forever, not an Extent.
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

// withShallow puts the sequence's own filter on a level's descriptor.
//
// A shallow filter is asked at every level independently, so a parent that does
// not match is gone and its children with it. Deep filtering -- keeping a parent
// where a DESCENDANT matches -- is a claim about everything beneath and cannot
// be made without looking, so it is eager by nature and is not here.
func (d *descent) withShallow(descriptor *DataSetDescriptor) *DataSetDescriptor {
	return d.set.withShallow(descriptor)
}

// withShallow is the same, asked of the sequence rather than of a walk in progress --
// which is what `reckon` needs, there being no walk when it asks.
func (v *treeDataSet) withShallow(descriptor *DataSetDescriptor) *DataSetDescriptor {
	if v.shallow == nil {
		if descriptor == nil {
			return &DataSetDescriptor{}
		}
		return descriptor
	}
	out := DataSetDescriptor{}
	if descriptor != nil {
		out = *descriptor
	}
	if out.Filter == nil {
		out.Filter = v.shallow
	} else {
		out.Filter = &Filter{Op: OpAnd, Children: []*Filter{out.Filter, v.shallow}}
	}
	return &out
}

// reach is how many children this node has: **a field, or a count, or nothing
// said.**
//
// The field first, where the row can say, which is free. Then a count of the
// child descriptor, which answers without reading a record -- Open, count, Close --
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
func (d *descent) emit(of Node, kind string, depth int, state Mark, children RecordCount,
	chain []string) {

	f := d.tree.opt.Fields
	out := make(Record, 0, len(of.Fields)+6)
	for _, m := range of.Fields {
		switch m.Name {
		case f.Depth, f.Path, f.Expandable, f.State, f.Kind, f.Chain:
		default:
			out = append(out, m)
		}
	}
	segs := make(Record, len(chain))
	for i, seg := range chain {
		segs[i] = At(seg)
	}
	out = append(out,
		Named(f.Depth, depth),
		Named(f.Path, of.Path),
		Named(f.State, NewSymbol(state.String())),
		Named(f.Kind, NewSymbol(kind)),
		Named(f.Chain, NewList(segs)))
	if children.Exact || children.N > 0 {
		out = append(out, Named(f.Expandable, int(children.N)))
	} else {
		// Nothing known, which is undefined rather than nought: a twisty is
		// drawn and the answer found on opening.
		out = append(out, Named(f.Expandable, nil))
	}

	// **Before the Extent, so counted and not kept.** The row is a row of the
	// flattening either way -- that is what makes it a position -- but nobody is
	// going to look at it, so nothing is held for it.
	if d.skip > 0 {
		d.skip--
		return
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

	// first is where the answer BEGAN, which matters only to a level that was
	// asked to begin somewhere. `Scope.From` is best effort -- a source honours it
	// as well as it can and says where it actually started -- so this is what tells
	// a descent whether the rows it was handed are the ones it skipped to or the
	// ones from the beginning. See descent.skip.
	first RecordCount

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
	l.done, l.err, l.first = true, c.Error, c.First
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

// began is the position this level's answer started at, and nought where it said
// nothing: a source that did not say is a source that started at the beginning,
// which is the one thing an unanswered `from` can safely be read as.
func (l *levelRows) began() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.first.Exact {
		return 0
	}
	return l.first.N
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
