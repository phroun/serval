package serval

// What a value is, and what a record is made of.
//
// A record is a run of named values, and a value is one of seven things. That
// is the whole of it: there is no statement, no argument, no flag, and nothing
// here knows how any of it might be written down. A value that came off a
// socket and a value read out of a file are the same value, and whoever brought
// it in owns the spelling.
//
// The seven kinds are chosen for what they MEAN rather than for how they are
// written. Text and bytes are apart because a run of bytes is not text that
// happens to need escaping -- it sorts after text, and no collation applies to
// it. A symbol is a name the data itself uses, which is not the same as text
// that happens to look like one. And true, false and nil are their own kinds
// rather than three reserved names, so that a field whose value is the WORD
// "true" and a field whose value IS true stay apart.
//
// Absence is a nil *Value: undefined, the bottom of every order. A field
// present with nothing under it reads as undefined too, because a name is a
// name and not a value.

import "strconv"

// Kind is what a value is.
type Kind int

const (
	NilValue    Kind = iota // nothing, deliberately: a field that has no value
	BoolValue               // true or false
	NumberValue             // an integer or a float; IsInt says which
	SymbolValue             // a name the data uses: an identifier, an enum
	TextValue               // text
	BytesValue              // bytes that are not text
	ListValue               // a record in its own right: a nested thing
)

// A Value is one value of a record.
//
// The fields a kind does not use are not read and carry no meaning. Str serves
// the three kinds that are a run of characters -- a symbol's name, text, and
// bytes as they stand -- because they differ in what they MEAN and not in how
// they are held.
type Value struct {
	Kind Kind

	Str   string  // SymbolValue, TextValue, BytesValue
	Int   int64   // NumberValue, when IsInt
	Num   float64 // NumberValue, otherwise
	IsInt bool    // NumberValue: written with no fractional part, and it fits
	Bool  bool    // BoolValue
	List  Record  // ListValue
}

// The constructors. A value is built, never parsed: whatever read the text owns
// the reading of it.
func NewNil() *Value            { return &Value{Kind: NilValue} }
func NewBool(b bool) *Value     { return &Value{Kind: BoolValue, Bool: b} }
func NewInt(n int64) *Value     { return &Value{Kind: NumberValue, Int: n, Num: float64(n), IsInt: true} }
func NewFloat(f float64) *Value { return &Value{Kind: NumberValue, Num: f} }
func NewSymbol(s string) *Value { return &Value{Kind: SymbolValue, Str: s} }
func NewText(s string) *Value   { return &Value{Kind: TextValue, Str: s} }
func NewBytes(b []byte) *Value  { return &Value{Kind: BytesValue, Str: string(b)} }
func NewList(r Record) *Value   { return &Value{Kind: ListValue, List: r} }

// Val is a Go value as a Value, for the callers that have one of a handful of
// ordinary types and do not want to say which. Anything it does not know
// becomes undefined rather than a guess.
func Val(v any) *Value {
	switch x := v.(type) {
	case nil:
		return nil
	case *Value:
		return x
	case bool:
		return NewBool(x)
	case int:
		return NewInt(int64(x))
	case int64:
		return NewInt(x)
	case float64:
		return NewFloat(x)
	case string:
		return NewText(x)
	case []byte:
		return NewBytes(x)
	case Record:
		return NewList(x)
	}
	return nil
}

// String is a value as readable text, for an error message, a log line or a
// test that wants to say what it expected in one line.
//
// Readable and not canonical: it is for a person, it is never parsed back, and
// nothing turns on it. Key is what tables are keyed by and Equal is what
// decides whether two values are the same one. The shape -- names and values
// separated by spaces inside braces -- is shared with Record and Filter so that
// one glance reads all three.
func (v *Value) String() string {
	if v == nil {
		return "undefined"
	}
	switch v.Kind {
	case NilValue:
		return "nil"
	case BoolValue:
		if v.Bool {
			return "true"
		}
		return "false"
	case NumberValue:
		if v.IsInt {
			return strconv.FormatInt(v.Int, 10)
		}
		return strconv.FormatFloat(v.Num, 'g', -1, 64)
	case SymbolValue:
		return v.Str
	case TextValue:
		return strconv.Quote(v.Str)
	case BytesValue:
		return "b" + strconv.Quote(v.Str)
	case ListValue:
		return v.List.String()
	}
	return "undefined"
}
