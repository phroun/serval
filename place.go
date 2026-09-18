package serval

// Where a record stood, remembered by whoever said so.
//
// A scope names its ends by identity, and an identity is not a position: it
// says which record, not where. A source that holds the records resolves one
// by looking it up. A source built on top of others cannot -- the records are
// somewhere else, and asking their owner "where is this?" is not a question the
// interface has -- so it remembers instead, as it hands each record on.
//
// The notes belong to the prepared sequence -- this source, this sort, this
// filter -- and not to any one reading of it. Those three are what decide which
// records are in a sequence and where they stand, so two readers naming the
// same three are reading the same sequence and a record stands in the same
// place for both. It is the same thing `dataSetKey` already dedupes an ordering
// by, and it is kept beside them for the same reason.
//
// That is not a cache of the data. It is a note of where this source put a
// record when it last said something about it, which is exactly what it needs
// to answer "and what comes after that one?" -- and it is bounded, because a
// reader that walks a million records should not leave a million notes behind.

import (
	"fmt"
	"sync"
)

// placesKept is how many positions one source remembers. A reader asks from
// where it got to, so the notes that matter are the recent ones; far more than
// a screenful is already generosity, and this is generous.
const placesKept = 1024

// A places remembers where records stood, oldest forgotten first.
//
// Guarded, because a note is written while a scope is being answered and
// FORGOTTEN whenever a source is told one of its records may have moved -- and
// those are two different goroutines with nothing between them.
type places struct {
	mu   sync.Mutex
	at   map[string][]*Value
	seen []string
}

func newPlaces() *places {
	return &places{at: map[string][]*Value{}}
}

// put notes where a record stood. Noting the same record twice moves it to the
// front of the queue rather than storing it again, because a record the reader
// keeps passing is one it is likely to ask from.
func (p *places) put(id *Value, tuple []*Value) {
	if id == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	k := Key(id)
	if _, had := p.at[k]; !had {
		p.seen = append(p.seen, k)
	}
	p.at[k] = tuple
	for len(p.seen) > placesKept {
		old := p.seen[0]
		p.seen = p.seen[1:]
		delete(p.at, old)
	}
}

// get is where a record stood, and false for one this source has not placed --
// which it has not if it never sent it, or sent it so long ago that the note
// has been forgotten.
func (p *places) get(id *Value) ([]*Value, bool) {
	if id == nil {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.at[Key(id)]
	return t, ok
}

// forget drops what was noted about one record, by the key `put` filed it
// under.
//
// Losing a note is not losing anything true. It says where this source WAS when
// it last spoke about that record, so a scope resuming from one it has not got
// is refused rather than answered wrongly -- which is what makes forgetting the
// safe half of this, and worth doing generously.
func (p *places) forget(k string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, had := p.at[k]; !had {
		return
	}
	delete(p.at, k)
	for i, seen := range p.seen {
		if seen == k {
			p.seen = append(p.seen[:i], p.seen[i+1:]...)
			break
		}
	}
}

// naming is the keys of every note whose tuple answers the test -- which is how
// a source finds the notes that mention a record, rather than the note OF one.
func (p *places) naming(hit func(tuple []*Value) bool) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for k, t := range p.at {
		if hit(t) {
			out = append(out, k)
		}
	}
	return out
}

// A placebook is one source's notes, kept per prepared sequence.
//
// A query is asked once and answered once, and a reader may let one data set
// go and open another over the same sequence between two scopes of it. What
// resumes a scope is therefore not the reading -- it is the sequence, which the
// source and the sort and the filter name between them, and which is what this
// is keyed by.
//
// Bounded the way the orderings are, and for the same reason: each entry is a
// sequence somebody is scrolling, the recent ones are the ones being scrolled,
// and an unbounded pile of them is not worth keeping.
type placebook struct {
	mu     sync.Mutex
	books  map[string]*book
	recent []string
}

// A book is one sequence's notes, and what each position in one means.
//
// The slots matter because a note may be a vector -- a composed source writes
// down where each of its INCLUDES stood -- and each position in it holds that
// include's OWN key. Without knowing which include a position belongs to, an
// inner key cannot be turned back into the outer one it appears under, and two
// includes keyed alike would be told apart by nothing.
type book struct {
	places []*places
	slots  []string
}

func newPlacebook() *placebook {
	return &placebook{books: map[string]*book{}}
}

// of is the notes for one sequence, made if this is the first scope of it.
// Each sequence gets `n` of them, because a source may have more than one thing
// to remember about the same record.
func (b *placebook) of(spec *Spec, n int, slots ...string) []*places {
	key := dataSetKey(spec)
	b.mu.Lock()
	defer b.mu.Unlock()
	if held := b.books[key]; held != nil {
		return held.places
	}
	p := make([]*places, n)
	for i := range p {
		p[i] = newPlaces()
	}
	b.books[key] = &book{places: p, slots: slots}
	b.recent = append(b.recent, key)
	for len(b.recent) > orderingsKept {
		delete(b.books, b.recent[0])
		b.recent = b.recent[1:]
	}
	return p
}

// all is every sequence's notes, taken as a copy: what is done to them may
// forget entries, and forgetting one while walking the list is how a walk goes
// wrong.
func (b *placebook) all() []*book {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*book, 0, len(b.books))
	for _, held := range b.books {
		out = append(out, held)
	}
	return out
}

// startedAt is where an answer began, for a source that does not hold its own
// records and so does not honour From: it begins at the start of the sequence
// whatever position was asked for, or carries on from a record it noted.
//
// The START is position zero whoever is answering, and that much it can say
// exactly. **Saying it is the point.** A reader that asked to begin at six
// hundred and is told it began at nought knows it did not get there -- where the
// same reader told NOTHING by a source that had quietly started at the top would
// paint the first rows of the sequence as though they were the six hundredth. An
// unhonoured From has to be visible in the answer, and this is where it is.
//
// That is also what makes the reader's loop self-limiting. Ask a different
// place, get the same answer back, and the source has said it cannot seek. The
// reader stops, and nobody negotiated a capability to find out.
//
// A resume from a note is somewhere this source cannot number, so that is
// Unknown until the note carries its position.
func startedAt(s *Scope, sent int) RecordCount {
	if sent == 0 {
		return Unknown() // no first record, so no position to report
	}
	if s == nil || s.After == nil {
		return Exactly(0)
	}
	return Unknown()
}

// bothEnds refuses a scope that says where to start twice over. A record is not
// a position: one names a place in a sequence and the other names a thing, and
// a scope carrying both has a bug that only ever shows here.
func bothEnds(s *Scope) error {
	if s != nil && s.After != nil && s.From != 0 {
		return fmt.Errorf("a scope says where to start with after or with from," +
			" not both: a record is not a position")
	}
	return nil
}
