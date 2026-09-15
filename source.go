package serval

// A body of records, and the way one is read.
//
// Records are sometimes here and sometimes somewhere that has to be asked. Both
// want the same answers to the same questions -- this filter, this sort, this
// stretch -- so they are one interface, and what is behind it can be a file
// read into memory, a table, or something across a connection that will answer
// in its own time.
//
// Nothing here waits. A scope is asked for and the records arrive at a sink as
// they are produced, which for a source holding its own records is at once and
// for one that has to ask is whenever the answer comes. That is the same shape
// either way, so a source built on other sources does not care which kind it
// got.

// A Source is a body of records that sequences can be read out of.
type Source interface {
	// Open states a sequence: one filter, one sort. It is refused where the
	// sequence cannot be produced exactly -- an op that is not implemented, a
	// collation that is not carried -- because an ordering that is quietly a
	// little different corrupts every answer after it and looks like data.
	Open(spec *Spec) (DataSet, error)
}

// A DataSet is one stated sequence -- this source, this sort, this filter --
// prepared once and drawn from until it is let go. It is what a query names,
// seen from the end that holds the records.
//
// Those three name it, and nothing else does: the fields a query asks for
// change what a scope carries rather than which records are in the sequence,
// and the direction belongs to the scope. So two queries naming the same three
// are reading one data set, and whatever was worked out for either of them --
// the ordering, where each record stands -- holds for both.
//
// It does not change. A different sort or a different filter is a different
// data set, opened alongside this one and taking its place -- which is also
// what keeps the source in use while the reader moves from one to the other.
type DataSet interface {
	// Read asks for one scope of the sequence and says where to put it.
	//
	// It does not wait for the answer. Records reach the sink as they are
	// produced -- immediately, for a source whose records are here; as they
	// arrive, for one whose records are somewhere else -- and the sink is told
	// what ended it when it ends. The error is for a request that could not be
	// started at all, never for one that has not finished.
	//
	// A scope names its ends by identity, and an identity means something only
	// to the source that issued it. So a scope handed here was issued here: a
	// source made of several others translates rather than relays.
	Read(s *Scope, out Sink) error

	// Close lets the data set go, and with it whatever it was holding.
	Close()
}

// A Sink takes an answer as it is produced: the records one at a time, and
// then what ended them.
//
// One at a time because a scope of a million records need not be assembled
// anywhere before the first of them moves, and because a source whose records
// are across a connection has them in that shape already.
type Sink interface {
	// Ordered says the records about to arrive are in the sequence's order.
	// It comes before the first of them or not at all, which is the only place
	// it is worth anything: a sink that learns it afterwards can no longer act
	// on the records it has already been given. Not being told means not
	// ordered, which is always safe.
	Ordered()

	// Record takes one record entire: its identity, and every field it has.
	//
	// Whole is worth saying because it outlives the scope that asked for it.
	// A record that arrived entire answers any question about that record, so
	// whoever holds it can answer the next query out of it instead of asking
	// again.
	//
	// The identity is beside the fields, not among them. A record may well
	// carry a field called `key`, and that field is data: it sorts, it
	// filters, it fills a column. What names the record is this.
	Record(id *Value, fields Record) error

	// Subset takes some of a record: its identity, and the fields that were
	// asked for, which are fewer than the record has.
	//
	// It answers the question that asked for it and no other. A later question
	// naming a field this one left out is not answered by what came back here,
	// however many of the same records it names.
	Subset(id *Value, fields Record) error

	Done(c Complete)
}

// Order is not in a Complete. It is said before the records, on the sink,
// because a sink told afterwards cannot act on what it already has.
