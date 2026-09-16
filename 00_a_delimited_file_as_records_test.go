package serval

// CSV and TSV read as records. What a cell MEANS is the point: a file somebody
// authored spells its own values, a file a spreadsheet wrote spells nothing,
// and a declared column settles it either way.

import (
	"strings"
	"testing"
)

// load reads a file and fails the test on anything that stopped it outright.
func load(t *testing.T, text string, opts Delimited) (*ListSource, []Complaint) {
	t.Helper()
	src, why, err := ParseDelimited(text, opts)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return src, why
}

// field is what one record of a source carries under a name.
func field(t *testing.T, src *ListSource, at int, name string) *Value {
	t.Helper()
	if at >= src.Len() {
		t.Fatalf("there is no row %d; there are %d", at, src.Len())
	}
	return src.rows[at].Field(name)
}

// shows renders a value the way a test can compare it, kind and all.
func shows(v *Value) string {
	if v == nil {
		return "undefined"
	}
	return string(rune('a'+int(v.Kind))) + ":" + v.String()
}

const spreadsheet = "name,size,note\r\n" +
	"parser.go,2048,\"a note, with a comma\"\r\n" +
	"lexer.go,310,\"he said \"\"hi\"\"\"\r\n"

// What Excel writes: CRLF, quotes only where they are needed, doubled quotes
// inside one, and a delimiter inside a quoted cell.
func TestASpreadsheetsFileReadsAsItWasMeant(t *testing.T) {
	opts := CSV()
	opts.Header = true
	src, why := load(t, spreadsheet, opts)
	if len(why) != 0 {
		t.Fatalf("it complained: %v", why)
	}
	if src.Len() != 2 {
		t.Fatalf("it read %d rows", src.Len())
	}
	if got := field(t, src, 0, "note"); got.Str != "a note, with a comma" {
		t.Errorf("the comma inside quotes read as %q", got.Str)
	}
	if got := field(t, src, 1, "note"); got.Str != `he said "hi"` {
		t.Errorf("the doubled quotes read as %q", got.Str)
	}
	// Infer is off, so a bare cell is what a spreadsheet meant by it: text.
	if got := field(t, src, 0, "size"); got.Kind != TextValue || got.Str != "2048" {
		t.Errorf("a bare cell read as %s, not the text a spreadsheet wrote", shows(got))
	}
}

// The mark Excel writes in front of a UTF-8 file rides the first header if it
// is left on, and every lookup on that column then misses.
func TestAByteOrderMarkDoesNotRideTheFirstHeader(t *testing.T) {
	opts := CSV()
	opts.Header = true
	src, _ := load(t, bom+"id,name\n7,parser.go\n", opts)
	if got := field(t, src, 0, "id"); got == nil {
		t.Error("the first column could not be found under its own name")
	}
}

// An authored file spells its values, by the rule the wire reads a bare token
// by: a number where it spells one, a symbol otherwise, a string where quoted.
func TestAnAuthoredFileSpellsItsOwnValues(t *testing.T) {
	opts := CSV()
	opts.Header, opts.Infer = true, true
	src, _ := load(t, "n,word,text,date,sci\n42,kebab-case,\"42\",2026-09-16,1e+21\n", opts)

	for _, c := range []struct {
		name string
		kind Kind
		want string
	}{
		{"n", NumberValue, "42"},
		{"word", SymbolValue, "kebab-case"},
		{"text", TextValue, "42"},
		{"date", SymbolValue, "2026-09-16"},
		{"sci", NumberValue, "1e+21"},
	} {
		got := field(t, src, 0, c.name)
		if got == nil || got.Kind != c.kind {
			t.Errorf("%s read as %s, wanted kind %d", c.name, shows(got), c.kind)
		}
	}
}

// A declared column settles what it holds whatever the cell looks like, which
// is what makes quote-minimal safe: quoting in one is transport and no more.
func TestADeclaredColumnSettlesWhatItHolds(t *testing.T) {
	opts := CSV()
	opts.Header, opts.Infer = true, true
	opts.Columns = map[string]ColumnKind{
		"sku": AsString, "tag": AsSymbol, "qty": AsNumber,
	}
	src, why := load(t, "sku,tag,qty\n4,\"quoted\",17\n", opts)
	if len(why) != 0 {
		t.Fatalf("it complained: %v", why)
	}
	if got := field(t, src, 0, "sku"); got.Kind != TextValue || got.Str != "4" {
		t.Errorf("a 4 in a string column read as %s", shows(got))
	}
	if got := field(t, src, 0, "tag"); got.Kind != SymbolValue || got.Str != "quoted" {
		t.Errorf("a quoted cell in a symbol column read as %s", shows(got))
	}
	if got := field(t, src, 0, "qty"); got.Kind != NumberValue || got.Int != 17 {
		t.Errorf("a number column read as %s", shows(got))
	}
}

// One bad cell costs that cell. The row stands, the load finishes, and the
// report says what was wrong and where.
func TestABadCellIsReportedAndNotFatal(t *testing.T) {
	opts := CSV()
	opts.Header = true
	opts.Columns = map[string]ColumnKind{"qty": AsNumber}
	src, why := load(t, "name,qty\nparser.go,n/a\nlexer.go,3\n", opts)

	if src.Len() != 2 {
		t.Fatalf("a bad cell took a row with it: %d rows", src.Len())
	}
	if field(t, src, 0, "qty") != nil {
		t.Error("the bad cell was given a value")
	}
	if field(t, src, 0, "name") == nil {
		t.Error("the rest of the row went with it")
	}
	if len(why) != 1 || why[0].Row != 0 || why[0].Column != "qty" || why[0].Cell != "n/a" {
		t.Fatalf("the report was %v", why)
	}
	if !strings.Contains(why[0].String(), "row 0, qty") {
		t.Errorf("it reads as %q", why[0].String())
	}
}

// Bare empty is undefined, which is an answer. An empty string is an empty
// QUOTED cell, which is a value.
func TestAnEmptyCellIsUndefinedAndAQuotedOneIsEmpty(t *testing.T) {
	opts := CSV()
	opts.Header = true
	src, _ := load(t, "a,b\n,\"\"\n", opts)
	if got := field(t, src, 0, "a"); got != nil {
		t.Errorf("a bare empty cell read as %s", shows(got))
	}
	if got := field(t, src, 0, "b"); got == nil || got.Kind != TextValue || got.Str != "" {
		t.Errorf("a quoted empty cell read as %s", shows(got))
	}
}

// Without headers a column is named by where it stands, which is how a
// position is named everywhere else a field is.
func TestWithoutHeadersAColumnIsNamedByItsPosition(t *testing.T) {
	src, _ := load(t, "parser.go,2048\nlexer.go,310\n", CSV())
	if src.Len() != 2 {
		t.Fatalf("it read %d rows", src.Len())
	}
	if got := field(t, src, 0, ".0"); got.Str != "parser.go" {
		t.Errorf(".0 read as %s", shows(got))
	}
	if got := field(t, src, 1, ".1"); got.Str != "310" {
		t.Errorf(".1 read as %s", shows(got))
	}
}

// The identity is the row's index unless a column is named for it.
func TestTheIdentityIsTheIndexOrAColumn(t *testing.T) {
	text := "sku,name\nA-1,parser.go\nA-2,lexer.go\n"

	opts := CSV()
	opts.Header = true
	src, _ := load(t, text, opts)
	if got := src.rows[1].Key(); got.Int != 1 {
		t.Errorf("by index, row 1 is %s", shows(got))
	}

	opts.Identity = IdentityColumn("sku")
	src, _ = load(t, text, opts)
	if got := src.rows[1].Key(); got.Str != "A-2" {
		t.Errorf("by name, row 1 is %s", shows(got))
	}

	opts.Identity = IdentityAt(1)
	src, _ = load(t, text, opts)
	if got := src.rows[0].Key(); got.Str != "parser.go" {
		t.Errorf("by position, row 0 is %s", shows(got))
	}
}

// One identity, one row. The second is dropped and said so, rather than
// quietly displacing the first in the ordering's index.
func TestADuplicateIdentityIsAComplaintAndTheRowGoes(t *testing.T) {
	opts := CSV()
	opts.Header, opts.Identity = true, IdentityColumn("sku")
	src, why := load(t, "sku,name\nA-1,first\nA-1,second\nA-2,third\n", opts)

	if src.Len() != 2 {
		t.Fatalf("it kept %d rows", src.Len())
	}
	if got := field(t, src, 1, "name"); got.Str != "third" {
		t.Errorf("the row that survived is %s", shows(got))
	}
	if len(why) != 1 || why[0].Row != 1 {
		t.Fatalf("the report was %v", why)
	}
}

// Uniqueness is by text, because an address carries a segment and not a type:
// 7 and "7" name one row once either is asked for.
func TestANumberAndItsTextAreOneIdentity(t *testing.T) {
	opts := CSV()
	opts.Header, opts.Infer = true, true
	opts.Identity = IdentityColumn("id")
	_, why := load(t, "id,name\n7,bare\n\"7\",quoted\n", opts)
	if len(why) != 1 {
		t.Fatalf("7 and \"7\" were taken for two rows: %v", why)
	}
}

// A named column becomes the record's value rather than a member of its own.
func TestAValueColumnBecomesTheRecordsValue(t *testing.T) {
	opts := CSV()
	opts.Header, opts.Value = true, "text"
	src, _ := load(t, "id,text,lang\ngreeting,Hello,en\n", opts)

	if got := field(t, src, 0, ValueField); got == nil || got.Str != "Hello" {
		t.Errorf("value read as %s", shows(got))
	}
	if got := field(t, src, 0, "text"); got != nil {
		t.Errorf("the value column is also a member: %s", shows(got))
	}
	if got := field(t, src, 0, "lang"); got.Str != "en" {
		t.Errorf("the other columns went with it: %s", shows(got))
	}
}

// Escapes are ours and not a spreadsheet's, which is why they are off: a
// Windows path is a Windows path.
func TestEscapesAreOffUnlessTheFileIsOurs(t *testing.T) {
	text := "path,note\n\"C:\\Users\\bob\",\"one\\ntwo\"\n"

	opts := CSV()
	opts.Header = true
	src, _ := load(t, text, opts)
	if got := field(t, src, 0, "path"); got.Str != `C:\Users\bob` {
		t.Errorf("a path read as %q", got.Str)
	}

	opts.Escapes = true
	src, why := load(t, text, opts)
	if len(why) == 0 {
		t.Error(`\U is not an escape, and nothing said so`)
	}
	if got := field(t, src, 0, "note"); got == nil || got.Str != "one\ntwo" {
		t.Errorf("with escapes on, the note read as %s", shows(got))
	}
}

// A byte column reads \xNN whatever Escapes says, because that is the only way
// a cell can spell a byte.
func TestAByteColumnAlwaysReadsItsEscapes(t *testing.T) {
	opts := CSV()
	opts.Header = true
	opts.Columns = map[string]ColumnKind{"blob": AsBytes}
	src, why := load(t, "name,blob\nicon,\"\\x89PNG\\x0d\"\n", opts)
	if len(why) != 0 {
		t.Fatalf("it complained: %v", why)
	}
	got := field(t, src, 0, "blob")
	if got == nil || got.Kind != BytesValue {
		t.Fatalf("it read as %s", shows(got))
	}
	if want := "\x89PNG\x0d"; got.Str != want {
		t.Errorf("the bytes read as %q", got.Str)
	}
}

// A comment line is not a row. It counts only where a row could have begun, so
// a prefix inside a quoted cell is content.
func TestACommentLineIsNotARow(t *testing.T) {
	opts := TSV()
	opts.Header = true
	opts.Comments = []string{"#", "--"}
	src, why := load(t, strings.Join([]string{
		"# what this file is",
		"name\tnote",
		"-- and who wrote it",
		"parser.go\t\"# not a comment\"",
		"lexer.go\tplain",
	}, "\n")+"\n", opts)

	if len(why) != 0 {
		t.Fatalf("it complained: %v", why)
	}
	if src.Len() != 2 {
		t.Fatalf("it read %d rows", src.Len())
	}
	if got := field(t, src, 0, "note"); got.Str != "# not a comment" {
		t.Errorf("a prefix inside a cell read as %s", shows(got))
	}
}

// A newline inside a quoted cell is part of the cell, which is what a
// spreadsheet means by one -- and a comment prefix on its second line is too.
func TestANewlineInsideAQuotedCellIsPartOfIt(t *testing.T) {
	opts := CSV()
	opts.Header, opts.Comments = true, []string{"#"}
	src, _ := load(t, "name,note\nparser.go,\"first\n# second\"\n", opts)
	if src.Len() != 1 {
		t.Fatalf("the cell was split into %d rows", src.Len())
	}
	if got := field(t, src, 0, "note"); got.Str != "first\n# second" {
		t.Errorf("it read as %q", got.Str)
	}
}

// A file that will not come apart at all is an error, not a report: there is
// nothing to hand back a row of.
func TestAnUnclosedQuoteStopsTheLoad(t *testing.T) {
	if _, _, err := ParseDelimited("name\n\"never closed\n", CSV()); err == nil {
		t.Error("it read a file whose last cell never ends")
	}
}

// And once loaded it is a source like any other, ordered and scoped the same.
func TestADelimitedSourceSortsAndScopesLikeAnyOther(t *testing.T) {
	opts := CSV()
	opts.Header, opts.Infer = true, true
	src, _ := load(t, "name,size\nparser.go,2048\nlexer.go,310\naudit.go,9001\n", opts)

	got, done := gather(t, src, &Spec{Sort: []SortLevel{{Field: "size"}}}, &Scope{Count: 2})
	if len(got) != 2 || got[0] != "1" || got[1] != "0" {
		t.Errorf("sorted by size it read %v", got)
	}
	if done.Stop != StopFilled {
		t.Errorf("it ended %s", done.Stop)
	}
	next, _ := gather(t, src, &Spec{Sort: []SortLevel{{Field: "size"}}},
		&Scope{After: done.Watermark, Count: 2})
	if len(next) != 1 || next[0] != "2" {
		t.Errorf("resuming read %v", next)
	}
}

// A comment counts only where a ROW could have begun. A cell that happens to
// start with the prefix is a cell.
func TestACommentPrefixInsideARowIsACell(t *testing.T) {
	opts := CSV()
	opts.Header, opts.Comments = true, []string{"#", "--"}
	src, _ := load(t, "name,note\nparser.go,#7\nlexer.go,--fast\n", opts)

	if src.Len() != 2 {
		t.Fatalf("a cell was taken for a comment: %d rows", src.Len())
	}
	if got := field(t, src, 0, "note"); got == nil || got.Str != "#7" {
		t.Errorf("the second cell read as %s", shows(got))
	}
	if got := field(t, src, 1, "note"); got == nil || got.Str != "--fast" {
		t.Errorf("the second cell read as %s", shows(got))
	}
}

// The number grammar is written out rather than handed to strconv, which takes
// spellings no file here means: ParseFloat reads 1_000 and 0x1p4 as numbers,
// and under Infer both are names.
func TestOnlyTheGrammarsOwnSpellingIsANumber(t *testing.T) {
	opts := CSV()
	opts.Header, opts.Infer = true, true
	src, _ := load(t, "under,hex,inf,plain\n1_000,0x1p4,infinity,1000\n", opts)

	for _, name := range []string{"under", "hex", "inf"} {
		if got := field(t, src, 0, name); got == nil || got.Kind != SymbolValue {
			t.Errorf("%s read as %s, and is not a number this grammar writes", name, shows(got))
		}
	}
	if got := field(t, src, 0, "plain"); got == nil || got.Kind != NumberValue {
		t.Errorf("a plain 1000 read as %s", shows(got))
	}
}
