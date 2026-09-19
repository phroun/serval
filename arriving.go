package serval

// A source whose answer comes LATER.
//
// Every source here answers `Read` by handing records to a sink, and most of
// them have the records already: the sink is full by the time Read returns, and
// whoever asked can use what it got. A source across a connection cannot work
// that way. It writes a statement and returns; the records arrive when the far
// end sends them, on whatever thread is reading.
//
// So a reader that only ever read once sees nothing. **It has to be told**, which
// is the same rule as everywhere else in this package -- nothing polls, nothing
// expires, and no generation is compared. The source says an answer landed and
// the reader reads again.
//
// # It is asked of the SOURCE, and a source that needs no telling says so
//
// `TellOnArrival` reports false for a source that answers at once, which is most
// of them, and a reader then has nothing to arrange. That is what keeps this from
// being a cost every reader pays for a case most of them never meet.
//
// # Which thread, and why not this one's business
//
// **A source fires the notice on whatever thread its records arrived on, and
// nothing here knows what that means.** This package knows nothing about threads
// anywhere else and this is not the place to start: a source over a socket fires
// from the socket's reader, a source over a channel from whatever fed it, and
// neither can know what its reader needs.
//
// So the READER arranges the hop where it needs one. A drawing surface posts the
// re-read to whatever thread owns its state; a test calls it where it stands. One
// place knows about threads instead of every source author having to be told a
// rule they cannot see.
//
// **And a view is the outermost inch of what is mostly a data question.** Sources,
// sequences, scopes, caches and the telling between them are a data service; that
// one of its readers happens to be drawn on a thread of its own is a fact about
// that reader. Pushing the thread down into the sources would make every one of
// them carry a UI's constraint to suit its last consumer.
//
// # Several readers, one source
//
// Two readers may share one source -- they are reading one body of records, and
// asking twice would pay twice for one answer. So a notice ADDS rather than
// replaces, and every reader that asked is told.

// An Arriving source can say when records have landed.
//
// Optional, as every one of these is: a source that does not implement it answers
// at once, and `TellOnArrival` is what a reader asks rather than asserting.
type Arriving interface {
	// WhenArrived adds something to be told once an answer has landed. It ADDS,
	// because several readers may share one source.
	//
	// It is called on whatever thread the records arrived on. A reader that
	// cannot be touched from there is the one that knows so, and arranges
	// accordingly.
	WhenArrived(tell func())
}

// TellOnArrival asks a source to say when records land, and reports whether it
// can.
//
// False is not a failure: it is a source that answers `Read` before it returns,
// which needs no telling and is the ordinary case. A reader that gets false has
// nothing to arrange and reads as it always did.
func TellOnArrival(src Source, tell func()) bool {
	if tell == nil {
		return false
	}
	a, ok := src.(Arriving)
	if !ok {
		return false
	}
	a.WhenArrived(tell)
	return true
}

// Arrives reports whether a source may answer AFTER its read returns -- itself, or
// anything it wraps.
//
// **It is not the same question as "does it implement Arriving", and that is the
// whole reason it exists.** A wrapper hands the notice on, so it implements the
// interface whatever its child does: a cache over a list of records in hand is
// `Arriving` and will never fire, because there is nothing under it to fire.
//
// Asserting the interface to decide whether to WAIT is therefore wrong, and wrong in
// the expensive direction. A tree that concluded its levels might answer late walked
// on a goroutine of its own and returned no rows, expecting a notice to bring the
// reader back -- and no notice ever came, the records having been there all along.
// The tree read empty forever. That is what this is for; see `answersLater`.
//
// A wrapper is asked about its child. Anything else is asked whether it can arrive
// at all, which for a source that is not one of these wrappers is the same question.
func Arrives(src Source) bool {
	switch s := src.(type) {
	case *CachedSource:
		return Arrives(s.child)
	case *AmendedSource:
		return Arrives(s.child)
	case *ComposedSource:
		for _, in := range s.includes {
			if Arrives(in.Source) {
				return true
			}
		}
		return false
	case *TreeSource:
		// A tree's own answer waits exactly when one of its levels does, which it
		// settled when it was stated.
		return s.later
	}
	_, ok := src.(Arriving)
	return ok
}

// --- and a wrapper passes it on ------------------------------------------

// A source that wraps another must hand the notice on, or wrapping would COST
// something.
//
// **That is the point, and it was not true before.** A source across a connection
// wrapped in a cache, a composition, or an amendment stopped being able to say its
// answer had landed -- so a reader over the wrapper waited forever for a notice
// the wrapper had swallowed. Wrapping is meant to add a capability, never to take
// one away, and an application's source is a source like any other: everything
// that composes with sources composes with it.
//
// Each of these forwards to whatever it wraps that can arrive, and says nothing
// itself. A wrapper over children that all answer at once never fires, which is
// the right answer rather than a missing one -- there is nothing to tell.

// WhenArrived passes the notice on to whatever this cache wraps.
//
// A cache does not fire one of its own: what it holds it holds, and an answer
// landing at the far end is the CHILD's news. Telling the cache its runs are stale
// is a different saying with a different name -- see invalidate.go.
func (c *CachedSource) WhenArrived(tell func()) { TellOnArrival(c.child, tell) }

// WhenArrived passes the notice on to the source this amends. The amendments are
// here and they do not arrive; the records do.
func (a *AmendedSource) WhenArrived(tell func()) { TellOnArrival(a.child, tell) }

// WhenArrived passes the notice on to every include that can arrive.
//
// **Any one of them is enough**, because a composition's answer is drawn from all
// of them: a record landing in one changes what the whole says, and a reader that
// re-reads on it reads the composition rather than the child. So there is no need
// to know WHICH arrived, and nothing here keeps track.
func (c *ComposedSource) WhenArrived(tell func()) {
	for _, inc := range c.includes {
		TellOnArrival(inc.Source, tell)
	}
}
