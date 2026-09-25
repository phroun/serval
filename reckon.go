package serval

// How long a flattening is, without walking it.
//
// **A tree's length is its walk, and that is the thing this gets round.** A
// flattening that stopped where its budget ran out knows there may be more and can
// only floor the figure -- so a reader holding a window of a hundred thousand rows
// was told "at least forty", drew a thumb against forty, and could not be dragged
// past the fortieth row. Learning the real length meant reading down to the end,
// which is the one thing a window exists not to do.
//
// It never needed the walk. Two facts already in hand say it:
//
//	the TOP LEVEL counts itself       one question, and the walk opens that set
//	                                  anyway, so it is already paid for
//	every OPEN node adds its children  which is the census -- one question per node
//	                                  TYPE and not per row -- and the marks are
//	                                  bounded by what somebody has touched, so this
//	                                  is a sum over a handful of them
//
// So with nothing open the length is one count; with thirty folders open it is one
// count and thirty lookups; and neither of those is a row read.
//
// # What it cannot say, and says so
//
// **An expand-all.** Under OpenAll the children inherit it, so an open node's
// contribution is its whole SUBTREE rather than its children -- and a subtree's size
// is not free: a census answers one level, and summing the levels beneath needs the
// walk that has not happened. This is the case that stays a floor, and it is the
// honest one: a reader who expanded a million rows asked for something nobody
// counted.
//
// **More than one kind of row.** One census serves every level of one node type,
// which is what makes the sum cheap; a tree that grafts a second source under rows
// of the first would need the KIND of each marked node to know which census to ask,
// and a mark is a chain of segments and does not carry one.
//
// **A row that says for itself how many children it has.** `SaysChildren` lets a
// record answer its own twisty, and the walk believes the record over the census --
// including when it says none, which stops the descent. A sum taken from the census
// would then count rows the walk would not have read.
//
// **A loop.** A node standing beneath itself is drawn and not descended into, so a
// mark on such an occurrence contributes no rows while its census count says
// otherwise. For an adjacency list the loop IS a repeated segment in the chain,
// which is cheap to see and is what this looks for.
//
// In every one of those the answer is a floor. But it is a better floor than the
// walk's own: **every top-level row is in the flattening**, so a sequence is at least
// as long as its top level however little of it anybody has read. An expand-all over
// two hundred folders floors at two hundred rather than at the thirty rows on screen,
// and the thumb stops lurching when somebody expands everything.

// reckon is the length, worked out from counts rather than from the walk.
//
// `held` is how many rows the walk has actually emitted. It is the floor of last
// resort, and it is also a check: a sum that comes out BELOW rows somebody is holding
// has something wrong in it, and evidence beats arithmetic.
func (v *treeDataSet) reckon(held int) RecordCount {
	opt := v.tree.opt
	nt := opt.Types.Default

	v.mu.Lock()
	top, counts := v.topCount, v.counts
	v.mu.Unlock()
	if !top.Exact {
		top = v.askTop()
	}

	// The floor, before anything else: the rows the walk emitted, or the top level
	// where that is more -- every one of its rows being in the flattening.
	floor := AtLeast(held)
	if top.Exact && top.N > held {
		floor = AtLeast(top.N)
	}
	if !reckonable(opt) || !top.Exact {
		return floor
	}
	total := top.N

	// The root's own state is asked apart from the rest, because `Showing` does not
	// yield it: an OpenAll root is an expand-all with nothing touched inside it, and
	// it would otherwise look like nothing being open at all.
	marks := v.tree.Marks()
	if marks.Mark() == OpenAll {
		return floor
	}

	var open [][]string
	fine := true
	marks.Showing(func(chain []string, state Mark) {
		if state == OpenAll || loops(chain) {
			fine = false
			return
		}
		open = append(open, chain)
	})
	if !fine {
		return floor
	}
	if len(open) == 0 {
		// Nothing open, so the top level is the whole of it -- and this is the
		// commonest state of every tree and the only state of a flat source read as
		// one generation, which is where the hundred thousand rows are.
		return exactlyOr(total, floor)
	}

	by, complete := groupCounts(counts[nt])
	if by == nil {
		return floor
	}
	paths := nt.Standing.Paths()
	for _, chain := range open {
		n, found := by[censusKey(chain[len(chain)-1], paths)]
		switch {
		case found && n.Exact:
			total += n.N
		case !found && complete:
			// A value the census has not got, from a census that saw the whole
			// sequence: nought children, which is a statement and not a silence.
		default:
			return floor
		}
	}
	return exactlyOr(total, floor)
}

// askTop asks the top level how many rows it has, where the walk did not find out.
//
// **The walk records it for free and that is usually enough, but not always.** A
// source across a connection cannot count until an answer has told it how many there
// are -- and by the time one has, the window is held and no walk happens to pick the
// figure up. So a sequence that became countable after the last walk would go on
// being floored for as long as nobody expanded anything.
//
// It asks only while the figure is NOT known, and files it when it arrives, so a
// source that counts is asked once and one that never will costs an Open and a Close
// -- no read, no records, and nothing on a wire. `Stale` brings the walk round again
// and refreshes it when the data changes, which is what keeps this from being a
// figure nobody revisits.
//
// The descriptor is the one the WALK used, sort and shallow filter and all. It has to
// be: a cache remembers a count against the sequence it was told about, and asking
// about a differently-stated sequence is asking something the cache has never heard.
func (v *treeDataSet) askTop() RecordCount {
	opt := v.tree.opt
	if opt.Source == nil {
		return Unknown()
	}
	set, err := opt.Source.Open(v.tree.sortFor("", v.withShallow(opt.Descriptor)))
	if err != nil {
		return Unknown()
	}
	got := CountOf(set)
	set.Close()

	// One guard and not two: filing it only where it says more is the same test a
	// second `got` against `had` would be, and the stored figure is the better of the
	// two either way.
	v.mu.Lock()
	if says(got, v.topCount) {
		v.topCount = got
	}
	best := v.topCount
	v.mu.Unlock()
	return best
}

// exactlyOr is the reckoning, unless it contradicts the floor -- which is evidence:
// rows the walk emitted, or a top level somebody counted. A sum that comes out below
// either has something wrong in it.
//
// No mutation kills this, and it is kept: with every guard above it correct the sum
// cannot come out low, so it is the net for one of those guards being wrong rather
// than a step of the arithmetic. It and the exactness check on the top level are the
// same kind of thing, and each covers a way of being wrong that the other does not.
func exactlyOr(total int, floor RecordCount) RecordCount {
	if total < floor.N {
		return floor
	}
	return Exactly(total)
}

// reckonable reports whether this tree's shape is one the sum above describes.
//
// Each of these is a condition the long comment at the top of this file gives a
// reason for. They are read together here so that a tree either is this shape or is
// not, rather than the question being asked in pieces at the point of each sum.
//
// **A criterion that does not PARTITION is not refused here**, though it has no
// census. It only matters where there is something open to look up, and a tree with
// nothing open is counted from its top level alone -- so a subtree criterion, which
// can never be censused, still gets a true length until somebody opens a node.
// Refusing it up front would have thrown that away for a reason that does not apply.
//
// The first two lines are a PAIR, and a mutation sweep kills neither on its own:
// drop the Named check and `Then` refuses the same tree; drop the `Then` check and
// Named refuses it. Both are kept because they refuse for different reasons -- one
// census to choose between, and a kind chosen per row -- and because a named type is
// only reachable through `Then` or `Field`, so each is load-bearing for a tree the
// other lets through.
func reckonable(opt TreeOptions) bool {
	nt := opt.Types.Default
	switch {
	case nt == nil, len(opt.Types.Named) > 0:
		return false // more than one kind, so more than one census
	case len(nt.Then) > 0, opt.Types.Field != "":
		return false // a row's children may be a kind chosen per row
	case opt.SaysChildren != "":
		return false // a record answers its own twisty, and the walk believes it
	}
	return true
}

// censusKey is the census group a mark segment names.
//
// **A segment is what the WALK spells**, and the two criteria that partition spell
// it the two ways a census is keyed. A level with no standing is marked by the
// record's identity, and `ChildrenByKey` groups by that identity -- so the segment
// already IS the census key. A level with a standing is marked by its path, and
// `ChildrenByLocation` groups by that path as TEXT -- so the segment is the value and
// wants encoding.
func censusKey(seg string, paths bool) string {
	if paths {
		return Key(NewText(seg))
	}
	return seg
}

// groupCounts is a census as a lookup from a group's key to its count, and whether
// the census saw the whole sequence.
//
// A map because the sum is over marks and the groups are over ROWS: a flat source of
// a hundred thousand records has a hundred thousand groups, and scanning them once
// per mark would make a cheap answer expensive.
func groupCounts(c *Census) (map[string]RecordCount, bool) {
	if c == nil {
		return nil, false
	}
	by := make(map[string]RecordCount, len(c.Groups))
	for _, g := range c.Groups {
		by[Key(g.Value)] = g.Count
	}
	return by, c.Total.Exact
}

// loops reports whether a chain names a node standing beneath itself.
//
// For an adjacency list a segment is a record's identity, so the same identity twice
// in one chain is the loop said plainly. A tree with a standing cannot loop -- a path
// grows with every level -- and a tree allowing revisits has chains that legitimately
// repeat, which this refuses rather than getting wrong.
func loops(chain []string) bool {
	for i, seg := range chain {
		for _, earlier := range chain[:i] {
			if earlier == seg {
				return true
			}
		}
	}
	return false
}
