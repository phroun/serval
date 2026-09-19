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
