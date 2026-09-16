package serval

// A delimited file as records: CSV, TSV, and whatever else a Delimited says.
//
// This is a LOADER. It turns text into rows and hands them to ListSource, which
// is the same engine a PSL document is read into, so nothing downstream can
// tell which of them it is reading.
//
// Two audiences, and they want opposite defaults. A file somebody AUTHORED
// spells its own values: a bare 42 is a number and a bare word is a symbol,
// which is the rule the wire already reads a bare token by. A file a
// SPREADSHEET wrote spells nothing, because Excel and LibreOffice have only one
// way to write a cell -- so under them every bare cell is text, and a column
// that means something else says so. Infer is which of the two this is.
//
// The rest of what those two produce is covered as it stands: the comma or
// semicolon or tab they were configured with, `""` to put a quote inside a
// quoted cell, a real newline inside a quoted cell, CRLF endings, and the byte
// order mark Excel writes in front of a UTF-8 file. What is NOT theirs is
// Escapes: neither of them has a backslash escape, and a Windows path in a cell
// would be mangled by one, so it is off unless the file is ours.

import (
	"fmt"
	"strconv"
	"strings"
)

// A ColumnKind is what a declared column holds, whatever its cells look like. It is
// the column that says so, so quoting in a declared column is transport and
// carries no meaning of its own.
type ColumnKind int

const (
	// Inferred is the cell's own business: see Delimited.Infer.
	Inferred ColumnKind = iota
	AsString
	AsSymbol
	AsNumber
	// AsBytes reads the wire's escapes whatever Escapes says, because \xNN is
	// the only way a cell can spell a byte.
	AsBytes
)

// An Identity says what identifies a row. The zero value is the row's index,
// which is what an authored file usually wants: these are versioned documents
// rather than tables somebody inserts into, so a row's position is its name.
type Identity struct {
	name string
	at   int
	kind identityKind
}

type identityKind int

const (
	identityIsIndex identityKind = iota
	identityIsNamed
	identityIsPositional
)

// RowIndex identifies a row by where it stands, counting from zero.
func RowIndex() Identity { return Identity{} }

// IdentityColumn identifies a row by a column's value, named as the row names
// it: a header, or `.2` where there are no headers.
func IdentityColumn(name string) Identity {
	return Identity{name: name, kind: identityIsNamed}
}

// IdentityAt identifies a row by a column's value, by where the column stands.
func IdentityAt(i int) Identity { return Identity{at: i, kind: identityIsPositional} }

// Delimited is how to read one file.
type Delimited struct {
	// Delimiter separates cells: one character, and one per file.
	Delimiter rune

	// Quote begins and ends a quoted cell, and doubled inside one stands for
	// itself. A quoted cell may hold the delimiter and may hold newlines.
	Quote rune

	// Header says the first row names the columns. Without it they are named
	// `.0`, `.1` and so on, which is how a position is named everywhere else.
	Header bool

	// Escapes reads the wire's escape set inside a quoted cell: \\ \" \n \t \r
	// \e and \xNN. It is OURS and not a spreadsheet's -- leave it off for
	// anything Excel or LibreOffice wrote, or a cell holding a Windows path
	// will be read as escapes it never meant.
	Escapes bool

	// Infer reads a bare cell by the rule the wire reads a bare token by: a
	// number where it spells one, a symbol otherwise. Off, every bare cell is
	// text, which is what a spreadsheet meant by it.
	Infer bool

	// Identity says what names a row.
	Identity Identity

	// Value names the column that becomes the record's `value` rather than a
	// member of its own, the way a scalar record carries one.
	Value string

	// Columns declares what a column holds, by the name the row names it.
	Columns map[string]ColumnKind

	// Comments begin a line that is not a row: "#", ";", "--", whatever this
	// file uses. They are prefixes rather than characters because "--" is two.
	//
	// A line counts as a comment only where a ROW could have started, so a
	// prefix inside a quoted cell -- including one spanning several lines -- is
	// part of the cell and not the start of a remark.
	Comments []string
}

// CSV is what a spreadsheet writes: commas, quotes where they are needed, no
// escapes, and every bare cell text.
func CSV() Delimited { return Delimited{Delimiter: ',', Quote: '"'} }

// TSV is the same with tabs.
func TSV() Delimited { return Delimited{Delimiter: '\t', Quote: '"'} }

// A Complaint is one thing that was wrong with one cell or one row.
//
// It is a report and not a refusal: the cell is left undefined, the row is
// kept, and the load carries on. One bad cell in a long file costs that cell.
type Complaint struct {
	Row    int    // the row it was in, counting data rows from zero
	Column string // the column, as the rows name it
	Cell   string // what was there
	Reason string
}

func (c Complaint) String() string {
	where := c.Column
	if where == "" {
		where = "the row"
	}
	return fmt.Sprintf("row %d, %s: %s", c.Row, where, c.Reason)
}

// ParseDelimited reads a delimited file as records.
//
// The error is for text that is not rows at all. Everything else is a
// complaint, and a file that produced complaints is still a source.
func ParseDelimited(text string, opts Delimited) (*ListSource, []Complaint, error) {
	if opts.Delimiter == 0 {
		return nil, nil, fmt.Errorf("a delimited file needs a delimiter")
	}
	if opts.Quote == opts.Delimiter {
		return nil, nil, fmt.Errorf("the quote and the delimiter are both %q", opts.Delimiter)
	}
	grid, err := opts.scan(stripBOM(text))
	if err != nil {
		return nil, nil, err
	}

	var complaints []Complaint
	names := opts.names(grid)
	if opts.Header && len(grid) > 0 {
		grid = grid[1:]
	}

	rows := make([]Row, 0, len(grid))
	seen := map[string]int{}
	for n, line := range grid {
		fields := make(Record, 0, len(line))
		var value *Value
		for i, c := range line {
			name := columnName(names, i)
			kind := opts.Columns[name]
			v, why := opts.value(c, kind)
			if why != "" {
				complaints = append(complaints, Complaint{
					Row: n, Column: name, Cell: c.text, Reason: why})
			}
			if name == opts.Value && opts.Value != "" {
				value = v
				continue
			}
			fields = append(fields, &Field{Name: name, Value: v})
		}
		if opts.Value != "" {
			fields = append(fields, &Field{Name: ValueField, Value: value})
		}

		id, why := opts.identity(n, names, line)
		if why != "" {
			complaints = append(complaints, Complaint{Row: n, Reason: why})
			continue
		}
		// Uniqueness is by TEXT, because an address carries a segment and not a
		// type: a row keyed 7 and a row keyed "7" are one identity once either
		// is asked for by name, so the second is a duplicate.
		at := segment(id)
		if first, dup := seen[at]; dup {
			complaints = append(complaints, Complaint{Row: n, Cell: at, Reason: fmt.Sprintf(
				"row %d already stands under this identity, so this row is dropped", first)})
			continue
		}
		seen[at] = n
		rows = append(rows, NewRow(id, fields))
	}
	return NewListSource(rows), complaints, nil
}

// bom is the UTF-8 byte order mark, written as an escape because a literal one
// in a source file is not legal Go.
const bom = "\ufeff"

// stripBOM takes off the byte order mark Excel writes in front of a UTF-8 file.
// Left on, it rides the first header and every lookup on that column misses.
func stripBOM(s string) string { return strings.TrimPrefix(s, bom) }

// names is what the columns are called: the header row where there is one, and
// otherwise nothing, which columnName reads as a position.
func (d Delimited) names(grid [][]cell) []string {
	if !d.Header || len(grid) == 0 {
		return nil
	}
	out := make([]string, len(grid[0]))
	for i, c := range grid[0] {
		out[i] = c.text
		if out[i] == "" {
			out[i] = positionName(i)
		}
	}
	return out
}

// columnName is what a column is called: its header, or its position.
func columnName(names []string, i int) string {
	if i < len(names) {
		return names[i]
	}
	return positionName(i)
}

// positionName is a column with no name of its own. The dot is how a position
// is named everywhere else a field is named, so it is how one is named here.
func positionName(i int) string { return "." + strconv.Itoa(i) }

// identity is what names one row, and why it could not be found.
func (d Delimited) identity(n int, names []string, line []cell) (*Value, string) {
	switch d.Identity.kind {
	case identityIsIndex:
		return NewInt(int64(n)), ""
	case identityIsPositional:
		if d.Identity.at >= len(line) {
			return nil, fmt.Sprintf("the identity is column %d and this row has %d",
				d.Identity.at, len(line))
		}
		return d.identityOf(line[d.Identity.at], columnName(names, d.Identity.at))
	default:
		for i := range line {
			if columnName(names, i) == d.Identity.name {
				return d.identityOf(line[i], d.Identity.name)
			}
		}
		return nil, fmt.Sprintf("no column called %q to take an identity from", d.Identity.name)
	}
}

// identityOf reads one cell as an identity. A row with nothing under its
// identity has none, which is not something to invent one for.
func (d Delimited) identityOf(c cell, name string) (*Value, string) {
	v, why := d.value(c, d.Columns[name])
	if why != "" {
		return nil, why
	}
	if v == nil {
		return nil, "this row has nothing under its identity"
	}
	return v, ""
}

// value is what one cell holds, and what was wrong with it.
//
// A bare empty cell is undefined whatever the column says: absence is an
// answer of its own here, and a declaration does not turn it into a value. An
// empty string is written as an empty QUOTED cell.
func (d Delimited) value(c cell, kind ColumnKind) (*Value, string) {
	if !c.quoted && c.text == "" {
		return nil, ""
	}
	switch kind {
	case AsBytes:
		b, err := unescape(c.text, true)
		if err != nil {
			return nil, err.Error()
		}
		return NewBytes([]byte(b)), ""
	case AsString:
		s, err := unescape(c.text, d.Escapes)
		if err != nil {
			return nil, err.Error()
		}
		return NewText(s), ""
	case AsSymbol:
		s, err := unescape(c.text, d.Escapes)
		if err != nil {
			return nil, err.Error()
		}
		return NewSymbol(s), ""
	case AsNumber:
		if n, ok := delimitedNumber(c.text); ok {
			return n, ""
		}
		return nil, fmt.Sprintf("this column holds numbers and %q is not one", c.text)
	}

	s, err := unescape(c.text, d.Escapes)
	if err != nil {
		return nil, err.Error()
	}
	switch {
	case c.quoted, !d.Infer:
		return NewText(s), ""
	}
	if n, ok := delimitedNumber(s); ok {
		return n, ""
	}
	return NewSymbol(s), ""
}

// delimitedNumber reads a bare cell as a number, by the grammar the wire reads
// a bare token by:
//
//	[+-]? digits ( "." digits )? ( [eE] [+-]? digits )?
//
// Written out rather than handed to strconv, which takes hex floats, Inf and
// NaN that no file here means -- and so that a date, a name and an identifier
// stay what they are.
func delimitedNumber(s string) (*Value, bool) {
	i, n := 0, len(s)
	if i < n && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := func() bool {
		start := i
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return i > start
	}
	if !digits() {
		return nil, false
	}
	fractional := false
	if i < n && s[i] == '.' {
		i++
		if !digits() {
			return nil, false
		}
		fractional = true
	}
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < n && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if !digits() {
			return nil, false
		}
		fractional = true
	}
	if i != n {
		return nil, false
	}
	if !fractional {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return NewInt(v), true
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, false
	}
	return NewFloat(v), true
}

// unescape reads the wire's escape set. With on false the text stands as it is,
// which is what a spreadsheet's backslash means: a backslash.
func unescape(s string, on bool) (string, error) {
	if !on || !strings.ContainsRune(s, '\\') {
		return s, nil
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			sb.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			return "", fmt.Errorf("this cell ends in an unfinished escape")
		}
		switch s[i] {
		case '\\':
			sb.WriteByte('\\')
		case '"':
			sb.WriteByte('"')
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		case 'e':
			sb.WriteByte(0x1b)
		case 'x':
			if i+2 >= len(s) {
				return "", fmt.Errorf(`\x wants two hex digits and this cell ends first`)
			}
			v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return "", fmt.Errorf(`\x%s is not two hex digits`, s[i+1:i+3])
			}
			sb.WriteByte(byte(v))
			i += 2
		default:
			return "", fmt.Errorf("there is no escape \\%c", s[i])
		}
	}
	return sb.String(), nil
}

// comment says where a comment line ends, or i where the text at i does not
// begin one. What it skips is the rest of the line and the ending after it.
func (d Delimited) comment(rs []rune, i int) int {
	var begins bool
	for _, mark := range d.Comments {
		if mark != "" && strings.HasPrefix(string(rs[i:min(i+len([]rune(mark)), len(rs))]), mark) {
			begins = true
			break
		}
	}
	if !begins {
		return i
	}
	for i < len(rs) && rs[i] != '\n' && rs[i] != '\r' {
		i++
	}
	if i < len(rs) && rs[i] == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
		i++
	}
	if i < len(rs) {
		i++
	}
	return i
}

// A cell is one field as it was written: its text, and whether it was quoted.
// Quoting is what tells an empty string from an absent one, and under Infer it
// tells a string from a symbol.
type cell struct {
	text   string
	quoted bool
}

// scan takes the text apart into rows of cells.
//
// A quoted cell runs until its closing quote whatever is inside it, so it may
// hold the delimiter and may hold newlines -- both of which a spreadsheet
// writes. A doubled quote inside one stands for itself. Where Escapes is on, a
// backslash also protects the character after it, so `\"` does not end the
// cell; what it MEANS is settled later, where a complaint can say which cell it
// was about.
func (d Delimited) scan(text string) ([][]cell, error) {
	var (
		grid []([]cell)
		line []cell
		sb   strings.Builder
		rs   = []rune(text)
	)
	for i := 0; i < len(rs); {
		// Only where a row could begin: inside a quoted cell this point is
		// never reached, so a prefix there is content.
		if line == nil {
			if n := d.comment(rs, i); n > i {
				i = n
				continue
			}
		}
		quoted := false
		sb.Reset()

		if rs[i] == d.Quote {
			quoted = true
			i++
			closed := false
			for i < len(rs) {
				switch {
				case rs[i] == d.Quote && i+1 < len(rs) && rs[i+1] == d.Quote:
					sb.WriteRune(d.Quote)
					i += 2
				case rs[i] == d.Quote:
					i++
					closed = true
				case d.Escapes && rs[i] == '\\' && i+1 < len(rs):
					sb.WriteRune(rs[i])
					sb.WriteRune(rs[i+1])
					i += 2
				default:
					sb.WriteRune(rs[i])
					i++
				}
				if closed {
					break
				}
			}
			if !closed {
				return nil, fmt.Errorf("a quoted cell was never closed")
			}
		}

		// Whatever follows a closing quote, or the whole of a bare cell, runs
		// to the delimiter or the end of the row.
		for i < len(rs) && rs[i] != d.Delimiter && rs[i] != '\n' && rs[i] != '\r' {
			sb.WriteRune(rs[i])
			i++
		}
		line = append(line, cell{text: sb.String(), quoted: quoted})

		switch {
		case i >= len(rs):
			i++ // done, and the row below is closed
		case rs[i] == d.Delimiter:
			i++
		default: // a row ending, of either spelling
			if rs[i] == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			i++
			grid = append(grid, line)
			line = nil
		}
	}
	if len(line) > 0 {
		grid = append(grid, line)
	}
	// A file ending in a newline is not a file with a blank row at the end.
	if n := len(grid); n > 0 && len(grid[n-1]) == 1 &&
		grid[n-1][0].text == "" && !grid[n-1][0].quoted {
		grid = grid[:n-1]
	}
	return grid, nil
}
