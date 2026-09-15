package serval

// Telling one value from another, for the tables that hold records.
//
// A cache, an ordering and a book of amendments all have to answer "is this the
// same record?" over and over. Key is a canonical form and Equal is a
// comparison, and neither has anything to do with how a value might be written
// down: a spelling that decided identity would make every table in every holder
// of records depend on a grammar, and move every key it holds the day that
// grammar changed.
//
// Key escapes nothing. It puts a length in front of each piece of text instead,
// so that no two different values can run together into the same string -- the
// symbols `awb` then `c` and `a` then `bwc` would otherwise key alike, a tag
// letter being no separator when the text can hold one. It is never parsed
// back.
//
// Key and Equal agree. Two values are Equal exactly when their Keys match, and
// 0_identity_test.go holds them to it over everything awkward there is.

import (
	"math"
	"strconv"
)

// Key is a value's canonical form: two values have the same Key exactly when
// Equal says they are the same value.
//
// Self-delimiting, so keys can be concatenated -- which is what a table keyed
// by more than one thing does, and what a list of them does inside one key.
func Key(v *Value) string {
	var buf [40]byte
	return string(AppendKey(buf[:0], v))
}

// AppendKey appends a value's Key to b, for a caller building a key out of more
// than one thing. A table keyed by a sequence and an identity wants one buffer
// and one string, not a key per part and a join afterwards.
func AppendKey(b []byte, v *Value) []byte {
	if v == nil {
		return append(b, '0')
	}
	switch v.Kind {
	case NilValue:
		return append(b, 'z')
	case BoolValue:
		if v.Bool {
			return append(b, 'T')
		}
		return append(b, 'F')
	case SymbolValue:
		return appendText(b, 'y', v.Str)
	case TextValue:
		return appendText(b, 't', v.Str)
	case BytesValue:
		return appendText(b, 'b', v.Str)
	case NumberValue:
		if v.IsInt {
			return append(strconv.AppendInt(append(b, 'i'), v.Int, 10), ';')
		}
		return append(strconv.AppendUint(
			append(b, 'f'), math.Float64bits(v.Num), 16), ';')
	case ListValue:
		b = append(b, '[')
		for _, f := range v.List {
			b = appendText(b, 'n', f.Name)
			b = AppendKey(b, f.Value)
		}
		return append(b, ']')
	}
	return append(b, '?')
}

// Equal reports whether two values are the same value.
//
// Three things it decides, and each is written down in 0_identity_test.go so
// that changing one is a deliberate act:
//
//   - Text and bytes of the same content are NOT the same value. They are
//     different kinds -- one is characters and one is not -- and they do not
//     even sort together.
//   - An integer and a float of the same magnitude are not the same value
//     either. 3 and 3.0 are different numbers here, and a boundary that changed
//     type between two holders of the same sequence would be comparing
//     different things.
//   - Floats compare by their bits and not by ==, which makes NaN equal to
//     itself and 0.0 different from -0.0. That is not a preference: a key must
//     be reflexive, or a record whose identity does not equal itself is filed
//     and never found again, and == on NaN gives exactly that.
func Equal(a, b *Value) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case NilValue:
		return true
	case BoolValue:
		return a.Bool == b.Bool
	case SymbolValue, TextValue, BytesValue:
		return a.Str == b.Str
	case NumberValue:
		if a.IsInt != b.IsInt {
			return false
		}
		if a.IsInt {
			return a.Int == b.Int
		}
		return math.Float64bits(a.Num) == math.Float64bits(b.Num)
	case ListValue:
		// Delegated rather than walked a second time: one traversal to be
		// wrong in, instead of two to keep in step.
		return Key(a) == Key(b)
	}
	return false
}

// SortKey and FilterKey name the other two halves of a prepared sequence, for
// the same reason and on the same terms. A sequence is a source, a sort and a
// filter, and whatever holds one per sequence has to be able to say when two
// are the same sequence.
func SortKey(levels []SortLevel) string {
	b := make([]byte, 0, 32)
	for _, l := range levels {
		b = appendText(b, 'f', l.Field)
		b = appendText(b, 'c', l.Collation)
		if l.Descending {
			b = append(b, '-')
		} else {
			b = append(b, '+')
		}
	}
	return string(b)
}

func FilterKey(f *Filter) string {
	return string(appendFilterKey(make([]byte, 0, 64), f))
}

// The counts in front of the values and the children are belt and braces, and a
// mutation test will tell you so: every key starts with a tag letter and a count
// with a digit, so where the values stop and the children start is already
// unambiguous without them. They are here so that it stays unambiguous if a tag
// is ever added that does not, which is exactly the kind of change nobody would
// think to check.
func appendFilterKey(b []byte, f *Filter) []byte {
	if f == nil {
		return append(b, '0')
	}
	b = appendText(b, 'o', f.Op)
	b = appendText(b, 'f', f.Field)
	b = appendText(b, 'c', f.Collate)
	b = append(strconv.AppendInt(b, int64(len(f.Values)), 10), ':')
	for _, v := range f.Values {
		b = AppendKey(b, v)
	}
	b = append(strconv.AppendInt(b, int64(len(f.Children)), 10), ':')
	for _, c := range f.Children {
		b = appendFilterKey(b, c)
	}
	return b
}

// appendText appends a tagged, length-prefixed run of text. The length is what
// keeps two pieces from running together into a third: without it the symbols
// `ab` and `c` would key the same as `a` and `bc`.
func appendText(b []byte, tag byte, s string) []byte {
	b = append(b, tag)
	b = strconv.AppendInt(b, int64(len(s)), 10)
	return append(append(b, ':'), s...)
}
